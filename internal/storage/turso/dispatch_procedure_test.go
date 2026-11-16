package turso

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// A start without memory records the intent, claims it, sends the parts with
// the intent ID as request ID and records the accepted outcome, with one
// journal event per transaction.
func TestDispatchStartWithoutMemory(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	harness := &fakeHarness{}
	command := startCommandFor("start-1", "s1")
	result := mustDispatch(t, dispatchService(store), harness, command)
	if result.Kind != dispatch.ResultDispatched || result.IntentState != execution.StateAccepted ||
		result.Outcome.Classification != execution.ClassTurnStarted || result.Outcome.AdapterTurnID != "turn-start-1" {
		t.Fatalf("result = %+v", result)
	}
	if harness.calls() != 1 || harness.starts[0].RequestID != "start-1" || !slices.Equal(harness.starts[0].Input, command.Parts) ||
		harness.starts[0].Assignment != command.Current || harness.starts[0].Session != command.Session {
		t.Fatalf("harness = %+v", harness.starts)
	}
	if got := intentState(t, store, "start-1"); got != "accepted" {
		t.Fatalf("state = %s", got)
	}
	if got := dispatchEventKinds(t, store, "start-1"); !slices.Equal(got, []string{execution.EventIntentRecorded, execution.EventClaimed, execution.EventSettled}) {
		t.Fatalf("events = %v", got)
	}
	if got := queryStrings(t, store, "SELECT event_id FROM event_journal WHERE causation_id = 'start-1' ORDER BY sequence"); !slices.Equal(got, []string{"start-1/recorded", "start-1/claimed", "start-1/settled"}) {
		t.Fatalf("event ids = %v", got)
	}
	if countRows(t, store, "SELECT count(*) FROM context_manifests") != 0 || sessionRevision(t, store, "s1") != 3 {
		t.Fatal("start without memory wrote a manifest or moved the revision")
	}
}

// A composed start stores the manifest, items and payload with the intent
// and sends the parts followed by the context part rebuilt from the payload.
func TestDispatchStartWithComposedMemory(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	first := humanAt(t, store, "Решение: паметта е локална", 0)
	second := humanAt(t, store, "Конвенция: тестовете са таблични", 1)
	request, composition := composeLinked(t, store, first.RecordID, second.RecordID)
	if composition.Kind != compose.Composed {
		t.Fatalf("composition = %+v", composition)
	}
	harness := &fakeHarness{}
	command := withMemory(startCommandFor("start-1", "s1"), request, composition)
	result := mustDispatch(t, dispatchService(store), harness, command)
	if result.Kind != dispatch.ResultDispatched || result.Outcome.Outcome != execution.OutcomeAccepted {
		t.Fatalf("result = %+v", result)
	}
	input := harness.starts[0].Input
	if !slices.Equal(inputKinds(input), []string{"instruction", "context"}) || input[1].Text != composition.Part.Text || input[1].URI != "" {
		t.Fatalf("input = %+v", input)
	}
	if got := queryStrings(t, store, "SELECT composition || ' ' || payload_hash || ' ' || redaction_policy_version || ' ' || CAST(revision AS TEXT) FROM context_manifests WHERE intent_id = 'start-1'"); !slices.Equal(got, []string{"composed " + composition.TextHash + " 1 3"}) {
		t.Fatalf("manifest = %v", got)
	}
	if got := queryStrings(t, store, "SELECT source_identity FROM context_manifest_items WHERE manifest_id = 'start-1' ORDER BY ordinal"); !slices.Equal(got, []string{first.RecordID, second.RecordID}) {
		t.Fatalf("items = %v", got)
	}
	if got := queryStrings(t, store, "SELECT status FROM context_manifest_payloads WHERE manifest_id = 'start-1' AND content = ?", composition.Part.Text); !slices.Equal(got, []string{"present"}) {
		t.Fatalf("payload = %v", got)
	}
}

