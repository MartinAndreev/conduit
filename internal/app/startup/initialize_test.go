package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/MartinAndreev/conduit/internal/configuration"
)

// testRoot is absolute on every OS (os.TempDir is absolute); nothing is
// created under it. Paths that configuration validation checks use it.
var testRoot = filepath.Join(os.TempDir(), "conduit-startup-test")
var explicitCodex = filepath.Join(testRoot, "explicit", "codex")

type fakeRepository struct {
	reads        []ReadResult
	events       *[]string
	publishes    int
	publishKind  PublishKind
	publishErr   error
	afterPublish func()
	proposed     configuration.Config
}

func (r *fakeRepository) Resolve(context.Context) (string, error) {
	*r.events = append(*r.events, "resolve")
	return filepath.Join("/", "home", ".conduit", "config.yaml"), nil
}
func (r *fakeRepository) Read(context.Context, string) ReadResult {
	*r.events = append(*r.events, "read")
	next := r.reads[0]
	if len(r.reads) > 1 {
		r.reads = r.reads[1:]
	}
	return next
}
func (r *fakeRepository) Publish(_ context.Context, _ string, c configuration.Config) (PublishKind, error) {
	*r.events = append(*r.events, "publish")
	r.publishes++
	r.proposed = c
	if r.afterPublish != nil {
		r.afterPublish()
	}
	if r.publishKind == "" && r.publishErr == nil {
		r.publishKind = Published
	}
	return r.publishKind, r.publishErr
}

type fakeDetector struct {
	events  *[]string
	kind    configuration.DiscoveryKind
	version string
	cancel  context.CancelFunc
	calls   int
	err     error
}

