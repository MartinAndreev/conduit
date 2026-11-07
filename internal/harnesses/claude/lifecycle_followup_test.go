package claude

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestFilterEnvironmentCaseFolding(t *testing.T) {
	parent := []string{
		"Path=C:\\bin", "Anthropic_Api_Key=key", "anthropic_auth_token=token", "ClaudeCode=1",
		"claude_code_entrypoint=cli", "Claude_Pid=7", "Anthropic_Base_Url=https://example.test", "Claude_Config_Dir=C:\\cfg",
		"ANTHROPIC_API_KEY=upper",
	}
	folded := filterEnvironment(parent, true)
	wantFolded := []string{"Path=C:\\bin", "Anthropic_Base_Url=https://example.test", "Claude_Config_Dir=C:\\cfg"}
	if !slices.Equal(folded, wantFolded) {
		t.Fatalf("case-folded environment = %q, want %q", folded, wantFolded)
	}
	exact := filterEnvironment(parent, false)
	wantExact := []string{
		"Path=C:\\bin", "Anthropic_Api_Key=key", "anthropic_auth_token=token", "ClaudeCode=1",
		"claude_code_entrypoint=cli", "Claude_Pid=7", "Anthropic_Base_Url=https://example.test", "Claude_Config_Dir=C:\\cfg",
	}
	if !slices.Equal(exact, wantExact) {
		t.Fatalf("case-sensitive environment = %q, want %q", exact, wantExact)
	}
}

func lossStatus(t *testing.T, events []provider.ProviderEvent) string {
	t.Helper()
	for _, event := range events {
		if body, ok := event.Body().(provider.SessionStateEvent); ok && body.Current == provider.SessionLost {
			return body.NativeStatus
		}
	}
	t.Fatal("no session_lost state event")
	return ""
}

// The root exits while a descendant keeps stdout open: the exit observer wins,
// publishes the loss and still kills the group exactly once; the transport
// stop that follows publishes nothing and does not kill again.
func TestUnexpectedRootExitKillsProcessGroupOnce(t *testing.T) {
	fake := newFakeClaude(t)
	s, stream := openTestSession(t, fake, noneContract)
	proc := fake.process()
	proc.exitRootOnly()
	events := assertSessionLost(t, s, stream)
	if status := lossStatus(t, events); status != "claude_process_exit" {
		t.Fatalf("loss published by %q, want the exit observer", status)
	}
	if calls := proc.killCalls.Load(); calls != 1 {
		t.Fatalf("KillTree calls = %d, want 1", calls)
	}
}

// The transport fails first while the root is alive: the transport path
// publishes the loss and kills once; the exit that follows does neither.
func TestTransportFailureKillsProcessGroupOnce(t *testing.T) {
	fake := newFakeClaude(t)
	s, stream := openTestSession(t, fake, noneContract)
	proc := fake.process()
	fake.write("not json")
	events := assertSessionLost(t, s, stream)
	if status := lossStatus(t, events); status != "claude_transport_closed" {
		t.Fatalf("loss published by %q, want the transport", status)
	}
	// exitDone closes after the exit observer ran its loss handling, so no
	// later path can request another kill.
	select {
	case <-s.(*session).exitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("owned process was not terminated and reaped")
	}
	if calls := proc.killCalls.Load(); calls != 1 {
		t.Fatalf("KillTree calls = %d, want 1", calls)
	}
}

// A failed open returns within the caller's deadline even though cleanup is
// still pending; the retained owner completes once the process is reaped.
func TestFailedOpenCleanupIsBoundedByCallerContext(t *testing.T) {
	fake := newFakeClaude(t)
	fake.silentInitialize = true
	fake.ignoreKill = true
	fake.exitOnStdinClose = false
	config := testConfig(fake)
	config.StopGrace = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := New(config).OpenSession(ctx, openRequest(t, noneContract))
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("failed open waited %s past the caller deadline", elapsed)
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorDeadline {
		t.Fatalf("open error = %v, want deadline", err)
	}
	var pending *CleanupPendingError
	if !errors.As(err, &pending) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("open error lost the pending cleanup owner: %v", err)
	}
	proc := fake.process()
	for deadline := time.Now().Add(2 * time.Second); proc.killCalls.Load() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not request group termination")
		}
	}
	select {
	case <-pending.Owner.Done():
		t.Fatal("cleanup completed before the process was reaped")
	default:
	}
	proc.exit()
	select {
	case <-pending.Owner.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("retained cleanup never completed")
	}
	if state := pending.Owner.Latest(); !state.Complete || !state.RootReaped || !state.ReadersDone {
		t.Fatalf("cleanup state = %+v", state)
	}
}

func interruptRequested(events []provider.ProviderEvent) []provider.ProviderEvent {
	var result []provider.ProviderEvent
	for _, event := range events {
		if body, ok := event.Body().(provider.TurnStateEvent); ok && body.Current == provider.TurnInterruptRequested {
			result = append(result, event)
		}
	}
	return result
}

