package startupinfra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
	"github.com/MartinAndreev/conduit/internal/process"
)

// TestProbeHelper is a fake executable mode, never a real harness invocation.
func TestProbeHelper(t *testing.T) {
	mode := os.Getenv("CONDUIT_PROBE_TEST_MODE")
	if mode == "" {
		return
	}
	if os.Args[len(os.Args)-1] != "--version" {
		os.Exit(20)
	}
	switch mode {
	case "codex":
		fmt.Print("codex-cli 0.159.3\n")
	case "opencode":
		fmt.Print("1.2.3\n")
	case "unknown":
		fmt.Print("unsafe secret output")
	case "failed":
		fmt.Fprint(os.Stderr, "unsafe secret output")
		os.Exit(3)
	case "overflow":
		fmt.Print(strings.Repeat("X", probeCaptureBytes*4))
	case "sleep":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

type helperLauncher struct {
	mode  string
	child process.Process
}

func (l *helperLauncher) Start(s process.Spec) (process.Process, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	s.Path = executable
	s.Args = append([]string{"-test.run=^TestProbeHelper$", "--"}, s.Args...)
	s.Env = []string{"CONDUIT_PROBE_TEST_MODE=" + l.mode}
	l.child, err = (process.ExecLauncher{}).Start(s)
	return l.child, err
}
func TestProbeOutcomesBoundedCapture(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		h       configuration.HarnessID
		kind    configuration.DiscoveryKind
		version string
	}{{"codex", configuration.Codex, configuration.Found, "0.159.3"}, {"opencode", configuration.OpenCode, configuration.Found, "1.2.3"}, {"unknown", configuration.Codex, configuration.Found, ""}, {"failed", configuration.Codex, configuration.ProbeFailed, ""}, {"overflow", configuration.Codex, configuration.ProbeFailed, ""}, {"sleep", configuration.Codex, configuration.ProbeTimeout, ""}} {
		t.Run(tc.mode, func(t *testing.T) {
			launcher := &helperLauncher{mode: tc.mode}
			detector := Detector{Launcher: launcher, LookPath: func(string) (string, error) { return os.Executable() }, Timeout: 2 * time.Second}
			started := time.Now()
			o, err := detector.Discover(context.Background(), tc.h, "")
			if err != nil || o.Kind != tc.kind || o.Version != tc.version {
				t.Fatal(o, err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("probe unbounded")
			}
			if launcher.child != nil {
				_ = launcher.child.Wait()
			}
		})
	}
}
func TestExplicitMissingNoFallback(t *testing.T) {
	calls := []string{}
	detector := Detector{LookPath: func(name string) (string, error) { calls = append(calls, name); return "", os.ErrNotExist }}
	path := filepath.Join(t.TempDir(), "missing")
	o, err := detector.Discover(context.Background(), configuration.Codex, path)
	if err != nil || o.Kind != configuration.Missing || len(calls) != 1 || calls[0] != path {
		t.Fatal(o, calls, err)
	}
}
func TestProbeCancellationReaps(t *testing.T) {
	launcher := &helperLauncher{mode: "sleep"}
	detector := Detector{Launcher: launcher, LookPath: func(string) (string, error) { return os.Executable() }}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	started := time.Now()
	_, err := detector.Discover(ctx, configuration.Codex, "")
	if err != context.Canceled || time.Since(started) > 3*time.Second {
		t.Fatal(err)
	}
	if launcher.child == nil {
		t.Fatal("probe did not start")
	}
	result := launcher.child.Wait()
	if result.ExitCode == 0 {
		t.Fatal("child was not killed")
	}
}
func TestCancelledProbeDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	launcher := &helperLauncher{mode: "sleep"}
	_, err := (Detector{Launcher: launcher}).Discover(ctx, configuration.Codex, "")
	if err != context.Canceled || launcher.child != nil {
		t.Fatal(err)
	}
}
func TestRealFakeExecutableMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable fixture")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "codex")
	script := "#!/bin/sh\n[ \"$1\" = --version ] || exit 22\nprintf 'codex-cli 0.159.3\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	o, err := (Detector{}).Discover(context.Background(), configuration.Codex, path)
	if err != nil || o.Kind != configuration.Found || o.Version != "0.159.3" || o.Path != path {
		t.Fatal(o, err)
	}
}

// cancellationLauncher records an effect that must never occur after lookup cancellation.
type cancellationLauncher struct{ calls int }

