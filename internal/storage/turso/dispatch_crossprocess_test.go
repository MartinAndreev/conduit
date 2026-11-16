package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/childprocess"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
)

const (
	dispatchChildPathVariable = "CONDUIT_TURSO_DISPATCH_CHILD_PATH"
	v1WriterChildPathVariable = "CONDUIT_TURSO_V1_WRITER_CHILD_PATH"
)

// childLine returns the detail of the child's CHILD-RESULT line.
func childLine(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if detail, ok := strings.CutPrefix(line, "CHILD-RESULT "); ok {
			return detail
		}
	}
	t.Fatalf("child reported nothing:\n%s", output)
	return ""
}

// TestChildProcessDispatch is the child of the double-claim test: it opens
// the shared database with multi-process WAL, waits at the start barrier and
// repeats the stored intent "shared-start" with a counting harness.
func TestChildProcessDispatch(t *testing.T) {
	path := os.Getenv(dispatchChildPathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	ctx := context.Background()
	store, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		fmt.Printf("CHILD-RESULT failed open: %v\n", err)
		return
	}
	defer store.Close()
	if err := childprocess.AwaitBegin(); err != nil {
		t.Fatal(err)
	}
	harness := &fakeHarness{}
	result, err := run(dispatchService(store), harness, startCommandFor("shared-start", "s1"))
	if err != nil {
		fmt.Printf("CHILD-RESULT failed dispatch: %v\n", err)
	} else {
		fmt.Printf("CHILD-RESULT kind=%s calls=%d\n", result.Kind, harness.calls())
	}
	if err := childprocess.Signal(childReported); err != nil {
		t.Error(err)
	}
	if err := childprocess.AwaitRelease(); err != nil {
		t.Error(err)
	}
}

// With multi-process WAL, two processes repeating one recorded intent claim
// it once: exactly one sends; the other finds it claimed or settled.
func TestCrossProcessDoubleClaimSendsOnce(t *testing.T) {
	requireMultiprocessPlatform(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shared.db")
	parent, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	seedDispatchSession(t, parent, "s1")
	crashed := dispatch.Service{Store: crashingStore{Store: parent.Dispatch(), claim: true}, Now: func() time.Time { return dispatchNow }}
	if _, err := run(crashed, &fakeHarness{}, startCommandFor("shared-start", "s1")); !errors.Is(err, errCrash) {
		t.Fatal(err)
	}
	env := dispatchChildPathVariable + "=" + path
	first := childprocess.Start(t, "TestChildProcessDispatch", env)
	second := childprocess.Start(t, "TestChildProcessDispatch", env)
	childprocess.BeginTogether(first, second)
	first.Await(childReported)
	second.Await(childReported)
	lines := []string{childLine(t, first.Finish()), childLine(t, second.Finish())}
	sent, other := 0, 0
	for _, line := range lines {
		switch line {
		case "kind=dispatched calls=1":
			sent++
		case "kind=delivery_unknown calls=0", "kind=recorded_outcome calls=0":
			other++
		}
	}
	if sent != 1 || other != 1 {
		t.Fatalf("children = %q", lines)
	}
	if got := intentState(t, parent, "shared-start"); got != "accepted" {
		t.Fatalf("state = %s", got)
	}
}

// TestChildProcessV1Writer is a v1 process: it opens the v1 database the way
// v1 did (no schema handling) with multi-process WAL and reads its session
// before the parent migrates, then, after the barrier, changes the session's
// assignment with the v1 UPDATE.
func TestChildProcessV1Writer(t *testing.T) {
	path := os.Getenv(v1WriterChildPathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	ctx := context.Background()
	connector, err := engine.NewConnector(path, Options{MultiprocessWAL: true}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	v1 := sessionRepository{db: db}
	if _, err := v1.Get(ctx, "v1-session"); err != nil {
		fmt.Printf("CHILD-RESULT failed read: %v\n", err)
		return
	}
	if err := childprocess.AwaitBegin(); err != nil {
		t.Fatal(err)
	}
	record, err := v1.Get(ctx, "v1-session")
	if err == nil {
		record.AssignmentID = "replaced-by-v1"
		_, err = v1.Save(ctx, record, record.Version)
	}
	switch {
	case err == nil:
		fmt.Println("CHILD-RESULT saved")
	case strings.Contains(err.Error(), schema.BindingLockedMessage):
		fmt.Println("CHILD-RESULT refused")
	default:
		fmt.Printf("CHILD-RESULT failed %v\n", err)
	}
	if err := childprocess.Signal(childReported); err != nil {
		t.Error(err)
	}
	if err := childprocess.AwaitRelease(); err != nil {
		t.Error(err)
	}
}

// A v1 process that opened the database before the migration is bound by
// the trigger the migration created: its binding change between the claim
// and the outcome is refused by the database in its own process; the
// outcome then moves the revision.
func TestCrossProcessOldV1WriterIsRefused(t *testing.T) {
	requireMultiprocessPlatform(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	child := childprocess.Start(t, "TestChildProcessV1Writer", v1WriterChildPathVariable+"="+path)
	child.Await(childprocess.Ready)
	store, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := schemaVersion(t, store.db); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
	start := startCommandFor("start-v1", "v1-session")
	start.Session.AdapterSessionID, start.Current.Revision = "adapter", 1
	turn := mustDispatch(t, dispatchService(store), &fakeHarness{}, start).Outcome.AdapterTurnID
	steer := steerCommandFor("steer-v1", sessions.ID("v1-session"), turn)
	steer.Session.AdapterSessionID, steer.Current.Revision, steer.New.Revision = "adapter", 1, 2
	harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
		child.Begin()
		child.Await(childReported)
		return codexReceipt(r), nil
	}}
	result := mustDispatch(t, dispatchService(store), harness, steer)
	if line := childLine(t, child.Finish()); line != "refused" {
		t.Fatalf("v1 child = %q", line)
	}
	got, err := store.Sessions().Get(ctx, "v1-session")
	if err != nil || result.Outcome.Classification != execution.ClassAdvanced || got.Revision != 2 || got.AssignmentID != "assignment" {
		t.Fatalf("session %+v, %v; result %+v", got, err, result)
	}
}
