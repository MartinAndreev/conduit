package turso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/sessions"
)

func TestStorePersistenceAndUnitOfWork(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 21, 10, 11, 12, 123, time.UTC)
	record := sessions.Record{ID: "session-'?", ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: 2, AdapterSessionID: "adapter", NativeBinding: []byte(`{"native":"private"}`), State: "open", CreatedAt: now, UpdatedAt: now}
	event := events.Event{ID: "event-1", RunID: "run", Kind: "session.opened", PayloadVersion: 1, Payload: json.RawMessage(`{"state":"open"}`), Provenance: "conduit", CausationID: "command-1", ObservedAt: now}
	if err := store.WithinUnitOfWork(ctx, func(sw sessions.Writer, ea events.Appender) error {
		if _, err := sw.Save(ctx, record, 0); err != nil {
			return err
		}
		_, err := ea.Append(ctx, event)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Sessions().Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.ID != record.ID {
		t.Fatalf("unexpected record: %#v", got)
	}
	got.State, got.UpdatedAt = "idle", now.Add(time.Second)
	got.CreatedAt = now.Add(24 * time.Hour)
	got, err = store.Sessions().Save(ctx, got, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || !got.CreatedAt.Equal(now) {
		t.Fatalf("updated record = %#v", got)
	}
	reread, err := store.Sessions().Get(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reread.CreatedAt.Equal(got.CreatedAt) || reread.Version != got.Version {
		t.Fatalf("saved and returned records differ: %#v %#v", got, reread)
	}
	if _, err := store.Sessions().Save(ctx, got, 1); !errors.Is(err, sessions.ErrVersionConflict) {
		t.Fatalf("stale save error = %v", err)
	}
	duplicate := record
	duplicate.State = "different"
	if _, err := store.Sessions().Save(ctx, duplicate, 0); !errors.Is(err, sessions.ErrVersionConflict) {
		t.Fatalf("duplicate create error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	replayed, err := store.Events().Replay(ctx, "run", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0].Sequence != 1 {
		t.Fatalf("unexpected replay: %#v", replayed)
	}
}

func TestJournalOrderingIdempotencyAndConflict(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Round(0)
	repo := store.Events()
	first := events.Event{ID: "one", RunID: "run", Kind: "kind", PayloadVersion: 1, Payload: json.RawMessage(`{"n":1}`), ObservedAt: now}
	stored, err := repo.Append(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.Append(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if again.Sequence != stored.Sequence {
		t.Fatalf("dedup changed sequence: %d != %d", again.Sequence, stored.Sequence)
	}
	conflict := first
	conflict.Payload = json.RawMessage(`{"n":2}`)
	if _, err := repo.Append(ctx, conflict); !errors.Is(err, events.ErrEventConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	otherRun := first
	otherRun.RunID = "other-run"
	if _, err := repo.Append(ctx, otherRun); !errors.Is(err, events.ErrEventConflict) {
		t.Fatalf("cross-run conflict error = %v", err)
	}
	second := first
	second.ID, second.Payload = "two", json.RawMessage(`{"n":2}`)
	if _, err := repo.Append(ctx, second); err != nil {
		t.Fatal(err)
	}
	page, err := repo.Replay(ctx, "run", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != "two" || page[0].Sequence != 2 {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestJournalConcurrentSequenceAllocation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const count = 16
	start := make(chan struct{})
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := store.Events().Append(ctx, events.Event{ID: events.EventID(fmt.Sprintf("event-%02d", index)), RunID: "run", Kind: "kind", PayloadVersion: 1, Payload: json.RawMessage(`{}`), ObservedAt: time.Now().UTC()})
			errs <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Events().Replay(ctx, "run", 0, count)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != count {
		t.Fatalf("event count = %d", len(got))
	}
	for index, event := range got {
		if event.Sequence != uint64(index+1) {
			t.Fatalf("sequence[%d] = %d", index, event.Sequence)
		}
	}
}

func TestUnitOfWorkRollback(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sentinel := errors.New("stop")
	err = store.WithinUnitOfWork(ctx, func(sw sessions.Writer, ea events.Appender) error {
		now := time.Now().UTC()
		if _, err := sw.Save(ctx, sessions.Record{ID: "rolled-back", ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", State: "open", CreatedAt: now, UpdatedAt: now}, 0); err != nil {
			return err
		}
		if _, err := ea.Append(ctx, events.Event{ID: "rolled-back", RunID: "r", Kind: "kind", PayloadVersion: 1, Payload: json.RawMessage(`{}`), ObservedAt: now}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("unit error = %v", err)
	}
	if _, err := store.Sessions().Get(ctx, "rolled-back"); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("session survived rollback: %v", err)
	}
	replayed, err := store.Events().Replay(ctx, "r", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 0 {
		t.Fatalf("events survived rollback: %#v", replayed)
	}
}

func TestRejectsIncompatibleDatabaseWithoutMutation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreign.db")
	original := []byte("not a database and must remain unchanged")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path); !errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("open error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("database changed: %q", after)
	}
}

func TestRejectsPartialConduitSchemaWithoutMutation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "partial.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE event_journal"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path); !errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("open error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("validation mutated partial database")
	}
}
