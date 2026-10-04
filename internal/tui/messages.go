package tui

import (
	"context"
	"github.com/MartinAndreev/conduit/internal/app/startup"
)

// Initializer keeps presentation dependent on the application procedure alone.
type Initializer interface {
	Initialize(context.Context, *startup.Progress) startup.Outcome
}
type progressMsg struct {
	generation uint64
	snapshot   startup.Snapshot
}
type outcomeMsg struct {
	generation uint64
	outcome    startup.Outcome
}
type cleanupMsg struct {
	generation  uint64
	observation startup.CleanupObservation
	timedOut    bool
}
type exitDeadlineMsg struct {
	generation uint64
	expired    bool
}
type operation struct {
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	progress   *startup.Progress
	done       chan struct{}
	outcome    startup.Outcome
}
