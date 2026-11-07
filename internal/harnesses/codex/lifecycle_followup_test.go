package codex

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// killCountingProcess reaps its root at once (Wait returns) while counting
// group-kill requests, like an App Server whose descendants outlive it.
type killCountingProcess struct {
	rpcTestProcess
	kills atomic.Int32
}

func (p *killCountingProcess) Wait() processruntime.Result { return processruntime.Result{} }
func (p *killCountingProcess) KillTree() error             { p.kills.Add(1); return nil }

// withRoutedReader gives a normalization session a JSON-RPC client whose
// reader has already routed its last line, as after a group kill.
func withRoutedReader(s *session, proc processruntime.Process) {
	s.process = proc
	s.rpc = newRPCClient(proc)
	close(s.rpc.routed)
}

func lossEvents(t *testing.T, s *session) []provider.SessionStateEvent {
	t.Helper()
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	var lost []provider.SessionStateEvent
	for {
		event, err := stream.Receive(context.Background())
		if err != nil {
			var providerErr *provider.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost {
				t.Fatalf("stream terminal = %v", err)
			}
			return lost
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && body.Current == provider.SessionLost {
			lost = append(lost, body)
		}
	}
}

func TestUnexpectedStopKillsProcessGroupOnceWhicheverPathWins(t *testing.T) {
	for _, exitFirst := range []bool{true, false} {
		name := "transport first"
		if exitFirst {
			name = "exit first"
		}
		t.Run(name, func(t *testing.T) {
			s := normalizationSession(t)
			proc := &killCountingProcess{rpcTestProcess: rpcTestProcess{stdin: &recordingWriteCloser{}, stdout: io.NopCloser(&emptyReader{}), stderr: io.NopCloser(&emptyReader{})}}
			withRoutedReader(s, proc)
			if exitFirst {
				s.observeProcessExit()
				s.handleTransportClosed(io.EOF)
			} else {
				s.handleTransportClosed(io.EOF)
				s.observeProcessExit()
			}
			lost := lossEvents(t, s)
			want := "app_server_transport_closed"
			if exitFirst {
				want = "app_server_process_exit"
			}
			if len(lost) != 1 || lost[0].NativeStatus != want {
				t.Fatalf("loss publications = %#v, want one from %s", lost, want)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("KillTree calls = %d, want 1", kills)
			}
		})
	}
}

func TestUnexpectedExitWhileClosingLeavesCleanupToClose(t *testing.T) {
	s := normalizationSession(t)
	proc := &killCountingProcess{rpcTestProcess: rpcTestProcess{stdin: &recordingWriteCloser{}, stdout: io.NopCloser(&emptyReader{}), stderr: io.NopCloser(&emptyReader{})}}
	withRoutedReader(s, proc)
	s.closing = true
	s.observeProcessExit()
	if kills := proc.kills.Load(); kills != 0 {
		t.Fatalf("exit observer killed during close: %d", kills)
	}
}

func TestStartTurnRejectsReleaseWhenNothingUnresolved(t *testing.T) {
	s := normalizationSession(t)
	contract := s.contract.Contract
	_, err := s.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "turn-1", Session: s.ref, Assignment: s.assignment,
		Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "work"}}, ResultContract: contract,
		ReleaseUnresolvedSteer: provider.Some(provider.MessageID("steer-message")),
	})
	assertProviderCode(t, err, provider.ErrorStaleBinding)
	if s.turnPending {
		t.Fatal("rejected release reserved a turn")
	}
	s.steerPending = &pendingSteer{request: provider.SteerRequest{MessageID: "steer-message"}}
	_, err = s.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "turn-2", Session: s.ref, Assignment: s.assignment,
		Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "work"}}, ResultContract: contract,
		ReleaseUnresolvedSteer: provider.Some(provider.MessageID("other-message")),
	})
	assertProviderCode(t, err, provider.ErrorStaleBinding)
	if s.turnPending || s.steerPending == nil {
		t.Fatal("mismatched release changed state")
	}
}

// heldPipeProcess models production process.Wait: the root has exited
// (RootExited is closed) while a descendant holds the pipes, so Wait blocks
// until KillTree ends the group.
type heldPipeProcess struct {
	rpcTestProcess
	rootDone  chan struct{}
	pipesDone chan struct{}
	kills     atomic.Int32
}

