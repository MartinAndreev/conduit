package process

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

func (ExecLauncher) Start(spec Spec) (started Process, err error) {
	if spec.Path == "" {
		return nil, errors.New("process executable path is empty")
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	switch {
	case spec.ReplaceEnv:
		cmd.Env = append([]string{}, spec.Env...)
	case spec.Env != nil:
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	owned, err := prepareOwnedProcess(cmd)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(err, owned.close())
		}
	}()
	pipes, err := newStdioPipes()
	if err != nil {
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pipes.childStdin, pipes.childStdout, pipes.childStderr
	if err := cmd.Start(); err != nil {
		pipes.closeAll()
		return nil, err
	}
	// The child holds its own copies now; the parent keeps only its ends, so
	// stdout/stderr reach EOF once the root and every descendant closed them.
	pipes.closeChildEnds()
	if err := owned.activate(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		pipes.closeParentEnds()
		return nil, err
	}
	transferred = true
	proc := &execProcess{
		owned:      owned,
		cmd:        cmd,
		stdin:      &onceWriteCloser{WriteCloser: pipes.stdin},
		stdout:     newTrackedReadCloser(pipes.stdout),
		stderr:     newTrackedReadCloser(pipes.stderr),
		rootExited: make(chan struct{}),
	}
	go proc.reapRoot()
	return proc, nil
}

// stdioPipes are created by the launcher instead of exec.Cmd's *Pipe helpers.
// The child's ends are *os.File values, so exec.Cmd starts no copy goroutines
// and its Wait does not close the parent's read ends; Wait can therefore reap
// the root as soon as it exits, while descendants may still hold the pipes.
type stdioPipes struct {
	stdin                                *os.File
	stdout, stderr                       *os.File
	childStdin, childStdout, childStderr *os.File
}

func newStdioPipes() (*stdioPipes, error) {
	var pipes stdioPipes
	var err error
	if pipes.childStdin, pipes.stdin, err = os.Pipe(); err != nil {
		return nil, err
	}
	if pipes.stdout, pipes.childStdout, err = os.Pipe(); err != nil {
		pipes.closeAll()
		return nil, err
	}
	if pipes.stderr, pipes.childStderr, err = os.Pipe(); err != nil {
		pipes.closeAll()
		return nil, err
	}
	return &pipes, nil
}

func (p *stdioPipes) closeChildEnds() {
	for _, file := range []*os.File{p.childStdin, p.childStdout, p.childStderr} {
		if file != nil {
			_ = file.Close()
		}
	}
}

func (p *stdioPipes) closeParentEnds() {
	for _, file := range []*os.File{p.stdin, p.stdout, p.stderr} {
		if file != nil {
			_ = file.Close()
		}
	}
}

func (p *stdioPipes) closeAll() {
	p.closeChildEnds()
	p.closeParentEnds()
}

type execProcess struct {
	cmd    *exec.Cmd
	owned  *ownedResources
	stdin  io.WriteCloser
	stdout *trackedReadCloser
	stderr *trackedReadCloser

	// rootExited closes when the reaper's single exec.Cmd.Wait returned;
	// rootResult is written before that close and read only after it.
	rootExited chan struct{}
	rootResult Result

	waitOnce sync.Once
	result   Result
}

// reapRoot is the only caller of exec.Cmd.Wait. With *os.File stdio it
// returns as soon as the root exits, regardless of descendants that still
// hold the pipes, and it closes no parent pipe end.
func (p *execProcess) reapRoot() {
	err := p.cmd.Wait()
	result := Result{ExitCode: -1, Err: err, ExitedAt: time.Now()}
	if state := p.cmd.ProcessState; state != nil {
		result.ExitCode = state.ExitCode()
		result.Signal = processSignal(state)
	}
	p.rootResult = result
	close(p.rootExited)
}

// RootResult is the root's exit status. It is valid once RootExited closed;
// ReleaseErr stays empty because owned resources are released only by Wait.
func (p *execProcess) RootResult() Result {
	<-p.rootExited
	return p.rootResult
}

// RootExited closes when the root process has exited and been reaped, even
// while a descendant keeps stdout or stderr open.
func (p *execProcess) RootExited() <-chan struct{} { return p.rootExited }

type trackedReadCloser struct {
	io.ReadCloser
	done      chan struct{}
	once      sync.Once
	closeOnce sync.Once
	closeErr  error
}

func newTrackedReadCloser(reader io.ReadCloser) *trackedReadCloser {
	return &trackedReadCloser{ReadCloser: reader, done: make(chan struct{})}
}

func (r *trackedReadCloser) Read(buffer []byte) (int, error) {
	n, err := r.ReadCloser.Read(buffer)
	if err != nil {
		r.once.Do(func() { close(r.done) })
	}
	return n, err
}

func (r *trackedReadCloser) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.ReadCloser.Close(); r.once.Do(func() { close(r.done) }) })
	return r.closeErr
}

func (p *execProcess) PID() int                { return p.cmd.Process.Pid }
func (p *execProcess) Stdin() io.WriteCloser   { return p.stdin }
func (p *execProcess) Stdout() io.ReadCloser   { return p.stdout }
func (p *execProcess) Stderr() io.ReadCloser   { return p.stderr }
func (p *execProcess) Interrupt() error        { return interruptOwnedProcess(p.cmd.Process.Pid) }
func (p *execProcess) KillTree() error         { return p.owned.kill(p.cmd.Process.Pid) }
func (p *execProcess) TreeKillSupported() bool { return ownedTreeKillSupported() }

// Wait returns once the root was reaped and both output pipes reached EOF or
// were closed. Descendants that retain either pipe keep it blocked, which is
// why adapters observe RootExited and use KillTree before claiming
// quiescence. Afterwards it releases the parent's pipe ends and the owned
// resources (on Windows, the kill-on-close job).
func (p *execProcess) Wait() Result {
	p.waitOnce.Do(func() {
		<-p.rootExited
		<-p.stdout.done
		<-p.stderr.done
		_ = p.stdout.Close()
		_ = p.stderr.Close()
		_ = p.stdin.Close()
		result := p.rootResult
		result.ReleaseErr = p.owned.close()
		p.result = result
	})
	return p.result
}

// onceWriteCloser closes stdin once and reports the first result to every
// caller, like exec.Cmd's StdinPipe, so a later Close after Wait is not an
// error.
type onceWriteCloser struct {
	io.WriteCloser
	once sync.Once
	err  error
}

func (w *onceWriteCloser) Close() error {
	w.once.Do(func() { w.err = w.WriteCloser.Close() })
	return w.err
}
