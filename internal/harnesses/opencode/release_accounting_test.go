package opencode

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// cliPromptControl answers the steer prompt through a real cliControl, so
// the command's release runs through the session; everything else is the
// fake control.
type cliPromptControl struct {
	*fakeControl
	cli *cliControl
}

func (c cliPromptControl) Prompt(ctx context.Context, request PromptRequest) (PromptResult, bool, error) {
	return c.cli.Prompt(ctx, request)
}

// awaitTurnEnded waits until the session reports no active turn.
func awaitTurnEnded(t *testing.T, s provider.Session) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := s.ObserveState(context.Background(), provider.ObserveStateRequest{Session: s.Ref()})
		if err != nil {
			t.Fatal(err)
		}
		if !snapshot.ActiveTurn.Present {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("turn did not end")
}

func closeForAccounting(t *testing.T, s provider.Session) provider.CloseSessionResult {
	t.Helper()
	result, err := s.Close(context.Background(), provider.CloseSessionRequest{Session: s.Ref(), Policy: provider.CloseInterruptThenClose})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func steerOnce(ctx context.Context, fixture reconcileFixture) error {
	_, err := fixture.session.Steer(ctx, provider.SteerRequest{Session: fixture.session.Ref(), ExpectedActiveTurn: fixture.started.Turn, CurrentAssignment: fixture.assigned, ExpectedRevision: fixture.assigned.Revision, NewAssignment: nextRevision(fixture.assigned), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	return err
}

// A run client that completed but whose release failed is not forgotten
// when its turn ends: Close reports resources not released, while the
// confirmed server stop still ends execution.
func TestRunReleaseFailureKeepsResourcesUnreleased(t *testing.T) {
	launcher := &fakeLauncher{delay: time.Millisecond, finalizeErr: errors.New("reap failed")}
	session := openActiveSession(t, launcher)
	awaitTurnEnded(t, session)
	if result := closeForAccounting(t, session); result.ExecutionState != provider.ExecutionNone || result.ResourcesReleased {
		t.Fatalf("close after a failed run release = %#v", result)
	}
	if again := closeForAccounting(t, session); again.ResourcesReleased {
		t.Fatalf("a failed run release was later reported released: %#v", again)
	}
}

// A finished control command whose release was not confirmed within the
// caller's context stays tracked: Close reports resources not released
// while it is pending, and released once it confirms within Close's wait.
func TestUnconfirmedControlReleaseIsAccountedByClose(t *testing.T) {
	block := make(chan struct{})
	commands := &fakeLauncher{output: `{"data":{"id":"receipt"}}`, delay: time.Millisecond, finalizeBlock: block}
	control := cliPromptControl{fakeControl: &fakeControl{}, cli: &cliControl{executable: "opencode", launcher: commands}}
	fixture := newReconcileFixture(t, control, 300*time.Millisecond, func(s *session) {
		s.config.StopGrace = 300 * time.Millisecond
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := steerOnce(ctx, fixture)
	cancel()
	if !errors.Is(err, errControlCleanup) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("steer = %v; want an unconfirmed release", err)
	}
	awaitTurnEnded(t, fixture.session)
	if first := closeForAccounting(t, fixture.session); first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
		t.Fatalf("close with a pending control release = %#v", first)
	}
	time.AfterFunc(50*time.Millisecond, func() { close(block) })
	if again := closeForAccounting(t, fixture.session); !again.ResourcesReleased || again.ExecutionState != provider.ExecutionNone {
		t.Fatalf("close after the control release confirmed = %#v", again)
	}
}

// A cancelled control command whose kill succeeded but whose release failed
// returns the release failure, and Close reports resources not released.
func TestCancelledControlReleaseFailureIsReported(t *testing.T) {
	reapErr := errors.New("reap failed")
	proc := &heldPipeControlProcess{stdin: newCloseRecorder(), stdout: newCloseRecorder(), stderr: newCloseRecorder(), finalizeErr: reapErr}
	control := cliPromptControl{fakeControl: &fakeControl{}, cli: &cliControl{executable: "opencode", launcher: heldPipeControlLauncher{proc}}}
	fixture := newReconcileFixture(t, control, 200*time.Millisecond, func(s *session) {
		s.config.StopGrace = 300 * time.Millisecond
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := steerOnce(ctx, fixture)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errControlCleanup) || !errors.Is(err, reapErr) {
		t.Fatalf("steer = %v; want the cancellation and the release failure", err)
	}
	if kills := proc.kills.Load(); kills != 1 {
		t.Fatalf("kills = %d, want one", kills)
	}
	awaitTurnEnded(t, fixture.session)
	if result := closeForAccounting(t, fixture.session); result.ExecutionState != provider.ExecutionNone || result.ResourcesReleased {
		t.Fatalf("close after a failed control release = %#v", result)
	}
}

// A tracked cleanup owner that completed without a confirmed release (its
// reap failed) is not dropped as finished: Close keeps reporting resources
// not released.
func TestCompletedOwnerWithFailedReleaseStaysTracked(t *testing.T) {
	proc := &heldPipeControlProcess{stdin: newCloseRecorder(), stdout: newCloseRecorder(), stderr: newCloseRecorder(), finalizeErr: errors.New("reap failed")}
	owner := processruntime.NewCleanupOwner(proc)
	owner.BeginCleanup()
	select {
	case <-owner.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not complete")
	}
	opened := openActiveSession(t, &fakeLauncher{delay: time.Millisecond})
	awaitTurnEnded(t, opened)
	opened.(*session).trackRelease(ownerRelease{owner: owner})
	for range 2 {
		if result := closeForAccounting(t, opened); result.ExecutionState != provider.ExecutionNone || result.ResourcesReleased {
			t.Fatalf("close with a failed tracked release = %#v", result)
		}
	}
}

// scriptedRunProcess is a run client whose stdin close can fail and whose
// group signals are refused once finalization was requested, as
// internal/process does after the root exited.
type scriptedRunProcess struct {
	*fakeProcess
	stdinErr  error
	finalized atomic.Bool
}

type failingCloser struct{ err error }

func (failingCloser) Write(b []byte) (int, error) { return len(b), nil }
func (c failingCloser) Close() error              { return c.err }

func (p *scriptedRunProcess) Stdin() io.WriteCloser { return failingCloser{err: p.stdinErr} }
func (p *scriptedRunProcess) KillTree() error {
	if p.finalized.Load() {
		return processruntime.ErrSignalsFinalized
	}
	return p.fakeProcess.KillTree()
}
func (p *scriptedRunProcess) Finalize(ctx context.Context) error {
	p.finalized.Store(true)
	return p.fakeProcess.Finalize(ctx)
}

type scriptedRunLauncher struct{ proc *scriptedRunProcess }

func (l scriptedRunLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	return l.proc, nil
}

func newScriptedRun(delay time.Duration) *scriptedRunProcess {
	return &scriptedRunProcess{fakeProcess: &fakeProcess{stdout: io.NopCloser(strings.NewReader("")), stderr: io.NopCloser(strings.NewReader("")), delay: delay}}
}

// A run client whose stdin close failed is killed, finalized right after
// the kill, and its failed release stays tracked: Close reports resources
// not released.
func TestStdinCloseFailureReleaseIsAccountedByClose(t *testing.T) {
	proc := newScriptedRun(time.Millisecond)
	proc.stdinErr = errors.New("stdin close failed")
	proc.finalizeErr = errors.New("reap failed")
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: scriptedRunLauncher{proc}, StopGrace: time.Second}))
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}, ResultContract: c, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c}); err == nil {
		t.Fatal("StartTurn succeeded with a failed stdin close")
	}
	if kills, finalizes := proc.kills.Load(), proc.finalizes.Load(); kills != 1 || finalizes == 0 {
		t.Fatalf("kills = %d, Finalize calls = %d", kills, finalizes)
	}
	if result := closeForAccounting(t, opened.Session); result.ExecutionState != provider.ExecutionNone || result.ResourcesReleased {
		t.Fatalf("close after a failed stdin-failure release = %#v", result)
	}
}