// The real Composer's empty DTO stores an empty manifest without a payload
// row and sends no context part.
func TestDispatchWithRealEmptyComposition(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	request, composition := composeLinked(t, store)
	if composition.Kind != compose.ComposedEmpty {
		t.Fatalf("composition = %+v", composition)
	}
	harness := &fakeHarness{}
	result := mustDispatch(t, dispatchService(store), harness, withMemory(startCommandFor("start-1", "s1"), request, composition))
	if result.Kind != dispatch.ResultDispatched || !slices.Equal(inputKinds(harness.starts[0].Input), []string{"instruction"}) {
		t.Fatalf("result %+v, input %+v", result, harness.starts[0].Input)
	}
	if got := queryStrings(t, store, "SELECT composition || ' ' || payload_hash || ' ' || coalesce(redaction_policy_version, 'NULL') || ' ' || search_status FROM context_manifests"); !slices.Equal(got, []string{"empty " + execution.EmptyPayloadHash + " NULL disabled"}) {
		t.Fatalf("manifest = %v", got)
	}
	if countRows(t, store, "SELECT count(*) FROM context_manifest_payloads") != 0 {
		t.Fatal("empty manifest has a payload row")
	}
	// The stored repetition of an empty manifest is claimed without payload.
	if got := mustDispatch(t, dispatchService(store), harness, stored(startCommandFor("start-1", "s1"))); got.Kind != dispatch.ResultRecordedOutcome || harness.calls() != 1 {
		t.Fatalf("repetition = %+v, calls %d", got, harness.calls())
	}
}

// Codex- and OpenCode-like steers prove native receipt for the new
// assignment: the intent is accepted and the revision moves with the
// session's optimistic version, in the outcome's transaction.
func TestDispatchSteerCodexLikeAdvances(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	turn := startAccepted(t, store, "s1")
	before, err := store.Sessions().Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	harness := &fakeHarness{}
	result := mustDispatch(t, dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	if result.Kind != dispatch.ResultDispatched || result.Outcome.Classification != execution.ClassAdvanced || !result.Outcome.Advanced {
		t.Fatalf("result = %+v", result)
	}
	steer := harness.steers[0]
	if steer.RequestID != "steer-1" || steer.MessageID != "steer-1" || steer.ExpectedRevision != 3 || steer.NewAssignment != dispatchAssignment(4) ||
		steer.CurrentAssignment != dispatchAssignment(3) || steer.ExpectedActiveTurn.AdapterTurnID != turn {
		t.Fatalf("steer request = %+v", steer)
	}
	after, err := store.Sessions().Get(context.Background(), "s1")
	if err != nil || after.Revision != 4 || after.Version != before.Version+1 || after.AssignmentID != before.AssignmentID {
		t.Fatalf("session = %+v, %v", after, err)
	}
	if got := queryStrings(t, store, "SELECT CAST(advanced AS TEXT) || ' ' || dispatched_for || ' ' || native_receipt_id FROM dispatch_outcomes WHERE intent_id = 'steer-1'"); !slices.Equal(got, []string{"1 matches native-receipt"}) {
		t.Fatalf("outcome = %v", got)
	}
}

// A Claude-like steer has no native receipt: unknown/transport_only, the
// revision stays, and the open intent blocks new intents and binding changes.
func TestDispatchSteerClaudeLikeStaysUnknownAndBlocks(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	turn := startAccepted(t, store, "s1")
	harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
		return claudeReceipt(r), nil
	}}
	result := mustDispatch(t, dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	if result.Kind != dispatch.ResultDispatched || result.IntentState != execution.StateUnknown || result.Outcome.Classification != execution.ClassTransportOnly {
		t.Fatalf("result = %+v", result)
	}
	if sessionRevision(t, store, "s1") != 3 {
		t.Fatal("transport-only steer moved the revision")
	}
	next := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-2", "s1"))
	if next.Kind != dispatch.ResultIntentRefused || next.Intent.Kind != execution.IntentBlockedByOpenIntent || harness.calls() != 1 {
		t.Fatalf("next = %+v", next)
	}
	record, err := store.Sessions().Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	record.AssignmentID = "other"
	if _, err := store.Sessions().Save(ctx, record, record.Version); !errors.Is(err, sessions.ErrBindingLocked) {
		t.Fatalf("binding change = %v", err)
	}
}

