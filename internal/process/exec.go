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
	if spec.Env != nil {
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
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	if err := owned.activate(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = cmd.Wait()
		return nil, err
	}
	transferred = true
	return &execProcess{
		owned:  owned,
		cmd:    cmd,
		stdin:  stdin,
		stdout: newTrackedReadCloser(stdout),
		stderr: newTrackedReadCloser(stderr),
	}, nil
}

type execProcess struct {
	cmd    *exec.Cmd
	owned  *ownedResources
	stdin  io.WriteCloser
	stdout *trackedReadCloser
	stderr *trackedReadCloser

	waitOnce sync.Once
	result   Result
}

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

func (p *execProcess) Wait() Result {
	p.waitOnce.Do(func() {
		// StdoutPipe/StderrPipe require reads to complete before Wait. Descendants
		// that retain either pipe keep this blocked, which is why adapters must use
		// KillTree before claiming quiescence.
		<-p.stdout.done
		<-p.stderr.done
		err := p.cmd.Wait()
		releaseErr := p.owned.close()
		result := Result{ExitCode: -1, Err: err, ReleaseErr: releaseErr, ExitedAt: time.Now()}
		if state := p.cmd.ProcessState; state != nil {
			result.ExitCode = state.ExitCode()
			result.Signal = processSignal(state)
		}
		p.result = result
	})
	return p.result
}
