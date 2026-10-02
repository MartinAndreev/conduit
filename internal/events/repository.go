package events

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrEventConflict = errors.New("event id already identifies different content")

type EventID string
type RunID string

// Event is a versioned durable application event. Payload is already encoded by
// its owning domain; callers must apply any required redaction before appending.
type Event struct {
	ID             EventID
	RunID          RunID
	Sequence       uint64
	Kind           string
	PayloadVersion uint32
	Payload        json.RawMessage
	Provenance     string
	CausationID    string
	ObservedAt     time.Time
}

type Appender interface {
	Append(context.Context, Event) (Event, error)
}

type Reader interface {
	Replay(context.Context, RunID, uint64, int) ([]Event, error)
}

type Repository interface {
	Appender
	Reader
}
