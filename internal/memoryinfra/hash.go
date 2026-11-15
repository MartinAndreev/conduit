package memoryinfra

import (
	"context"
	"errors"
	"strings"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
)

// ErrGitOutput: git exited successfully but did not print one valid blob hash
// per path, or printed more than that bound allows. Nothing from that run is
// trusted, and nothing is retried.
var ErrGitOutput = errors.New("git hash-object did not print one blob hash per path")

// ErrWorkspaceUnusable: git failed for the batch and also failed to hash empty
// input in the workspace, so the workspace itself (its repository or git
// configuration) cannot be used.
var ErrWorkspaceUnusable = errors.New("git cannot hash in the workspace")

// maxPerFileRetries bounds the per-file retry after a failed batch: a batch of
// at most this many paths is retried one path per git process; a larger one
// is marked unreadable as a whole. With the batch and the control run, one
// call therefore starts at most maxPerFileRetries+2 git processes.
const maxPerFileRetries = 8

// hash returns the observations of the admitted paths, in their order.
//
// Failure model: one `git hash-object --stdin-paths` process hashes the batch.
// When it exits 0 it must print exactly one valid blob hash per path, within
// the output bound, or the call fails with ErrGitOutput without a control run
// or retries. When it exits non-zero, git stops at the
// first file it cannot hash (an unreadable file, a failing clean or LFS
// filter) or fails before any file (broken repository or configuration), and
// its partial output is discarded. A control run hashing empty input then
// tells the two apart: if the control also fails the call fails with
// ErrWorkspaceUnusable; otherwise the cause is file-specific, so a batch of
// more than one and at most maxPerFileRetries paths is retried one path per
// process (a failing path is unreadable) and a larger batch, or a single
// path, is unreadable as a whole. A failed batch never marks a file present
// without its own successful hash. Git missing from PATH, a start failure,
// cancellation (ObserveFiles bounds the whole call with gitTimeout) and an
// unconfirmed process cleanup fail the call.
func (w *WorkspaceSnapshots) hash(ctx context.Context, root string, admitted []admittedPath) ([]compose.FileObservation, error) {
	paths := make([]string, len(admitted))
	for index, entry := range admitted {
		paths[index] = entry.clean
	}
	hashes, ok, err := w.hashPaths(ctx, root, paths)
	if err != nil {
		return nil, err
	}
	if ok {
		return present(hashes), nil
	}
	if _, ok, err := w.run(ctx, root, []string{"hash-object", "--stdin"}, nil, 1); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrWorkspaceUnusable
	}
	observed := make([]compose.FileObservation, len(paths))
	for index := range observed {
		observed[index] = compose.FileObservation{Observation: memory.SourceUnreadable}
	}
	if len(paths) == 1 || len(paths) > maxPerFileRetries {
		return observed, nil
	}
	for index, path := range paths {
		hashes, ok, err := w.hashPaths(ctx, root, []string{path})
		if err != nil {
			return nil, err
		}
		if ok {
			observed[index] = present(hashes)[0]
		}
	}
	return observed, nil
}

// hashPaths runs one `git hash-object --stdin-paths` over paths. ok is false
// when git exited non-zero; then hashes is nil.
func (w *WorkspaceSnapshots) hashPaths(ctx context.Context, root string, paths []string) ([]string, bool, error) {
	stdout, ok, err := w.run(ctx, root, []string{"hash-object", "--stdin-paths"}, paths, len(paths))
	if err != nil || !ok {
		return nil, ok, err
	}
	hashes, err := parseHashes(stdout, len(paths))
	if err != nil {
		return nil, false, err
	}
	return hashes, true, nil
}

// parseHashes reads exactly want lines, each a valid blob hash, in order.
func parseHashes(stdout []byte, want int) ([]string, error) {
	text := string(stdout)
	if !strings.HasSuffix(text, "\n") {
		return nil, ErrGitOutput
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != want {
		return nil, ErrGitOutput
	}
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if !memory.ValidBlobHash(line) {
			return nil, ErrGitOutput
		}
		lines[index] = line
	}
	return lines, nil
}

func present(hashes []string) []compose.FileObservation {
	observed := make([]compose.FileObservation, len(hashes))
	for index, hash := range hashes {
		observed[index] = compose.FileObservation{Observation: memory.SourcePresent, Hash: hash}
	}
	return observed
}
