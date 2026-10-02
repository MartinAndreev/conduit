package codex

import (
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// checkSessionReference validates this immutable adapter handle. It does not
// acquire a lock; callers keep mutable binding checks and reservation atomic.
func (s *session) checkSessionReference(operation provider.Operation, ref provider.SessionRef) error {
	if ref != s.ref {
		return adaptererror.New(operation, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, "session reference differs")
	}
	return nil
}
