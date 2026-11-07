package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// scriptedLauncher answers each control command with the next scripted
// stdout and exit code, like `opencode api` printing a body and exiting 1 on
// an HTTP error.
type scriptedLauncher struct {
	mu        sync.Mutex
	responses []scriptedResponse
	specs     []processruntime.Spec
}

type scriptedResponse struct {
	stdout   string
	exitCode int
}

func (l *scriptedLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	var response scriptedResponse
	err := lockscope.WithMutexLock(&l.mu, func() error {
		l.specs = append(l.specs, spec)
		if len(l.responses) == 0 {
			return errors.New("unexpected control command")
		}
		response = l.responses[0]
		l.responses = l.responses[1:]
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &fakeProcess{stdout: io.NopCloser(strings.NewReader(response.stdout)), stderr: io.NopCloser(strings.NewReader("")), result: processruntime.Result{ExitCode: response.exitCode}, delay: time.Millisecond}, nil
}

const steerQueryMessage = "msg_00000000000000000000000001"

func TestCLIControlQuerySteerDelivery(t *testing.T) {
	found := scriptedResponse{stdout: `{"data":{"id":"` + steerQueryMessage + `","type":"user","text":"amend","time":{"created":1}}}`}
	notFound := scriptedResponse{stdout: `{"_tag":"MessageNotFoundError","sessionID":"ses_1","messageID":"` + steerQueryMessage + `","message":"not found"}`, exitCode: 1}
	inboxEmpty := scriptedResponse{stdout: `{"data":[]}`}
	inboxOther := scriptedResponse{stdout: `{"data":[{"id":"msg_other","type":"user"}]}`}
	inboxPending := scriptedResponse{stdout: `{"data":[{"id":"` + steerQueryMessage + `","type":"user","delivery":"steer"}]}`}
	tests := []struct {
		name      string
		responses []scriptedResponse
		want      SteerDeliveryObservation
		wantErr   bool
		commands  int
	}{
		{"present in history", []scriptedResponse{found}, SteerDeliveryObservation{InHistory: true}, false, 1},
		{"absent everywhere", []scriptedResponse{notFound, inboxOther, notFound}, SteerDeliveryObservation{}, false, 3},
		{"absent with empty inbox", []scriptedResponse{notFound, inboxEmpty, notFound}, SteerDeliveryObservation{}, false, 3},
		{"delivered between history and inbox reads", []scriptedResponse{notFound, inboxEmpty, found}, SteerDeliveryObservation{InHistory: true}, false, 3},
		{"second history read failure", []scriptedResponse{notFound, inboxEmpty, {stdout: "", exitCode: 1}}, SteerDeliveryObservation{}, true, 3},
		{"pending in inbox", []scriptedResponse{notFound, inboxPending}, SteerDeliveryObservation{InInbox: true}, false, 2},
		{"session not found is a failure", []scriptedResponse{{stdout: `{"_tag":"SessionNotFoundError","sessionID":"ses_1","message":"gone"}`, exitCode: 1}}, SteerDeliveryObservation{}, true, 1},
		{"not found for another message is a failure", []scriptedResponse{{stdout: `{"_tag":"MessageNotFoundError","sessionID":"ses_1","messageID":"msg_other","message":"x"}`, exitCode: 1}}, SteerDeliveryObservation{}, true, 1},
		{"unreadable failure body", []scriptedResponse{{stdout: "", exitCode: 1}}, SteerDeliveryObservation{}, true, 1},
		{"found response naming another message", []scriptedResponse{{stdout: `{"data":{"id":"msg_other"}}`}}, SteerDeliveryObservation{}, true, 1},
		{"inbox failure keeps no answer", []scriptedResponse{notFound, {stdout: `{"_tag":"UnknownError"}`, exitCode: 1}}, SteerDeliveryObservation{}, true, 2},
		{"process cannot start", nil, SteerDeliveryObservation{}, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := &scriptedLauncher{responses: tt.responses}
			control := &cliControl{executable: "opencode", launcher: launcher}
			got, err := control.QuerySteerDelivery(context.Background(), SteerDeliveryQuery{SessionID: "ses_1", Directory: "/work", MessageID: steerQueryMessage})
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("observation = %#v err = %v", got, err)
			}
			if len(launcher.specs) != tt.commands {
				t.Fatalf("commands = %d, want %d", len(launcher.specs), tt.commands)
			}
			wantGet := []string{"api", "session.message.get", "--param", "sessionID=ses_1", "--param", "messageID=" + steerQueryMessage}
			if !slices.Equal(launcher.specs[0].Args, wantGet) || launcher.specs[0].Dir != "/work" {
				t.Fatalf("message query = %q in %q", launcher.specs[0].Args, launcher.specs[0].Dir)
			}
			if len(launcher.specs) > 1 && !slices.Equal(launcher.specs[1].Args, []string{"api", "session.inbox.list", "--param", "sessionID=ses_1"}) {
				t.Fatalf("inbox query = %q", launcher.specs[1].Args)
			}
			if len(launcher.specs) > 2 && !slices.Equal(launcher.specs[2].Args, wantGet) {
				t.Fatalf("second history query = %q", launcher.specs[2].Args)
			}
		})
	}
}