func (d *fakeDetector) Discover(_ context.Context, h configuration.HarnessID, override string) (configuration.Observation, error) {
	*d.events = append(*d.events, string(h)+":"+override)
	d.calls++
	if d.cancel != nil {
		d.cancel()
	}
	return configuration.Observation{Kind: d.kind, Path: func() string {
		if d.kind == configuration.Missing {
			return ""
		}
		return filepath.Join(testRoot, "tools", string(h))
	}(), Version: d.version}, d.err
}
func setup(t *testing.T) (*fakeRepository, *fakeDetector, *[]string) {
	t.Helper()
	events := []string{}
	r := &fakeRepository{events: &events, reads: []ReadResult{{Kind: Absent}, {Kind: Valid}}}
	d := &fakeDetector{events: &events, kind: configuration.Found, version: "1.2.3"}
	return r, d, &events
}
func TestFirstRunOrder(t *testing.T) {
	r, d, events := setup(t)
	p := NewProgress()
	result := (Service{Repository: r, Detector: d}).Initialize(context.Background(), p)
	if result.Snapshot.Status != Ready || !result.Published {
		t.Fatal(result)
	}
	if !reflect.DeepEqual(*events, []string{"resolve", "read", "codex:", "opencode:", "claude:", "fx:", "publish", "read"}) {
		t.Fatal(*events)
	}
	if len(r.proposed.Assignments) != 0 {
		t.Fatal(r.proposed)
	}
	last, ok := p.Latest()
	if !ok || last.Revision != result.Snapshot.Revision {
		t.Fatal(last)
	}
}
func TestNoHarnessWarning(t *testing.T) {
	r, d, _ := setup(t)
	d.kind = configuration.Missing
	d.version = ""
	result := (Service{Repository: r, Detector: d}).Initialize(context.Background(), nil)
	if result.Snapshot.Status != ReadyWithWarnings || len(result.Snapshot.Diagnostics) != len(configuration.Supported()) {
		t.Fatal(result)
	}
}
func TestExistingConfigUsesOverridesWithoutWrite(t *testing.T) {
	r, d, _ := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: configuration.Config{SchemaVersion: configuration.CurrentSchema, Harnesses: map[configuration.HarnessID]configuration.Harness{configuration.Codex: {Executable: "/explicit"}}}}}
	out := (Service{Repository: r, Detector: d}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Ready || r.publishes != 0 {
		t.Fatal(out)
	}
	if (*r.events)[2] != "codex:/explicit" {
		t.Fatal(*r.events)
	}
}
func TestBadInitialConfigStopsEffects(t *testing.T) {
	for _, kind := range []ReadKind{Invalid, Unreadable, FutureSchema} {
		t.Run(string(kind), func(t *testing.T) {
			r, d, _ := setup(t)
			r.reads = []ReadResult{{Kind: kind, Err: errors.New("unsafe-secret")}}
			out := (Service{Repository: r, Detector: d}).Initialize(context.Background(), nil)
			if out.Snapshot.Status != Blocked || d.calls != 0 || r.publishes != 0 {
				t.Fatal(out)
			}
			if out.Snapshot.Diagnostics[0].Details == "unsafe-secret" {
				t.Fatal("leaked error")
			}
		})
	}
}
func TestCancellationBeforeDuringAndAfterPublish(t *testing.T) {
	for _, point := range []string{"before", "probe", "publish"} {
		t.Run(point, func(t *testing.T) {
			r, d, _ := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch point {
			case "before":
				cancel()
			case "probe":
				d.cancel = cancel
			case "publish":
				r.afterPublish = cancel
			}
			out := (Service{Repository: r, Detector: d}).Initialize(ctx, nil)
			if out.Snapshot.Status != Cancelled || !errors.Is(out.Err, context.Canceled) {
				t.Fatal(out)
			}
			if point == "publish" && !out.Published {
				t.Fatal("publication lost")
			}
			if point != "publish" && r.publishes != 0 {
				t.Fatal("published after cancellation")
			}
			if point == "probe" && d.calls != 1 {
				t.Fatal(d.calls)
			}
		})
	}
}
func TestPublicationReconciliationAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       PublishKind
		publishErr error
		final      ReadKind
		published  bool
	}{{"winner", AlreadyExists, nil, Valid, false}, {"invalid winner", AlreadyExists, nil, Invalid, false}, {"failed reread", Published, nil, Unreadable, true}, {"write failure", "", errors.New("disk full"), Valid, false}, {"post publish sync failure", Published, errors.New("sync failed"), Valid, true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, d, _ := setup(t)
			r.publishKind = tc.kind
			r.publishErr = tc.publishErr
			r.reads[1] = ReadResult{Kind: tc.final, Err: errors.New("read failed")}
			out := (Service{Repository: r, Detector: d}).Initialize(context.Background(), nil)
			want := Blocked
			if tc.kind == AlreadyExists && tc.final == Valid {
				want = Ready
			}
			if out.Snapshot.Status != want || out.Published != tc.published {
				t.Fatal(out)
			}
		})
	}
}
func TestRetryCreatesNewOperationAndRereads(t *testing.T) {
	r, d, _ := setup(t)
	r.reads = []ReadResult{{Kind: Invalid}, {Kind: Valid, Config: configuration.Config{SchemaVersion: configuration.CurrentSchema}}}
	s := Service{Repository: r, Detector: d}
	first := s.Initialize(context.Background(), nil)
	second := s.Initialize(context.Background(), nil)
	if first.Snapshot.Status != Blocked || second.Snapshot.Status != Ready || first.Snapshot.OperationID == second.Snapshot.OperationID {
		t.Fatal(first, second)
	}
}
func TestMailboxCopiesAndDoesNotBlock(t *testing.T) {
	p := NewProgress()
	for i := 0; i < 10000; i++ {
		p.publish(Snapshot{Revision: uint64(i), Steps: []StepState{{Status: Completed}}})
	}
	snapshot, ok := p.Latest()
	if !ok || snapshot.Revision != 9999 {
		t.Fatal(snapshot)
	}
	snapshot.Steps[0].Status = Failed
	again, _ := p.Latest()
	if again.Steps[0].Status != Completed {
		t.Fatal("mutable snapshot")
	}
	p.Close()
	p.publish(Snapshot{})
	p.Close()
}
func TestMailboxConcurrentCloseReadPublish(t *testing.T) {
	p := NewProgress()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p.publish(Snapshot{Revision: uint64(i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p.Latest()
		}
	}()
	go func() { defer wg.Done(); p.Close() }()
	wg.Wait()
}

func TestClosedMailboxDoesNotLoseTerminalOutcome(t *testing.T) {
	r, d, _ := setup(t)
	p := NewProgress()
	p.Close()
	out := (Service{Repository: r, Detector: d}).Initialize(context.Background(), p)
	if out.Snapshot.Status != Ready {
		t.Fatal(out)
	}
}
func TestExplicitMissingHasSpecificDiagnostic(t *testing.T) {
	r, d, _ := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: configuration.Config{SchemaVersion: configuration.CurrentSchema, Harnesses: map[configuration.HarnessID]configuration.Harness{configuration.Codex: {Executable: "/explicit"}}}}}
	d.kind = configuration.Missing
	d.version = ""
	out := (Service{Repository: r, Detector: d}).Initialize(context.Background(), nil)
	if out.Snapshot.Diagnostics[0].Code != "harness_override_missing" {
		t.Fatal(out)
	}
}

