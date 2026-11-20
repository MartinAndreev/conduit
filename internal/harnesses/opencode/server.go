package opencode

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// Environment variables of the password: the server takes it from
// OPENCODE_SERVER_PASSWORD, clients from OPENCODE_PASSWORD (OpenCode 2.0.22).
const (
	serverPasswordEnv = "OPENCODE_SERVER_PASSWORD"
	clientPasswordEnv = "OPENCODE_PASSWORD"
)

// maxServerLineBytes bounds one line of server stdout while readiness is
// awaited; a longer line fails the launch.
const maxServerLineBytes = 64 * 1024

// newServerPassword draws 32 random bytes from crypto/rand, hex encoded.
func newServerPassword() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// ServerEndpoint is a session's own OpenCode server. Every client command of
// the session connects to it; a request without one is refused, so nothing
// falls back to the user's shared background service. The per-session
// password is held in a closure, so formatting an endpoint or a request with
// any verb prints only a function address, never the password.
type ServerEndpoint struct {
	URL      string
	password func() string
}

func newServerEndpoint(url, password string) ServerEndpoint {
	return ServerEndpoint{URL: url, password: func() string { return password }}
}

// clientArgs are the flags that select the server; they precede the
// command's own arguments.
func (e ServerEndpoint) clientArgs() []string { return []string{"--server", e.URL} }

// clientEnv is appended to the inherited environment, so it wins over an
// inherited OPENCODE_PASSWORD (the last duplicate wins).
func (e ServerEndpoint) clientEnv() []string {
	if e.password == nil {
		return []string{}
	}
	return []string{clientPasswordEnv + "=" + e.password()}
}

// carriesSecret reports whether a decoded value contains this session's
// password. Native IDs decoded from control output are checked with it.
func (e ServerEndpoint) carriesSecret(value string) bool {
	return e.password != nil && strings.Contains(value, e.password())
}

// errSecretInNativeID refuses a native ID that contains the session password:
// it would reach events, receipts or later command arguments. It is never
// replaced by a redacted value, which would no longer name the native object.
var errSecretInNativeID = errors.New("OpenCode returned a native ID containing the session secret")

// usable reports whether a client command can target this endpoint.
func (e ServerEndpoint) usable() bool { return e.URL != "" && e.password != nil }

// errNoServer refuses a client command without the session's own server.
var errNoServer = errors.New("no session-owned OpenCode server; the shared background service is never used")

// Readiness failures. They never contain server output, which may carry a
// secret.
var (
	errServerExited       = errors.New("OpenCode server exited before reporting readiness")
	errServerOutputEnded  = errors.New("OpenCode server output ended before reporting readiness")
	errServerOutputBound  = errors.New("OpenCode server output exceeded the line bound before reporting readiness")
	errServerReadyTimeout = errors.New("OpenCode server did not report readiness in time")
)

// ownedServer is the session's own `opencode serve` process. Its cleanup
// owner performs at most one stop, however often it is requested. It keeps
// the launch's sandbox decision and hash, not its spec, which holds the
// password in its environment.
type ownedServer struct {
	proc       processruntime.Process
	endpoint   ServerEndpoint
	sandbox    rules.SandboxDecision
	configHash string
	rootExited <-chan struct{}
	cleanup    *processruntime.CleanupOwner
	stopGrace  time.Duration
	// afterStopAwait is a test seam called in abortServer after stopOutcome
	// returns and before checking cleanup.Done; nil in production.
	afterStopAwait func()
}

// readiness is the outcome of reading server stdout until the listening line.
type readiness struct {
	url string
	err error
}