func historyPage(next string, items ...string) scriptedResponse {
	cursor := "null"
	if next != "" {
		cursor = `"` + next + `"`
	}
	return scriptedResponse{stdout: `{"data":[` + strings.Join(items, ",") + `],"cursor":{"previous":null,"next":` + cursor + `}}`}
}

func historyItem(id, kind string) string { return `{"id":"` + id + `","type":"` + kind + `"}` }

func TestCLIControlQueryReleasedSteer(t *testing.T) {
	released := historyItem(steerQueryMessage, "user")
	idle := historyItem("msg_idle", "idle")
	assistant := historyItem("msg_assistant", "assistant")
	emptyPage := historyPage("")
	inboxEmpty := scriptedResponse{stdout: `{"data":[]}`}
	inboxPending := scriptedResponse{stdout: `{"data":[{"id":"` + steerQueryMessage + `"}]}`}
	tests := []struct {
		name      string
		responses []scriptedResponse
		want      ReleasedSteerObservation
		wantErr   bool
	}{
		{"idle newer than the message", []scriptedResponse{historyPage("c1", idle, assistant, released)}, ReleasedSteerObservation{InHistory: true, IdleAfterMessage: true}, false},
		{"idle only older than the message", []scriptedResponse{historyPage("c1", assistant, released, idle)}, ReleasedSteerObservation{InHistory: true}, false},
		{"message on a later page", []scriptedResponse{historyPage("c1", idle, assistant), historyPage("c2", released)}, ReleasedSteerObservation{InHistory: true, IdleAfterMessage: true}, false},
		{"never admitted", []scriptedResponse{historyPage("c1", assistant), emptyPage, inboxEmpty, emptyPage}, ReleasedSteerObservation{}, false},
		{"still in inbox", []scriptedResponse{emptyPage, inboxPending}, ReleasedSteerObservation{InInbox: true}, false},
		{"delivered between reads", []scriptedResponse{emptyPage, inboxEmpty, historyPage("c1", released)}, ReleasedSteerObservation{InHistory: true}, false},
		{"history failure", []scriptedResponse{{stdout: `{"_tag":"SessionNotFoundError"}`, exitCode: 1}}, ReleasedSteerObservation{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := &scriptedLauncher{responses: tt.responses}
			control := &cliControl{executable: "opencode", launcher: launcher}
			got, err := control.QueryReleasedSteer(context.Background(), SteerDeliveryQuery{SessionID: "ses_1", Directory: "/work", MessageID: steerQueryMessage})
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("observation = %#v err = %v", got, err)
			}
			first := []string{"api", "session.message.list", "--param", "sessionID=ses_1", "--param", "limit=100", "--param", "order=desc"}
			if !slices.Equal(launcher.specs[0].Args, first) {
				t.Fatalf("first page args = %q", launcher.specs[0].Args)
			}
			if tt.name == "message on a later page" && !slices.Equal(launcher.specs[1].Args, []string{"api", "session.message.list", "--param", "sessionID=ses_1", "--param", "limit=100", "--param", "cursor=c1"}) {
				t.Fatalf("second page args = %q", launcher.specs[1].Args)
			}
		})
	}
}

