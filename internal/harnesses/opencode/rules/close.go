package rules

type CloseKind string

const (
	CloseBindingStale             CloseKind = "binding_stale"
	CloseActiveWorkRequiresDetach CloseKind = "active_work_requires_detach"
	// CloseStopServer: no turn is active; the session's own server is stopped.
	CloseStopServer CloseKind = "stop_server"
	// CloseStopClientAndServer: the active turn's run client is terminated,
	// then the session's own server.
	CloseStopClientAndServer CloseKind = "stop_client_and_server"
	// CloseDetachStopsServer: explicit detach of an active turn. The server is
	// owned by this session and nothing may outlive the session, so the run
	// client and the server are stopped as for interrupt_then_close; the
	// active work is ended, not left running.
	CloseDetachStopsServer CloseKind = "detach_stops_server"
)

type CloseFacts struct {
	SessionMatches     bool
	ActiveTurn         bool
	ExplicitDetach     bool
	InterruptThenClose bool
}
type CloseDecision struct {
	Kind   CloseKind
	Reason string
}

// StopsClient reports whether the active turn's run client is terminated.
func (d CloseDecision) StopsClient() bool {
	return d.Kind == CloseStopClientAndServer || d.Kind == CloseDetachStopsServer
}

// Closes reports whether the session handle is closed and its server stopped.
func (d CloseDecision) Closes() bool {
	return d.Kind == CloseStopServer || d.StopsClient()
}

// DecideClose answers how a Close request ends the session. Every accepted
// close stops the session's own OpenCode server, where its tools execute;
// whether that proves execution ended is decided by DecideServerStop.
func DecideClose(f CloseFacts) CloseDecision {
	switch {
	case !f.SessionMatches:
		return CloseDecision{CloseBindingStale, StaleBindingReason}
	case !f.ActiveTurn:
		return CloseDecision{Kind: CloseStopServer, Reason: "no turn is active; the session's own OpenCode server is stopped"}
	case f.InterruptThenClose:
		return CloseDecision{Kind: CloseStopClientAndServer, Reason: "the owned run client is terminated, then the session's own OpenCode server"}
	case f.ExplicitDetach:
		return CloseDecision{Kind: CloseDetachStopsServer, Reason: "explicit detach cannot leave the work running: the session's own OpenCode server is stopped, which ends it"}
	default:
		return CloseDecision{Kind: CloseActiveWorkRequiresDetach, Reason: "active OpenCode work requires explicit_detach or interrupt_then_close"}
	}
}
