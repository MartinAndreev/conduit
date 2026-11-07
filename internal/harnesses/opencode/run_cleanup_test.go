package opencode

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// runEndingSession starts one turn whose owned run client ends as launcher
// describes and waits for the turn's final session state.
func runEndingSession(t *testing.T, launcher *fakeLauncher, config Config) (*fakeProcess, provider.SessionState) {
	t.Helper()
	config.Control, config.Launcher = &fakeControl{}, launcher
	adapter := New(config)
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
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: assignment, ResultContract: contract, Input: []provider.InputPart{{Text: "work"}}}); err != nil {
		t.Fatal(err)
	}
	proc := launcher.last
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("stream ended before the run ended: %v", err)
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && (body.Current == provider.SessionLost || body.Current == provider.SessionIdleObserved) {
			return proc, body.Current
		}
	}
}

func TestFailedRunKillsOwnedGroupOnce(t *testing.T) {
	cases := map[string]struct {
		launcher *fakeLauncher
		config   Config
	}{
		"nonzero exit":   {&fakeLauncher{output: "", result: processruntime.Result{ExitCode: 1}, delay: time.Millisecond}, Config{}},
		"oversized line": {&fakeLauncher{output: strings.Repeat("a", 4096) + "\n", delay: time.Millisecond}, Config{MaxEventLineBytes: 1024}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			proc, state := runEndingSession(t, tc.launcher, tc.config)
			if state != provider.SessionLost {
				t.Fatalf("state = %s, want lost", state)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("KillTree calls = %d, want 1", kills)
			}
		})
	}
}

func TestCleanRunExitDoesNotKill(t *testing.T) {
	proc, state := runEndingSession(t, &fakeLauncher{output: "", delay: time.Millisecond}, Config{})
	if state != provider.SessionIdleObserved || proc.kills.Load() != 0 {
		t.Fatalf("state = %s kills = %d", state, proc.kills.Load())
	}
}

// heldRunLauncher starts a run client whose root has already exited with
// rootExit while a descendant keeps stderr (and, unless stdoutEnds, stdout)
// open until KillTree. Wait returns only once both pipes ended, as with
// production process.Wait.
type heldRunLauncher struct {
	rootExit   int
	stdoutEnds bool
	last       *heldRunProcess
}

type heldRunProcess struct {
	stdout, stderr       *io.PipeReader
	stdoutPipe, errPipe  *io.PipeWriter
	rootDone, pipesEnded chan struct{}
	rootExit             int
	kills                atomic.Int32
	closeOnce            sync.Once
}

func (l *heldRunLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	stdout, stdoutPipe := io.Pipe()
	stderr, errPipe := io.Pipe()
	root := make(chan struct{})
	close(root)
	l.last = &heldRunProcess{stdout: stdout, stdoutPipe: stdoutPipe, stderr: stderr, errPipe: errPipe, rootDone: root, pipesEnded: make(chan struct{}), rootExit: l.rootExit}
	if l.stdoutEnds {
		_ = stdoutPipe.Close()
	}
	return l.last, nil
}

func (*heldRunProcess) PID() int                { return 1 }
func (*heldRunProcess) Stdin() io.WriteCloser   { return nopWriteCloser{Writer: io.Discard} }
func (p *heldRunProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *heldRunProcess) Stderr() io.ReadCloser { return p.stderr }
func (*heldRunProcess) Interrupt() error        { return nil }
func (*heldRunProcess) TreeKillSupported() bool { return true }
func (p *heldRunProcess) RootExited() <-chan struct{} {
	return p.rootDone
}
func (p *heldRunProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: p.rootExit}
}

// releasePipes ends the descendant: both pipes reach EOF.
func (p *heldRunProcess) releasePipes() {
	p.closeOnce.Do(func() {
		_ = p.stdoutPipe.Close()
		_ = p.errPipe.Close()
		close(p.pipesEnded)
	})
}
func (p *heldRunProcess) KillTree() error {
	p.kills.Add(1)
	p.releasePipes()
	return nil
}
func (p *heldRunProcess) Wait() processruntime.Result {
	<-p.pipesEnded
	return processruntime.Result{ExitCode: p.rootExit}
}

func startHeldRun(t *testing.T, launcher *heldRunLauncher) (provider.Session, provider.EventStream) {
	t.Helper()
	adapter := New(Config{Control: &fakeControl{}, Launcher: launcher})
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
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: assignment, ResultContract: contract, Input: []provider.InputPart{{Text: "work"}}}); err != nil {
		t.Fatal(err)
	}
	return opened.Session, stream
}

func receiveRunEnd(t *testing.T, stream provider.EventStream, want provider.SessionState) {
	t.Helper()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("run never reached %s: %v", want, err)
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && (body.Current == provider.SessionLost || body.Current == provider.SessionIdleObserved) {
			if body.Current != want {
				t.Fatalf("run ended %s, want %s", body.Current, want)
			}
			return
		}
	}
}

func TestFailedRootExitWithHeldStdoutKillsGroup(t *testing.T) {
	launcher := &heldRunLauncher{rootExit: 1}
	_, stream := startHeldRun(t, launcher)
	receiveRunEnd(t, stream, provider.SessionLost)
	if kills := launcher.last.kills.Load(); kills != 1 {
		t.Fatalf("KillTree calls = %d, want 1", kills)
	}
}

// stdout ends first while a descendant keeps only stderr: the failed root
// exit still kills the group, Wait completes and the turn ends.
func TestFailedRootExitWithOnlyStderrHeldKillsGroup(t *testing.T) {
	launcher := &heldRunLauncher{rootExit: 1, stdoutEnds: true}
	s, stream := startHeldRun(t, launcher)
	receiveRunEnd(t, stream, provider.SessionLost)
	if kills := launcher.last.kills.Load(); kills != 1 {
		t.Fatalf("KillTree calls = %d, want 1", kills)
	}
	if snapshot, err := s.ObserveState(context.Background(), provider.ObserveStateRequest{Session: s.Ref()}); err != nil || snapshot.SessionState != provider.SessionLost {
		t.Fatalf("snapshot = %#v err = %v", snapshot, err)
	}
}

// A clean root exit kills nothing (the group may hold a process meant to
// outlive the client); the observation ends when the pipes end. readRun
// joins the root-exit watch before its final events, so the kill count is
// final once the idle event arrives.
func TestCleanRootExitWithHeldStdoutDoesNotKill(t *testing.T) {
	launcher := &heldRunLauncher{rootExit: 0}
	_, stream := startHeldRun(t, launcher)
	launcher.last.releasePipes()
	receiveRunEnd(t, stream, provider.SessionIdleObserved)
	if kills := launcher.last.kills.Load(); kills != 0 {
		t.Fatalf("clean root exit killed the group %d times", kills)
	}
}
