package execution

import (
	"slices"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// ExistingIntent is a stored intent with the command's ID.
type ExistingIntent struct {
	RequestHash  string
	HasManifest  bool
	ManifestHash string
}

// SessionBinding is a session's current binding.
type SessionBinding struct {
	Assignment       provider.AssignmentRef
	AdapterSessionID provider.AdapterSessionID
}

// AcceptedStart is an accepted start_turn intent of the session with the
// adapter turn its outcome recorded. Assignment.Revision is the intent's
// expected revision.
type AcceptedStart struct {
	SessionSequence  int64
	AdapterSessionID provider.AdapterSessionID
	Assignment       provider.AssignmentRef
	AdapterTurnID    provider.AdapterTurnID
	CreatedAt        time.Time
}

// StoredSessionFacts are what storage holds about the command's session,
// read in the record transaction.
type StoredSessionFacts struct {
	SessionFound bool
	Session      SessionBinding
	// OpenIntent reports an intent of the session in an open state.
	OpenIntent bool
	// AcceptedStarts are accepted start_turn intents of the session. The rule
	// chooses among them; a reader may return only the candidates.
	AcceptedStarts []AcceptedStart
}

// IntentFacts decide whether a command is recorded. RequestHash and, for
// composed memory, ManifestHash are the command's. The existing-ID phase uses
// only Existing; the new-ID phase uses Stored and, for composed memory,
// Recheck.
type IntentFacts struct {
	Command       Command
	RequestHash   string
	ManifestHash  string
	ExistingFound bool
	Existing      ExistingIntent
	Stored        StoredSessionFacts
	Recheck       memory.ManifestRecheckFacts
}

type IntentDecisionKind string

const (
	IntentInvalidFacts IntentDecisionKind = "invalid_facts"
	// IntentExisting: the same command was recorded; nothing new is written.
	IntentExisting              IntentDecisionKind = "existing"
	IntentIDConflict            IntentDecisionKind = "id_conflict"
	IntentStoredWithoutManifest IntentDecisionKind = "stored_without_manifest"
	IntentStoredNotFound        IntentDecisionKind = "stored_not_found"
	IntentSessionNotFound       IntentDecisionKind = "session_not_found"
	IntentBindingMismatch       IntentDecisionKind = "binding_mismatch"
	IntentStaleRevision         IntentDecisionKind = "stale_revision"
	IntentCorrectionRefused     IntentDecisionKind = "correction_refused"
	IntentTurnUnknown           IntentDecisionKind = "turn_unknown"
	IntentTurnMismatch          IntentDecisionKind = "turn_mismatch"
	IntentBlockedByOpenIntent   IntentDecisionKind = "blocked_by_open_intent"
	IntentRecompose             IntentDecisionKind = "recompose"
	IntentRecord                IntentDecisionKind = "record"
)

// IntentDecision carries Correction for IntentCorrectionRefused and Recheck
// for IntentRecompose.
type IntentDecision struct {
	Kind       IntentDecisionKind
	Reason     string
	Correction sessions.CorrectionKind
	Recheck    memory.ManifestRecheckDecision
}

func intentDecision(kind IntentDecisionKind, reason string) IntentDecision {
	return IntentDecision{Kind: kind, Reason: reason}
}

// DecideDispatchIntent decides, inside the record transaction, whether a
// command is recorded.
//
// An existing intent with the command's ID is judged only on identity, with
// no recheck and no other fact: the same request (and, for composed memory,
// the same manifest; for stored memory, a recorded manifest) is existing,
// anything else id_conflict. A stored repetition needs a recorded manifest.
//
// A new ID is judged in order: invalid facts, a stored repetition with nothing
// stored, a missing session, a binding other than the session's (identity or
// adapter session), a stale revision; for Steer the correction rule and the
// target turn; an open intent of the session; for composed memory the commit
// recheck of the records the composition relied on; then record.
//
// The Steer target turn is the latest accepted start, by session sequence, on
// the same adapter session and assignment identity whose expected revision is
// not above the current one. Later rejected or invalidated starts do not
// replace it, a later open start blocks through blocked_by_open_intent, and
// only a later accepted start defines a new turn. Whether the turn is still
// active is the adapter's to prove.
func DecideDispatchIntent(f IntentFacts) IntentDecision {
	c := f.Command
	if len(f.RequestHash) != 64 || !slices.Contains(IntentKinds(), c.Kind) ||
		!slices.Contains([]MemoryKind{MemoryInputNone, MemoryInputComposed, MemoryInputStored}, c.Memory.Kind) ||
		c.Memory.Kind == MemoryInputComposed && len(f.ManifestHash) != 64 {
		return intentDecision(IntentInvalidFacts, "intent facts need a request hash, a known kind and memory input, and a manifest hash for composed memory")
	}
	if f.ExistingFound {
		return decideExisting(f)
	}
	if !validStarts(f.Stored.AcceptedStarts) {
		return intentDecision(IntentInvalidFacts, "accepted starts need positive, distinct session sequences")
	}
	if c.Memory.Kind == MemoryInputStored {
		return intentDecision(IntentStoredNotFound, "a stored repetition names no recorded intent")
	}
	if !f.Stored.SessionFound {
		return intentDecision(IntentSessionNotFound, "session does not exist")
	}
	session := f.Stored.Session
	if !session.Assignment.SameIdentity(c.Current) || session.AdapterSessionID != c.Session.AdapterSessionID {
		return intentDecision(IntentBindingMismatch, "command assignment or adapter session differs from the session's binding")
	}
	if session.Assignment.Revision != c.Current.Revision {
		return intentDecision(IntentStaleRevision, "command revision differs from the session's current revision")
	}
	if c.Kind == IntentSteer {
		correction := sessions.DecideCorrection(session.Assignment, c.Current, c.Current.Revision, c.New)
		if correction.Kind != sessions.CorrectionAllowed {
			return IntentDecision{Kind: IntentCorrectionRefused, Reason: correction.Reason, Correction: correction.Kind}
		}
		start, found := targetStart(f.Stored)
		if !found {
			return intentDecision(IntentTurnUnknown, "no accepted start on this adapter session and assignment defines the turn to steer")
		}
		if start.AdapterTurnID != c.ExpectedTurn.AdapterTurnID {
			return intentDecision(IntentTurnMismatch, "expected turn differs from the latest accepted start's turn")
		}
	}
	if f.Stored.OpenIntent {
		return intentDecision(IntentBlockedByOpenIntent, "the session has an open intent; until recovery releases it no new intent is recorded")
	}
	if c.Memory.Kind == MemoryInputComposed {
		recheck := memory.DecideManifestRecheck(f.Recheck)
		switch recheck.Kind {
		case memory.RecheckCommit:
		case memory.RecheckRecompose:
			return IntentDecision{Kind: IntentRecompose, Reason: recheck.Reason, Recheck: recheck}
		default:
			return IntentDecision{Kind: IntentInvalidFacts, Reason: "recheck: " + recheck.Reason, Recheck: recheck}
		}
	}
	return intentDecision(IntentRecord, "command may be recorded")
}

func decideExisting(f IntentFacts) IntentDecision {
	existing := f.Existing
	if existing.RequestHash != f.RequestHash {
		return intentDecision(IntentIDConflict, "intent id already names a different request")
	}
	switch f.Command.Memory.Kind {
	case MemoryInputStored:
		if !existing.HasManifest {
			return intentDecision(IntentStoredWithoutManifest, "the recorded intent has no manifest to repeat")
		}
	case MemoryInputComposed:
		if !existing.HasManifest || existing.ManifestHash != f.ManifestHash {
			return intentDecision(IntentIDConflict, "intent id already names a different manifest")
		}
	default:
		if existing.HasManifest {
			return intentDecision(IntentIDConflict, "intent id already names a request with a manifest")
		}
	}
	return intentDecision(IntentExisting, "the same command is already recorded")
}

func validStarts(starts []AcceptedStart) bool {
	seen := make(map[int64]bool, len(starts))
	for _, start := range starts {
		if start.SessionSequence < 1 || seen[start.SessionSequence] {
			return false
		}
		seen[start.SessionSequence] = true
	}
	return true
}

// targetStart returns the latest accepted start on the session's adapter
// session and assignment identity, at or below its current revision.
func targetStart(f StoredSessionFacts) (AcceptedStart, bool) {
	var latest AcceptedStart
	found := false
	for _, start := range f.AcceptedStarts {
		if start.AdapterSessionID != f.Session.AdapterSessionID || !start.Assignment.SameIdentity(f.Session.Assignment) ||
			start.Assignment.Revision > f.Session.Assignment.Revision {
			continue
		}
		if !found || start.SessionSequence > latest.SessionSequence {
			latest, found = start, true
		}
	}
	return latest, found
}
