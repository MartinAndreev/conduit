package startupinfra

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MartinAndreev/conduit/internal/configuration"
)

func TestExecutableLookupFailureDistinctions(t *testing.T) {
	permission := &os.PathError{Op: "lookup", Path: "codex", Err: os.ErrPermission}
	for _, tc := range []struct {
		name    string
		cause   error
		cancel  bool
		missing bool
	}{
		{name: "permission", cause: permission},
		{name: "permission and cancellation", cause: permission, cancel: true},
		{name: "PATH absence", cause: &exec.Error{Name: "codex", Err: exec.ErrNotFound}, missing: true},
		{name: "explicit absence", cause: &os.PathError{Op: "stat", Path: "/missing", Err: os.ErrNotExist}, missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			launcher := &cancellationLauncher{}
			calls := 0
			override := ""
			if tc.name == "explicit absence" {
				override = filepath.Join(t.TempDir(), "missing")
			}
			detector := Detector{Launcher: launcher, LookPath: func(name string) (string, error) {
				calls++
				if override != "" && name != override {
					t.Fatalf("unexpected fallback: %q", name)
				}
				if tc.cancel {
					cancel()
				}
				return "", tc.cause
			}}
			observation, err := detector.Discover(ctx, configuration.Codex, override)
			if tc.missing {
				if err != nil || observation.Kind != configuration.Missing {
					t.Fatalf("observation=%+v error=%v", observation, err)
				}
			} else {
				var native *os.PathError
				if !errors.Is(err, os.ErrPermission) || !errors.As(err, &native) || native != permission {
					t.Fatalf("lost lookup cause: %v", err)
				}
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if calls != 1 || launcher.calls != 0 {
				t.Fatalf("lookups=%d launches=%d", calls, launcher.calls)
			}
		})
	}
}

type boundaryReadFile struct {
	info                             os.FileInfo
	cancel                           context.CancelFunc
	statErr                          error
	closeErr                         error
	cancelAt                         string
	statCalls, readCalls, closeCalls int
}

func (f *boundaryReadFile) Stat() (os.FileInfo, error) {
	f.statCalls++
	if f.cancelAt == "stat" {
		f.cancel()
	}
	return f.info, f.statErr
}
func (f *boundaryReadFile) Read([]byte) (int, error) {
	f.readCalls++
	return 0, errors.New("unexpected read")
}
func (f *boundaryReadFile) Close() error { f.closeCalls++; return f.closeErr }

func TestFileQueryCancellationAtIOBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	cause := &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	closeCause := errors.New("close failure")
	for _, tc := range []struct {
		name, boundary     string
		cause, closeErr    error
		wantOpen, wantStat int
	}{
		{name: "Lstat stops Open", boundary: "lstat"},
		{name: "Lstat preserves failure", boundary: "lstat", cause: cause},
		{name: "Open stops Stat and closes", boundary: "open", wantOpen: 1},
		{name: "Stat stops Read and closes", boundary: "stat", wantOpen: 1, wantStat: 1},
		{name: "Stat preserves failure and closes", boundary: "stat", cause: cause, wantOpen: 1, wantStat: 1},
		{name: "Stat preserves cleanup failure", boundary: "stat", cause: cause, closeErr: closeCause, wantOpen: 1, wantStat: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := &boundaryReadFile{info: info, cancel: cancel, cancelAt: tc.boundary, closeErr: tc.closeErr}
			if tc.boundary == "stat" {
				file.statErr = tc.cause
			}
			opens := 0
			source := configurationFileSource{
				lstat: func(string) (os.FileInfo, error) {
					if tc.boundary == "lstat" {
						cancel()
						return info, tc.cause
					}
					return info, nil
				},
				open: func(string) (configurationFile, error) {
					opens++
					if tc.boundary == "open" {
						cancel()
					}
					return file, nil
				},
			}
			facts := readConfigurationFile(ctx, path, source)
			if facts.kind != fileUnreadable || !errors.Is(facts.err, context.Canceled) {
				t.Fatalf("facts=%+v", facts)
			}
			if tc.cause != nil {
				var native *os.PathError
				if !errors.Is(facts.err, tc.cause) || !errors.As(facts.err, &native) {
					t.Fatalf("lost technical cause: %v", facts.err)
				}
			}
			if tc.closeErr != nil && !errors.Is(facts.err, tc.closeErr) {
				t.Fatalf("lost close cause: %v", facts.err)
			}
			if opens != tc.wantOpen || file.statCalls != tc.wantStat || file.readCalls != 0 || file.closeCalls != tc.wantOpen {
				t.Fatalf("opens=%d stats=%d reads=%d closes=%d", opens, file.statCalls, file.readCalls, file.closeCalls)
			}
		})
	}
}
