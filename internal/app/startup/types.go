// Package startup composes first-run preparation without presentation effects.
package startup

import (
	"context"
	"errors"
	"sync"

	"github.com/MartinAndreev/conduit/internal/configuration"
	"github.com/MartinAndreev/conduit/internal/lockscope"
)

type Status string

const (
	Running           Status = "running"
	Ready             Status = "ready"
	ReadyWithWarnings Status = "ready_with_warnings"
	Blocked           Status = "blocked"
	Cancelled         Status = "cancelled"
)

type StepID string

const (
	CheckConfiguration StepID = "check_configuration"
	DiscoverHarnesses  StepID = "discover_harnesses"
	CreateSettings     StepID = "create_settings"
	CheckReadiness     StepID = "check_readiness"
)

type StepStatus string

const (
	Pending       StepStatus = "pending"
	Working       StepStatus = "running"
	Completed     StepStatus = "completed"
	Skipped       StepStatus = "skipped"
	Failed        StepStatus = "failed"
	StepCancelled StepStatus = "cancelled"
)

type StepState struct {
	ID     StepID
	Status StepStatus
	Reason string
}
type Severity string

const (
	Warning Severity = "warning"
	Error   Severity = "error"
)

type Diagnostic struct {
	Code     string
	Severity Severity
	Step     StepID
	Harness  configuration.HarnessID
	Details  string
}
type Capabilities struct {
	SessionProtocol string
	Authentication  string
	Models          string
}
type HarnessObservation struct {
	Harness      configuration.HarnessID
	Discovery    configuration.Observation
	Capabilities Capabilities
}
type Snapshot struct {
	OperationID  string
	Revision     uint64
	Status       Status
	Steps        []StepState
	Observations []HarnessObservation
	Diagnostics  []Diagnostic
	ConfigPath   string
}
type Outcome struct {
	Snapshot       Snapshot
	Published      bool
	Err            error
	PendingCleanup CleanupObserver
}
type ReadKind string

const (
	Absent       ReadKind = "absent"
	Valid        ReadKind = "valid"
	Invalid      ReadKind = "invalid"
	Unreadable   ReadKind = "unreadable"
	FutureSchema ReadKind = "future_schema"
)

type ReadResult struct {
	Kind   ReadKind
	Config configuration.Config
	Err    error
}
type PublishKind string

const (
	Published     PublishKind = "published"
	AlreadyExists PublishKind = "already_exists"
)

type Repository interface {
	Resolve(context.Context) (string, error)
	Read(context.Context, string) ReadResult
	Publish(context.Context, string, configuration.Config) (PublishKind, error)
}

// ErrProbeCleanup means discovery could not confirm cleanup of an owned probe.
// Adapters preserve the underlying cause; callers must stop further effects.
var ErrProbeCleanup = errors.New("probe cleanup could not be confirmed")

type Detector interface {
	Discover(context.Context, configuration.HarnessID, string) (configuration.Observation, error)
}

// Progress is deliberately a concrete latest-value mailbox, never an arbitrary
// callback which could block the application producer.
type Progress struct {
	mu        sync.Mutex
	latest    Snapshot
	hasLatest bool
	closed    bool
	changed   chan struct{}
}

func NewProgress() *Progress { return &Progress{changed: make(chan struct{}, 1)} }

// Changed is a coalescing wake-up notification. Consumers query Latest after it.
// Close owns this signal; Initialize never closes a presentation-owned mailbox.
func (p *Progress) Changed() <-chan struct{} { return p.changed }
func (p *Progress) Latest() (snapshot Snapshot, available bool) {
	_ = lockscope.WithMutexLock(&p.mu, func() error {
		snapshot = clone(p.latest)
		available = p.hasLatest
		return nil
	})
	return snapshot, available
}
func (p *Progress) Close() {
	_ = lockscope.WithMutexLock(&p.mu, func() error {
		if !p.closed {
			p.closed = true
			close(p.changed)
		}
		return nil
	})
}
func (p *Progress) publish(s Snapshot) {
	if p == nil {
		return
	}
	_ = lockscope.WithMutexLock(&p.mu, func() error {
		if p.closed {
			return nil
		}
		p.latest = clone(s)
		p.hasLatest = true
		select {
		case p.changed <- struct{}{}:
		default:
		}
		return nil
	})
}
func clone(s Snapshot) Snapshot {
	s.Steps = append([]StepState(nil), s.Steps...)
	s.Observations = append([]HarnessObservation(nil), s.Observations...)
	s.Diagnostics = append([]Diagnostic(nil), s.Diagnostics...)
	return s
}

// CleanupObservation exposes safe lifecycle facts, never raw probe output.
type CleanupClosure struct {
	Complete bool
	Err      error
}
type CleanupObservation struct {
	PipesClosed   bool
	StdinClosure  CleanupClosure
	StdoutClosure CleanupClosure
	StderrClosure CleanupClosure

	TreeTerminationRequested bool
	TreeTerminationErr       error
	PipeCloseErr             error
	StdinCloseErr            error
	ReadersDone              bool
	ResourceReleaseErr       error
	RootReaped               bool
	Complete                 bool
	Err                      error
}

// CleanupObserver remains usable while the application lives. Await controls
// only caller waiting; cancellation never relinquishes process ownership.
type CleanupObserver interface {
	Latest() CleanupObservation
	Done() <-chan struct{}
	Await(context.Context) (CleanupObservation, error)
}
type PendingCleanupError interface {
	error
	CleanupObserver() CleanupObserver
}
