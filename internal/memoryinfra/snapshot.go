// Package memoryinfra implements memory's infrastructure ports that are not
// storage: the workspace snapshot reader, which hashes workspace files with
// git. Memory storage stays in internal/storage/turso.
package memoryinfra

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/process"
)

// ErrInvalidSnapshot: the snapshot ref is not the absolute, clean path of an
// existing directory. Its text never repeats the ref.
var ErrInvalidSnapshot = errors.New("snapshot ref is not an absolute clean path of an existing directory")

// WorkspaceSnapshots reads a workspace directory as a memory snapshot
// (compose.SnapshotReader). A compose.SnapshotRef is the absolute path of the
// workspace directory, so the snapshot is the working tree with its
// uncommitted changes. A present file's hash is the blob hash git would store
// for it: `git hash-object --stdin-paths` run in the workspace, so
// .gitattributes filters, core.autocrlf, LFS clean filters and the
// repository's object format (SHA-1 or SHA-256) apply. A directory that is
// not in a git repository is hashed as SHA-1. The workspace is expected to be
// its worktree's root: git resolves attributes relative to the directory it
// runs in.
//
// ObserveFiles checks every path with the memory rules first
// (memory.DecideSnapshotPath, then memory.DecideSnapshotFile on lstat facts)
// and hashes only the admitted ones with one git process per call; only a
// failed batch adds at most one control run and maxPerFileRetries
// single-path runs (see hash).
type WorkspaceSnapshots struct {
	launcher process.Launcher
	// maxBytes overrides memory.MaxSnapshotFileBytes in tests.
	maxBytes int64
	files    fileSystem
}

// fileSystem is the reader's filesystem lookups; tests wrap them.
type fileSystem struct {
	lstat        func(string) (fs.FileInfo, error)
	evalSymlinks func(string) (string, error)
}

// NewWorkspaceSnapshots returns the reader; git is launched through
// launcher, or through process.ExecLauncher when it is nil.
func NewWorkspaceSnapshots(launcher process.Launcher) *WorkspaceSnapshots {
	if launcher == nil {
		launcher = process.ExecLauncher{}
	}
	return &WorkspaceSnapshots{
		launcher: launcher,
		maxBytes: memory.MaxSnapshotFileBytes,
		files:    fileSystem{lstat: os.Lstat, evalSymlinks: filepath.EvalSymlinks},
	}
}

var _ compose.SnapshotReader = (*WorkspaceSnapshots)(nil)