// Close's kill is sent once: when the first Close timed out waiting for the
// release, a second Close re-awaits the same release without a new kill
// (finalization would refuse it) and reports its outcome.
func TestRepeatedCloseReawaitsClientRelease(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reapErr  error
		released bool
	}{
		{"release confirms later", nil, true},
		{"release fails later", errors.New("reap failed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc := newScriptedRun(300 * time.Millisecond)
			block := make(chan struct{})
			proc.finalizeBlock = block
			session := openActiveSession(t, scriptedRunLauncher{proc})
			if first := closeForAccounting(t, session); first.ResourcesReleased || first.ExecutionState != provider.ExecutionNone {
				t.Fatalf("close with a pending client release = %#v", first)
			}
			// Written before block closes; Finalize reads it after.
			proc.finalizeErr = tc.reapErr
			close(block)
			if again := closeForAccounting(t, session); again.ResourcesReleased != tc.released || again.ExecutionState != provider.ExecutionNone {
				t.Fatalf("repeated close = %#v, want released %v", again, tc.released)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("kills = %d, want Close's single kill", kills)
			}
		})
	}
}

// Wait's own release error (for example a Windows Job Object close) fails a
// release whose finalization succeeded: for a finished control command, a
// run client that ended on its own, and a run client Close stopped.
func TestWaitReleaseErrorBlocksRelease(t *testing.T) {
	jobErr := errors.New("job object close failed")
	t.Run("control command", func(t *testing.T) {
		launcher := &fakeLauncher{output: "{}", delay: time.Millisecond, result: processruntime.Result{ReleaseErr: jobErr}}
		control := &cliControl{executable: "opencode", launcher: launcher}
		_, sent, err := control.run(context.Background(), testEndpoint, "/work", "session", "list")
		var unconfirmed *releasePendingError
		if !sent || !errors.Is(err, errControlCleanup) || !errors.Is(err, jobErr) || !errors.As(err, &unconfirmed) {
			t.Fatalf("run = sent %v, %v; want the release failure", sent, err)
		}
	})
	t.Run("finished run client", func(t *testing.T) {
		session := openActiveSession(t, &fakeLauncher{delay: time.Millisecond, result: processruntime.Result{ReleaseErr: jobErr}})
		awaitTurnEnded(t, session)
		if result := closeForAccounting(t, session); result.ResourcesReleased || result.ExecutionState != provider.ExecutionNone {
			t.Fatalf("close = %#v", result)
		}
	})
	t.Run("stopped run client", func(t *testing.T) {
		session := openActiveSession(t, &fakeLauncher{delay: 200 * time.Millisecond, result: processruntime.Result{ReleaseErr: jobErr}})
		if result := closeForAccounting(t, session); result.ResourcesReleased || result.ExecutionState != provider.ExecutionNone {
			t.Fatalf("close = %#v", result)
		}
	})
}
