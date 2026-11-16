package execution

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
)

// Journal event kinds of a dispatch intent, one per transaction (spec 003).
const (
	EventIntentRecorded = "dispatch.intent_recorded"
	EventClaimed        = "dispatch.claimed"
	EventSettled        = "dispatch.settled"
)

// EventPayloadVersion is the version of every dispatch event payload.
const EventPayloadVersion = 1

// IntentRecordedPayload is the dispatch.intent_recorded payload. It carries no
// input text or memory.
type IntentRecordedPayload struct {
	IntentID         IntentID       `json:"intent_id"`
	Kind             IntentKind     `json:"kind"`
	SessionID        string         `json:"session_id"`
	AdapterSessionID string         `json:"adapter_session_id"`
	AssignmentID     string         `json:"assignment_id"`
	ExpectedRevision uint64         `json:"expected_revision"`
	TargetRevision   uint64         `json:"target_revision"`
	Memory           MemoryPresence `json:"memory"`
	RequestHash      string         `json:"request_hash"`
	ManifestHash     string         `json:"manifest_hash,omitempty"`
}

// ClaimedPayload is the dispatch.claimed payload.
type ClaimedPayload struct {
	IntentID IntentID `json:"intent_id"`
}

// SettledPayload is the dispatch.settled payload: the state the settlement
// wrote and the classified observation, without native text.
type SettledPayload struct {
	IntentID       IntentID        `json:"intent_id"`
	State          IntentState     `json:"state"`
	Outcome        Outcome         `json:"outcome"`
	Classification Classification  `json:"classification"`
	Observation    ObservationKind `json:"observation"`
	Advanced       bool            `json:"advanced"`
}

// IntentEvent builds the journal event of one dispatch transaction. Its ID is
// "<intent ID>/<phase>", so a repetition of the same transaction with the
// same content finds the stored event instead of appending another.
func IntentEvent(intentID IntentID, runID string, kind string, payload any, observedAt time.Time) (events.Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return events.Event{}, fmt.Errorf("encode %s payload: %w", kind, err)
	}
	return events.Event{
		ID:             events.EventID(string(intentID) + "/" + phase(kind)),
		RunID:          events.RunID(runID),
		Kind:           kind,
		PayloadVersion: EventPayloadVersion,
		Payload:        encoded,
		Provenance:     "conduit",
		CausationID:    string(intentID),
		ObservedAt:     observedAt,
	}, nil
}

func phase(kind string) string {
	switch kind {
	case EventIntentRecorded:
		return "recorded"
	case EventClaimed:
		return "claimed"
	default:
		return "settled"
	}
}
