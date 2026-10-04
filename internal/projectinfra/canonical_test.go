package projectinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type observedEntry struct {
	os.DirEntry
	info func() (os.FileInfo, error)
}

func (e observedEntry) Info() (os.FileInfo, error) { return e.info() }

func TestCanonicalIdentityReadBoundaries(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "RealCase")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	target, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	cause := &os.PathError{Op: "readdir", Path: base, Err: os.ErrPermission}
	for _, phase := range []string{"stat", "read", "entry"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stats, reads, infos := 0, 0, 0
			source := directorySource{
				stat: func(string) (os.FileInfo, error) {
					stats++
					if phase == "stat" {
						cancel()
						return nil, cause
					}
					return target, nil
				},
				readDir: func(string) ([]os.DirEntry, error) {
					reads++
					if phase == "read" {
						cancel()
						return nil, cause
					}
					return []os.DirEntry{observedEntry{DirEntry: entries[0], info: func() (os.FileInfo, error) { infos++; cancel(); return nil, cause }}}, nil
				},
			}
			_, err := realCasePath(ctx, path, source)
			if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
				t.Fatal(err)
			}
			if stats != 1 || (phase == "stat" && reads != 0) || (phase == "read" && infos != 0) || (phase == "entry" && infos != 1) {
				t.Fatal(stats, reads, infos)
			}
		})
	}
}

func TestEntrySpellingRequiresMatchingIdentity(t *testing.T) {
	base := t.TempDir()
	targetPath := filepath.Join(base, "RealCase")
	otherPath := filepath.Join(base, "other")
	for _, path := range []string{targetPath, otherPath} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := directoryEntryName(context.Background(), entries, "REALCASE", target)
	if err != nil || actual != "RealCase" {
		t.Fatal(actual, err)
	}
	_, err = directoryEntryName(context.Background(), entries, "other", target)
	if !errors.Is(err, ErrDirectoryChanged) {
		t.Fatal("different identity accepted", err)
	}
}
