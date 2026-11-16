package rules

import (
	"strconv"
	"strings"
)

// serverListeningPrefix starts the line `opencode serve` prints on stdout
// once it accepts connections (OpenCode 2.0.22, manual probe 2026-11-16).
const serverListeningPrefix = "server listening on "

// serverPasswordPrefix starts the line `opencode serve` prints when it
// generated its own password because OPENCODE_SERVER_PASSWORD was not
// applied. The line carries a credential.
const serverPasswordPrefix = "server password"

// loopbackURLPrefix is the only accepted server address: the IPv4 loopback
// over plain HTTP, followed only by the port.
const loopbackURLPrefix = "http://127.0.0.1:"

// ServerReadyFacts is one line of the owned server's stdout, without the line
// terminator.
type ServerReadyFacts struct {
	Line string
}

type ServerReadyKind string

const (
	// ServerReadyListening: the server listens on the loopback address in URL.
	ServerReadyListening ServerReadyKind = "listening"
	// ServerReadyOtherOutput: the line is not about readiness; keep reading.
	ServerReadyOtherOutput ServerReadyKind = "other_output"
	// ServerReadyAddressRejected: the server reported a listening address
	// other than http://127.0.0.1:<1-65535>.
	ServerReadyAddressRejected ServerReadyKind = "address_rejected"
	// ServerReadyPasswordGenerated: the server ignored the supplied password
	// and generated its own, which the adapter neither knows nor may expose.
	ServerReadyPasswordGenerated ServerReadyKind = "password_generated"
)

type ServerReadyDecision struct {
	Kind   ServerReadyKind
	Reason string
	// URL is the server address; it is set only for ServerReadyListening.
	URL string
}

// DecideServerReady classifies one stdout line of the owned `opencode serve`.
// The listening line must be exact: the prefix, then http://127.0.0.1:, then a
// decimal port 1-65535 without sign or leading zero, and nothing else. Any
// other address in a listening line is refused rather than normalized. The
// reasons never quote the line, because server output may carry a secret.
func DecideServerReady(f ServerReadyFacts) ServerReadyDecision {
	if strings.HasPrefix(f.Line, serverPasswordPrefix) {
		return ServerReadyDecision{Kind: ServerReadyPasswordGenerated, Reason: "OpenCode server generated its own password instead of the supplied one"}
	}
	address, found := strings.CutPrefix(f.Line, serverListeningPrefix)
	if !found {
		return ServerReadyDecision{Kind: ServerReadyOtherOutput, Reason: "line does not report readiness"}
	}
	port, loopback := strings.CutPrefix(address, loopbackURLPrefix)
	if !loopback || !validPort(port) {
		return ServerReadyDecision{Kind: ServerReadyAddressRejected, Reason: "OpenCode server reported an address other than http://127.0.0.1:<port>"}
	}
	return ServerReadyDecision{Kind: ServerReadyListening, Reason: "OpenCode server listens on the loopback address", URL: address}
}

func validPort(port string) bool {
	if port == "" || len(port) > 5 || port[0] == '0' {
		return false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
	}
	value, err := strconv.Atoi(port)
	return err == nil && value >= 1 && value <= 65535
}

// ServerStopFacts describe one stop of the owned server's process group.
type ServerStopFacts struct {
	// CleanupComplete is true when the cleanup owner finished within the
	// bound: tree termination requested, pipes closed, root reaped and the
	// output drains ended.
	CleanupComplete bool
	// RootReaped is true when the server root was reaped.
	RootReaped bool
	// TreeKillSupported is true when the platform terminates the whole owned
	// process group rather than only the root.
	TreeKillSupported bool
	// TreeKillFailed is true when the OS refused the tree termination; a group
	// that was already gone is not a failure.
	TreeKillFailed bool
}

type ServerStopKind string

const (
	// ServerStopConfirmed: the server and its process group are terminated,
	// so no tool of this session executes any more.
	ServerStopConfirmed ServerStopKind = "confirmed"
	// ServerStopPending: cleanup did not finish within the bound.
	ServerStopPending ServerStopKind = "pending"
	// ServerStopKillFailed: the OS refused the tree termination.
	ServerStopKillFailed ServerStopKind = "kill_failed"
	// ServerStopTreeUnsupported: only the root could be terminated, so a
	// descendant (a running tool) may remain.
	ServerStopTreeUnsupported ServerStopKind = "tree_unsupported"
)

type ServerStopDecision struct {
	Kind   ServerStopKind
	Reason string
}

// Confirmed reports whether the stop proves that execution ended.
func (d ServerStopDecision) Confirmed() bool { return d.Kind == ServerStopConfirmed }

// DecideServerStop answers whether stopping the owned server proves that the
// session's native execution ended. Tools run inside the server's process
// group, so a reaped root plus a successful group termination is the proof;
// anything less keeps execution unknown.
func DecideServerStop(f ServerStopFacts) ServerStopDecision {
	switch {
	case f.TreeKillFailed:
		return ServerStopDecision{Kind: ServerStopKillFailed, Reason: "the OS refused to terminate the owned OpenCode server's process group; native execution is unknown"}
	case !f.CleanupComplete || !f.RootReaped:
		return ServerStopDecision{Kind: ServerStopPending, Reason: "the owned OpenCode server was not confirmed stopped within the bound; native execution is unknown"}
	case !f.TreeKillSupported:
		return ServerStopDecision{Kind: ServerStopTreeUnsupported, Reason: "only the owned OpenCode server root was terminated; a descendant may still run"}
	default:
		return ServerStopDecision{Kind: ServerStopConfirmed, Reason: "the owned OpenCode server and its process group were terminated and reaped"}
	}
}

