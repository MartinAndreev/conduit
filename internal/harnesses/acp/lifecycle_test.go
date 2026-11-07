package acp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func openHarnessWithHooks(t *testing.T, serve func(*agentConn), hooks testHooks) *testHarness {
	t.Helper()
	return openHarnessFull(t, &fakeLauncher{serve: serve}, nil, hooks)
}

func waitSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Errorf("timed out waiting for %s", what)
	}
}

// The reply reaches the reader before StartTurn publishes the activation:
// the terminal must still follow "active".
func TestFastReplyIsPublishedAfterActivation(t *testing.T) {
	replyReceived := make(chan struct{})
	var once sync.Once
	h := openHarnessWithHooks(t, promptAgent(endTurn), testHooks{
		hookPromptReplyReceived: func(*session) { once.Do(func() { close(replyReceived) }) },
		hookBeforeActivation:    func(*session) { waitSignal(t, replyReceived, "prompt reply") },
	})
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	turns := bodiesOf[provider.TurnStateEvent](events)
	if len(turns) != 2 || turns[0].Current != provider.TurnActive || turns[1].Current != provider.TurnNativeTerminal || turns[1].Previous != provider.TurnActive {
		t.Fatalf("turn states = %+v", turns)
	}
}

// A new StartTurn issued while the previous turn publishes its terminal is
// refused, and the old turn's events keep its own assignment revision.
func TestEndingTurnHoldsLeaseAndKeepsItsAssignment(t *testing.T) {
	revisionTwo := testAssignment
	revisionTwo.Revision = 2
	var once sync.Once
	duringTerminal := make(chan error, 1)
	var h *testHarness
	h = openHarnessWithHooks(t, promptAgent(endTurn), testHooks{
		hookBeforeTerminal: func(*session) {
			once.Do(func() {
				_, err := h.session.StartTurn(context.Background(), provider.StartTurnRequest{
					Session: h.session.Ref(), Assignment: revisionTwo,
					Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "second"}},
					ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
				})
				duringTerminal <- err
			})
		},
	})
	first, err := h.startTurn(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, <-duringTerminal, provider.ErrorStaleBinding)
	for _, event := range events {
		if event.Envelope.Turn.Present && event.Envelope.Turn.Value == first.Turn && event.Envelope.Assignment.Value != testAssignment {
			t.Fatalf("first turn event carries %+v", event.Envelope.Assignment)
		}
	}

	// Barrier: the first turn released its lease after its terminal.
	h.awaitTurnReleased()
	second, err := h.session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session: h.session.Ref(), Assignment: revisionTwo,
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "second"}},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
	})
	if err != nil {
		t.Fatalf("StartTurn after terminal: %v", err)
	}
	events, err = h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	terminal := events[len(events)-1]
	if terminal.Envelope.Turn.Value != second.Turn || terminal.Envelope.Assignment.Value != revisionTwo {
		t.Fatalf("second terminal envelope = %+v", terminal.Envelope)
	}
}

func TestOpenTimeoutReturnsWithinDeadlineAndCleanupCompletes(t *testing.T) {
	release := make(chan struct{})
	serve := func(conn *agentConn) {
		for {
			if _, ok := conn.next(); !ok {
				break
			}
			// Never answer initialize.
		}
		<-release // slow exit after the kill
	}
	launcher := &fakeLauncher{serve: serve}
	adapter := newTestAdapter(launcher, func(config *Config) { config.StopGrace = 5 * time.Second })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := adapter.OpenSession(ctx, openRequest(t.TempDir()))
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("open returned after %v, beyond the caller deadline", elapsed)
	}
	requireCode(t, err, provider.ErrorDeadline)
	var pending *CleanupPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("pending cleanup is not observable: %v", err)
	}
	close(release)
	waitSignal(t, pending.Owner.Done(), "cleanup completion")
	if state := pending.Owner.Latest(); !state.Complete || !state.RootReaped || !state.TreeTerminationRequested {
		t.Fatalf("cleanup state = %+v", state)
	}
}

