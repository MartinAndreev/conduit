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
	finalizes atomic.Int32
}

// Finalize implements processruntime.Finalizer like production: every call
// registers the release (counted here) and waits for the root's exit,
// bounded by ctx; it never kills.
func (p *heldPipeProcess) Finalize(ctx context.Context) error {
	p.finalizes.Add(1)
	select {
	case <-p.rootDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	if calls := proc.finalizes.Load(); calls != 1 {
		t.Fatalf("loss path finalized %d times, want once after the kill", calls)
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

// lateExitProcess ignores KillTree and exits only when the test says so. Its
// Finalize models production: the first call owns the release, which
// completes once the root exited.
type lateExitProcess struct {
	rpcTestProcess
	rootDone     chan struct{}
	finalizes    atomic.Int32
	finalizeOnce sync.Once
	released     chan struct{}
}

func (p *lateExitProcess) Wait() processruntime.Result { <-p.rootDone; return processruntime.Result{} }
func (p *lateExitProcess) Finalize(ctx context.Context) error {
	p.finalizes.Add(1)
	p.finalizeOnce.Do(func() {
		go func() {
			<-p.rootDone
			close(p.released)
		}()
	})
	select {
	case <-p.released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close whose bounded waits end before the root exited still hands the
// release to Finalize: once the root exits later, it is released although
// Close already returned.
func TestTimedOutCloseStillReleasesRootAfterLateExit(t *testing.T) {
	s := normalizationSession(t)
	s.config.CloseGrace = 20 * time.Millisecond
	proc := &lateExitProcess{rpcTestProcess: rpcTestProcess{stdin: &recordingWriteCloser{}, stdout: io.NopCloser(&emptyReader{}), stderr: io.NopCloser(&emptyReader{})}, rootDone: make(chan struct{}), released: make(chan struct{})}
	s.process = proc
	s.terminateOwnedProcess()
	if s.closeResult.ResourcesReleased {
		t.Fatalf("timed-out close = %#v; want resources unconfirmed", s.closeResult)
	}
	if proc.finalizes.Load() == 0 {
		t.Fatal("timed-out Close did not finalize")
	}
	select {
	case <-proc.released:
		t.Fatal("root released before it exited")
	default:
	}
	close(proc.rootDone)
	select {
	case <-proc.released:
	case <-time.After(2 * time.Second):
		t.Fatal("root that exited after Close was never released")
	}
}

// escapedPipeProcess keeps its pipes after the group kill (a descendant
// outside the group holds them), so Wait stays blocked.
type escapedPipeProcess struct{ *heldPipeProcess }

func (p escapedPipeProcess) KillTree() error { p.kills.Add(1); return nil }

// The loss path registers finalization right after its kill, independently
// of Wait, which stays blocked here.
func TestLossFinalizesWithoutWaitingForPipes(t *testing.T) {
	s := normalizationSession(t)
	held := newHeldPipeProcess()
	proc := escapedPipeProcess{held}
	withRoutedReader(s, proc)
	go s.observeProcessExit()
	deadline := time.Now().Add(2 * time.Second)
	for held.finalizes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("loss path did not register finalization while Wait is blocked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if held.kills.Load() == 0 {
		t.Fatal("finalization registered before the loss kill")
	}
	select {
	case <-s.exitDone:
		t.Fatal("Wait returned although the pipes are held")
	default:
	}
	close(held.pipesDone)
}

// deniedKillProcess: the OS refuses the group kill; once finalization was
// registered and the root exited, KillTree is refused by the finalization
// gate, like production.
type deniedKillProcess struct {
	*heldPipeProcess
	registered atomic.Bool
}

func (p *deniedKillProcess) KillTree() error {
	p.kills.Add(1)
	if p.registered.Load() {
		return processruntime.ErrSignalsFinalized
	}
	return errors.New("kill denied")
}

func (p *deniedKillProcess) Finalize(ctx context.Context) error {
	p.registered.Store(true)
	return p.heldPipeProcess.Finalize(ctx)
}

// The loss kill fails, then finalization refuses Close's kill. That refusal
// is no termination evidence, so Close reports neither quiescence nor
// released resources.
func TestFailedLossKillIsNotConfirmedByFinalization(t *testing.T) {
	s := normalizationSession(t)
	s.config.CloseGrace = 50 * time.Millisecond
	proc := &deniedKillProcess{heldPipeProcess: newHeldPipeProcess()}
	withRoutedReader(s, proc)
	go s.observeProcessExit()
	deadline := time.Now().Add(2 * time.Second)
	for !proc.registered.Load() {
		if time.Now().After(deadline) {
			t.Fatal("loss path did not register finalization")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(proc.pipesDone)
	<-s.exitDone
	s.terminateOwnedProcess()
	if s.closeResult.ResourcesReleased || s.closeResult.ExecutionState != provider.ExecutionUnknown {
		t.Fatalf("close = %#v; want unconfirmed termination", s.closeResult)
	}
	if !s.lossKilled || s.lossKillErr == nil {
		t.Fatalf("loss kill outcome not recorded: killed %v, err %v", s.lossKilled, s.lossKillErr)
	}
}
