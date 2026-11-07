package startupinfra

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
)

type localMissing struct{}

func (localMissing) Discover(context.Context, configuration.HarnessID, string) (configuration.Observation, error) {
	return configuration.Observation{Kind: configuration.Missing}, nil
}
func TestApplicationFilesInitialExistingAndCancelAfterPublish(t *testing.T) {
	home := t.TempDir()
	f := Files{Home: func() (string, error) { return home, nil }}
	s := startup.Service{Repository: f, Detector: localMissing{}}
	first := s.Initialize(context.Background(), nil)
	if first.Snapshot.Status != startup.ReadyWithWarnings || !first.Published {
		t.Fatal(first)
	}
	path, _ := f.Resolve(context.Background())
	before, _ := os.ReadFile(path)
	second := s.Initialize(context.Background(), nil)
	after, _ := os.ReadFile(path)
	if second.Published || !bytes.Equal(before, after) {
		t.Fatal(second)
	}
	home = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Repository = cancelAfterPublication{Files: f, cancel: cancel}
	cancelled := s.Initialize(ctx, nil)
	if cancelled.Snapshot.Status != startup.Cancelled || !cancelled.Published {
		t.Fatal(cancelled)
	}
	s.Repository = f
	retry := s.Initialize(context.Background(), nil)
	if retry.Snapshot.Status != startup.ReadyWithWarnings || retry.Published {
		t.Fatal(retry)
	}
}

type cancelAfterPublication struct {
	Files
	cancel context.CancelFunc
}

func (f cancelAfterPublication) Publish(ctx context.Context, path string, c configuration.Config) (startup.PublishKind, error) {
	kind, err := f.Files.Publish(ctx, path, c)
	if kind == startup.Published {
		f.cancel()
	}
	return kind, err
}
func TestConcurrentStartupWinnerIsValidated(t *testing.T) {
	home := t.TempDir()
	f := Files{Home: func() (string, error) { return home, nil }}
	var wg sync.WaitGroup
	results := make(chan startup.Outcome, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- (startup.Service{Repository: f, Detector: localMissing{}}).Initialize(context.Background(), nil)
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for out := range results {
		if out.Snapshot.Status != startup.ReadyWithWarnings {
			t.Fatal(out)
		}
		if out.Published {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal(winners)
	}
}

type invalidWinner struct{ Files }

func (f invalidWinner) Publish(ctx context.Context, path string, _ configuration.Config) (startup.PublishKind, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte("schema_version: invalid\n"), 0600); err != nil {
		return "", err
	}
	return startup.AlreadyExists, nil
}
func TestConcurrentInvalidWinnerRemains(t *testing.T) {
	home := t.TempDir()
	f := Files{Home: func() (string, error) { return home, nil }}
	out := (startup.Service{Repository: invalidWinner{f}, Detector: localMissing{}}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != startup.Blocked || out.Published || !errors.Is(out.Err, configuration.ErrInvalid) {
		t.Fatal(out)
	}
	path, _ := f.Resolve(context.Background())
	data, _ := os.ReadFile(path)
	if string(data) != "schema_version: invalid\n" {
		t.Fatal("invalid winner changed")
	}
}

type failingMigration struct{ cause error }

func (m failingMigration) PublishMigration(context.Context, string, startup.ConfigSource, configuration.Config) (startup.MigrationKind, error) {
	return "", m.cause
}

// Real file adapter: a v1 file whose migration write fails, and a v3 file, both
// stay byte-for-byte unchanged and block startup visibly.
func TestApplicationFilesKeepV1AndFutureSchemaUntouched(t *testing.T) {
	for name, tc := range map[string]struct {
		data string
		code string
		err  error
	}{
		"v1 write failure": {"schema_version: 1\nharnesses: {codex: {}, opencode: {}}\nassignments: {}\n", "configuration_migration_failed", errors.New("disk full")},
		"v3 future":        {"schema_version: 3\nharnesses: {codex: {}}\nassignments: {}\n", "configuration_future_schema", configuration.ErrFutureSchema},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			f := Files{Home: func() (string, error) { return home, nil }}
			path, _ := f.Resolve(context.Background())
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			cause := tc.err
			out := (startup.Service{Repository: f, Detector: localMissing{}, Migrator: failingMigration{cause}}).Initialize(context.Background(), nil)
			if out.Snapshot.Status != startup.Blocked || out.Published || !errors.Is(out.Err, cause) {
				t.Fatal(out)
			}
			diagnostics := out.Snapshot.Diagnostics
			if diagnostics[len(diagnostics)-1].Code != tc.code {
				t.Fatal(diagnostics)
			}
			after, _ := os.ReadFile(path)
			if string(after) != tc.data {
				t.Fatal("configuration changed")
			}
			entries, _ := os.ReadDir(filepath.Dir(path))
			if len(entries) != 1 {
				t.Fatal(entries)
			}
		})
	}
}

type timeoutFor struct{ harness configuration.HarnessID }

func (d timeoutFor) Discover(_ context.Context, h configuration.HarnessID, _ string) (configuration.Observation, error) {
	if h == d.harness {
		return configuration.Observation{Kind: configuration.ProbeTimeout, Path: filepath.Join(os.TempDir(), "conduit-fixture", string(h))}, nil
	}
	return configuration.Observation{Kind: configuration.Missing}, nil
}

// Real files: inconclusive discovery for a new harness leaves v1 byte-for-byte
// unchanged (no backup either) and startup continues with a warning.
func TestApplicationDefersMigrationOnInconclusiveDiscovery(t *testing.T) {
	home := t.TempDir()
	f := Files{Home: func() (string, error) { return home, nil }}
	path, _ := f.Resolve(context.Background())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(v1Data), 0600); err != nil {
		t.Fatal(err)
	}
	out := (startup.Service{Repository: f, Detector: timeoutFor{configuration.Claude}, Migrator: f}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != startup.ReadyWithWarnings || out.Published {
		t.Fatal(out)
	}
	assertFile(t, path, v1Data)
	if names := directoryNames(t, path); !slices.Equal(names, []string{"config.yaml"}) {
		t.Fatal(names)
	}
}