func TestOpenDoesNotResurrectSessionLostAfterLastReply(t *testing.T) {
	serve := func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				conn.reply(frame.ID, map[string]any{"sessionId": testSessionID})
				_ = conn.output.Close()
			}
		}
	}
	launcher := &fakeLauncher{serve: serve}
	adapter := newTestAdapter(launcher, nil)
	adapter.hooks = testHooks{hookBeforeOpenTransition: func(s *session) { waitSignal(t, s.rpc.finished, "transport stop") }}
	_, err := adapter.OpenSession(context.Background(), openRequest(t.TempDir()))
	requireCode(t, err, provider.ErrorSessionLost)
	if !launcher.process(t).reaped() {
		t.Fatal("lost agent was not reaped")
	}
}

func TestRequiredSettingMustBeConfirmedByReply(t *testing.T) {
	cases := []struct {
		name  string
		reply any
		want  provider.ErrorCode
	}{
		{"reply still reports ask", map[string]any{"configOptions": []any{modeOption("ask", "auto", "ask")}}, provider.ErrorCapabilityUnsupported},
		{"reply drops the option", map[string]any{"configOptions": []any{}}, provider.ErrorCapabilityUnsupported},
		{"reply without configOptions", map[string]any{}, provider.ErrorProtocolViolation},
		{"null reply", nil, provider.ErrorProtocolViolation},
		{"malformed configOptions", map[string]any{"configOptions": "auto"}, provider.ErrorProtocolViolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			launcher, _, err := openWithSettings(t, settingsAgent([]any{modeOption("ask", "auto", "ask")}, func(conn *agentConn, frame clientFrame) {
				conn.reply(frame.ID, tc.reply)
			}))
			requireCode(t, err, tc.want)
			if len(launcher.framesFor("session/set_config_option")) != 1 {
				t.Fatal("set_config_option was not sent once")
			}
			if !launcher.process(t).reaped() {
				t.Fatal("failed open left the agent running")
			}
		})
	}
}

// The agent stops reading stdin in the middle of a large prompt, closes its
// stdout and stays alive. The transport stop must kill it so the blocked write
// fails, StartTurn returns, and the session is lost without hanging.
func TestStdoutEOFWithBlockedPromptWriteDoesNotHang(t *testing.T) {
	agentExited := make(chan struct{})
	serve := func(conn *agentConn) {
		defer close(agentExited)
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				conn.reply(frame.ID, map[string]any{"sessionId": testSessionID})
				// Take only the first bytes of the prompt, so its write
				// stays blocked, then close stdout while staying alive.
				_, _ = conn.input.ReadByte()
				_ = conn.output.Close()
				<-conn.killed
				return
			}
		}
	}
	h := openHarness(t, serve, nil)
	started := make(chan error, 1)
	go func() {
		_, err := h.startTurn(context.Background(), strings.Repeat("large prompt ", 16*1024))
		started <- err
	}()
	select {
	case err := <-started:
		requireCode(t, err, provider.ErrorDeliveryUnknown)
	case <-time.After(5 * time.Second):
		t.Fatal("StartTurn hung on a blocked write after stdout EOF")
	}
	assertSessionLost(t, h)
	waitSignal(t, agentExited, "agent goroutine exit")
	if proc := h.launcher.process(t); proc.kills.Load() == 0 || !proc.reaped() {
		t.Fatalf("agent kills=%d reaped=%v", proc.kills.Load(), proc.reaped())
	}
}