func TestCLIControlPromptSendsNativeMessageID(t *testing.T) {
	launcher := &fakeLauncher{output: `{"data":{"id":"` + steerQueryMessage + `","sessionID":"native-session"}}`}
	control := &cliControl{executable: "opencode", launcher: launcher}
	if _, _, err := control.Prompt(context.Background(), PromptRequest{SessionID: "native-session", Directory: "/work", MessageID: steerQueryMessage, Text: "fix", Delivery: "steer"}); err != nil {
		t.Fatal(err)
	}
	if !containsPair(launcher.spec.Args, "--data", `{"id":"`+steerQueryMessage+`","text":"fix","delivery":"steer"}`) {
		t.Fatalf("prompt args = %q", launcher.spec.Args)
	}
}

// reconcileFixture opens a session and starts a turn whose owned run client
// ends after runFor. The steer's session.prompt reply is lost after dispatch.
type reconcileFixture struct {
	session  provider.Session
	stream   provider.EventStream
	started  provider.StartTurnResult
	assigned provider.AssignmentRef
}

func newReconcileFixture(t *testing.T, control Control, runFor time.Duration, configure ...func(*session)) reconcileFixture {
	t.Helper()
	launcher := &fakeLauncher{output: `{"type":"text","sessionID":"native-session","part":{"id":"text-1","text":"hello"}}` + "\n", delay: runFor}
	var ids sync.Mutex
	next := 0
	adapter := New(Config{Control: control, Launcher: launcher, Now: func() time.Time { return time.UnixMilli(1_700_000_000_000) }, NewID: func(prefix string) (string, error) {
		var id string
		_ = lockscope.WithMutexLock(&ids, func() error {
			next++
			id = fmt.Sprintf("%s-%06d", prefix, next)
			return nil
		})
		return id, nil
	}})
	assignment := provider.AssignmentRef{ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", Revision: 1}
	contract := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: assignment, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "m"}, ResultContract: contract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := opened.Session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	// Seams are set before the first turn starts its reader goroutine.
	for _, apply := range configure {
		apply(opened.Session.(*session))
	}
	started, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: assignment, ResultContract: contract, Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	return reconcileFixture{session: opened.Session, stream: stream, started: started, assigned: assignment}
}

func (f reconcileFixture) steer(t *testing.T) provider.SteerRequest {
	t.Helper()
	next := f.assigned
	next.Revision++
	request := provider.SteerRequest{RequestID: "steer", MessageID: "steer-message", Session: f.session.Ref(), ExpectedActiveTurn: f.started.Turn, CurrentAssignment: f.assigned, ExpectedRevision: 1, NewAssignment: next, Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "amend"}}}
	_, err := f.session.Steer(context.Background(), request)
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || pe.Code != provider.ErrorDeliveryUnknown {
		t.Fatalf("steer err = %v, want delivery_unknown", err)
	}
	return request
}

func (f reconcileFixture) startNext(revision uint64, release provider.Optional[provider.MessageID]) (provider.StartTurnResult, error) {
	assignment := f.assigned
	assignment.Revision = revision
	return f.session.StartTurn(context.Background(), provider.StartTurnRequest{Session: f.session.Ref(), Assignment: assignment, ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone}, Input: []provider.InputPart{{Text: "next"}}, ReleaseUnresolvedSteer: release})
}

func receiveOpenCodeUntil(t *testing.T, stream provider.EventStream, match func(provider.ProviderEvent) bool) []provider.ProviderEvent {
	t.Helper()
	var events []provider.ProviderEvent
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("stream ended before the expected event: %v", err)
		}
		events = append(events, event)
		if match(event) {
			return events
		}
	}
}

