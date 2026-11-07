// Package startup composes first-run preparation without presentation effects.
package startup

import (
	"context"
	"errors"
	"os"
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
	// Source is the file the Config was decoded from, present for a readable
	// file. Migration compares it with the target immediately before replacing.
	Source ConfigSource
}

// ConfigSource holds the exact bytes and file identity that a read decided on.
type ConfigSource struct {
	Data     []byte
	Identity os.FileInfo
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

type MigrationKind string

const (
	// MigrationReplaced: the target now holds v2. An error alongside it is a
	// post-replacement durability failure, as with Published.
	MigrationReplaced MigrationKind = "replaced"
	// MigrationSourceChanged: the target differed from Source before the
	// replacement, so nothing was written. Startup re-reads and decides again.
	MigrationSourceChanged MigrationKind = "source_changed"
)

// ConfigMigrator replaces a valid v1 configuration file with its v2 equivalent
// (spec 009, "Замяна на v1 файла"). Unless the kind is MigrationReplaced the
// original file is byte-for-byte unchanged; every failure is returned.
type ConfigMigrator interface {
	PublishMigration(ctx context.Context, path string, source ConfigSource, next configuration.Config) (MigrationKind, error)
}

// ErrMigrationBackupConflict means an existing v1 backup differs from the read
// original. It is never overwritten, so migration stops without writing.
var ErrMigrationBackupConflict = errors.New("existing configuration backup differs from the original")

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
