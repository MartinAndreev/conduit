package execution

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// IntentID is the application command ID of one external command. It is also
// the provider RequestID and, for Steer, the MessageID.
type IntentID string

// MaxIdentityBytes bounds every stored identity of a dispatch intent and its
// outcome (intent ID, bindings, adapter turn and native receipt IDs), in UTF-8
// bytes. Storage's CHECKs state the same bound.
const MaxIdentityBytes = 256

// MaxManifestPayloadBytes is the largest composed context, in UTF-8 bytes of
// the exact text that is stored and sent, that a manifest may carry. It is a
// standalone product limit (Martin, 2026-11-16): a larger composition is
// refused as payload_too_large, never truncated. Storage's CHECK on
// context_manifest_payloads.content states the same bound.
const MaxManifestPayloadBytes = 1024 * 1024

// MaxStoredRevision is the largest revision storage can hold (SQLite INTEGER).
const MaxStoredRevision = math.MaxInt64

// IntentKind is the external command an intent records.
type IntentKind string

const (
	IntentStartTurn IntentKind = "start_turn"
	IntentSteer     IntentKind = "steer"
)

func IntentKinds() []IntentKind { return []IntentKind{IntentStartTurn, IntentSteer} }

// IntentState is how far one intent got. Open states block the session.
type IntentState string

const (
	// StateRecorded: recorded (with its manifest and payload, if any); no send
	// was attempted.
	StateRecorded IntentState = "recorded"
	// StateDispatching: the claim committed; the send may have started.
	StateDispatching IntentState = "dispatching"
	// StateAccepted: a correlated result with evidence; a local advancement,
	// if any, is recorded.
	StateAccepted IntentState = "accepted"
	// StateRejected: proven not sent or explicitly refused.
	StateRejected IntentState = "rejected"
	// StateUnknown: attempted without a proven outcome.
	StateUnknown IntentState = "unknown"
	// StateDiverged: the adapter proved acceptance with advancement, but the
	// local advancement could not be recorded.
	StateDiverged IntentState = "diverged"
	// StateInvalidated: invalidated before sending (task 4.3).
	StateInvalidated IntentState = "invalidated"
)

func IntentStates() []IntentState {
	return []IntentState{StateRecorded, StateDispatching, StateAccepted, StateRejected, StateUnknown, StateDiverged, StateInvalidated}
}

// OpenIntentStates block new intents and binding changes of their session.
func OpenIntentStates() []IntentState {
	return []IntentState{StateRecorded, StateDispatching, StateUnknown, StateDiverged}
}

// Open reports whether s blocks its session.
func (s IntentState) Open() bool { return slices.Contains(OpenIntentStates(), s) }

// MemoryPresence is whether an intent carries a context manifest.
type MemoryPresence string

const (
	MemoryNone         MemoryPresence = "none"
	MemoryWithManifest MemoryPresence = "with_manifest"
)

func MemoryPresences() []MemoryPresence { return []MemoryPresence{MemoryNone, MemoryWithManifest} }

// MemoryKind is how a command supplies memory.
type MemoryKind string

const (
	// MemoryInputNone: no manifest.
	MemoryInputNone MemoryKind = "none"
	// MemoryInputComposed: a fresh composition; a new command.
	MemoryInputComposed MemoryKind = "composed"
	// MemoryInputStored: a repetition of a recorded intent that has a
	// manifest; it carries no composition.
	MemoryInputStored MemoryKind = "stored"
)

// ManifestComposition is whether a manifest carries a payload.
type ManifestComposition string

const (
	CompositionComposed ManifestComposition = "composed"
	// CompositionEmpty: nothing was admitted; no payload row and no part.
	CompositionEmpty ManifestComposition = "empty"
)

func ManifestCompositions() []ManifestComposition {
	return []ManifestComposition{CompositionComposed, CompositionEmpty}
}

// EmptyPayloadHash is the SHA-256 hex of the empty string, the payload hash
// of an empty composition.
const EmptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// MemoryInput is the memory a command supplies.
type MemoryInput struct {
	Kind MemoryKind
	// Request and Composition are set only for MemoryInputComposed.
	Request     compose.Request
	Composition compose.Composition
}