func isControlReceipt(event provider.ProviderEvent) bool {
	_, ok := event.Body().(provider.ControlReceiptEvent)
	return ok
}

func isWarningCode(code string) func(provider.ProviderEvent) bool {
	return func(event provider.ProviderEvent) bool {
		warning, ok := event.Body().(provider.ProtocolWarningEvent)
		return ok && warning.Code == code
	}
}

func requireStartStale(t *testing.T, err error, context string) {
	t.Helper()
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || pe.Code != provider.ErrorStaleBinding || pe.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("%s: err = %v, want stale_binding", context, err)
	}
}

func uncertainPromptControl() *fakeControl {
	return &fakeControl{promptSent: true, promptErr: io.ErrUnexpectedEOF}
}

func TestUnresolvedSteerDeliveredAfterTurnEndAdvancesAssignment(t *testing.T) {
	control := uncertainPromptControl()
	control.queryObservation = SteerDeliveryObservation{InHistory: true}
	f := newReconcileFixture(t, control, 200*time.Millisecond)
	request := f.steer(t)
	if !strings.HasPrefix(control.prompt.MessageID, "msg_") || len(control.prompt.MessageID) != len("msg_")+26 {
		t.Fatalf("steer native message ID = %q", control.prompt.MessageID)
	}
	if _, calls := control.queried(); calls != 0 {
		t.Fatal("delivery was queried while the steered turn was still observed")
	}
	events := receiveOpenCodeUntil(t, f.stream, isControlReceipt)
	query, calls := control.queried()
	if calls != 1 || query.MessageID != control.prompt.MessageID || query.SessionID != "native-session" || query.Directory != "/work" {
		t.Fatalf("query = %#v calls = %d", query, calls)
	}
	idle := slices.IndexFunc(events, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.SessionStateEvent)
		return ok && body.Current == provider.SessionIdleObserved
	})
	if idle < 0 || events[idle].Envelope.Assignment.Present {
		t.Fatal("turn-end observation during the unresolved steer was missing or attributed")
	}
	last := events[len(events)-1]
	receipt := last.Body().(provider.ControlReceiptEvent).Receipt
	if receipt.MessageID != "steer-message" || receipt.Disposition != provider.DeliveryTransportAccepted || receipt.Receipt != provider.ReceiptNativeKnown ||
		receipt.DispatchedFor.Value != request.NewAssignment || string(receipt.NativeReceiptID.Value) != control.prompt.MessageID {
		t.Fatalf("delivered receipt = %#v", receipt)
	}
	if !last.Envelope.Assignment.Present || last.Envelope.Assignment.Value != request.NewAssignment {
		t.Fatalf("attribution after delivery = %#v", last.Envelope.Assignment)
	}
	if _, err := f.startNext(2, provider.None[provider.MessageID]()); err != nil {
		t.Fatalf("start at the delivered revision: %v", err)
	}
}

func TestUnresolvedSteerAbsentAfterTurnEndReleasesBinding(t *testing.T) {
	control := uncertainPromptControl()
	f := newReconcileFixture(t, control, 200*time.Millisecond)
	f.steer(t)
	events := receiveOpenCodeUntil(t, f.stream, isControlReceipt)
	if !slices.ContainsFunc(events, isWarningCode("steer_not_delivered")) {
		t.Fatal("not-delivered release published no warning")
	}
	last := events[len(events)-1]
	receipt := last.Body().(provider.ControlReceiptEvent).Receipt
	if receipt.MessageID != "steer-message" || receipt.Disposition != provider.DeliveryRejected || receipt.Receipt != provider.ReceiptNone || receipt.DispatchedFor.Present {
		t.Fatalf("not-delivered receipt = %#v", receipt)
	}
	if !last.Envelope.Assignment.Present || last.Envelope.Assignment.Value != f.assigned {
		t.Fatalf("attribution after release = %#v", last.Envelope.Assignment)
	}
	_, err := f.startNext(2, provider.None[provider.MessageID]())
	requireStartStale(t, err, "start at the undelivered revision")
	if _, err := f.startNext(1, provider.None[provider.MessageID]()); err != nil {
		t.Fatalf("start after not-delivered release: %v", err)
	}
}

