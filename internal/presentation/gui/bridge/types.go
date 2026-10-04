// Package bridge translates presentation commands into the shared startup use case.
package bridge

import (
	"context"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"sync"
)

type Initializer interface {
	Initialize(context.Context, *startup.Progress) startup.Outcome
}

// State is a copied presentation read model. Generation protects reloads and late events.
type State struct {
	Generation     uint64
	Revision       uint64
	Version        uint64
	OperationID    string
	Status         startup.Status
	Steps          []startup.StepState
	Observations   []startup.HarnessObservation
	Diagnostics    []startup.Diagnostic
	ConfigPath     string
	Active         bool
	PendingCleanup bool
	CanRetry       bool
	Closing        bool
}

// Bridge owns one GUI operation and its cleanup observer for the window lifetime.
type Bridge struct {
	mu          sync.Mutex
	initializer Initializer
	context     context.Context
	emit        func(State)
	quit        func()
	generation  uint64
	state       State
	cancel      context.CancelFunc
	done        chan struct{}
	outcome     startup.Outcome
}