// Command is one StartTurn or Steer command. Current is the target assignment
// for StartTurn and the current one for Steer; New, ExpectedTurn and Mode are
// Steer only; ResultContract is StartTurn only. Parts exclude the memory part,
// which is always rebuilt from the claimed payload.
type Command struct {
	IntentID       IntentID
	Kind           IntentKind
	SessionID      sessions.ID
	Session        provider.SessionRef
	Current        provider.AssignmentRef
	New            provider.AssignmentRef
	ExpectedTurn   provider.TurnRef
	Mode           provider.DeliveryMode
	ResultContract provider.ResultContractRef
	Parts          []provider.InputPart
	Memory         MemoryInput
}

// Presence is the manifest presence the command's memory implies.
func (c Command) Presence() MemoryPresence {
	if c.Memory.Kind == MemoryInputNone {
		return MemoryNone
	}
	return MemoryWithManifest
}

// TargetRevision is the revision the intent leaves the assignment at when it
// is accepted: Current's for StartTurn, New's for Steer.
func (c Command) TargetRevision() uint64 {
	if c.Kind == IntentSteer {
		return c.New.Revision
	}
	return c.Current.Revision
}

// ErrInvalidCommand reports a command that cannot be recorded. It is a caller
// error, found before any I/O.
var ErrInvalidCommand = errors.New("invalid dispatch command")

// ValidateCommand checks the shape storage and the rules rely on. It does not
// decide eligibility; DecideDispatchIntent does.
func ValidateCommand(c Command) error {
	if problem := commandProblem(c); problem != "" {
		return fmt.Errorf("%w: %s", ErrInvalidCommand, problem)
	}
	return nil
}

func commandProblem(c Command) string {
	switch {
	case !boundedIdentity(string(c.IntentID)):
		return "intent id must have 1 to 256 bytes"
	case !slices.Contains(IntentKinds(), c.Kind):
		return "unknown intent kind"
	case !boundedIdentity(string(c.SessionID)):
		return "session id must have 1 to 256 bytes"
	case !boundedIdentity(string(c.Session.AdapterSessionID)):
		return "adapter session id must have 1 to 256 bytes"
	case !validAssignment(c.Current):
		return "current assignment needs bounded identities and a revision from 1 to the storage maximum"
	case !validParts(c.Parts):
		return "an input part has an unknown kind or is a context part"
	}
	if problem := memoryProblem(c.Memory); problem != "" {
		return problem
	}
	if c.Kind == IntentStartTurn {
		if c.New != (provider.AssignmentRef{}) || c.ExpectedTurn != (provider.TurnRef{}) || c.Mode != "" {
			return "start turn carries no new assignment, expected turn or delivery mode"
		}
		return ""
	}
	switch {
	case !validAssignment(c.New):
		return "new assignment needs bounded identities and a revision from 1 to the storage maximum"
	case !boundedIdentity(string(c.ExpectedTurn.AdapterTurnID)):
		return "expected turn must have 1 to 256 bytes"
	case c.Mode != provider.DeliverActiveTurn && c.Mode != provider.DeliverNextBoundary:
		return "unknown delivery mode"
	case !isZeroContract(c.ResultContract):
		return "steer carries no result contract"
	}
	return ""
}

func memoryProblem(m MemoryInput) string {
	switch m.Kind {
	case MemoryInputComposed:
		return ""
	case MemoryInputNone, MemoryInputStored:
		if m.Composition.Kind != "" || m.Request.ProjectID != "" {
			return "only composed memory carries a request and a composition"
		}
		return ""
	default:
		return "unknown memory input kind"
	}
}

func isZeroContract(r provider.ResultContractRef) bool {
	return r.Name == "" && r.Version == "" && len(r.Schema) == 0 && r.Requested == "" && len(r.AllowedFallbacks) == 0
}

// validParts refuses an unknown kind and a context part: memory reaches the
// harness only as the part rebuilt from the claimed payload.
func validParts(parts []provider.InputPart) bool {
	for _, part := range parts {
		if part.Kind != provider.InputInstruction && part.Kind != provider.InputArtifactRef {
			return false
		}
	}
	return true
}

func boundedIdentity(value string) bool {
	return len(value) >= 1 && len(value) <= MaxIdentityBytes
}

func validAssignment(a provider.AssignmentRef) bool {
	return boundedIdentity(string(a.ProjectID)) && boundedIdentity(string(a.RunID)) &&
		boundedIdentity(string(a.AttemptID)) && boundedIdentity(string(a.AssignmentID)) &&
		a.Revision >= 1 && a.Revision <= MaxStoredRevision
}