// ACP refuses Steer without sending: rejected/not_sent, nothing blocks.
func TestDispatchSteerACPLikeIsRejected(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	turn := startAccepted(t, store, "s1")
	harness := &fakeHarness{onSteer: func(context.Context, provider.SteerRequest) (provider.DeliveryReceipt, error) {
		return provider.DeliveryReceipt{}, &provider.ProviderError{Code: provider.ErrorCapabilityUnsupported, Operation: provider.OperationSteer, RemoteEffect: provider.RemoteEffectNotSent, Retry: provider.RetryNever}
	}}
	result := mustDispatch(t, dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	if result.IntentState != execution.StateRejected || result.Outcome.Classification != execution.ClassNotSent || result.Outcome.ErrorCode != provider.ErrorCapabilityUnsupported {
		t.Fatalf("result = %+v", result)
	}
	if next := mustDispatch(t, dispatchService(store), &fakeHarness{}, steerCommandFor("steer-2", "s1", turn)); next.Outcome.Classification != execution.ClassAdvanced {
		t.Fatalf("steer after a rejected one = %+v", next)
	}
}

// Repeating a command after any outcome returns the recorded outcome and
// never calls the harness again.
func TestDispatchRepetitionNeverResends(t *testing.T) {
	for name, answer := range map[string]func(context.Context, provider.StartTurnRequest) (provider.StartTurnResult, error){
		"accepted": nil,
		"rejected": func(context.Context, provider.StartTurnRequest) (provider.StartTurnResult, error) {
			return provider.StartTurnResult{}, &provider.ProviderError{Code: provider.ErrorStaleBinding, Operation: provider.OperationStartTurn, RemoteEffect: provider.RemoteEffectNotSent}
		},
		"unknown": func(context.Context, provider.StartTurnRequest) (provider.StartTurnResult, error) {
			return provider.StartTurnResult{}, &provider.ProviderError{Code: provider.ErrorDeliveryUnknown, Operation: provider.OperationStartTurn, RemoteEffect: provider.RemoteEffectPossiblySent}
		},
		"invalid": func(_ context.Context, r provider.StartTurnRequest) (provider.StartTurnResult, error) {
			result := acceptedStart(r)
			result.Turn.AdapterTurnID = ""
			return result, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := openMemoryStore(t)
			seedDispatchSession(t, store, "s1")
			harness := &fakeHarness{onStart: answer}
			first := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1"))
			again := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1"))
			if again.Kind != dispatch.ResultRecordedOutcome || again.Outcome != first.Outcome || again.IntentState != first.IntentState || harness.calls() != 1 {
				t.Fatalf("first %+v, again %+v, calls %d", first, again, harness.calls())
			}
			if got := countRows(t, store, "SELECT count(*) FROM event_journal WHERE causation_id = 'start-1'"); got != 3 {
				t.Fatalf("events = %d", got)
			}
		})
	}
}

// Same ID with different content is refused; nothing is written.
func TestDispatchSameIDDifferentContentConflicts(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	harness := &fakeHarness{}
	mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1"))
	changed := startCommandFor("start-1", "s1")
	changed.Parts[0].Text = "друга задача"
	before := dispatchTables(t, store)
	result := mustDispatch(t, dispatchService(store), harness, changed)
	if result.Kind != dispatch.ResultIntentRefused || result.Intent.Kind != execution.IntentIDConflict || harness.calls() != 1 {
		t.Fatalf("result = %+v", result)
	}
	if after := dispatchTables(t, store); !mapsEqual(before, after) {
		t.Fatalf("tables %v -> %v", before, after)
	}
}

// A repetition after a memory record changed or its content was deleted, or
// after the payload was removed, returns the recorded outcome: the identity
// check comes before any recheck. A new composition under the same ID is a
// new command and conflicts.
func TestDispatchRepetitionAfterMemoryChanges(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	kept := humanAt(t, store, "Решение: паметта е локална", 0)
	doomed := humanAt(t, store, "Факт: ще бъде изтрит", 1)
	request, composition := composeLinked(t, store, kept.RecordID, doomed.RecordID)
	harness := &fakeHarness{}
	command := withMemory(startCommandFor("start-1", "s1"), request, composition)
	first := mustDispatch(t, dispatchService(store), harness, command)

	mustExec(t, store, "UPDATE memory_records SET version = version + 1 WHERE id = ?", kept.RecordID)
	if _, err := (deletion.Service{Store: store.MemoryDeletion()}).DeleteRecord(ctx, deletion.Request{
		ProjectID: "project", RecordID: doomed.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Now: dispatchNow,
	}); err != nil {
		t.Fatal(err)
	}
	for _, repeat := range []execution.Command{command, stored(startCommandFor("start-1", "s1"))} {
		if again := mustDispatch(t, dispatchService(store), harness, repeat); again.Kind != dispatch.ResultRecordedOutcome || again.Outcome != first.Outcome {
			t.Fatalf("after memory changes = %+v", again)
		}
	}
	mustExec(t, store, "UPDATE context_manifest_payloads SET status = 'removed_after_delivery', content = NULL, removed_at = ? WHERE manifest_id = 'start-1'", memoryTime)
	if again := mustDispatch(t, dispatchService(store), harness, command); again.Kind != dispatch.ResultRecordedOutcome {
		t.Fatalf("after payload removal = %+v", again)
	}
	newRequest, newComposition := composeLinked(t, store, kept.RecordID)
	if again := mustDispatch(t, dispatchService(store), harness, withMemory(startCommandFor("start-1", "s1"), newRequest, newComposition)); again.Intent.Kind != execution.IntentIDConflict {
		t.Fatalf("new composition, same id = %+v", again)
	}
	if harness.calls() != 1 {
		t.Fatalf("calls = %d", harness.calls())
	}
}