// The root exits during a turn while a descendant keeps stdout open until the
// group is killed. Root-exit observation must stop the transport after
// StopGrace: the turn is lost, the session is lost, the group is killed and
// the process is fully reaped.
func TestRootExitWithHeldStdoutLosesTurnAndSession(t *testing.T) {
	serve := func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				conn.reply(frame.ID, map[string]any{"sessionId": testSessionID})
			case "session/prompt":
				// The root exits without answering; a descendant keeps
				// stdout open until the group is killed.
				conn.descendantHoldsStdout = true
				return
			}
		}
	}
	h := openHarness(t, serve, func(config *Config) { config.StopGrace = 50 * time.Millisecond })
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	proc := h.launcher.process(t)
	waitSignal(t, proc.RootExited(), "root exit")
	assertSessionLost(t, h)
}

// A prompt reply is already being handled (held mid-callback) when root-exit
// handling stops the transport. The stop must wait for that callback, so the
// turn's native terminal is published before SessionLost.
func TestHeldPromptReplyIsPublishedBeforeSessionLost(t *testing.T) {
	stopping := make(chan struct{})
	var stopOnce, holdOnce sync.Once
	serve := func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				conn.reply(frame.ID, map[string]any{"sessionId": testSessionID})
			case "session/prompt":
				endTurn(conn, frame)
				// The root exits; a descendant keeps stdout open.
				conn.descendantHoldsStdout = true
				return
			}
		}
	}
	h := openHarnessFull(t, &fakeLauncher{serve: serve}, func(config *Config) { config.StopGrace = time.Second }, testHooks{
		hookTransportStopping: func(*session) { stopOnce.Do(func() { close(stopping) }) },
		hookPromptReplyReceived: func(*session) {
			holdOnce.Do(func() { waitSignal(t, stopping, "root-exit transport stop") })
		},
	})
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(func(provider.ProviderEvent) bool { return false })
	requireCode(t, err, provider.ErrorSessionLost)
	terminal, lost := -1, -1
	for index, event := range events {
		switch body := event.Body().(type) {
		case provider.TurnStateEvent:
			if body.Current == provider.TurnNativeTerminal && body.TerminalReason == "end_turn" {
				terminal = index
			}
		case provider.SessionStateEvent:
			if body.Current == provider.SessionLost {
				lost = index
			}
		}
	}
	if terminal < 0 || lost < 0 || terminal > lost {
		t.Fatalf("terminal must precede SessionLost:\n%s", describe(events))
	}
}

// KillTree fails and the agent neither reads stdin nor exits. The stop path
// still closes our pipe ends, so the blocked prompt write fails, StartTurn
// returns, the session is lost and the kill failure is kept in the cause.
func TestTransportStopSettlesWhenKillFails(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	killErr := errors.New("kill refused")
	serve := func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				conn.reply(frame.ID, map[string]any{"sessionId": testSessionID})
				// Take only the first bytes of the prompt, close stdout and
				// keep running (and not reading) despite the kill.
				_, _ = conn.input.ReadByte()
				_ = conn.output.Close()
				<-release
				return
			}
		}
	}
	h := openHarnessFull(t, &fakeLauncher{serve: serve, killErr: killErr}, func(config *Config) { config.StopGrace = 50 * time.Millisecond }, nil)
	started := make(chan error, 1)
	go func() {
		_, err := h.startTurn(context.Background(), strings.Repeat("large prompt ", 16*1024))
		started <- err
	}()
	select {
	case err := <-started:
		requireCode(t, err, provider.ErrorDeliveryUnknown)
	case <-time.After(5 * time.Second):
		t.Fatal("StartTurn hung after a failed kill")
	}
	events, err := h.collectUntil(func(provider.ProviderEvent) bool { return false })
	requireCode(t, err, provider.ErrorSessionLost)
	if !errors.Is(err, killErr) {
		t.Fatalf("kill failure lost from the cause: %v", err)
	}
	turns := bodiesOf[provider.TurnStateEvent](events)
	if len(turns) == 0 || turns[len(turns)-1].Current != provider.TurnLost {
		t.Fatalf("turn not lost:\n%s", describe(events))
	}
	if h.launcher.process(t).kills.Load() == 0 {
		t.Fatal("kill was not attempted")
	}
}
