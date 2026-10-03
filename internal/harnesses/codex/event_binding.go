package codex

import (
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// eventBinding freezes correlation and attribution together. Contract is the
// same negotiation observed with that turn/assignment, not a subsequent read.
type eventBinding struct {
	Turn       provider.Optional[provider.TurnRef]
	Assignment provider.Optional[provider.AssignmentRef]
	Native     provider.NativeIDs
	Contract   provider.ResultContractNegotiation
}

// snapshotEventBinding is called under eventMu before taking the session mutex;
// no caller holds the session mutex across event publication.
func (s *session) snapshotEventBinding() eventBinding {
	var binding eventBinding
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		binding = eventBinding{
			Turn: s.active, Assignment: optionalAssignment(s.assignment, s.steerPending != nil),
			Native:   provider.NativeIDs{Session: provider.Some(provider.NativeSessionID(s.nativeThread))},
			Contract: s.contract,
		}
		if s.active.Present {
			binding.Native.Turn = provider.Some(provider.NativeTurnID(s.active.Value.AdapterTurnID))
		}
		return nil
	})
	return binding
}