func TestUnresolvedSteerDoubtRemainsUntilExplicitRelease(t *testing.T) {
	cases := map[string]func(*fakeControl){
		"query failure":    func(c *fakeControl) { c.queryErr = errors.New("opencode api unavailable") },
		"pending in inbox": func(c *fakeControl) { c.queryObservation = SteerDeliveryObservation{InInbox: true} },
	}
	for name, configure := range cases {
		t.Run(name, func(t *testing.T) {
			control := uncertainPromptControl()
			configure(control)
			f := newReconcileFixture(t, control, 200*time.Millisecond)
			f.steer(t)
			events := receiveOpenCodeUntil(t, f.stream, isWarningCode("steer_delivery_unknown"))
			if slices.ContainsFunc(events, isControlReceipt) || events[len(events)-1].Envelope.Assignment.Present {
				t.Fatal("doubt was resolved or attributed")
			}
			_, err := f.startNext(1, provider.None[provider.MessageID]())
			requireStartStale(t, err, "start without release")
			_, err = f.startNext(1, provider.Some(provider.MessageID("other-message")))
			requireStartStale(t, err, "mismatched release")
			started, err := f.startNext(1, provider.Some(provider.MessageID("steer-message")))
			if err != nil {
				t.Fatalf("matching release: %v", err)
			}
			if started.Assignment != f.assigned {
				t.Fatalf("released start assignment = %#v", started.Assignment)
			}
			// The released steer may still run in this turn: nothing in it is
			// attributed, and an unsettled query keeps it so.
			events = receiveOpenCodeUntil(t, f.stream, isWarningCode("released_steer_pending"))
			for _, event := range events {
				if event.Envelope.Assignment.Present || event.Envelope.Turn.Present {
					t.Fatalf("output after release attributed: %#v", event.Envelope)
				}
			}
		})
	}
}

func firstTextEvent(t *testing.T, stream provider.EventStream) provider.ProviderEvent {
	t.Helper()
	events := receiveOpenCodeUntil(t, stream, func(event provider.ProviderEvent) bool {
		_, ok := event.Body().(provider.AssistantTextEvent)
		return ok
	})
	return events[len(events)-1]
}

func waitWarnings(t *testing.T, stream provider.EventStream, codes ...string) []provider.ProviderEvent {
	t.Helper()
	remaining := slices.Clone(codes)
	return receiveOpenCodeUntil(t, stream, func(event provider.ProviderEvent) bool {
		if warning, ok := event.Body().(provider.ProtocolWarningEvent); ok {
			if index := slices.Index(remaining, warning.Code); index >= 0 {
				remaining = slices.Delete(remaining, index, index+1)
			}
		}
		return len(remaining) == 0
	})
}

// After a release, output stays unattributed until a native query proves the
// released message's work ended: delivered with a later idle marker. Delivery
// alone keeps it unsettled.
func TestReleasedSteerOutputUnattributedUntilSettled(t *testing.T) {
	control := uncertainPromptControl()
	control.queryErr = errors.New("opencode api unavailable")
	f := newReconcileFixture(t, control, 200*time.Millisecond)
	f.steer(t)
	released := control.prompt.MessageID
	receiveOpenCodeUntil(t, f.stream, isWarningCode("steer_delivery_unknown"))
	control.setReleased(released, releasedOutcome{observation: ReleasedSteerObservation{InHistory: true}})
	if _, err := f.startNext(1, provider.Some(provider.MessageID("steer-message"))); err != nil {
		t.Fatal(err)
	}
	if text := firstTextEvent(t, f.stream); text.Envelope.Assignment.Present || text.Envelope.Turn.Present {
		t.Fatalf("output after release attributed: %#v", text.Envelope)
	}
	receiveOpenCodeUntil(t, f.stream, isWarningCode("released_steer_pending"))
	// Delivered without a later idle marker: the next run is still guarded.
	control.setReleased(released, releasedOutcome{observation: ReleasedSteerObservation{InHistory: true, IdleAfterMessage: true}})
	if _, err := f.startNext(1, provider.None[provider.MessageID]()); err != nil {
		t.Fatal(err)
	}
	if text := firstTextEvent(t, f.stream); text.Envelope.Assignment.Present || text.Envelope.Turn.Present {
		t.Fatalf("output before settlement attributed: %#v", text.Envelope)
	}
	receiveOpenCodeUntil(t, f.stream, isWarningCode("released_steer_settled"))
	next, err := f.startNext(1, provider.None[provider.MessageID]())
	if err != nil {
		t.Fatal(err)
	}
	if text := firstTextEvent(t, f.stream); !text.Envelope.Assignment.Present || text.Envelope.Assignment.Value != f.assigned || text.Envelope.Turn.Value != next.Turn {
		t.Fatalf("output after settlement = %#v", text.Envelope)
	}
}

