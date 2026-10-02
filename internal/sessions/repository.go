package sessions

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound        = errors.New("session not found")
	ErrVersionConflict = errors.New("session version conflict")
)

type ID string

// Record is Conduit's durable logical session state. Provider-native details
// remain opaque and private to the session boundary.
type Record struct {
	ID               ID
	ProjectID        string
	RunID            string
	AttemptID        string
	AssignmentID     string
	Revision         uint64
	AdapterSessionID string
	NativeBinding    []byte
	State            string
	Version          uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Reader is the read boundary required by session consumers.
type Reader interface {
	Get(context.Context, ID) (Record, error)
}

// Writer persists a record only when expectedVersion matches. Zero creates a
// new record; successful writes return the record with its incremented version.
type Writer interface {
	Save(context.Context, Record, uint64) (Record, error)
}

type Repository interface {
	Reader
	Writer
}
