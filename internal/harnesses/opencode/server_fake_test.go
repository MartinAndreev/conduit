package opencode

import (
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

const (
	fakeServerPID      = 11
	testServerURL      = "http://127.0.0.1:4096"
	testServerPassword = "test-server-password"
	testListeningLine  = "server listening on " + testServerURL + "\n"
)

// testEndpoint is a ready session server as OpenSession records it.
var testEndpoint = newServerEndpoint(testServerURL, testServerPassword)

// fakeServerProcess is an owned `opencode serve`: it writes its stdout script
// and then runs until KillTree or exit. Its root exit is observable without
// Wait, like production processes.
type fakeServerProcess struct {
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	exited  chan struct{}
	once    sync.Once
	kills   atomic.Int32
	// killErr makes KillTree fail without terminating the process.
	killErr error
	// noTreeKill reports that only the root can be terminated.
	noTreeKill bool
	// keepStdout keeps stdout open after the root exited, as a descendant
	// holding the pipe would.
	keepStdout bool
}

func newFakeServerProcess(script string) *fakeServerProcess {
	reader, writer := io.Pipe()
	p := &fakeServerProcess{stdoutR: reader, stdoutW: writer, exited: make(chan struct{})}
	go func() { _, _ = io.WriteString(writer, script) }()
	return p
}

// exit ends the root on its own, as a crashed server would.
func (p *fakeServerProcess) exit() {
	p.once.Do(func() {
		close(p.exited)
		if !p.keepStdout {
			_ = p.stdoutW.Close()
		}
	})
}

func (*fakeServerProcess) PID() int                { return fakeServerPID }
func (*fakeServerProcess) Stdin() io.WriteCloser   { return nopWriteCloser{Writer: io.Discard} }
func (p *fakeServerProcess) Stdout() io.ReadCloser { return p.stdoutR }
func (*fakeServerProcess) Stderr() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (*fakeServerProcess) Interrupt() error        { return nil }
func (p *fakeServerProcess) TreeKillSupported() bool {
	return !p.noTreeKill
}
func (p *fakeServerProcess) KillTree() error {
	p.kills.Add(1)
	if p.killErr != nil {
		return p.killErr
	}
	p.exit()
	_ = p.stdoutW.Close()
	return nil
}
func (p *fakeServerProcess) Wait() processruntime.Result {
	<-p.exited
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}
func (p *fakeServerProcess) RootExited() <-chan struct{} { return p.exited }
func (p *fakeServerProcess) RootResult() processruntime.Result {
	<-p.exited
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}

// fakeServerLauncher starts fake servers that print script (by default the
// listening line) and records every server spec.
type fakeServerLauncher struct {
	mu        sync.Mutex
	script    string
	startErr  error
	configure func(*fakeServerProcess)
	specs     []processruntime.Spec
	servers   []*fakeServerProcess
}

func (l *fakeServerLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	var proc *fakeServerProcess
	err := lockscope.WithMutexLock(&l.mu, func() error {
		l.specs = append(l.specs, spec)
		if l.startErr != nil {
			return l.startErr
		}
		script := l.script
		if script == "" {
			script = testListeningLine
		}
		proc = newFakeServerProcess(script)
		if l.configure != nil {
			l.configure(proc)
		}
		l.servers = append(l.servers, proc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return proc, nil
}

func (l *fakeServerLauncher) last() (*fakeServerProcess, processruntime.Spec) {
	var proc *fakeServerProcess
	var spec processruntime.Spec
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		if len(l.servers) > 0 {
			proc = l.servers[len(l.servers)-1]
		}
		if len(l.specs) > 0 {
			spec = l.specs[len(l.specs)-1]
		}
		return nil
	})
	return proc, spec
}

// withFakeServer completes a test config with a fake server launcher, a
// Linux host (no sandbox) and a fixed password, unless the test set them.
func withFakeServer(config Config) Config {
	if config.serverLauncher == nil {
		config.serverLauncher = &fakeServerLauncher{}
	}
	if config.host.homeDir == nil {
		config.host = fakeSandboxHost("linux", false, "/home/u", nil)
	}
	if config.newPassword == nil {
		config.newPassword = func() (string, error) { return testServerPassword, nil }
	}
	return config
}

var errFakeKill = errors.New("kill refused")