// Two releases: settling the second (never admitted) does not lift the guard
// while the first is still pending.
func TestSecondReleaseSettlingDoesNotLiftGuardOfFirst(t *testing.T) {
	control := uncertainPromptControl()
	control.queryErr = errors.New("opencode api unavailable")
	f := newReconcileFixture(t, control, 200*time.Millisecond)
	f.steer(t)
	first := control.prompt.MessageID
	receiveOpenCodeUntil(t, f.stream, isWarningCode("steer_delivery_unknown"))
	control.setReleased(first, releasedOutcome{observation: ReleasedSteerObservation{InInbox: true}})
	second, err := f.startNext(1, provider.Some(provider.MessageID("steer-message")))
	if err != nil {
		t.Fatal(err)
	}
	// Steering stays allowed while a release is unsettled; its output is
	// still guarded.
	next := f.assigned
	next.Revision++
	_, err = f.session.Steer(context.Background(), provider.SteerRequest{RequestID: "steer-b", MessageID: "steer-message-b", Session: f.session.Ref(), ExpectedActiveTurn: second.Turn, CurrentAssignment: f.assigned, ExpectedRevision: 1, NewAssignment: next, Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "amend again"}}})
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || pe.Code != provider.ErrorDeliveryUnknown {
		t.Fatalf("second steer err = %v", err)
	}
	secondID := control.prompt.MessageID
	if secondID == first {
		t.Fatal("second steer reused the first native message ID")
	}
	waitWarnings(t, f.stream, "steer_delivery_unknown", "released_steer_pending")
	control.setReleased(secondID, releasedOutcome{observation: ReleasedSteerObservation{}})
	if _, err := f.startNext(1, provider.Some(provider.MessageID("steer-message-b"))); err != nil {
		t.Fatal(err)
	}
	events := waitWarnings(t, f.stream, "released_steer_settled", "released_steer_pending")
	for _, event := range events {
		if event.Envelope.Assignment.Present || event.Envelope.Turn.Present {
			t.Fatalf("event attributed while the first release is pending: %#v", event.Body())
		}
	}
	if _, err := f.startNext(1, provider.None[provider.MessageID]()); err != nil {
		t.Fatal(err)
	}
	if text := firstTextEvent(t, f.stream); text.Envelope.Assignment.Present || text.Envelope.Turn.Present {
		t.Fatalf("output attributed while the first release is pending: %#v", text.Envelope)
	}
}

func TestStartTurnReleaseWithNothingUnresolvedIsStale(t *testing.T) {
	control := &fakeControl{}
	f := newReconcileFixture(t, control, time.Millisecond)
	receiveOpenCodeUntil(t, f.stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.SessionStateEvent)
		return ok && body.Current == provider.SessionIdleObserved
	})
	_, err := f.startNext(1, provider.Some(provider.MessageID("steer-message")))
	requireStartStale(t, err, "release with nothing unresolved")
	if _, err := f.startNext(1, provider.None[provider.MessageID]()); err != nil {
		t.Fatalf("plain start after rejected release: %v", err)
	}
}