// ObserveFiles observes paths in the workspace snapshot, one observation per
// path in order. Phases: resolve the workspace root once; decide each path
// with the memory rules from lstat facts; hash the admitted paths with git.
// An error means the workspace could not be used (invalid ref, git missing,
// cancellation, git broke its output contract or failed in the workspace
// itself); it never contains a path, the ref, file contents or git's stderr.
//
// gitTimeout bounds the whole call, from its start. The context is checked
// between steps and before returning, so a cancelled call is always an
// error; a filesystem call already blocked is not interrupted.
func (w *WorkspaceSnapshots) ObserveFiles(ctx context.Context, snapshot compose.SnapshotRef, paths []string) ([]compose.FileObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := resolveRoot(string(snapshot))
	if err != nil {
		return nil, err
	}
	observed := make([]compose.FileObservation, len(paths))
	var admitted []admittedPath
	for index, declared := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind, clean := w.decidePath(root, declared)
		switch kind {
		case memory.SnapshotFileHash:
			admitted = append(admitted, admittedPath{index: index, clean: clean})
		case memory.SnapshotFileMissing:
			observed[index] = compose.FileObservation{Observation: memory.SourceMissing}
		default:
			observed[index] = compose.FileObservation{Observation: memory.SourceUnreadable}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(admitted) == 0 {
		return observed, nil
	}
	hashed, err := w.hash(ctx, root, admitted)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for position, entry := range admitted {
		observed[entry.index] = hashed[position]
	}
	return observed, nil
}

// admittedPath is a path the rules admitted for hashing, with its position in
// the request.
type admittedPath struct {
	index int
	clean string
}

// resolveRoot validates the ref and resolves its symlinks once; every path is
// then looked up and hashed under the resolved root.
func resolveRoot(ref string) (string, error) {
	if ref == "" || !filepath.IsAbs(ref) || filepath.Clean(ref) != ref || strings.ContainsRune(ref, 0) {
		return "", ErrInvalidSnapshot
	}
	resolved, err := filepath.EvalSymlinks(ref)
	if err != nil {
		return "", opaque(ErrInvalidSnapshot, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", opaque(ErrInvalidSnapshot, err)
	}
	if !info.IsDir() {
		return "", ErrInvalidSnapshot
	}
	return resolved, nil
}

// decidePath applies the path rule, gathers lstat facts for an admitted path
// and applies the file rule. It returns the cleaned path to hash.
func (w *WorkspaceSnapshots) decidePath(root, declared string) (memory.SnapshotFileKind, string) {
	decision := memory.DecideSnapshotPath(declared)
	if decision.Kind != memory.SnapshotPathAdmitted {
		return memory.SnapshotFileUnreadable, ""
	}
	facts := w.files.facts(root, decision.Clean)
	facts.MaxBytes = w.maxBytes
	return memory.DecideSnapshotFile(facts).Kind, decision.Clean
}

// facts resolves the directory of clean under root first and looks up
// the leaf (lstat, not following a final symlink) only under a directory
// that resolves inside root, so nothing is learnt about files outside it.
func (f fileSystem) facts(root, clean string) memory.SnapshotFileFacts {
	dir, parent, absent := f.resolveParent(root, clean)
	facts := memory.SnapshotFileFacts{Parent: parent}
	switch {
	case parent != memory.SnapshotInsideRoot:
		return facts
	case absent:
		facts.Lookup = memory.SnapshotLookupMissing
		return facts
	}
	info, err := f.lstat(filepath.Join(dir, path.Base(clean)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		facts.Lookup = memory.SnapshotLookupMissing
		return facts
	case err != nil:
		facts.Lookup = memory.SnapshotLookupFailed
		return facts
	}
	facts.Lookup = memory.SnapshotLookupFound
	facts.Symlink = info.Mode()&fs.ModeSymlink != 0
	facts.Regular = info.Mode().IsRegular()
	facts.Size = info.Size()
	return facts
}

// resolveParent resolves the directory of clean (a cleaned, relative,
// slash-separated path) under root and reports where it lies. When that
// directory is absent, the nearest existing ancestor decides: inside root,
// and its next component on the way truly absent, the path is absent (dir is
// then empty); a next component that exists but does not resolve (a dangling
// symlink) is unresolved.
func (f fileSystem) resolveParent(root, clean string) (dir string, where memory.SnapshotResolution, absent bool) {
	segments := strings.Split(clean, "/")
	segments = segments[:len(segments)-1]
	for depth := len(segments); ; depth-- {
		resolved, err := f.evalSymlinks(filepath.Join(root, filepath.FromSlash(strings.Join(segments[:depth], "/"))))
		if errors.Is(err, fs.ErrNotExist) && depth > 0 {
			continue
		}
		switch {
		case err != nil:
			return "", memory.SnapshotUnresolved, false
		case !within(root, resolved):
			return "", memory.SnapshotOutsideRoot, false
		case depth == len(segments):
			return resolved, memory.SnapshotInsideRoot, false
		}
		if _, err := f.lstat(filepath.Join(resolved, segments[depth])); errors.Is(err, fs.ErrNotExist) {
			return "", memory.SnapshotInsideRoot, true
		}
		return "", memory.SnapshotUnresolved, false
	}
}

// within reports whether path is root or lies under it; both are resolved
// and clean.
func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// opaqueError keeps a cause for errors.Is and errors.As while its text is
// only the sentinel's: OS and exec errors name paths.
type opaqueError struct {
	sentinel error
	cause    error
}

func opaque(sentinel, cause error) error { return &opaqueError{sentinel: sentinel, cause: cause} }

func (e *opaqueError) Error() string   { return e.sentinel.Error() }
func (e *opaqueError) Unwrap() []error { return []error{e.sentinel, e.cause} }