func interruptTestSession(t *testing.T, resultFirst bool) (provider.Session, provider.EventStream, provider.StartTurnResult) {
	t.Helper()
	fake := newFakeClaude(t)
	var inputUUID string
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		inputUUID = messageUUID(message)
		echoAndStart(f, message)
	}
	fake.onInterrupt = func(f *fakeClaude, requestID string) {
		receipt := fmt.Sprintf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`, requestID)
		result := resultLine(f, "completed", inputUUID)
		if resultFirst {
			f.write(result)
			f.write(receipt)
			return
		}
		f.write(receipt)
		f.write(result)
	}
	s, stream := openTestSession(t, fake, noneContract)
	started := startTurn(t, s, noneContract)
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive
	})
	if _, err := s.InterruptTurn(context.Background(), provider.InterruptRequest{
		RequestID: "interrupt", Session: s.Ref(), ExpectedActiveTurn: started.Turn, Assignment: testAssignment(1),
	}); err != nil {
		t.Fatal(err)
	}
	return s, stream, started
}

// Receipt then terminal result back to back: the receipt is published in wire
// order, bound to the interrupted turn, before the terminal state.
func TestInterruptReceiptPrecedesTerminalInWireOrder(t *testing.T) {
	_, stream, started := interruptTestSession(t, false)
	events := receiveUntil(t, stream, isTurnTerminal)
	requested := interruptRequested(events)
	if len(requested) != 1 || !requested[0].Envelope.Turn.Present || requested[0].Envelope.Turn.Value != started.Turn {
		t.Fatalf("interrupt receipts = %#v", requested)
	}
}

// Terminal result then receipt: the turn already ended, so the receipt is
// suppressed and cannot attach to a new turn.
func TestInterruptReceiptAfterTerminalIsSuppressed(t *testing.T) {
	s, stream, _ := interruptTestSession(t, true)
	receiveUntil(t, stream, isTurnTerminal)
	next, err := s.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "next", Session: s.Ref(), Assignment: testAssignment(1),
		Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "next"}}, ResultContract: noneContract,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The receipt line precedes the new turn's echo on the wire, so the new
	// turn's activation is a barrier for any receipt event.
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive && event.Envelope.Turn.Value == next.Turn
	})
	if requested := interruptRequested(events); len(requested) != 0 {
		t.Fatalf("receipt published after the turn ended (new turn %v): %#v", next.Turn, requested)
	}
}

// The process writes its final result and exits at once, before the reader
// has handled the result: the terminal events are still published, and the
// loss follows them.
func TestRootExitPublishesBufferedFinalEventsBeforeLoss(t *testing.T) {
	for run := 0; run < 20; run++ {
		fake := newFakeClaude(t)
		fake.onUser = func(f *fakeClaude, message map[string]any) {
			echoAndStart(f, message)
			f.write(assistantTextLine(f, "msg_final01", "done"))
			f.write(queuedResultLine(f, "0", messageUUID(message)))
			f.process().exitRootOnly()
		}
		s, stream := openTestSession(t, fake, noneContract)
		startTurnRacingLoss(t, s)
		// The native result is real here, so the stream must carry it; only
		// the loss terminal is checked.
		events, err := receiveUntilQuiet(stream, 2*time.Second)
		var providerErr *provider.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost {
			t.Fatalf("run %d: stream terminal = %v, want session_lost", run, err)
		}
		terminal := slices.IndexFunc(events, isTurnTerminal)
		lost := slices.IndexFunc(events, func(event provider.ProviderEvent) bool {
			body, ok := event.Body().(provider.SessionStateEvent)
			return ok && body.Current == provider.SessionLost
		})
		if terminal < 0 || lost < terminal {
			t.Fatalf("run %d: terminal at %d, loss at %d; buffered final events were dropped", run, terminal, lost)
		}
		if status := lossStatus(t, events); status != "claude_process_exit" {
			t.Fatalf("run %d: loss published by %q", run, status)
		}
	}
}

// A writer failure starts the transport-loss path while the exit observer
// records the root exit and the final result is still unrouted. The observer
// owns the publication: the result is routed first and the loss follows it.
func TestWriterFailureDuringRootExitKeepsBufferedFinalEvents(t *testing.T) {
	fake := newFakeClaude(t)
	fake.exitOnStdinClose = false
	fake.ignoreKill = true
	var inputUUID string
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		inputUUID = messageUUID(message)
		echoAndStart(f, message)
	}
	config := testConfig(fake)
	config.StopGrace = 5 * time.Second
	opened, err := New(config).OpenSession(context.Background(), openRequest(t, noneContract))
	if err != nil {
		t.Fatal(err)
	}
	s := opened.Session.(*session)
	stream, err := s.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	proc := fake.process()
	t.Cleanup(proc.exit)
	startTurn(t, s, noneContract)
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive
	})

	entered, proceed := make(chan struct{}), make(chan struct{})
	s.beforeTransportLoss = func() {
		close(entered)
		<-proceed
	}
	// Writer failure: the fake stops reading stdin, so the next write fails.
	_ = proc.serverInput.Close()
	go func() { _, _ = s.transport.send(context.Background(), map[string]string{"type": "noop"}) }()
	<-entered
	// The exit observer records the root exit while the transport handler
	// is paused, kills (ignored by the fake) and waits for routing.
	proc.exitRootOnly()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(time.Millisecond) {
		recorded := false
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			recorded = s.rootExited
			return nil
		})
		if recorded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("root exit was not recorded")
		}
	}
	close(proceed)
	// The final result is routed only now, then stdout ends.
	fake.write(queuedResultLine(fake, "0", inputUUID))
	proc.exit()
	events, err := receiveUntilQuiet(stream, 2*time.Second)
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost {
		t.Fatalf("stream terminal = %v, want session_lost", err)
	}
	terminal := slices.IndexFunc(events, isTurnTerminal)
	lost := slices.IndexFunc(events, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.SessionStateEvent)
		return ok && body.Current == provider.SessionLost
	})
	if terminal < 0 || lost < terminal {
		t.Fatalf("terminal at %d, loss at %d; the transport path finished the stream early", terminal, lost)
	}
	if status := lossStatus(t, events); status != "claude_process_exit" {
		t.Fatalf("loss published by %q, want the exit observer", status)
	}
	losses := 0
	for _, body := range bodiesOf[provider.SessionStateEvent](events) {
		if body.Current == provider.SessionLost {
			losses++
		}
	}
	if losses != 1 {
		t.Fatalf("loss published %d times", losses)
	}
}