// A new ID whose composition relied on a record that changed since is
// refused as recompose; nothing is written or sent.
func TestDispatchRecomposeOnlyForNewID(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	record := humanAt(t, store, "Решение: паметта е локална", 0)
	request, composition := composeLinked(t, store, record.RecordID)
	mustExec(t, store, "UPDATE memory_records SET version = version + 1 WHERE id = ?", record.RecordID)
	harness := &fakeHarness{}
	before := dispatchTables(t, store)
	result := mustDispatch(t, dispatchService(store), harness, withMemory(startCommandFor("start-1", "s1"), request, composition))
	if result.Kind != dispatch.ResultIntentRefused || result.Intent.Kind != execution.IntentRecompose ||
		result.Intent.Recheck.Change != memory.ChangeRecordChanged || harness.calls() != 0 {
		t.Fatalf("result = %+v", result)
	}
	if after := dispatchTables(t, store); !mapsEqual(before, after) {
		t.Fatalf("tables %v -> %v", before, after)
	}
}

// A composition over the payload limit is refused before anything is
// written.
func TestDispatchRefusesOversizedComposition(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	record := humanAt(t, store, "Решение: паметта е локална", 0)
	request, composition := composeLinked(t, store, record.RecordID)
	composition.Part.Text += string(make([]byte, execution.MaxManifestPayloadBytes))
	composition.TextHash = execution.PayloadHash(composition.Part.Text)
	result := mustDispatch(t, dispatchService(store), &fakeHarness{}, withMemory(startCommandFor("start-1", "s1"), request, composition))
	if result.Kind != dispatch.ResultManifestRefused || result.Manifest.Kind != execution.ManifestPayloadTooLarge {
		t.Fatalf("result = %+v", result)
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_intents") != 0 {
		t.Fatal("refused composition wrote an intent")
	}
}

// crashingStore stops the flow after the record (claim fails) or after the
// claim (settlement fails), as a crash would.
type crashingStore struct {
	dispatch.Store
	claim, settle bool
}

var errCrash = errors.New("simulated crash")

func (s crashingStore) WithinIntentClaim(ctx context.Context, op func(dispatch.ClaimTransaction) error) error {
	if s.claim {
		return errCrash
	}
	return s.Store.WithinIntentClaim(ctx, op)
}

func (s crashingStore) SettleIntent(ctx context.Context, settlement dispatch.Settlement) (dispatch.SettleResult, error) {
	if s.settle {
		return dispatch.SettleResult{}, errCrash
	}
	return s.Store.SettleIntent(ctx, settlement)
}

