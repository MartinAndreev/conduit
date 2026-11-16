package opencode

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// gatedRunProcess is a run client that ignores KillTree and exits cleanly
// (code 0) only once released, so its end is processed after the server
// loss was recorded.
type gatedRunProcess struct {
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	release chan struct{}
	kills   atomic.Int32
}

func newGatedRunProcess() *gatedRunProcess {
	reader, writer := io.Pipe()
	return &gatedRunProcess{stdoutR: reader, stdoutW: writer, release: make(chan struct{})}
}
func (*gatedRunProcess) PID() int                { return 21 }
func (*gatedRunProcess) Stdin() io.WriteCloser   { return nopWriteCloser{Writer: io.Discard} }
func (p *gatedRunProcess) Stdout() io.ReadCloser { return p.stdoutR }
func (*gatedRunProcess) Stderr() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (*gatedRunProcess) Interrupt() error        { return nil }
func (*gatedRunProcess) TreeKillSupported() bool { return true }
func (p *gatedRunProcess) KillTree() error       { p.kills.Add(1); return nil }
func (p *gatedRunProcess) Wait() processruntime.Result {
	<-p.release
	return processruntime.Result{ExitCode: 0}
}
func (p *gatedRunProcess) end() {
	_ = p.stdoutW.Close()
	close(p.release)
}

type singleRunLauncher struct{ proc processruntime.Process }

func (l singleRunLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	return l.proc, nil
}

// A run client that exits cleanly after its server died must not revive the
// lost session.
func TestCleanRunEndAfterServerLossKeepsSessionLost(t *testing.T) {
	run := newGatedRunProcess()
	servers := &fakeServerLauncher{}
	control := &fakeControl{}
	adapter := New(withFakeServer(Config{Control: control, Launcher: singleRunLauncher{proc: run}, serverLauncher: servers, StopGrace: 200 * time.Millisecond}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	session := opened.Session
	stream, _ := session.Events(context.Background(), provider.EventCursor{})
	request := openRequest()
	started, err := session.StartTurn(context.Background(), provider.StartTurnRequest{Session: session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract})
	if err != nil {
		t.Fatal(err)
	}
	server, _ := servers.last()
	server.exit()
	waitForState(t, session, provider.SessionLost)
	run.end()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatal("the run client's end was not processed")
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && strings.Contains(body.NativeStatus, "CLI client completed") {
			if body.Current != provider.SessionLost {
				t.Fatalf("run end published %s after the server loss", body.Current)
			}
			break
		}
	}
	waitForState(t, session, provider.SessionLost)
	_, err = session.StartTurn(context.Background(), provider.StartTurnRequest{Session: session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract})
	if err == nil {
		t.Fatal("turn started on a lost session")
	}
	_, err = session.Steer(context.Background(), provider.SteerRequest{Session: session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: request.Assignment, ExpectedRevision: request.Assignment.Revision, NewAssignment: nextRevision(request.Assignment), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost || control.promptCalls != 0 {
		t.Fatalf("steer on a lost session = %v", err)
	}
	snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
	if snapshot.SessionState != provider.SessionLost {
		t.Fatalf("state = %s", snapshot.SessionState)
	}
}

func waitForState(t *testing.T, session provider.Session, want provider.SessionState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
		if snapshot.SessionState == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("state = %s, want %s", snapshot.SessionState, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The server dies between the run end's state transition and the publication
// of its event: the published order must still match the state order, so the
// last session state event is the loss, never a stale idle.
func TestServerLossBetweenRunTransitionAndPublication(t *testing.T) {
	run := newGatedRunProcess()
	servers := &fakeServerLauncher{}
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: singleRunLauncher{proc: run}, serverLauncher: servers}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	sess := opened.Session
	defer sess.Close(context.Background(), provider.CloseSessionRequest{Session: sess.Ref(), Policy: provider.CloseInterruptThenClose})
	stream, _ := sess.Events(context.Background(), provider.EventCursor{})
	request := openRequest()
	if _, err := sess.StartTurn(context.Background(), provider.StartTurnRequest{Session: sess.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract}); err != nil {
		t.Fatal(err)
	}
	server, _ := servers.last()
	var once sync.Once
	sess.(*session).transitioned = func() {
		once.Do(func() {
			server.exit()
			// Give the server watcher every chance to publish in between.
			time.Sleep(100 * time.Millisecond)
		})
	}
	run.end()
	var states []provider.SessionState
	for len(states) == 0 || states[len(states)-1] != provider.SessionLost {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("states = %v", states)
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok {
			states = append(states, body.Current)
		}
	}
	waitForState(t, sess, provider.SessionLost)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			break
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok {
			t.Fatalf("state event %s published after the loss (states %v)", body.Current, states)
		}
	}
	if want := []provider.SessionState{provider.SessionOpen, provider.SessionIdleObserved, provider.SessionLost}; !slices.Equal(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}

// A server that dies during OpenSession is reported lost only after the
// session was reported open.
func TestSessionOpenPrecedesServerLoss(t *testing.T) {
	servers := &fakeServerLauncher{}
	control := &fakeControl{}
	control.onInfo = func() {
		server, _ := servers.last()
		server.exit()
		// Let the root exit be observable before OpenSession continues.
		time.Sleep(20 * time.Millisecond)
	}
	adapter := New(withFakeServer(Config{Control: control, Launcher: &fakeLauncher{}, serverLauncher: servers}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, opened.Session, provider.SessionLost)
	stream, _ := opened.Session.Events(context.Background(), provider.EventCursor{})
	var states []provider.SessionState
	for len(states) < 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("states = %v", states)
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok {
			states = append(states, body.Current)
		}
	}
	if !slices.Equal(states, []provider.SessionState{provider.SessionOpen, provider.SessionLost}) {
		t.Fatalf("states = %v", states)
	}
}

// Steer is refused while Close stops the session and after a Close whose run
// client stop stayed unconfirmed, although the turn is still recorded.
func TestSteerRefusedDuringAndAfterClose(t *testing.T) {
	run := newGatedRunProcess()
	defer run.end()
	control := &fakeControl{}
	adapter := New(withFakeServer(Config{Control: control, Launcher: singleRunLauncher{proc: run}, StopGrace: 300 * time.Millisecond}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	sess := opened.Session
	request := openRequest()
	started, err := sess.StartTurn(context.Background(), provider.StartTurnRequest{Session: sess.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract})
	if err != nil {
		t.Fatal(err)
	}
	steer := func() error {
		_, err := sess.Steer(context.Background(), provider.SteerRequest{Session: sess.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: request.Assignment, ExpectedRevision: request.Assignment.Revision, NewAssignment: nextRevision(request.Assignment), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
		return err
	}
	closed := make(chan provider.CloseSessionResult, 1)
	go func() {
		result, _ := sess.Close(context.Background(), provider.CloseSessionRequest{Session: sess.Ref(), Policy: provider.CloseInterruptThenClose})
		closed <- result
	}()
	waitForState(t, sess, provider.SessionClosed)
	assertClosedRefusal := func(err error) {
		t.Helper()
		var providerErr *provider.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorStaleBinding || providerErr.RemoteEffect != provider.RemoteEffectNotSent || !strings.Contains(providerErr.Detail, "closed") {
			t.Fatalf("steer = %v", err)
		}
	}
	assertClosedRefusal(steer())
	result := <-closed
	if result.ResourcesReleased {
		t.Fatal("the gated run client was reported released")
	}
	assertClosedRefusal(steer())
	if control.promptCalls != 0 {
		t.Fatal("a steer was sent after Close")
	}
}
