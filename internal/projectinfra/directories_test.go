package projectinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	domain "github.com/MartinAndreev/conduit/internal/projects"
)

var _ app.Directories = Directories{}

func TestResolveAliasesAndExplicitBase(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "MiXeD")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	directories := Directories{}
	original, err := directories.Resolve(context.Background(), project, "")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(original.CanonicalPath) || original.Name != "MiXeD" || original.Kind != domain.Available {
		t.Fatal(original)
	}
	relative, err := directories.Resolve(context.Background(), filepath.Join(".", "MiXeD"), base)
	if err != nil || relative.CanonicalPath != original.CanonicalPath {
		t.Fatal(relative, err)
	}
	for _, alias := range []string{filepath.Join(base, "MiXeD", "..", "MiXeD"), project + string(filepath.Separator)} {
		got, err := directories.Resolve(context.Background(), alias, "")
		if err != nil || got.CanonicalPath != original.CanonicalPath {
			t.Fatal(got, err)
		}
	}
	link := filepath.Join(base, "alias")
	if err := os.Symlink(project, link); err != nil {
		t.Logf("symlink unavailable on this platform: %v", err)
	} else {
		got, err := directories.Resolve(context.Background(), link, "")
		if err != nil || got.CanonicalPath != original.CanonicalPath || got.Name != original.Name {
			t.Fatal(got, err)
		}
	}
	// Detect the actual filesystem's case behavior rather than assuming it from OS.
	caseAlias := filepath.Join(base, strings.ToUpper(filepath.Base(project)))
	if _, err := os.Stat(caseAlias); err == nil {
		got, err := directories.Resolve(context.Background(), caseAlias, "")
		if err != nil || got.CanonicalPath != original.CanonicalPath {
			t.Fatal(got, err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(caseAlias, 0700); err != nil {
			t.Fatal(err)
		}
		got, err := directories.Resolve(context.Background(), caseAlias, "")
		if err != nil || got.CanonicalPath == original.CanonicalPath {
			t.Fatal("case-sensitive directories merged", got, err)
		}
	} else {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(project)
	if err != nil || len(entries) != 0 {
		t.Fatal("directory queries wrote project files", entries, err)
	}
}

func TestDistinctDirectoriesWithSameName(t *testing.T) {
	var paths []string
	for i := 0; i < 2; i++ {
		path := filepath.Join(t.TempDir(), "same-name")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		got, err := (Directories{}).Resolve(context.Background(), path, "")
		if err != nil || got.Name != "same-name" {
			t.Fatal(got, err)
		}
		paths = append(paths, got.CanonicalPath)
	}
	if paths[0] == paths[1] {
		t.Fatal("distinct directory identities merged")
	}
}

func TestDirectoryAvailability(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input, base string
		kind              domain.AvailabilityKind
		cause             error
	}{
		{name: "empty", kind: domain.InvalidPath, cause: ErrInvalidPath},
		{name: "whitespace", input: "  ", kind: domain.InvalidPath, cause: ErrInvalidPath},
		{name: "relative without base", input: "project", kind: domain.InvalidPath, cause: ErrInvalidPath},
		{name: "relative base", input: "project", base: "relative", kind: domain.InvalidPath, cause: ErrInvalidPath},
		{name: "file", input: file, kind: domain.NotDirectory, cause: ErrNotDirectory},
		{name: "missing", input: filepath.Join(base, "missing"), kind: domain.Missing, cause: os.ErrNotExist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (Directories{}).Resolve(context.Background(), tc.input, tc.base)
			if got.Kind != tc.kind || !errors.Is(err, tc.cause) || !errors.Is(got.Cause, tc.cause) {
				t.Fatal(got, err)
			}
		})
	}
	got, err := (Directories{}).Inspect(context.Background(), base)
	if err != nil || got.Kind != domain.Available || got.CanonicalPath != base {
		t.Fatal(got, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	got, err = (Directories{}).Inspect(context.Background(), file)
	if got.Kind != domain.Missing || !errors.Is(err, os.ErrNotExist) {
		t.Fatal(got, err)
	}
}

func TestCancellationAndPermissionBoundaries(t *testing.T) {
	base := t.TempDir()
	info, err := os.Stat(base)
	if err != nil {
		t.Fatal(err)
	}
	cause := &os.PathError{Op: "read", Path: base, Err: os.ErrPermission}
	for _, tc := range []struct {
		name, phase string
		cancel      bool
		cause       error
	}{
		{name: "permission stat", phase: "stat", cause: cause},
		{name: "permission directory read", phase: "read", cause: cause},
		{name: "cancelled before stat", phase: "before", cancel: true},
		{name: "stat cancellation", phase: "stat", cancel: true},
		{name: "stat error and cancellation", phase: "stat", cancel: true, cause: cause},
		{name: "read cancellation", phase: "read", cancel: true},
		{name: "read error and cancellation", phase: "read", cancel: true, cause: cause},
		{name: "symlink failure", phase: "eval", cause: cause},
		{name: "symlink cancellation", phase: "eval", cancel: true},
		{name: "symlink error and cancellation", phase: "eval", cancel: true, cause: cause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stats, reads, evals := 0, 0, 0
			if tc.phase == "before" {
				cancel()
			}
			source := directorySource{
				stat: func(string) (os.FileInfo, error) {
					stats++
					if tc.phase == "stat" {
						if tc.cancel {
							cancel()
						}
						return info, tc.cause
					}
					return info, nil
				},
				readDir: func(string) ([]os.DirEntry, error) {
					reads++
					if tc.phase == "read" {
						if tc.cancel {
							cancel()
						}
						return nil, tc.cause
					}
					return nil, nil
				},
				evalSymlinks: func(string) (string, error) {
					evals++
					if tc.cancel {
						cancel()
					}
					return base, tc.cause
				},
			}
			got, err := (Directories{source: &source}).Resolve(ctx, base, "")
			if err == nil || got.Cause == nil {
				t.Fatal(got, err)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if tc.cause != nil {
				var native *os.PathError
				if !errors.Is(err, os.ErrPermission) || !errors.As(err, &native) || native != cause {
					t.Fatal("lost native cause", err)
				}
			}
			switch tc.phase {
			case "before":
				if stats+reads+evals != 0 {
					t.Fatal(stats, reads, evals)
				}
			case "stat":
				if stats != 1 || reads+evals != 0 {
					t.Fatal(stats, reads, evals)
				}
			case "read":
				if stats != 1 || reads != 1 || evals != 0 {
					t.Fatal(stats, reads, evals)
				}
			case "eval":
				if stats != 1 || reads != 1 || evals != 1 || got.Kind != domain.CanonicalizationFailed {
					t.Fatal(got, stats, reads, evals)
				}
			}
		})
	}
}

func TestRealPermissionFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unreadable")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Skipf("permissions not supported: %v", err)
	}
	defer os.Chmod(path, 0700)
	if _, err := os.ReadDir(path); err == nil {
		t.Skip("current platform/user bypasses directory permissions; deterministic permission test still runs")
	}
	got, err := (Directories{}).Resolve(context.Background(), path, "")
	if got.Kind != domain.Unreadable || !errors.Is(err, os.ErrPermission) {
		t.Fatal(got, err)
	}
}

func TestResolveRejectsFileReplacementDuringSymlinkResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	source := directorySource{
		stat:    os.Stat,
		readDir: os.ReadDir,
		evalSymlinks: func(input string) (string, error) {
			// Initial inspection has completed. Replace the empty directory with a
			// regular file before canonicalization observes its final identity.
			if err := os.Remove(input); err != nil {
				return "", err
			}
			if err := os.WriteFile(input, []byte("replacement"), 0600); err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(input)
		},
	}
	directory, err := (Directories{source: &source}).Resolve(context.Background(), path, "")
	if directory.Kind != domain.NotDirectory || !errors.Is(err, ErrNotDirectory) || !errors.Is(directory.Cause, ErrNotDirectory) {
		t.Fatalf("replacement accepted: directory=%+v error=%v", directory, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("query changed replacement: data=%q error=%v", data, err)
	}
}