// After a crash between the record and the claim, a stored repetition sends
// exactly the recorded bytes, marker included, without a composition.
func TestDispatchStoredRepetitionAfterCrashBeforeClaim(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	record := humanAt(t, store, "Решение: паметта е локална", 0)
	request, composition := composeLinked(t, store, record.RecordID)
	harness := &fakeHarness{}
	crashed := dispatch.Service{Store: crashingStore{Store: store.Dispatch(), claim: true}, Now: func() time.Time { return dispatchNow }}
	if _, err := run(crashed, harness, withMemory(startCommandFor("start-1", "s1"), request, composition)); !errors.Is(err, errCrash) {
		t.Fatalf("crash = %v", err)
	}
	if got := intentState(t, store, "start-1"); got != "recorded" || harness.calls() != 0 {
		t.Fatalf("state %s, calls %d", got, harness.calls())
	}
	// The record changes meanwhile; the stored intent is still sent as
	// recorded.
	mustExec(t, store, "UPDATE memory_records SET version = version + 1 WHERE id = ?", record.RecordID)
	result := mustDispatch(t, dispatchService(store), harness, stored(startCommandFor("start-1", "s1")))
	if result.Kind != dispatch.ResultDispatched || harness.calls() != 1 {
		t.Fatalf("result = %+v", result)
	}
	sent := harness.starts[0].Input[1].Text
	if sent != composition.Part.Text || !containsAll(sent, string(request.Marker)) {
		t.Fatal("stored repetition did not send the recorded bytes")
	}
	// A stored repetition of an intent without a manifest is refused.
	seedDispatchSession(t, store, "s2")
	mustDispatch(t, dispatchService(store), &fakeHarness{}, startCommandFor("plain", "s2"))
	if got := mustDispatch(t, dispatchService(store), harness, stored(startCommandFor("plain", "s2"))); got.Intent.Kind != execution.IntentIDConflict {
		t.Fatalf("stored without manifest = %+v", got)
	}
	if got := mustDispatch(t, dispatchService(store), harness, stored(startCommandFor("never", "s2"))); got.Intent.Kind != execution.IntentStoredNotFound {
		t.Fatalf("stored, unknown id = %+v", got)
	}
}

// After a crash between the claim and the outcome, the repetition is
// delivery_unknown and the harness is not called again, also after the store
// is reopened.
func TestDispatchCrashAfterClaimIsDeliveryUnknown(t *testing.T) {
	path := t.TempDir() + "/dispatch.db"
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	seedDispatchSession(t, store, "s1")
	harness := &fakeHarness{}
	crashed := dispatch.Service{Store: crashingStore{Store: store.Dispatch(), settle: true}, Now: func() time.Time { return dispatchNow }}
	_, err = run(crashed, harness, startCommandFor("start-1", "s1"))
	var settlementError *dispatch.SettlementError
	if !errors.Is(err, dispatch.ErrSettlementFailed) || !errors.Is(err, errCrash) || !errors.As(err, &settlementError) ||
		settlementError.Outcome.Classification != execution.ClassTurnStarted || settlementError.Commit != dispatch.CommitUnknown {
		t.Fatalf("crash = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1"))
	if result.Kind != dispatch.ResultDeliveryUnknown || result.IntentState != execution.StateDispatching || harness.calls() != 1 {
		t.Fatalf("result = %+v, calls %d", result, harness.calls())
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_outcomes") != 0 {
		t.Fatal("delivery_unknown wrote an outcome")
	}
}

// Cancellation of the caller ends its wait; the adapter decides the remote
// effect and the outcome is still recorded.
func TestDispatchCancellation(t *testing.T) {
	// A caller cancelled before the record transaction records nothing.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	harness := &fakeHarness{}
	if _, err := runContext(cancelled, dispatchService(store), harness, startCommandFor("start-1", "s1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled before the record = %v", err)
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_intents") != 0 || harness.calls() != 0 {
		t.Fatal("a cancelled caller recorded or sent")
	}
	// The record and claim honour the caller's context; once claimed, the
	// settlement does not.
	store = openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	turn := startAccepted(t, store, "s1")
	ctx, cancelLater := context.WithCancel(context.Background())
	harness = &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
		cancelLater()
		return codexReceipt(r), nil
	}}
	result, err := runContext(ctx, dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	if err != nil || result.IntentState != execution.StateAccepted || sessionRevision(t, store, "s1") != 4 {
		t.Fatalf("accepted after cancellation = %+v, %v", result, err)
	}
	for _, tc := range []struct {
		effect provider.RemoteEffect
		state  string
	}{{provider.RemoteEffectNotSent, "rejected"}, {provider.RemoteEffectPossiblySent, "unknown"}} {
		store := openMemoryStore(t)
		seedDispatchSession(t, store, "s1")
		ctx, cancelLater := context.WithCancel(context.Background())
		harness := &fakeHarness{onStart: func(context.Context, provider.StartTurnRequest) (provider.StartTurnResult, error) {
			cancelLater()
			return provider.StartTurnResult{}, &provider.ProviderError{Code: provider.ErrorCallerCancelled, Operation: provider.OperationStartTurn, RemoteEffect: tc.effect}
		}}
		if _, err := runContext(ctx, dispatchService(store), harness, startCommandFor("start-1", "s1")); err != nil {
			t.Fatal(err)
		}
		if got := intentState(t, store, "start-1"); got != tc.state {
			t.Fatalf("%s: state %s", tc.effect, got)
		}
	}
}