func TestProbeCleanupFailureStopsStartupAndPreservesCause(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked", true: "cancelled"}[cancelled], func(t *testing.T) {
			repository, detector, _ := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("unsafe technical cause")
			detector.err = errors.Join(ErrProbeCleanup, cause)
			if cancelled {
				detector.cancel = cancel
			}
			outcome := (Service{Repository: repository, Detector: detector}).Initialize(ctx, nil)
			want := Blocked
			if cancelled {
				want = Cancelled
			}
			if outcome.Snapshot.Status != want || repository.publishes != 0 || detector.calls != 1 {
				t.Fatal(outcome, repository.publishes, detector.calls)
			}
			if !errors.Is(outcome.Err, ErrProbeCleanup) || !errors.Is(outcome.Err, cause) {
				t.Fatal("cleanup cause lost", outcome.Err)
			}
			if cancelled && !errors.Is(outcome.Err, context.Canceled) {
				t.Fatal(outcome.Err)
			}
			diagnostics := outcome.Snapshot.Diagnostics
			if len(diagnostics) != 1 || diagnostics[0].Code != "harness_probe_cleanup_unconfirmed" || diagnostics[0].Severity != Error || diagnostics[0].Harness != configuration.Codex {
				t.Fatal(diagnostics)
			}
			if diagnostics[0].Details == cause.Error() {
				t.Fatal("technical cause leaked")
			}
		})
	}
}

type testCleanupObserver struct{ done chan struct{} }

func (o *testCleanupObserver) Latest() CleanupObservation { return CleanupObservation{} }
func (o *testCleanupObserver) Done() <-chan struct{}      { return o.done }
func (o *testCleanupObserver) Await(ctx context.Context) (CleanupObservation, error) {
	return o.Latest(), ctx.Err()
}

type testPendingCleanup struct {
	observer CleanupObserver
	cause    error
}

func (e testPendingCleanup) Error() string                    { return "cleanup pending" }
func (e testPendingCleanup) Unwrap() error                    { return errors.Join(ErrProbeCleanup, e.cause) }
func (e testPendingCleanup) CleanupObserver() CleanupObserver { return e.observer }
func TestOutcomeRetainsPendingCleanupObserver(t *testing.T) {
	repository, detector, _ := setup(t)
	observer := &testCleanupObserver{done: make(chan struct{})}
	detector.err = testPendingCleanup{observer, context.DeadlineExceeded}
	outcome := (Service{Repository: repository, Detector: detector}).Initialize(context.Background(), nil)
	if outcome.PendingCleanup != observer || repository.publishes != 0 || outcome.Snapshot.Status != Blocked {
		t.Fatal(outcome)
	}
}

func TestCancelledDiscoveryPreservesLaunchCause(t *testing.T) {
	repository, detector, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("launch failed")
	detector.cancel = cancel
	detector.err = errors.Join(context.Canceled, cause)
	outcome := (Service{Repository: repository, Detector: detector}).Initialize(ctx, nil)
	if outcome.Snapshot.Status != Cancelled || !errors.Is(outcome.Err, context.Canceled) || !errors.Is(outcome.Err, cause) || repository.publishes != 0 {
		t.Fatal(outcome)
	}
}
func TestCancelledPublishedConfigurationPreservesSyncCause(t *testing.T) {
	repository, detector, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("directory sync failed after publish")
	repository.publishKind = Published
	repository.publishErr = cause
	repository.afterPublish = cancel
	outcome := (Service{Repository: repository, Detector: detector}).Initialize(ctx, nil)
	if outcome.Snapshot.Status != Cancelled || !outcome.Published || !errors.Is(outcome.Err, context.Canceled) || !errors.Is(outcome.Err, cause) {
		t.Fatal(outcome)
	}
}

type fakeMigrator struct {
	events  *[]string
	calls   int
	next    configuration.Config
	source  ConfigSource
	results []MigrationKind
	err     error
	file    *[]byte
}