func newHeldPipeProcess() *heldPipeProcess {
	root := make(chan struct{})
	close(root)
	return &heldPipeProcess{
		rpcTestProcess: rpcTestProcess{stdin: &recordingWriteCloser{}, stdout: io.NopCloser(&emptyReader{}), stderr: io.NopCloser(&emptyReader{})},
		rootDone:       root, pipesDone: make(chan struct{}),
	}
}

func (p *heldPipeProcess) RootExited() <-chan struct{} { return p.rootDone }
func (p *heldPipeProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: 1}
}
func (p *heldPipeProcess) Wait() processruntime.Result {
	<-p.pipesDone
	return processruntime.Result{ExitCode: 1}
}
func (p *heldPipeProcess) KillTree() error {
	if p.kills.Add(1) == 1 {
		close(p.pipesDone)
	}
	return nil
}

func TestRootExitWithHeldPipesKillsGroupAndCompletesWait(t *testing.T) {
	s := normalizationSession(t)
	proc := newHeldPipeProcess()
	withRoutedReader(s, proc)
	done := make(chan struct{})
	go func() { s.observeProcessExit(); close(done) }()
	select {
	case <-s.exitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("root exit with held pipes never completed Wait")
	}
	<-done
	if kills := proc.kills.Load(); kills != 1 {
		t.Fatalf("KillTree calls = %d, want 1", kills)
	}
	if lost := lossEvents(t, s); len(lost) != 1 || lost[0].NativeStatus != "app_server_process_exit" {
		t.Fatalf("loss publications = %#v", lost)
	}
}

// bufferedExitProcess serves stdout from a pipe; its root has exited and
// KillTree ends the pipe, so lines written before the exit are still read.
type bufferedExitProcess struct {
	*heldPipeProcess
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	closeOnce    sync.Once
}

func (p *bufferedExitProcess) Stdout() io.ReadCloser { return p.stdoutReader }
func (p *bufferedExitProcess) KillTree() error {
	p.closeOnce.Do(func() { _ = p.stdoutWriter.Close() })
	return p.heldPipeProcess.KillTree()
}

// The App Server writes turn/completed and exits before the reader handled
// it: the terminal turn state is published, and the loss follows it.
func TestRootExitPublishesBufferedFinalEventsBeforeLoss(t *testing.T) {
	for run := 0; run < 20; run++ {
		s := normalizationSession(t)
		reader, writer := io.Pipe()
		proc := &bufferedExitProcess{heldPipeProcess: newHeldPipeProcess(), stdoutReader: reader, stdoutWriter: writer}
		proc.rootDone = make(chan struct{})
		s.process = proc
		s.rpc = newRPCClient(proc)
		s.rpc.onClosed = s.handleTransportClosed
		s.rpc.start(s.handleNotification, s.handleServerRequest)
		beginTurn(t, s, "turn-1")
		stream, err := s.broker.Subscribe()
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { s.observeProcessExit(); close(done) }()
		go func() {
			_, _ = writer.Write(nativeNotification("turn/completed", map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}}))
			close(proc.rootDone)
		}()
		var events []provider.ProviderEvent
		for {
			event, err := stream.Receive(context.Background())
			if err != nil {
				var providerErr *provider.ProviderError
				if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost {
					t.Fatalf("run %d: stream terminal = %v", run, err)
				}
				break
			}
			events = append(events, event)
		}
		<-done
		terminal := slices.IndexFunc(events, func(event provider.ProviderEvent) bool {
			body, ok := event.Body().(provider.TurnStateEvent)
			return ok && body.Current == provider.TurnNativeTerminal
		})
		lost := slices.IndexFunc(events, func(event provider.ProviderEvent) bool {
			body, ok := event.Body().(provider.SessionStateEvent)
			return ok && body.Current == provider.SessionLost && body.NativeStatus == "app_server_process_exit"
		})
		if terminal < 0 || lost < terminal {
			t.Fatalf("run %d: terminal at %d, loss at %d", run, terminal, lost)
		}
	}
}
