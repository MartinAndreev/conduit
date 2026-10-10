package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// burstLauncher answers requests like an App Server and writes extra native
// lines in the same write as the turn/start reply, as a real server may.
type burstLauncher struct{ afterTurnStart func(turnID string) []byte }

func (l *burstLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	proc := &scriptedProcess{stdin: clientInput, stdout: clientOutput, stderr: io.NopCloser(&emptyReader{}), done: make(chan struct{}), serverInput: serverInput, serverOutput: serverOutput}
	go func() {
		defer close(proc.done)
		defer serverOutput.Close()
		scanner := bufio.NewScanner(serverInput)
		for scanner.Scan() {
			var request map[string]any
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				continue
			}
			id, ok := request["id"]
			if !ok {
				continue
			}
			var result any = map[string]any{}
			switch request["method"] {
			case "thread/start":
				result = map[string]any{"thread": map[string]any{"id": "thread-1"}}
			case "turn/start":
				result = map[string]any{"turn": map[string]any{"id": "turn-1"}}
			}
			reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
			out := append(reply, '\n')
			if request["method"] == "turn/start" && l.afterTurnStart != nil {
				out = append(out, l.afterTurnStart("turn-1")...)
			}
			if _, err := serverOutput.Write(out); err != nil {
				return
			}
		}
	}()
	return proc, nil
}

func nativeNotification(method string, params any) []byte {
	encoded, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	return append(encoded, '\n')
}

var robustnessContract = provider.ResultContractRef{Schema: json.RawMessage(`{"type":"object"}`), Requested: provider.SchemaNative}

func openStartedSession(t *testing.T, launcher processruntime.Launcher) (provider.Session, provider.EventStream) {
	t.Helper()
	adapter := New(Config{Launcher: launcher, CloseGrace: 20 * time.Millisecond})
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: testAssignment(1), Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: true}, Model: provider.ModelSelection{ModelID: "model"}, ResultContract: robustnessContract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := opened.Session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: testAssignment(1), Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "work"}}, ResultContract: robustnessContract}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
	})
	return opened.Session, stream
}

func receiveUntilQuiet(stream provider.EventStream, quiet time.Duration) ([]provider.ProviderEvent, error) {
	var events []provider.ProviderEvent
	for {
		ctx, cancel := context.WithTimeout(context.Background(), quiet)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
}

// Notifications that follow the turn/start reply on the wire belong to the new
// turn even when the waiting caller has not been scheduled yet.
func TestTurnStartReplyIsCommittedBeforeFollowingNotifications(t *testing.T) {
	for run := 0; run < 25; run++ {
		launcher := &burstLauncher{afterTurnStart: func(turnID string) []byte {
			started := nativeNotification("turn/started", map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": turnID}})
			delta := nativeNotification("item/agentMessage/delta", map[string]any{"threadId": "thread-1", "turnId": turnID, "itemId": "message", "delta": "hello"})
			return append(started, delta...)
		}}
		_, stream := openStartedSession(t, launcher)
		events, _ := receiveUntilQuiet(stream, 200*time.Millisecond)
		active, text := false, false
		for _, event := range events {
			switch body := event.Body().(type) {
			case provider.ProtocolWarningEvent:
				t.Fatalf("run %d: notification after reply was rejected: %s", run, body.Code)
			case provider.TurnStateEvent:
				active = active || body.Current == provider.TurnActive
			case provider.AssistantTextEvent:
				text = true
			}
		}
		if !active || !text {
			t.Fatalf("run %d: turn active=%v text=%v", run, active, text)
		}
	}
}

// A native line beyond the reader bound must end the session visibly instead
// of leaving it "open" with a silent stream and a blocked App Server.
func TestTransportStopLosesSessionVisibly(t *testing.T) {
	huge := strings.Repeat("a", 5*1024*1024)
	launcher := &burstLauncher{afterTurnStart: func(turnID string) []byte {
		return nativeNotification("item/completed", map[string]any{"threadId": "thread-1", "turnId": turnID, "item": map[string]any{"id": "command", "type": "commandExecution", "aggregatedOutput": huge}})
	}}
	session, stream := openStartedSession(t, launcher)
	_, err := receiveUntilQuiet(stream, 2*time.Second)
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost {
		t.Fatalf("stream terminal = %v, want session_lost", err)
	}
	snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
	if snapshot.SessionState != provider.SessionLost {
		t.Fatalf("session state = %s, want lost", snapshot.SessionState)
	}
}

type gatedFailingWriter struct{ gate chan struct{} }

func (w *gatedFailingWriter) Write([]byte) (int, error) {
	<-w.gate
	return 0, errors.New("broken pipe")
}
func (*gatedFailingWriter) Close() error { return nil }

// A request queued behind a failed write must fail with the transport error
// instead of waiting for its context (forever with context.Background()).
func TestQueuedCallFailsWhenWriterStops(t *testing.T) {
	writer := &gatedFailingWriter{gate: make(chan struct{})}
	stdout, _ := io.Pipe()
	client := newRPCClient(newRPCProcess(writer, stdout))
	closed := make(chan error, 1)
	client.onClosed = func(err error) { closed <- err }
	client.start(nil, nil)
	first := make(chan error, 1)
	go func() { _, _, err := client.call(context.Background(), "first", nil, nil); first <- err }()
	time.Sleep(20 * time.Millisecond)
	second := make(chan error, 1)
	go func() { _, _, err := client.call(context.Background(), "second", nil, nil); second <- err }()
	time.Sleep(20 * time.Millisecond)
	close(writer.gate)
	for name, result := range map[string]chan error{"first": first, "second": second} {
		select {
		case err := <-result:
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s call err = %v, want transport failure", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s call stranded after writer stopped", name)
		}
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("transport closure was not reported")
	}
}

func TestInvalidJSONFinalAnswerWarnsWithoutCandidate(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	s.handleNotification("item/completed", notificationJSON("thread-1", "turn-1", map[string]any{"id": "message", "type": "agentMessage", "phase": "final_answer", "text": "Done: {not json"}))
	s.broker.Finish(nil)
	warned := false
	for {
		event, err := stream.Receive(context.Background())
		if err != nil {
			break
		}
		if _, ok := event.Body().(provider.ResultCandidateEvent); ok {
			t.Fatal("invalid JSON became a result candidate")
		}
		if _, err := json.Marshal(event.Body()); err != nil {
			t.Fatalf("event body is not encodable: %v", err)
		}
		if warning, ok := event.Body().(provider.ProtocolWarningEvent); ok && warning.Code == "result_candidate_invalid_json" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("invalid final answer was not reported")
	}
}

func TestCommandOutputIsBounded(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	stream, _ := s.broker.Subscribe()
	output := strings.Repeat("x", maxNativeTextBytes+10)
	s.handleNotification("item/completed", notificationJSON("thread-1", "turn-1", map[string]any{"id": "command", "type": "commandExecution", "aggregatedOutput": output}))
	event := receiveEvent(t, stream)
	completed := event.Body().(provider.CommandCompletedEvent)
	if !completed.Output.Truncated || len(completed.Output.Text) != maxNativeTextBytes || completed.Output.OriginalBytes.Value != uint64(len(output)) {
		t.Fatalf("command output not bounded: truncated=%v len=%d", completed.Output.Truncated, len(completed.Output.Text))
	}
}