// The fake returns results in order; the last one repeats.
func (m *fakeMigrator) PublishMigration(_ context.Context, _ string, source ConfigSource, next configuration.Config) (MigrationKind, error) {
	*m.events = append(*m.events, "migrate")
	m.calls++
	m.next = next
	m.source = source
	kind := m.results[0]
	if len(m.results) > 1 {
		m.results = m.results[1:]
	}
	if m.err == nil && kind == MigrationReplaced && m.file != nil {
		*m.file = []byte("schema_version: 2\n")
	}
	return kind, m.err
}

func v1Config() configuration.Config {
	return configuration.Config{SchemaVersion: configuration.SchemaV1, Assignments: map[string]string{}, Harnesses: map[configuration.HarnessID]configuration.Harness{
		configuration.Codex:    {Executable: explicitCodex, InitialDiscovery: &configuration.Observation{Kind: configuration.Found, Path: explicitCodex, Version: "0.1.0"}},
		configuration.OpenCode: {},
	}}
}

func TestV1MigrationRecordsOnlyMissingHarnesses(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: v1Config()}, {Kind: Valid}}
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Ready || !out.Published || r.publishes != 0 || m.calls != 1 {
		t.Fatal(out, r.publishes, m.calls)
	}
	// One discovery pass per supported harness, then the migration write and
	// the final reread. No extra probes run for the migration.
	if !reflect.DeepEqual(*events, []string{"resolve", "read", "codex:" + explicitCodex, "opencode:", "claude:", "fx:", "migrate", "read"}) {
		t.Fatal(*events)
	}
	original := v1Config()
	if m.next.SchemaVersion != configuration.CurrentSchema || !reflect.DeepEqual(m.next.Harnesses[configuration.Codex], original.Harnesses[configuration.Codex]) || !reflect.DeepEqual(m.next.Harnesses[configuration.OpenCode], original.Harnesses[configuration.OpenCode]) {
		t.Fatal("existing v1 entries changed", m.next)
	}
	for _, id := range []configuration.HarnessID{configuration.Claude, configuration.FX} {
		o := m.next.Harnesses[id].InitialDiscovery
		if o == nil || o.Kind != configuration.Found || o.Path != filepath.Join(testRoot, "tools", string(id)) {
			t.Fatal(id, o)
		}
	}
	if out.Snapshot.Steps[2].Status != Completed {
		t.Fatal(out.Snapshot.Steps)
	}
}

func TestV1MigrationFailureKeepsOriginalAndIsVisible(t *testing.T) {
	for name, m := range map[string]*fakeMigrator{
		"write failure":   {err: errors.New("disk full"), results: []MigrationKind{""}},
		"backup conflict": {err: ErrMigrationBackupConflict, results: []MigrationKind{""}},
		"unknown result":  {results: []MigrationKind{"other"}},
		"no migrator":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			r, d, events := setup(t)
			original := []byte("schema_version: 1\nharnesses: {codex: {}, opencode: {}}\nassignments: {}\n")
			file := append([]byte(nil), original...)
			r.reads = []ReadResult{{Kind: Valid, Config: v1Config()}}
			s := Service{Repository: r, Detector: d}
			if m != nil {
				m.events = events
				m.file = &file
				s.Migrator = m
			}
			out := s.Initialize(context.Background(), nil)
			if out.Snapshot.Status != Blocked || out.Published || out.Err == nil || r.publishes != 0 {
				t.Fatal(out)
			}
			if string(file) != string(original) {
				t.Fatal("v1 file changed")
			}
			if m != nil && m.err != nil && !errors.Is(out.Err, m.err) {
				t.Fatal("cause lost", out.Err)
			}
			last := out.Snapshot.Diagnostics[len(out.Snapshot.Diagnostics)-1]
			want := "configuration_migration_failed"
			if m == nil {
				want = "configuration_migration_unavailable"
			} else if errors.Is(m.err, ErrMigrationBackupConflict) {
				want = "configuration_migration_backup_conflict"
			}
			if last.Code != want || last.Severity != Error || last.Step != CreateSettings {
				t.Fatal(last)
			}
		})
	}
}

func TestV1MigrationStopsOnDiscoveryFailure(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: v1Config()}}
	d.err = errors.Join(ErrProbeCleanup, errors.New("cleanup"))
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Blocked || m.calls != 0 || out.Published {
		t.Fatal(out, m.calls)
	}
}

