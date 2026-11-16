package turso

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

var dispatchNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func dispatchAssignment(revision uint64) provider.AssignmentRef {
	return provider.AssignmentRef{ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: revision}
}

// seedDispatchSession saves session id at revision 3 on adapter session
// "adapter-<id>".
func seedDispatchSession(t *testing.T, store *Store, id sessions.ID) sessions.Record {
	t.Helper()
	record := sessions.Record{
		ID: id, ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: 3,
		AdapterSessionID: "adapter-" + string(id), State: "open", CreatedAt: dispatchNow, UpdatedAt: dispatchNow,
	}
	saved, err := store.Sessions().Save(context.Background(), record, 0)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func startCommandFor(id execution.IntentID, session sessions.ID) execution.Command {
	return execution.Command{
		IntentID:  id,
		Kind:      execution.IntentStartTurn,
		SessionID: session,
		Session:   provider.SessionRef{AdapterSessionID: provider.AdapterSessionID("adapter-" + string(session))},
		Current:   dispatchAssignment(3),
		ResultContract: provider.ResultContractRef{
			Name: "result", Version: "1", Schema: []byte(`{"type":"object"}`), Requested: provider.SchemaNative,
		},
		Parts:  []provider.InputPart{{Kind: provider.InputInstruction, Text: "Направи задачата"}},
		Memory: execution.MemoryInput{Kind: execution.MemoryInputNone},
	}
}

func steerCommandFor(id execution.IntentID, session sessions.ID, turn provider.AdapterTurnID) execution.Command {
	return execution.Command{
		IntentID:     id,
		Kind:         execution.IntentSteer,
		SessionID:    session,
		Session:      provider.SessionRef{AdapterSessionID: provider.AdapterSessionID("adapter-" + string(session))},
		Current:      dispatchAssignment(3),
		New:          dispatchAssignment(4),
		ExpectedTurn: provider.TurnRef{AdapterTurnID: turn},
		Mode:         provider.DeliverActiveTurn,
		Parts:        []provider.InputPart{{Kind: provider.InputInstruction, Text: "Коригирай посоката"}},
		Memory:       execution.MemoryInput{Kind: execution.MemoryInputNone},
	}
}

func dispatchService(store *Store) dispatch.Service {
	return dispatch.Service{Store: store.Dispatch(), Now: func() time.Time { return dispatchNow }}
}

// composeLinked composes the project's memory for the linked records with a
// fresh marker, through the real reader and rules.
func composeLinked(t *testing.T, store *Store, linked ...string) (compose.Request, compose.Composition) {
	t.Helper()
	composer, err := compose.New(store.MemoryReader(), noFiles{}, redaction.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request := compose.Request{ProjectID: "project", AttemptID: "attempt", Snapshot: "snapshot", ExplicitRecordIDs: linked, Marker: marker}
	composition, err := composer.ComposeContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return request, composition
}

func withMemory(c execution.Command, request compose.Request, composition compose.Composition) execution.Command {
	c.Memory = execution.MemoryInput{Kind: execution.MemoryInputComposed, Request: request, Composition: composition}
	return c
}

func stored(c execution.Command) execution.Command {
	c.Memory = execution.MemoryInput{Kind: execution.MemoryInputStored}
	return c
}

// fakeHarness is a provider.Session whose StartTurn and Steer answer from
// functions and count every call. Without a function, StartTurn accepts with
// a turn named after the request and Steer answers like Codex: native
// receipt for the new assignment.
type fakeHarness struct {
	mu      sync.Mutex
	starts  []provider.StartTurnRequest
	steers  []provider.SteerRequest
	onStart func(context.Context, provider.StartTurnRequest) (provider.StartTurnResult, error)
	onSteer func(context.Context, provider.SteerRequest) (provider.DeliveryReceipt, error)
}

var _ provider.Session = (*fakeHarness)(nil)

func (h *fakeHarness) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.starts) + len(h.steers)
}

func (h *fakeHarness) StartTurn(ctx context.Context, request provider.StartTurnRequest) (provider.StartTurnResult, error) {
	h.mu.Lock()
	h.starts = append(h.starts, request)
	answer := h.onStart
	h.mu.Unlock()
	if answer != nil {
		return answer(ctx, request)
	}
	return acceptedStart(request), nil
}

func (h *fakeHarness) Steer(ctx context.Context, request provider.SteerRequest) (provider.DeliveryReceipt, error) {
	h.mu.Lock()
	h.steers = append(h.steers, request)
	answer := h.onSteer
	h.mu.Unlock()
	if answer != nil {
		return answer(ctx, request)
	}
	return codexReceipt(request), nil
}

func acceptedStart(request provider.StartTurnRequest) provider.StartTurnResult {
	return provider.StartTurnResult{
		Turn:              provider.TurnRef{AdapterTurnID: provider.AdapterTurnID("turn-" + string(request.RequestID))},
		Assignment:        request.Assignment,
		TargetingStrength: provider.TargetNativeTurnID,
		ResultEnforcement: provider.SchemaNative,
	}
}

