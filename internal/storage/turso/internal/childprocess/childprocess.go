// Package childprocess runs one test of the current test binary as a child
// process behind explicit barriers, so the stores' cross-process tests prove
// overlap by synchronization rather than by elapsed time.
//
// The parent holds the child's stdin. One byte on it is the start barrier;
// closing it releases the child. Readiness and progress are signalled with
// files in a directory private to the child. Every wait is bounded: a child
// that does not signal or exit in time is killed and reaped, and the test
// fails with its output. A parent that dies closes the pipe, which releases
// the child as well.
package childprocess

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const directoryVariable = "CONDUIT_CHILD_PROCESS_SIGNALS"

// Ready is signalled by AwaitBegin before the child blocks on the start barrier.
const Ready = "ready"

// timeout bounds every parent wait on a child.
const timeout = 10 * time.Second

// Signal records that the child reached the named point.
func Signal(name string) error {
	return os.WriteFile(filepath.Join(os.Getenv(directoryVariable), name), nil, 0o600)
}

// AwaitBegin signals Ready and blocks until the parent calls Begin.
func AwaitBegin() error {
	if err := Signal(Ready); err != nil {
		return err
	}
	_, err := io.ReadFull(os.Stdin, make([]byte, 1))
	return err
}

// AwaitRelease blocks until the parent calls Release or Finish, or exits.
func AwaitRelease() error {
	_, err := io.Copy(io.Discard, os.Stdin)
	return err
}

// Child is the parent's handle on one child process.
type Child struct {
	t         testing.TB
	command   *exec.Cmd
	stdin     io.WriteCloser
	output    strings.Builder
	signals   string
	exited    chan struct{}
	exitError error
}

// Start runs the named test of this binary as a child with env added. The
// child is reaped when the test ends, whatever its outcome.
func Start(t testing.TB, test string, env ...string) *Child {
	t.Helper()
	c := &Child{t: t, signals: t.TempDir(), exited: make(chan struct{})}
	c.command = exec.Command(os.Args[0], "-test.run=^"+test+"$", "-test.count=1")
	c.command.Env = append(append(os.Environ(), env...), directoryVariable+"="+c.signals)
	c.command.Stdout, c.command.Stderr = &c.output, &c.output
	stdin, err := c.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.stdin = stdin
	if err := c.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		c.exitError = c.command.Wait()
		close(c.exited)
	}()
	t.Cleanup(c.stop)
	return c
}

// Begin passes the start barrier. Called before the child reaches it, the
// byte waits in the pipe.
func (c *Child) Begin() {
	c.t.Helper()
	if _, err := c.stdin.Write([]byte{0}); err != nil {
		c.fail("start barrier: %v", err)
	}
}

// BeginTogether waits until every child is at the start barrier, then passes
// all of them through it.
func BeginTogether(children ...*Child) {
	for _, c := range children {
		c.t.Helper()
		c.Await(Ready)
	}
	for _, c := range children {
		c.Begin()
	}
}

// Await blocks until the child signals name. A child that exits first or does
// not signal in time fails the test.
func (c *Child) Await(name string) {
	c.t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(c.signals, name)); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			c.fail("await %s: %v", name, err)
		}
		select {
		case <-c.exited:
			if _, err := os.Stat(filepath.Join(c.signals, name)); err == nil {
				return
			}
			c.fail("child exited before %s: %v", name, c.exitError)
		case <-deadline:
			c.fail("child did not signal %s within %v", name, timeout)
		case <-tick.C:
		}
	}
}

// Finish releases the child, waits for it to exit successfully and returns
// everything it wrote.
func (c *Child) Finish() string {
	c.t.Helper()
	c.stdin.Close()
	select {
	case <-c.exited:
	case <-time.After(timeout):
		c.fail("child did not exit within %v of its release", timeout)
	}
	if c.exitError != nil {
		c.t.Fatalf("child: %v\n%s", c.exitError, c.output.String())
	}
	return c.output.String()
}

// fail kills and reaps the child before failing, so its output can be read.
func (c *Child) fail(format string, args ...any) {
	c.t.Helper()
	c.stdin.Close()
	c.command.Process.Kill()
	<-c.exited
	c.t.Fatalf(format+"\n%s", append(args, c.output.String())...)
}

// stop releases the child and, if it does not exit in time, kills it. It
// returns only after the child has been reaped.
func (c *Child) stop() {
	c.stdin.Close()
	select {
	case <-c.exited:
		return
	case <-time.After(timeout):
	}
	c.command.Process.Kill()
	<-c.exited
}