func TestV1MigrationCancelledBeforeWrite(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: v1Config()}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.cancel = cancel
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(ctx, nil)
	if out.Snapshot.Status != Cancelled || m.calls != 0 {
		t.Fatal(out, m.calls)
	}
}

func TestCurrentAndPartialV2DoNotMigrate(t *testing.T) {
	r, d, events := setup(t)
	partial := configuration.Config{SchemaVersion: configuration.CurrentSchema, Assignments: map[string]string{}, Harnesses: map[configuration.HarnessID]configuration.Harness{configuration.Codex: {}}}
	r.reads = []ReadResult{{Kind: Valid, Config: partial}, {Kind: Valid}}
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Ready || m.calls != 0 || r.publishes != 0 || out.Snapshot.Steps[2].Status != Skipped {
		t.Fatal(out)
	}
}

func TestMigrationRuleRejectsUndefinedValidVersions(t *testing.T) {
	for name, version := range map[string]int{"future": configuration.CurrentSchema + 1, "zero": 0} {
		t.Run(name, func(t *testing.T) {
			r, d, events := setup(t)
			r.reads = []ReadResult{{Kind: Valid, Config: configuration.Config{SchemaVersion: version}}}
			m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
			out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
			want := "configuration_invalid"
			if version > configuration.CurrentSchema {
				want = "configuration_future_schema"
			}
			if out.Snapshot.Status != Blocked || d.calls != 0 || m.calls != 0 || out.Snapshot.Diagnostics[0].Code != want {
				t.Fatal(out)
			}
		})
	}
}

func v1Read() ReadResult {
	return ReadResult{Kind: Valid, Config: v1Config(), Source: ConfigSource{Data: []byte("v1 bytes")}}
}

func TestV1MigrationPassesReadSource(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{v1Read(), {Kind: Valid}}
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Ready || string(m.source.Data) != "v1 bytes" {
		t.Fatal(out, m.source)
	}
}

func TestV1MigrationSourceChangedRereadsOnce(t *testing.T) {
	current := ReadResult{Kind: Valid, Config: configuration.Config{SchemaVersion: configuration.CurrentSchema}}
	for name, tc := range map[string]struct {
		reread   ReadResult
		results  []MigrationKind
		status   Status
		code     string
		migrates int
		events   []string
	}{
		"other instance migrated": {current, []MigrationKind{MigrationSourceChanged}, Ready, "", 1,
			[]string{"resolve", "read", "codex:" + explicitCodex, "opencode:", "claude:", "fx:", "migrate", "read", "read"}},
		"still v1 then replaced": {v1Read(), []MigrationKind{MigrationSourceChanged, MigrationReplaced}, Ready, "", 2,
			[]string{"resolve", "read", "codex:" + explicitCodex, "opencode:", "claude:", "fx:", "migrate", "read", "migrate", "read"}},
		"changed twice": {v1Read(), []MigrationKind{MigrationSourceChanged}, Blocked, "configuration_migration_conflict", 2,
			[]string{"resolve", "read", "codex:" + explicitCodex, "opencode:", "claude:", "fx:", "migrate", "read", "migrate"}},
		"now invalid": {ReadResult{Kind: Invalid, Err: configuration.ErrInvalid}, []MigrationKind{MigrationSourceChanged}, Blocked, "configuration_invalid", 1,
			[]string{"resolve", "read", "codex:" + explicitCodex, "opencode:", "claude:", "fx:", "migrate", "read"}},
		"now deleted": {ReadResult{Kind: Absent}, []MigrationKind{MigrationSourceChanged}, Ready, "", 1,
			[]string{"resolve", "read", "codex:" + explicitCodex, "opencode:", "claude:", "fx:", "migrate", "read", "publish", "read"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, d, events := setup(t)
			r.reads = []ReadResult{v1Read(), tc.reread, current}
			m := &fakeMigrator{events: events, results: tc.results}
			out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
			if out.Snapshot.Status != tc.status || m.calls != tc.migrates || d.calls != len(configuration.Supported()) {
				t.Fatal(out, m.calls, d.calls)
			}
			if !reflect.DeepEqual(*events, tc.events) {
				t.Fatal(*events)
			}
			if tc.code != "" {
				last := out.Snapshot.Diagnostics[len(out.Snapshot.Diagnostics)-1]
				if last.Code != tc.code || last.Step != CreateSettings {
					t.Fatal(last)
				}
			}
		})
	}
}