// codexReceipt is a Codex- or OpenCode-like synchronous success.
func codexReceipt(request provider.SteerRequest) provider.DeliveryReceipt {
	return provider.DeliveryReceipt{
		RequestID: request.RequestID, MessageID: request.MessageID,
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown,
		Targeting: provider.TargetNativeTurnCAS, DispatchedFor: provider.Some(request.NewAssignment),
		NativeReceiptID: provider.Some[provider.NativeReceiptID]("native-receipt"),
	}
}

// claudeReceipt is a Claude-like success: written locally, no native receipt.
func claudeReceipt(request provider.SteerRequest) provider.DeliveryReceipt {
	return provider.DeliveryReceipt{
		RequestID: request.RequestID, MessageID: request.MessageID,
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNone, Targeting: provider.TargetSessionLease,
	}
}

func (h *fakeHarness) Ref() provider.SessionRef { return provider.SessionRef{} }
func (h *fakeHarness) Capabilities() provider.CapabilitySnapshot {
	return provider.CapabilitySnapshot{}
}
func (h *fakeHarness) Events(context.Context, provider.EventCursor) (provider.EventStream, error) {
	return nil, errors.New("fake harness has no events")
}
func (h *fakeHarness) ObserveState(context.Context, provider.ObserveStateRequest) (provider.SessionSnapshot, error) {
	return provider.SessionSnapshot{}, errors.New("fake harness has no state")
}
func (h *fakeHarness) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, errors.New("not used")
}
func (h *fakeHarness) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, errors.New("not used")
}
func (h *fakeHarness) InterruptTurn(context.Context, provider.InterruptRequest) (provider.InterruptResult, error) {
	return provider.InterruptResult{}, errors.New("not used")
}
func (h *fakeHarness) Close(context.Context, provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	return provider.CloseSessionResult{}, nil
}

// mustDispatch runs the procedure for the command's kind and fails on error.
func mustDispatch(t *testing.T, service dispatch.Service, harness *fakeHarness, command execution.Command) dispatch.Result {
	t.Helper()
	result, err := run(service, harness, command)
	if err != nil {
		t.Fatalf("dispatch %s: %v", command.IntentID, err)
	}
	return result
}

func run(service dispatch.Service, harness *fakeHarness, command execution.Command) (dispatch.Result, error) {
	return runContext(context.Background(), service, harness, command)
}

func runContext(ctx context.Context, service dispatch.Service, harness *fakeHarness, command execution.Command) (dispatch.Result, error) {
	if command.Kind == execution.IntentSteer {
		return service.DispatchSteer(ctx, harness, command)
	}
	return service.DispatchStartTurn(ctx, harness, command)
}

// startAccepted dispatches an accepted start on the session and returns its
// adapter turn.
func startAccepted(t *testing.T, store *Store, session sessions.ID) provider.AdapterTurnID {
	t.Helper()
	id := execution.IntentID("start-" + string(session))
	result := mustDispatch(t, dispatchService(store), &fakeHarness{}, startCommandFor(id, session))
	if result.Kind != dispatch.ResultDispatched || result.Outcome.Classification != execution.ClassTurnStarted {
		t.Fatalf("start = %+v", result)
	}
	return result.Outcome.AdapterTurnID
}

func intentState(t *testing.T, store *Store, id execution.IntentID) string {
	t.Helper()
	got := queryStrings(t, store, "SELECT state FROM dispatch_intents WHERE id = ?", string(id))
	if len(got) != 1 {
		t.Fatalf("intent %s rows = %v", id, got)
	}
	return got[0]
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func dispatchEventKinds(t *testing.T, store *Store, id execution.IntentID) []string {
	t.Helper()
	return queryStrings(t, store, "SELECT kind FROM event_journal WHERE causation_id = ? ORDER BY sequence", string(id))
}

func sessionRevision(t *testing.T, store *Store, id sessions.ID) uint64 {
	t.Helper()
	record, err := store.Sessions().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return record.Revision
}

// dispatchTables counts the rows dispatch writes, for "nothing written".
func dispatchTables(t *testing.T, store *Store) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, name := range []string{"dispatch_intents", "dispatch_outcomes", "context_manifests", "context_manifest_items", "context_manifest_exclusions", "context_manifest_payloads"} {
		counts[name] = countRows(t, store, "SELECT count(*) FROM "+name)
	}
	counts["dispatch events"] = countRows(t, store, "SELECT count(*) FROM event_journal WHERE kind LIKE 'dispatch.%'")
	return counts
}

func inputKinds(parts []provider.InputPart) []string {
	kinds := make([]string, len(parts))
	for i, part := range parts {
		kinds[i] = string(part.Kind)
	}
	return kinds
}

func containsAll(text string, parts ...string) bool {
	return !slices.ContainsFunc(parts, func(part string) bool { return !strings.Contains(text, part) })
}

// sameOutcome compares two outcomes field by field, the time as an instant.
func sameOutcome(a, b execution.OutcomeRecord) bool {
	at, bt := a.ObservedAt, b.ObservedAt
	a.ObservedAt, b.ObservedAt = time.Time{}, time.Time{}
	return a == b && at.Equal(bt)
}