// Deletion around the claim (spec 012 §6): invalidated before the claim is
// never sent; removing the payload after the claim does not stop the send,
// which goes on from the claimed envelope, and the outcome is recorded with
// the payload left removed.
func TestDispatchPayloadRemovalAroundClaim(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	record := humanAt(t, store, "Решение: паметта е локална", 0)
	request, composition := composeLinked(t, store, record.RecordID)
	crashed := dispatch.Service{Store: crashingStore{Store: store.Dispatch(), claim: true}, Now: func() time.Time { return dispatchNow }}
	if _, err := run(crashed, &fakeHarness{}, withMemory(startCommandFor("start-1", "s1"), request, composition)); !errors.Is(err, errCrash) {
		t.Fatal(err)
	}
	// Before the claim, task 4.3 invalidates and removes atomically.
	mustExec(t, store, "UPDATE dispatch_intents SET state = 'invalidated', invalidated_at = ? WHERE id = 'start-1'", memoryTime)
	mustExec(t, store, "UPDATE context_manifest_payloads SET status = 'removed_before_dispatch', content = NULL, removed_at = ? WHERE manifest_id = 'start-1'", memoryTime)
	harness := &fakeHarness{}
	if got := mustDispatch(t, dispatchService(store), harness, stored(startCommandFor("start-1", "s1"))); got.Kind != dispatch.ResultInvalidated || harness.calls() != 0 {
		t.Fatalf("invalidated = %+v", got)
	}

	seedDispatchSession(t, store, "s2")
	request, composition = composeLinked(t, store, record.RecordID)
	harness = &fakeHarness{onStart: func(_ context.Context, r provider.StartTurnRequest) (provider.StartTurnResult, error) {
		mustExec(t, store, "UPDATE context_manifest_payloads SET status = 'removed_delivery_unknown', content = NULL, removed_at = ? WHERE manifest_id = 'start-2'", memoryTime)
		return acceptedStart(r), nil
	}}
	result := mustDispatch(t, dispatchService(store), harness, withMemory(startCommandFor("start-2", "s2"), request, composition))
	if result.IntentState != execution.StateAccepted || harness.starts[0].Input[1].Text != composition.Part.Text {
		t.Fatalf("send after the claim = %+v", result)
	}
	if got := queryStrings(t, store, "SELECT status FROM context_manifest_payloads WHERE manifest_id = 'start-2'"); !slices.Equal(got, []string{"removed_delivery_unknown"}) {
		t.Fatalf("payload = %v", got)
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

// The clock is checked before anything is written; a clock that fails only
// after the send does not drop the harness answer: it is returned with the
// reason, nothing is settled, and the repetition is delivery_unknown.
func TestDispatchClockFailures(t *testing.T) {
	clock := func(valid int) func() time.Time {
		calls := 0
		return func() time.Time {
			calls++
			if calls > valid {
				return time.Time{}
			}
			return dispatchNow
		}
	}
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	harness := &fakeHarness{}
	if _, err := run(dispatch.Service{Store: store.Dispatch(), Now: clock(0)}, harness, startCommandFor("start-1", "s1")); !errors.Is(err, dispatch.ErrInvalidClock) {
		t.Fatalf("zero clock before the record = %v", err)
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_intents") != 0 || harness.calls() != 0 {
		t.Fatal("a zero clock recorded or sent")
	}
	_, err := run(dispatch.Service{Store: store.Dispatch(), Now: clock(2)}, harness, startCommandFor("start-1", "s1"))
	var observation *dispatch.ObservationError
	if !errors.Is(err, dispatch.ErrObservationNotRecorded) || !errors.As(err, &observation) ||
		observation.StartResult.Turn.AdapterTurnID != "turn-start-1" || observation.Reason == "" || harness.calls() != 1 {
		t.Fatalf("zero clock after the send = %v", err)
	}
	if got := intentState(t, store, "start-1"); got != "dispatching" || countRows(t, store, "SELECT count(*) FROM dispatch_outcomes") != 0 {
		t.Fatalf("state = %s", got)
	}
	if again := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1")); again.Kind != dispatch.ResultDeliveryUnknown || harness.calls() != 1 {
		t.Fatalf("repetition = %+v", again)
	}
}