// startOwnedServer is the step that launches this session's server and waits
// for its exact listening line, bounded by timeout and ctx. Stdout and stderr
// are drained for the server's whole life and their content is discarded. On
// a failure after the launch the returned server is non-nil and must be
// stopped by the caller.
func startOwnedServer(ctx context.Context, launcher processruntime.Launcher, launch serverLaunch, password string, timeout, stopGrace time.Duration) (*ownedServer, error) {
	// Checked again right before the effect: the host queries may have taken
	// long enough for the caller to cancel.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proc, err := launcher.Start(launch.spec)
	if err != nil {
		return nil, err
	}
	ready := make(chan readiness, 1)
	drained := make(chan struct{})
	var drains sync.WaitGroup
	drains.Add(2)
	go func() { defer drains.Done(); readServerStdout(proc.Stdout(), ready) }()
	go func() { defer drains.Done(); _, _ = io.Copy(io.Discard, proc.Stderr()) }()
	go func() { drains.Wait(); close(drained) }()
	rootExited, _ := processruntime.ObserveRootExit(proc)
	server := &ownedServer{
		proc:       proc,
		sandbox:    launch.sandbox,
		configHash: launch.configHash,
		rootExited: rootExited,
		cleanup:    processruntime.NewCleanupOwnerWithDrain(proc, drained),
		stopGrace:  stopGrace,
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case outcome := <-ready:
		if outcome.err != nil {
			return server, outcome.err
		}
		server.endpoint = newServerEndpoint(outcome.url, password)
		return server, nil
	case <-rootExited:
		return server, errServerExited
	case <-timer.C:
		return server, errServerReadyTimeout
	case <-ctx.Done():
		return server, ctx.Err()
	}
}

// readServerStdout reports the first readiness outcome, then keeps draining
// stdout until it ends, so the server never blocks on a full pipe.
func readServerStdout(stdout io.Reader, ready chan<- readiness) {
	reported := false
	report := func(outcome readiness) {
		if !reported {
			reported = true
			ready <- outcome
		}
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxServerLineBytes)
	for scanner.Scan() {
		if reported {
			continue
		}
		decision := rules.DecideServerReady(rules.ServerReadyFacts{Line: scanner.Text()})
		switch decision.Kind {
		case rules.ServerReadyListening:
			report(readiness{url: decision.URL})
		case rules.ServerReadyAddressRejected, rules.ServerReadyPasswordGenerated:
			report(readiness{err: errors.New(decision.Reason)})
		}
	}
	if scanner.Err() != nil {
		report(readiness{err: errServerOutputBound})
	} else {
		report(readiness{err: errServerOutputEnded})
	}
	_, _ = io.Copy(io.Discard, stdout)
}

// serverStopOutcome separates execution-end confirmation (DecideServerStop)
// from full process resource release (DecideRelease, which also checks
// Wait's ReleaseErr and pipe/stdin close errors).
type serverStopOutcome struct {
	stop    rules.ServerStopDecision
	release rules.ReleaseDecision
	state   processruntime.CleanupState
}

// stop terminates the server's process group and waits for its cleanup,
// bounded by ctx and the stop grace. Repeated calls share the one cleanup.
func (o *ownedServer) stop(ctx context.Context) rules.ServerStopDecision {
	return o.stopOutcome(ctx).stop
}

func (o *ownedServer) outcomeFromState(state processruntime.CleanupState, complete bool) serverStopOutcome {
	stop := rules.DecideServerStop(rules.ServerStopFacts{
		CleanupComplete:   complete,
		RootReaped:        state.RootReaped,
		TreeKillSupported: o.proc.TreeKillSupported(),
		TreeKillFailed:    state.TreeTerminationErr != nil,
	})
	releaseFacts := cleanupReleaseFacts(state)
	releaseFacts.Finished = complete
	release := rules.DecideRelease(releaseFacts)
	return serverStopOutcome{stop: stop, release: release, state: state}
}

func (o *ownedServer) stopOutcome(ctx context.Context) serverStopOutcome {
	o.cleanup.BeginCleanup()
	waitCtx, cancel := context.WithTimeout(ctx, o.stopGrace)
	defer cancel()
	state, err := o.cleanup.Await(waitCtx)
	return o.outcomeFromState(state, err == nil && state.Complete)
}

func (o *ownedServer) latestOutcome() serverStopOutcome {
	state := o.cleanup.Latest()
	return o.outcomeFromState(state, state.Complete)
}
