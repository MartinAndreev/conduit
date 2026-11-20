package acp

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func closeSession(h *testHarness, policy provider.ClosePolicy) (provider.CloseSessionResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.session.Close(ctx, provider.CloseSessionRequest{RequestID: "close-1", Session: h.session.Ref(), Policy: policy})
}

func TestCloseIdleSessionReleasesOwnedAgent(t *testing.T) {
	for _, policy := range []provider.ClosePolicy{provider.CloseRequireQuiescent, provider.CloseExplicitDetach, provider.CloseInterruptThenClose} {
		t.Run(string(policy), func(t *testing.T) {
			h := openHarness(t, promptAgent(endTurn), nil)
			result, err := closeSession(h, policy)
			if err != nil || !result.HandleClosed || !result.ResourcesReleased || result.ExecutionState != provider.ExecutionNone {
				t.Fatalf("Close = %+v, %v", result, err)
			}
			proc := h.launcher.process(t)
			if !proc.reaped() || proc.kills.Load() == 0 {
				t.Fatalf("agent reaped=%v kills=%d", proc.reaped(), proc.kills.Load())
			}
			if calls := proc.finalizes.Load(); calls != 1 {
				t.Fatalf("Close finalized %d times, want once", calls)
			}
			if len(h.launcher.framesFor("session/close")) != 1 || len(h.launcher.framesFor("session/cancel")) != 0 {
				t.Fatalf("frames = %+v", h.launcher.recorded())
			}
			events, err := h.collectUntil(func(provider.ProviderEvent) bool { return false })
			if !errors.Is(err, io.EOF) {
				t.Fatalf("stream end = %v", err)
			}
			states := bodiesOf[provider.SessionStateEvent](events)
			if states[len(states)-1].Current != provider.SessionClosed || states[len(states)-2].Current != provider.SessionClosing {
				t.Fatalf("states = %+v", states)
			}
			_, err = closeSession(h, policy)
			requireCode(t, err, provider.ErrorSessionLost)
		})
	}
}

func TestCloseWithUnresolvedPrompt(t *testing.T) {
	stubbornPrompt := func(conn *agentConn, frame clientFrame) {
		for {
			next, ok := conn.next()
			if !ok {
				return
			}
			switch next.Method {
			case "session/cancel":
				conn.reply(frame.ID, map[string]any{"stopReason": "cancelled"})
			case "session/close":
				conn.reply(next.ID, nil)
			}
		}
	}
	h := openHarness(t, promptAgent(stubbornPrompt), nil)
	if _, err := h.startTurn(context.Background(), "long"); err != nil {
		t.Fatal(err)
	}

	_, err := closeSession(h, provider.CloseRequireQuiescent)
	requireCode(t, err, provider.ErrorStaleBinding)
	_, err = closeSession(h, provider.CloseExplicitDetach)
	requireCode(t, err, provider.ErrorCapabilityUnsupported)

	result, err := closeSession(h, provider.CloseInterruptThenClose)
	if err != nil || !result.ResourcesReleased {
		t.Fatalf("Close = %+v, %v", result, err)
	}
	frames := h.launcher.recorded()
	cancelIndex, closeIndex := -1, -1
	for index, frame := range frames {
		switch frame.Method {
		case "session/cancel":
			cancelIndex = index
		case "session/close":
			closeIndex = index
		}
	}
	if cancelIndex < 0 || closeIndex < cancelIndex {
		t.Fatalf("cancel must precede close: %+v", frames)
	}
	events, _ := h.collectUntil(func(provider.ProviderEvent) bool { return false })
	turns := bodiesOf[provider.TurnStateEvent](events)
	if last := turns[len(turns)-1]; last.Current != provider.TurnNativeTerminal || last.TerminalReason != "cancelled" {
		t.Fatalf("turn states = %+v", turns)
	}
}

func TestCloseKillsAgentThatIgnoresStdinClose(t *testing.T) {
	serve := func(conn *agentConn) {
		standardAgent(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}}, nil, nil)(conn)
		// Keep running after stdin EOF until the group is killed.
		<-conn.killed
	}
	h := openHarness(t, serve, func(config *Config) { config.StopGrace = 50 * time.Millisecond })
	started := time.Now()
	result, err := closeSession(h, provider.CloseRequireQuiescent)
	if err != nil || !result.ResourcesReleased {
		t.Fatalf("Close = %+v, %v", result, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("close wait was not bounded by StopGrace")
	}
	if !h.launcher.process(t).reaped() {
		t.Fatal("agent was not reaped")
	}
	if len(h.launcher.framesFor("session/close")) != 0 {
		t.Fatal("session/close sent without advertisement")
	}
}

func TestCloseReportsUnprovenQuiescenceWhenAgentCannotBeReaped(t *testing.T) {
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	serve := func(conn *agentConn) {
		standardAgent(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}}, nil, nil)(conn)
		<-hold // never exits during the test; Wait does not return
	}
	h := openHarness(t, serve, func(config *Config) { config.StopGrace = 20 * time.Millisecond })
	result, err := closeSession(h, provider.CloseRequireQuiescent)
	if err != nil || !result.HandleClosed || result.ResourcesReleased || result.ExecutionState != provider.ExecutionUnknown {
		t.Fatalf("Close = %+v, %v", result, err)
	}
}

// Close whose bounded waits end before the root exited (the kill is refused)
// still hands the release to Finalize: once the root exits later, it is
// released although Close already returned.
func TestTimedOutCloseStillReleasesRootAfterLateExit(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
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
				<-release
				return
			}
		}
	}
	h := openHarnessFull(t, &fakeLauncher{serve: serve, killErr: errors.New("kill refused")}, func(config *Config) { config.StopGrace = 20 * time.Millisecond }, nil)
	result, err := closeSession(h, provider.CloseRequireQuiescent)
	if err != nil || result.ResourcesReleased {
		t.Fatalf("timed-out close = %+v, %v; want resources unconfirmed", result, err)
	}
	proc := h.launcher.process(t)
	if proc.finalizes.Load() == 0 {
		t.Fatal("timed-out Close did not finalize")
	}
	select {
	case <-proc.released:
		t.Fatal("root released before it exited")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-proc.released:
	case <-time.After(2 * time.Second):
		t.Fatal("root that exited after Close was never released")
	}
}

// The transport stop's kill is refused by the OS and finalization then
// releases the root, so Close's own kill is refused by the finalization
// gate. That refusal is no termination evidence: Close reports neither
// quiescence nor released resources.
func TestFailedStopKillIsNotConfirmedByFinalization(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
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
				// Stop the transport (stdout ends) and keep running.
				_ = conn.output.Close()
				<-release
				return
			}
		}
	}
	h := openHarnessFull(t, &fakeLauncher{serve: serve, killErr: errors.New("kill refused")}, func(config *Config) { config.StopGrace = 50 * time.Millisecond }, nil)
	go func() { _, _ = h.startTurn(context.Background(), "go") }()
	proc := h.launcher.process(t)
	deadline := time.Now().Add(2 * time.Second)
	for proc.finalizes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("transport stop did not register finalization")
		}
		time.Sleep(5 * time.Millisecond)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-proc.released:
	case <-time.After(2 * time.Second):
		t.Fatal("root was not released after its exit")
	}
	result, err := closeSession(h, provider.CloseRequireQuiescent)
	if err != nil || result.ResourcesReleased || result.ExecutionState != provider.ExecutionUnknown {
		t.Fatalf("Close = %+v, %v; want unconfirmed termination", result, err)
	}
}
