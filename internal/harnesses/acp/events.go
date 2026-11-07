package acp

import (
	"errors"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// eventBinding is one snapshot of correlation and attribution for an event.
type eventBinding struct {
	turn       provider.Optional[provider.TurnRef]
	assignment provider.AssignmentRef
	native     string
}

func (s *session) snapshotBinding() eventBinding {
	var binding eventBinding
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		binding = eventBinding{assignment: s.assignment, native: s.nativeSession}
		if s.turn != nil {
			binding.turn = provider.Some(s.turn.bound.ref)
			binding.assignment = s.turn.bound.assignment
		}
		return nil
	})
	return binding
}

// emit publishes an event bound to the current turn, if any.
func (s *session) emit(body provider.EventBody) {
	s.publish(body, provider.None[boundTurn]())
}

// emitTurn publishes an event bound to turn and its frozen assignment, even
// after it stopped being the active turn.
func (s *session) emitTurn(turn boundTurn, body provider.EventBody) {
	s.publish(body, provider.Some(turn))
}

// publish serializes event construction and publication. It is never called
// with the session mutex held. ACP has no steering, so assignment attribution
// is always the reconciled binding.
func (s *session) publish(body provider.EventBody, turn provider.Optional[boundTurn]) {
	var idErr, publishErr error
	_ = lockscope.WithMutexLock(&s.eventMu, func() error {
		binding := s.snapshotBinding()
		if turn.Present {
			binding.turn = provider.Some(turn.Value.ref)
			binding.assignment = turn.Value.assignment
		}
		eventID, err := s.config.NewID(s.descriptor.ProviderName + "-event")
		if err != nil {
			idErr = err
			return nil
		}
		s.eventSeq++
		envelope := provider.EventEnvelope{
			EventID: provider.EventID(eventID), Session: s.ref,
			Turn: binding.turn, Assignment: provider.Some(binding.assignment),
			AdapterSeq: s.eventSeq,
			Native:     provider.NativeIDs{Session: optionalNativeSession(binding.native)},
			ObservedAt: s.config.Now(),
		}
		event, err := provider.NewProviderEvent(envelope, body)
		if err != nil {
			publishErr = err
			return nil
		}
		publishErr = s.broker.Publish(event)
		return nil
	})
	switch {
	case idErr != nil:
		s.loseObservation(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, idErr, "could not allocate event ID"))
	case errors.Is(publishErr, eventstream.ErrBufferOverflow):
		// The broker already ended the stream with backpressure_exceeded.
		s.loseObservation(nil)
	}
}

// loseObservation marks the session lost after a local observation failure,
// ends the stream (unless it already ended) and stops the owned agent. It runs
// after every event/session lock is released.
func (s *session) loseObservation(cause error) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if !s.closing {
			s.state = provider.SessionLost
		}
		return nil
	})
	if cause != nil {
		s.broker.Finish(cause)
	}
	_ = s.process.KillTree()
}

// warnOnce publishes a warning at most once per key, protecting the bounded
// buffer from repeated unknown observations.
func (s *session) warnOnce(key string, warning provider.ProtocolWarningEvent) {
	first := false
	_ = lockscope.WithMutexLock(&s.warnMu, func() error {
		if _, seen := s.warned[key]; !seen {
			s.warned[key] = struct{}{}
			first = true
		}
		return nil
	})
	if first {
		s.emit(warning)
	}
}

type textCursor struct {
	version uint64
	bytes   uint64
}

// textDelta versions one appended chunk. Offsets advance by the original byte
// count even when the emitted text was truncated.
func (s *session) textDelta(item, segment, text string) provider.TextSegmentUpdate {
	bounded := boundedtext.String(text, s.config.MaxTextBytes)
	key := item + "\x00" + segment
	var cursor textCursor
	_ = lockscope.WithMutexLock(&s.textMu, func() error {
		cursor = s.texts[key]
		s.texts[key] = textCursor{version: cursor.version + 1, bytes: cursor.bytes + bounded.OriginalBytes.Value}
		return nil
	})
	return provider.TextSegmentUpdate{
		ItemID: provider.ItemID(item), SegmentID: provider.SegmentID(segment), Text: bounded.Text,
		Mode: provider.UpdateDelta, BaseVersion: provider.Some(cursor.version), Version: cursor.version + 1,
		ByteOffset: cursor.bytes, Truncated: bounded.Truncated, OriginalBytes: bounded.OriginalBytes,
	}
}

// resetTurnObservations drops per-turn tool tracking when a turn ends. Text
// cursors are keyed by native message identity and stay valid.
func (s *session) resetTurnObservations() {
	_ = lockscope.WithMutexLock(&s.toolMu, func() error {
		clear(s.tools)
		return nil
	})
}
