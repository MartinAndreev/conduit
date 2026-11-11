package turso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
)

// noFiles is a snapshot the composed records never consult: they have no
// file sources.
type noFiles struct{}

func (noFiles) ObserveFile(context.Context, compose.SnapshotRef, string) (compose.FileObservation, error) {
	return compose.FileObservation{Observation: memory.SourceMissing}, nil
}

// Through the real reader, an oversized linked record blocks composition
// without its text ever being loaded, and one within the limit is composed.
func TestComposeWithTursoReaderBlocksOversizedLinkedRecord(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	for _, record := range []struct{ id, content string }{
		{"within", "Решение: паметта е локална"},
		{"over", strings.Repeat("п", memory.MaxMaterialBytes/2+1)},
	} {
		fixture := newMemoryFixture(record.id, "project")
		sum := sha256.Sum256([]byte(record.content))
		fixture.version.Content, fixture.version.SearchText = &record.content, optional(normalizeSearchText(record.content))
		fixture.version.ContentHash = "sha256:" + hex.EncodeToString(sum[:])
		mustInsertMemory(t, store.db, fixture)
		if err := insertSource(ctx, store.db, model.MemorySources{ID: record.id + "-spec", VersionID: fixture.version.ID, Kind: string(memory.SourceSpec), Ref: optional("specs/012")}); err != nil {
			t.Fatal(err)
		}
		mustReview(t, store, newReview(record.id+"-approval", fixture.version, memory.ActorHuman, memory.ReviewApprove))
	}
	composer, err := compose.New(store.MemoryReader(), noFiles{}, redaction.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	run := func(id string) compose.Composition {
		t.Helper()
		got, err := composer.ComposeContext(ctx, compose.Request{ProjectID: "project", Snapshot: "snapshot", ExplicitRecordIDs: []string{id}, Marker: marker})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := run("over"); got.Kind != compose.BlockedMandatoryOversized || got.Items != nil {
		t.Fatalf("oversized linked record: %s %s", got.Kind, got.Reason)
	}
	if got := run("within"); got.Kind != compose.Composed || !strings.Contains(got.Part.Text, "паметта е локална") {
		t.Fatalf("linked record within the limit: %s %s", got.Kind, got.Reason)
	}
}

// fakeToken has a GitHub token's shape and no real value. It matches the
// record id grammar, so the schema stores it as an id.
var fakeToken = "ghp_" + strings.Repeat("A", 36)

// Reader failures reach the composition (search) or the caller (linked read)
// classified by sentinel errors, never with a stored id or value in them.
func TestReaderFailuresDoNotEchoStoredValues(t *testing.T) {
	ctx := context.Background()
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed := func(t *testing.T, store *Store, change func(*memoryFixture)) {
		t.Helper()
		fixture := newMemoryFixture("holder", "project")
		fixture.version.ID, fixture.record.CurrentVersionID = fakeToken, fakeToken
		change(&fixture)
		mustInsertMemory(t, store.db, fixture)
	}
	for _, tc := range []struct {
		name   string
		change func(*memoryFixture)
		want   error
	}{
		{"normalization mismatch", func(f *memoryFixture) { f.version.SearchNormalizationVersion = searchNormalizationVersion + 1 }, ErrSearchNormalizationMismatch},
		{"stored time is the token", func(f *memoryFixture) { f.version.CreatedAt = fakeToken }, ErrInvalidStoredTimestamp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openMemoryStore(t)
			seed(t, store, tc.change)
			composer, err := compose.New(store.MemoryReader(), noFiles{}, redaction.Policy{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := composer.ComposeContext(ctx, compose.Request{ProjectID: "project", Snapshot: "snapshot", Query: "паметта", Marker: marker})
			if err != nil {
				t.Fatal(err)
			}
			if got.Search.Status != compose.SearchDegraded || !errors.Is(got.Search.Cause, tc.want) {
				t.Fatalf("search = %+v", got.Search)
			}
			for _, format := range []string{"%v", "%+v", "%#v"} {
				if strings.Contains(fmt.Sprintf(format, got), fakeToken) || strings.Contains(fmt.Sprintf(format, got.Search.Cause), fakeToken) {
					t.Fatalf("composition printed with %s repeats the stored token", format)
				}
			}
		})
	}
	t.Run("linked read", func(t *testing.T) {
		store := openMemoryStore(t)
		seed(t, store, func(f *memoryFixture) { f.version.CreatedAt = fakeToken })
		composer, err := compose.New(store.MemoryReader(), noFiles{}, redaction.Policy{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = composer.ComposeContext(ctx, compose.Request{ProjectID: "project", Snapshot: "snapshot", ExplicitRecordIDs: []string{"holder"}, Marker: marker})
		if !errors.Is(err, ErrInvalidStoredTimestamp) {
			t.Fatalf("linked read error = %v", err)
		}
		if strings.Contains(err.Error(), fakeToken) || strings.Contains(fmt.Sprintf("%#v", err), fakeToken) {
			t.Fatal("linked read error repeats the stored token")
		}
	})
}
