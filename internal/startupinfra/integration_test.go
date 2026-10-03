package startupinfra

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