func (l *cancellationLauncher) Start(process.Spec) (process.Process, error) {
	l.calls++
	return nil, errors.New("unexpected launch")
}
func TestCancellationDuringLookupDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	launcher := &cancellationLauncher{}
	detector := Detector{Launcher: launcher, LookPath: func(string) (string, error) {
		cancel()
		return os.Executable()
	}}
	_, err := detector.Discover(ctx, configuration.Codex, "")
	if !errors.Is(err, context.Canceled) || launcher.calls != 0 {
		t.Fatalf("cancellation error=%v launches=%d", err, launcher.calls)
	}
}

type cancelOnLaunch struct {
	cancel context.CancelFunc
	cause  error
}

func (l cancelOnLaunch) Start(process.Spec) (process.Process, error) {
	l.cancel()
	return nil, l.cause
}
func TestCancellationDuringFailedLaunchPreservesCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("launch failed")
	detector := Detector{Launcher: cancelOnLaunch{cancel, cause}, LookPath: func(string) (string, error) { return os.Executable() }}
	_, err := detector.Discover(ctx, configuration.Codex, "")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatal(err)
	}
}

type cleanupFailingProcess struct {
	process.Process
	cause error
}

func (p cleanupFailingProcess) KillTree() error { return p.cause }

type cleanupFailingLauncher struct {
	helperLauncher
	cause error
}

func (l *cleanupFailingLauncher) Start(spec process.Spec) (process.Process, error) {
	child, err := l.helperLauncher.Start(spec)
	if err != nil {
		return nil, err
	}
	return cleanupFailingProcess{child, l.cause}, nil
}
func TestExitedProbeCleanupFailurePreservesCause(t *testing.T) {
	for _, mode := range []string{"codex", "failed"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("tree cleanup denied")
			launcher := &cleanupFailingLauncher{helperLauncher: helperLauncher{mode: mode}, cause: cause}
			detector := Detector{Launcher: launcher, LookPath: func(string) (string, error) { return os.Executable() }}
			observation, err := detector.Discover(context.Background(), configuration.Codex, "")
			if observation.Kind != configuration.ProbeFailed || !errors.Is(err, startup.ErrProbeCleanup) || !errors.Is(err, cause) {
				t.Fatal(observation, err)
			}
		})
	}
}

type unreapableProbe struct {
	process.Process
	release chan struct{}
	cause   error
}

func (p *unreapableProbe) Wait() process.Result { <-p.release; return process.Result{ExitCode: 0} }
func (p *unreapableProbe) KillTree() error      { return p.cause }

type oneProbeLauncher struct{ child process.Process }

func (l oneProbeLauncher) Start(process.Spec) (process.Process, error) { return l.child, nil }
func TestCleanupDeadlineReturnsObserverAndEventuallyReaps(t *testing.T) {
	cause := errors.New("kill refused")
	base := &helperLauncher{mode: "codex"}
	child, err := base.Start(process.Spec{Args: []string{"--version"}})
	if err != nil {
		t.Fatal(err)
	}
	held := &unreapableProbe{Process: child, release: make(chan struct{}), cause: cause}
	defer func() { close(held.release); _ = child.KillTree(); _ = child.Wait() }()
	started := time.Now()
	_, err = (Detector{Launcher: oneProbeLauncher{held}, LookPath: func(string) (string, error) { return os.Executable() }, Timeout: 20 * time.Millisecond}).Discover(context.Background(), configuration.Codex, "")
	if time.Since(started) > time.Second || !errors.Is(err, startup.ErrProbeCleanup) || !errors.Is(err, cause) {
		t.Fatal(err)
	}
	var pending startup.PendingCleanupError
	if !errors.As(err, &pending) {
		t.Fatal("missing retained cleanup observer")
	}
	observer := pending.CleanupObserver()
	if observer.Latest().RootReaped {
		t.Fatal("root incorrectly reaped")
	}
	// Release the fake reap independently of the real fixture cleanup above.
	held.release <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := observer.Await(ctx)
	if err != nil || !state.RootReaped || !state.Complete || !errors.Is(state.Err, cause) || !state.StdinClosure.Complete || state.StdinClosure.Err != nil || !state.ReadersDone {
		t.Fatal(state, err)
	}
}

func TestProbeDescriptorsCoverSupportedSchema(t *testing.T) {
	if len(probeDescriptors) != len(configuration.Supported()) {
		t.Fatal("descriptor/schema mismatch")
	}
	for _, id := range configuration.Supported() {
		descriptor, ok := probeDescriptorFor(id)
		if !ok || descriptor.command == "" || len(descriptor.args) != 1 || descriptor.args[0] != "--version" || descriptor.parse == nil {
			t.Fatal(id, descriptor)
		}
	}
	if _, ok := probeDescriptorFor(configuration.HarnessID("unsupported")); ok {
		t.Fatal("unknown schema ID accepted")
	}
}