// ServerExitFacts describe the owned server's root exit.
type ServerExitFacts struct {
	// Closed is true when the session was already closed, so Close stopped
	// the server.
	Closed bool
}

type ServerExitKind string

const (
	// ServerExitExpected: Close stopped the server.
	ServerExitExpected ServerExitKind = "expected"
	// ServerExitSessionLost: the server exited on its own while the session
	// was open, so the session is lost.
	ServerExitSessionLost ServerExitKind = "session_lost"
)

type ServerExitDecision struct {
	Kind   ServerExitKind
	Reason string
}

// DecideServerExit classifies the owned server's root exit. Only an exit
// while the session is open loses it; whatever the server ran before it
// exited may have had effects, so native execution state is unknown.
func DecideServerExit(f ServerExitFacts) ServerExitDecision {
	if f.Closed {
		return ServerExitDecision{Kind: ServerExitExpected, Reason: "owned OpenCode server stopped by Close"}
	}
	return ServerExitDecision{Kind: ServerExitSessionLost, Reason: "owned OpenCode server exited while the session was open; native execution state is unknown"}
}

// ServerInfoFacts is the owned server's `server.info` answer, checked against
// the launch it should describe.
type ServerInfoFacts struct {
	// QueryFailed is true when the call failed or its answer was unreadable.
	QueryFailed bool
	Version     string
	URLs        []string
	PID         int
	// ExpectedURL is the session endpoint from the listening line.
	ExpectedURL string
	// ExpectedPID is the owned server's root PID; an unknown (non-positive)
	// PID proves nothing.
	ExpectedPID int
}

type ServerInfoKind string

const (
	// ServerInfoAccepted: the answer comes from the owned server and its
	// version is the harness version.
	ServerInfoAccepted ServerInfoKind = "accepted"
	// ServerInfoQueryFailed: no readable answer.
	ServerInfoQueryFailed ServerInfoKind = "query_failed"
	// ServerInfoVersionInvalid: the answer proves the server is ours, but the
	// version is not MAJOR.MINOR.PATCH.
	ServerInfoVersionInvalid ServerInfoKind = "version_invalid"
	// ServerInfoURLMismatch: the answer does not list exactly the session's
	// endpoint, so it may come from another server.
	ServerInfoURLMismatch ServerInfoKind = "url_mismatch"
	// ServerInfoPIDMismatch: the answering server is not the owned process.
	ServerInfoPIDMismatch ServerInfoKind = "pid_mismatch"
)

type ServerInfoDecision struct {
	Kind   ServerInfoKind
	Reason string
	// Version is set only for ServerInfoAccepted.
	Version string
}

// ProvesOwnServer reports whether the answer came from the owned server:
// only then may the session use it. A version that cannot be parsed still
// proves the server; it leaves only the harness version unknown.
func (d ServerInfoDecision) ProvesOwnServer() bool {
	return d.Kind == ServerInfoAccepted || d.Kind == ServerInfoVersionInvalid
}

// DecideServerInfo answers whether server.info proves the server is the owned
// one and whether its version is the harness version. Proof needs a readable
// answer whose URLs are exactly the session endpoint and whose PID is the
// owned root PID (sandbox-exec execs OpenCode, so the PID is the same); any
// other answer fails the open. Only then is the version checked: an
// unparseable version keeps the session but leaves the version, and so
// protected paths, unknown. Reasons never quote the answer.
func DecideServerInfo(f ServerInfoFacts) ServerInfoDecision {
	switch {
	case f.QueryFailed:
		return ServerInfoDecision{Kind: ServerInfoQueryFailed, Reason: "OpenCode server.info could not be read, so the server is not proven to be the session's own"}
	case len(f.URLs) != 1 || f.ExpectedURL == "" || f.URLs[0] != f.ExpectedURL:
		return ServerInfoDecision{Kind: ServerInfoURLMismatch, Reason: "OpenCode server.info does not name exactly the session's own server"}
	case f.ExpectedPID <= 0 || f.PID != f.ExpectedPID:
		return ServerInfoDecision{Kind: ServerInfoPIDMismatch, Reason: "OpenCode server.info does not come from the owned server process"}
	case !strictVersion(f.Version):
		return ServerInfoDecision{Kind: ServerInfoVersionInvalid, Reason: "OpenCode server.info reports no MAJOR.MINOR.PATCH version; the harness version is unknown"}
	default:
		return ServerInfoDecision{Kind: ServerInfoAccepted, Reason: "harness version reported by the owned OpenCode server", Version: f.Version}
	}
}

// strictVersion accepts three dot-separated non-empty decimal components and
// nothing else (no prefix, pre-release or build suffix).
func strictVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}
