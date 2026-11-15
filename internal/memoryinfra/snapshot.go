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
	"time"

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
//
// An admitted file whose lstat fingerprint equals the one stored after an
// earlier successful hash, within snapshotCacheTTL, takes the stored hash
// without git (snapshotCache). The rules still run on every call, so the
// cache never admits a path they refuse. A WorkspaceSnapshots is safe for
// concurrent use.
type WorkspaceSnapshots struct {
	launcher process.Launcher
	// maxBytes overrides memory.MaxSnapshotFileBytes in tests.
	maxBytes int64
	files    fileSystem
	cache    *snapshotCache
	// now is the clock of the racy rule and the cache's TTL; tests set it.
	now func() time.Time
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
		cache:    newSnapshotCache(snapshotCacheEntries, snapshotCacheTTL),
		now:      time.Now,
	}
}

var _ compose.SnapshotReader = (*WorkspaceSnapshots)(nil)

// ObserveFiles observes paths in the workspace snapshot, one observation per
// path in order. Phases: resolve the workspace root once; decide each path
// with the memory rules from lstat facts; take the hash of an admitted path
// whose fingerprint is cached; hash the other admitted paths with git (none
// left: git is not started); look up what git hashed again, as the rules did,
// and publish the stable results to the cache in one step (see storable and
// decideSnapshotCacheStore). Results served from the cache are not part of
// the git batch, so the failure fallback of hash (per-file retries up to
// maxPerFileRetries) counts only the misses.
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
	callStart := w.now()
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
		kind, clean, leaf := w.decidePath(root, declared)
		switch kind {
		case memory.SnapshotFileHash:
			if hash, hit := w.cached(root, clean, leaf); hit {
				observed[index] = compose.FileObservation{Observation: memory.SourcePresent, Hash: hash}
				continue
			}
			admitted = append(admitted, admittedPath{index: index, clean: clean, leaf: leaf})
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
	storable, err := w.storable(ctx, callStart, root, admitted, hashed)
	if err != nil {
		return nil, err
	}
	// The publish point: the entries are stored together, or not at all
	// when the call was cancelled before the cache lock was taken. A
	// cancellation after it still fails the call; the entries stay, as
	// each is a stable file's hash.
	if err := w.cache.storeAll(ctx, storable); err != nil {
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
// the request and its leaf as looked up for the rules.
type admittedPath struct {
	index int
	clean string
	leaf  leafFile
}

// leafFile is the leaf of an admitted path: where it was looked up (under its
// resolved directory) and the fingerprint of that lstat, when known.
type leafFile struct {
	path        string
	fingerprint fileFingerprint
	known       bool
}

// cached returns the stored hash of an admitted file whose fingerprint is
// known and unchanged.
func (w *WorkspaceSnapshots) cached(root, clean string, leaf leafFile) (string, bool) {
	if !leaf.known {
		return "", false
	}
	return w.cache.lookup(snapshotCacheKey{root: root, clean: clean}, leaf.fingerprint, w.now())
}

// storable looks each file git hashed as present up again, the way the
// rules did (directory resolution and leaf lstat), and returns the entries
// decideSnapshotCacheStore admits: the file rule must still admit the path,
// under the same resolved leaf, with the same fingerprint. A directory
// symlink switched while git ran therefore keeps the file out of the cache.
// The context is checked before each lookup; the caller publishes the
// entries only if the call is not cancelled.
func (w *WorkspaceSnapshots) storable(ctx context.Context, callStart time.Time, root string, admitted []admittedPath, hashed []compose.FileObservation) ([]snapshotCacheEntry, error) {
	var entries []snapshotCacheEntry
	for position, entry := range admitted {
		facts := snapshotCacheStoreFacts{
			Observation: hashed[position],
			BeforeKnown: entry.leaf.known,
			Before:      entry.leaf.fingerprint,
			CallStart:   callStart,
		}
		// The rule refuses these anyway; skipping them saves the lookup.
		if facts.Observation.Observation != memory.SourcePresent || !facts.BeforeKnown {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind, _, leaf := w.decidePath(root, entry.clean)
		facts.SameLeaf = kind == memory.SnapshotFileHash && leaf.path == entry.leaf.path
		facts.After, facts.AfterKnown = leaf.fingerprint, leaf.known
		if decideSnapshotCacheStore(facts).Kind != snapshotCacheStore {
			continue
		}
		entries = append(entries, snapshotCacheEntry{
			key:         snapshotCacheKey{root: root, clean: entry.clean},
			fingerprint: facts.Before,
			hash:        facts.Observation.Hash,
			stored:      w.now(),
		})
	}
	return entries, nil
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
// and applies the file rule. It returns the cleaned path to hash and its
// leaf.
func (w *WorkspaceSnapshots) decidePath(root, declared string) (memory.SnapshotFileKind, string, leafFile) {
	decision := memory.DecideSnapshotPath(declared)
	if decision.Kind != memory.SnapshotPathAdmitted {
		return memory.SnapshotFileUnreadable, "", leafFile{}
	}
	facts, leaf := w.files.facts(root, decision.Clean)
	facts.MaxBytes = w.maxBytes
	return memory.DecideSnapshotFile(facts).Kind, decision.Clean, leaf
}

// facts resolves the directory of clean under root first and looks up
// the leaf (lstat, not following a final symlink) only under a directory
// that resolves inside root, so nothing is learnt about files outside it.
// The leaf is returned once it was found.
func (f fileSystem) facts(root, clean string) (memory.SnapshotFileFacts, leafFile) {
	dir, parent, absent := f.resolveParent(root, clean)
	facts := memory.SnapshotFileFacts{Parent: parent}
	switch {
	case parent != memory.SnapshotInsideRoot:
		return facts, leafFile{}
	case absent:
		facts.Lookup = memory.SnapshotLookupMissing
		return facts, leafFile{}
	}
	leaf := leafFile{path: filepath.Join(dir, path.Base(clean))}
	info, err := f.lstat(leaf.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		facts.Lookup = memory.SnapshotLookupMissing
		return facts, leafFile{}
	case err != nil:
		facts.Lookup = memory.SnapshotLookupFailed
		return facts, leafFile{}
	}
	facts.Lookup = memory.SnapshotLookupFound
	facts.Symlink = info.Mode()&fs.ModeSymlink != 0
	facts.Regular = info.Mode().IsRegular()
	facts.Size = info.Size()
	leaf.fingerprint, leaf.known = fingerprintOf(info)
	return facts, leaf
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