func TestV1MigrationDurabilityFailureIsReplaced(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{v1Read()}
	cause := errors.New("directory sync failed")
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}, err: cause}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Blocked || !out.Published || !errors.Is(out.Err, cause) {
		t.Fatal(out)
	}
	last := out.Snapshot.Diagnostics[len(out.Snapshot.Diagnostics)-1]
	if last.Code != "configuration_migration_unconfirmed" || last.Step != CreateSettings {
		t.Fatal(last)
	}
}

type cancellingMigrator struct {
	*fakeMigrator
	cancel context.CancelFunc
}

func (m cancellingMigrator) PublishMigration(ctx context.Context, path string, source ConfigSource, next configuration.Config) (MigrationKind, error) {
	kind, err := m.fakeMigrator.PublishMigration(ctx, path, source, next)
	m.cancel()
	return kind, err
}

func TestV1MigrationDurabilityFailureWithCancellationKeepsDiagnostic(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{v1Read()}
	cause := errors.New("directory sync failed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := cancellingMigrator{&fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}, err: cause}, cancel}
	out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(ctx, nil)
	if out.Snapshot.Status != Cancelled || !out.Published || !errors.Is(out.Err, cause) || !errors.Is(out.Err, context.Canceled) {
		t.Fatal(out)
	}
	last := out.Snapshot.Diagnostics[len(out.Snapshot.Diagnostics)-1]
	if last.Code != "configuration_migration_unconfirmed" || last.Step != CreateSettings || out.Snapshot.Steps[2].Status != StepCancelled {
		t.Fatal(out.Snapshot)
	}
}

// Inconclusive discovery for a harness the migration would record defers the
// migration: no write, a visible warning, and startup continues on v1.
func TestV1MigrationDeferredOnInconclusiveDiscovery(t *testing.T) {
	for _, kind := range []configuration.DiscoveryKind{configuration.ProbeFailed, configuration.ProbeTimeout} {
		t.Run(string(kind), func(t *testing.T) {
			r, d, events := setup(t)
			d.kind = kind
			d.version = ""
			r.reads = []ReadResult{v1Read(), v1Read()}
			m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
			out := (Service{Repository: r, Detector: d, Migrator: m}).Initialize(context.Background(), nil)
			if out.Snapshot.Status != ReadyWithWarnings || m.calls != 0 || r.publishes != 0 || out.Published || out.Err != nil {
				t.Fatal(out, m.calls)
			}
			if out.Snapshot.Steps[2].Status != Skipped {
				t.Fatal(out.Snapshot.Steps)
			}
			deferred := 0
			for _, diagnostic := range out.Snapshot.Diagnostics {
				if diagnostic.Code == "configuration_migration_deferred" && diagnostic.Severity == Warning && diagnostic.Step == CreateSettings {
					deferred++
				}
			}
			if deferred != 1 {
				t.Fatal(out.Snapshot.Diagnostics)
			}
		})
	}
}

// Inconclusive discovery of a harness that already has a v1 entry is not
// recorded by the migration and does not defer it.
func TestV1MigrationIgnoresInconclusiveExistingHarness(t *testing.T) {
	r, d, events := setup(t)
	r.reads = []ReadResult{v1Read(), {Kind: Valid}}
	failing := &kindByHarness{fakeDetector: d, kinds: map[configuration.HarnessID]configuration.DiscoveryKind{configuration.Codex: configuration.ProbeFailed}}
	m := &fakeMigrator{events: events, results: []MigrationKind{MigrationReplaced}}
	out := (Service{Repository: r, Detector: failing, Migrator: m}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != ReadyWithWarnings || m.calls != 1 || !out.Published {
		t.Fatal(out, m.calls)
	}
}

type kindByHarness struct {
	*fakeDetector
	kinds map[configuration.HarnessID]configuration.DiscoveryKind
}

func (d *kindByHarness) Discover(ctx context.Context, h configuration.HarnessID, override string) (configuration.Observation, error) {
	o, err := d.fakeDetector.Discover(ctx, h, override)
	if kind, ok := d.kinds[h]; ok {
		o.Kind = kind
		o.Version = ""
	}
	return o, err
}