type delayedStdin struct {
	release chan struct{}
	cause   error
}

func (p delayedStdin) Write(b []byte) (int, error) { return len(b), nil }
func (p delayedStdin) Close() error                { <-p.release; return p.cause }

type delayedStdinProbe struct {
	process.Process
	stdin delayedStdin
}

func (p delayedStdinProbe) Stdin() io.WriteCloser { return p.stdin }
func TestProbeSlowStdinCloseReturnsWithinCleanupBudget(t *testing.T) {
	base := &helperLauncher{mode: "codex"}
	child, err := base.Start(process.Spec{Args: []string{"--version"}})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	cause := errors.New("stdin close failed")
	fixture := delayedStdinProbe{child, delayedStdin{release, cause}}
	started := time.Now()
	_, err = (Detector{Launcher: oneProbeLauncher{fixture}, LookPath: func(string) (string, error) { return os.Executable() }}).Discover(context.Background(), configuration.Codex, "")
	if time.Since(started) > 2*time.Second || !errors.Is(err, startup.ErrProbeCleanup) {
		close(release)
		t.Fatal(err)
	}
	var pending startup.PendingCleanupError
	if !errors.As(err, &pending) {
		close(release)
		t.Fatal("missing observer")
	}
	pendingState := pending.CleanupObserver().Latest()
	if pendingState.StdinClosure.Complete || !pendingState.PipesClosed || !pendingState.ReadersDone {
		close(release)
		t.Fatal(pendingState)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := pending.CleanupObserver().Await(ctx)
	if err != nil || !state.Complete || !errors.Is(state.Err, cause) || !state.StdinClosure.Complete || !errors.Is(state.StdinClosure.Err, cause) || !state.ReadersDone {
		t.Fatal(state, err)
	}
	_ = child.Stdin().Close()
	_ = child.Wait()
}

// stagedCloseReader exposes an immediate stdout failure and a stderr Close
// which remains blocked until the test explicitly releases it.
type stagedCloseReader struct {
	release <-chan struct{}
	cause   error
}

func (r stagedCloseReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r stagedCloseReader) Close() error {
	if r.release != nil {
		<-r.release
	}
	return r.cause
}

type stagedCloseProbe struct {
	process.Process
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (p stagedCloseProbe) Stdin() io.WriteCloser { return delayedStdin{release: closedSignal()} }
func (p stagedCloseProbe) Stdout() io.ReadCloser { return p.stdout }
func (p stagedCloseProbe) Stderr() io.ReadCloser { return p.stderr }
func (p stagedCloseProbe) Wait() process.Result  { return process.Result{ExitCode: 0} }
func (p stagedCloseProbe) KillTree() error       { return nil }
func closedSignal() chan struct{}                { done := make(chan struct{}); close(done); return done }
func TestObservedStdoutCloseFailureSurvivesBlockedStderrClose(t *testing.T) {
	cause := errors.New("stdout close failed")
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	fixture := stagedCloseProbe{stdout: stagedCloseReader{cause: cause}, stderr: stagedCloseReader{release: release}}
	started := time.Now()
	_, err := (Detector{Launcher: oneProbeLauncher{fixture}, LookPath: func(string) (string, error) { return os.Executable() }}).Discover(context.Background(), configuration.Codex, "")
	if time.Since(started) > time.Second || !errors.Is(err, startup.ErrProbeCleanup) || !errors.Is(err, cause) {
		t.Fatal(err)
	}
	var pending startup.PendingCleanupError
	if !errors.As(err, &pending) {
		t.Fatal("missing observer")
	}
	observer := pending.CleanupObserver()
	state := observer.Latest()
	if !errors.Is(state.Err, cause) || !errors.Is(state.PipeCloseErr, cause) || !state.StdoutClosure.Complete || state.StderrClosure.Complete || state.PipesClosed || state.Complete {
		t.Fatal(state)
	}
	releaseOnce.Do(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err = observer.Await(ctx)
	if err != nil || !state.Complete || !state.RootReaped || !state.ReadersDone || !state.StderrClosure.Complete || state.PipesClosed || !errors.Is(state.Err, cause) {
		t.Fatal(state, err)
	}
}