// A steer whose prompt reply is lost after the turn already ended misses the
// turn-end trigger; reconciliation starts from the failure path instead.
func TestUnresolvedSteerAfterTurnEndReconcilesFromFailurePath(t *testing.T) {
	control := &blockingQueryControl{fakeControl: uncertainPromptControl(), entered: make(chan struct{}), release: make(chan struct{})}
	control.queryObservation = SteerDeliveryObservation{InHistory: true}
	f := newReconcileFixture(t, control, time.Millisecond)
	receiveOpenCodeUntil(t, f.stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.SessionStateEvent)
		return ok && body.Current == provider.SessionIdleObserved
	})
	s := f.session.(*session)
	// Model a steer reserved before the turn ended whose reply was lost after.
	request := provider.SteerRequest{RequestID: "steer", MessageID: "steer-message", NewAssignment: provider.AssignmentRef{ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", Revision: 2}, ExpectedRevision: 1}
	_ = s.handleSteerDeliveryFailure(request, "msg_late", true, io.ErrUnexpectedEOF)
	<-control.entered
	close(control.release)
	events := receiveOpenCodeUntil(t, f.stream, isControlReceipt)
	if receipt := events[len(events)-1].Body().(provider.ControlReceiptEvent).Receipt; receipt.DispatchedFor.Value != request.NewAssignment {
		t.Fatalf("receipt = %#v", receipt)
	}
	if query, _ := control.queried(); query.MessageID != "msg_late" {
		t.Fatalf("queried %q", query.MessageID)
	}
}

// blockingQueryControl holds the first delivery query until release closes;
// later queries pass through.
type blockingQueryControl struct {
	*fakeControl
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingQueryControl) QuerySteerDelivery(ctx context.Context, query SteerDeliveryQuery) (SteerDeliveryObservation, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.fakeControl.QuerySteerDelivery(ctx, query)
}

// A release accepted while the native query runs supersedes its answer.
func TestReleaseDuringQuerySupersedesReconciliation(t *testing.T) {
	control := &blockingQueryControl{fakeControl: uncertainPromptControl(), entered: make(chan struct{}), release: make(chan struct{})}
	control.queryObservation = SteerDeliveryObservation{InHistory: true}
	applied := make(chan rules.SteerReconcileKind, 1)
	f := newReconcileFixture(t, control, 200*time.Millisecond, func(s *session) {
		s.steerReconcileApplied = func(kind rules.SteerReconcileKind) { applied <- kind }
	})
	f.steer(t)
	<-control.entered
	if _, err := f.startNext(1, provider.Some(provider.MessageID("steer-message"))); err != nil {
		t.Fatalf("release during query: %v", err)
	}
	close(control.release)
	select {
	case kind := <-applied:
		if kind != rules.SteerReconcileSuperseded {
			t.Fatalf("reconciliation = %s, want superseded", kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconciliation was never applied")
	}
	s := f.session.(*session)
	var assignment provider.AssignmentRef
	var unresolved bool
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		assignment, unresolved = s.assignment, s.unresolvedSteer != nil
		return nil
	})
	if assignment != f.assigned || unresolved {
		t.Fatalf("superseded query changed state: assignment=%#v unresolved=%v", assignment, unresolved)
	}
}

func TestApplySteerReconcileDiscardsSupersededAnswer(t *testing.T) {
	s, r := steerTestSession(&fakeControl{})
	queried := &unresolvedSteer{request: r, nativeMessageID: "msg_a", reconciling: true}
	replacement := &unresolvedSteer{request: r, nativeMessageID: "msg_b"}
	s.unresolvedSteer = replacement
	decision := s.applySteerReconcile(queried, SteerDeliveryObservation{InHistory: true}, false)
	if decision.Kind != rules.SteerReconcileSuperseded || s.unresolvedSteer != replacement || s.assignment != r.CurrentAssignment {
		t.Fatalf("decision = %#v, state changed", decision)
	}
}
