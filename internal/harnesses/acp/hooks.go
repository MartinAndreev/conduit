package acp

// hookPoint names a scheduling point that in-package tests use to force
// interleavings deterministically. Production sessions have no hooks.
type hookPoint int

const (
	// hookBeforeActivation: StartTurn settled dispatch, activation not yet
	// published.
	hookBeforeActivation hookPoint = iota
	// hookPromptReplyReceived: a prompt reply reached the reader.
	hookPromptReplyReceived
	// hookBeforeTerminal: the turn holds its ending lease, terminal events
	// are not yet published.
	hookBeforeTerminal
	// hookBeforeOpenTransition: the last opening reply was handled, the
	// session is not yet open.
	hookBeforeOpenTransition
	// hookTurnReleased: a turn's terminal events were published and its
	// ending lease released; a new turn may now be reserved.
	hookTurnReleased
	// hookTransportStopping: the transport stopped and owned-agent
	// termination was started; pending replies are not yet failed.
	hookTransportStopping
)

type testHooks map[hookPoint]func(*session)

func (s *session) at(point hookPoint) {
	if hook := s.hooks[point]; hook != nil {
		hook(s)
	}
}
