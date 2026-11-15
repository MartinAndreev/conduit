package memoryinfra

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/process"
)

// settledTime is a modification time far outside the racy window.
var settledTime = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

// testClock is an injectable clock. It starts an hour ahead of the real
// clock so that files written by the test, whose change time (Unix ctime)
// is the real time and cannot be set back, are outside the racy window.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock { return &testClock{now: time.Now().Add(time.Hour)} }

func (c *testClock) Now() (now time.Time) {
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		now = c.now
		return nil
	})
	return now
}

func (c *testClock) advance(by time.Duration) {
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		c.now = c.now.Add(by)
		return nil
	})
}

// cachingReader is a reader on clock.
func cachingReader(launcher process.Launcher, clock *testClock) *WorkspaceSnapshots {
	reader := NewWorkspaceSnapshots(launcher)
	reader.now = clock.Now
	return reader
}

// settle writes a file and sets its modification time to settledTime.
func settle(t *testing.T, dir, name, content string) {
	t.Helper()
	write(t, dir, name, content)
	setMtime(t, dir, name, settledTime)
}

func setMtime(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(name)), mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// fakeHash is a valid SHA-1 blob hash made of one repeated hex digit.
func fakeHash(digit string) string { return strings.Repeat(digit, 40) }

// hashing is a scripted git run that prints hashes, one per line, and exits 0.
func hashing(hashes ...string) scriptedProcess {
	var stdout strings.Builder
	for _, hash := range hashes {
		stdout.WriteString(hash + "\n")
	}
	return newScriptedProcess(stdout.String(), 0)
}

// scripted is a launcher handing out children in order; any further start
// fails the call, so it also asserts how many git processes ran.
func scripted(children ...scriptedProcess) *scriptedLauncher {
	return &scriptedLauncher{children: children}
}

// hookLauncher runs onStart before delegating each start.
type hookLauncher struct {
	process.Launcher
	onStart func()
}

func (l hookLauncher) Start(spec process.Spec) (process.Process, error) {
	l.onStart()
	return l.Launcher.Start(spec)
}

func observeWithStarts(t *testing.T, reader *WorkspaceSnapshots, launcher *scriptedLauncher, wantStarts int32, dir string, paths ...string) []compose.FileObservation {
	t.Helper()
	got := observe(t, reader, dir, paths...)
	if starts := launcher.starts.Load(); starts != wantStarts {
		t.Fatalf("started %d git processes, want %d", starts, wantStarts)
	}
	return got
}

func wantSame(t *testing.T, got []compose.FileObservation, want ...compose.FileObservation) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func cacheLen(reader *WorkspaceSnapshots) (count int) {
	_ = lockscope.WithMutexLock(&reader.cache.mu, func() error {
		count = len(reader.cache.entries)
		return nil
	})
	return count
}

// A second call over unchanged files takes every hash from the cache and
// starts no git process.
func TestCacheSkipsGitForUnchangedFiles(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	settle(t, dir, "a.go", "a\n")
	settle(t, dir, "docs/b.md", "b\n")
	launcher := scripted(hashing(fakeHash("a"), fakeHash("b")))
	reader := cachingReader(launcher, newTestClock())
	first := observeWithStarts(t, reader, launcher, 1, dir, "a.go", "docs/b.md")
	wantSame(t, first, presentAs(fakeHash("a")), presentAs(fakeHash("b")))
	second := observeWithStarts(t, reader, launcher, 1, dir, "a.go", "./docs/../docs/b.md")
	wantSame(t, second, first...)
}

// A changed fingerprint is a miss: git runs again only for the changed
// path (a scripted run printing one hash fails the call if it was given
// two), and the new hash is cached in turn.
func TestCacheMissesWhenTheFileChanges(t *testing.T) {
	requireGit(t)
	changes := map[string]func(t *testing.T, dir string){
		"size": func(t *testing.T, dir string) {
			settle(t, dir, "a.go", "a changed\n")
		},
		"mtime": func(t *testing.T, dir string) {
			setMtime(t, dir, "a.go", settledTime.Add(time.Minute))
		},
		"mode": func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, "a.go"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"replaced with equal size and mtime": func(t *testing.T, dir string) {
			settle(t, dir, "replacement", "A\n")
			if err := os.Rename(filepath.Join(dir, "replacement"), filepath.Join(dir, "a.go")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			if name == "mode" && runtime.GOOS == "windows" {
				t.Skip("Windows keeps no permission bits")
			}
			dir := workspace(t)
			settle(t, dir, "a.go", "a\n")
			settle(t, dir, "b.go", "b\n")
			launcher := scripted(hashing(fakeHash("a"), fakeHash("b")), hashing(fakeHash("c")))
			reader := cachingReader(launcher, newTestClock())
			observeWithStarts(t, reader, launcher, 1, dir, "a.go", "b.go")
			change(t, dir)
			got := observeWithStarts(t, reader, launcher, 2, dir, "a.go", "b.go")
			wantSame(t, got, presentAs(fakeHash("c")), presentAs(fakeHash("b")))
			got = observeWithStarts(t, reader, launcher, 2, dir, "a.go", "b.go")
			wantSame(t, got, presentAs(fakeHash("c")), presentAs(fakeHash("b")))
		})
	}
}

// A file whose modification or change time is not strictly older than the
// call's start minus racyMargin is hashed but not cached.
func TestRacyFileIsNotCached(t *testing.T) {
	requireGit(t)
	t.Run("mtime inside the window", func(t *testing.T) {
		dir := workspace(t)
		clock := newTestClock()
		write(t, dir, "a.go", "a\n")
		setMtime(t, dir, "a.go", clock.Now().Add(-racyMargin+time.Second))
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
		reader := cachingReader(launcher, clock)
		observeWithStarts(t, reader, launcher, 1, dir, "a.go")
		observeWithStarts(t, reader, launcher, 2, dir, "a.go")
	})
	t.Run("mtime exactly at the window's edge", func(t *testing.T) {
		dir := workspace(t)
		clock := newTestClock()
		write(t, dir, "a.go", "a\n")
		setMtime(t, dir, "a.go", clock.Now().Add(-racyMargin))
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
		reader := cachingReader(launcher, clock)
		observeWithStarts(t, reader, launcher, 1, dir, "a.go")
		observeWithStarts(t, reader, launcher, 2, dir, "a.go")
	})
	t.Run("ctime inside the window", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no change time; only mtime is checked")
		}
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		// The real clock: the file's ctime is now, its mtime long ago.
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
		reader := NewWorkspaceSnapshots(launcher)
		observeWithStarts(t, reader, launcher, 1, dir, "a.go")
		observeWithStarts(t, reader, launcher, 2, dir, "a.go")
	})
}

// A file whose fingerprint after git returned differs from the one the rules
// saw, or that cannot be looked up again, is returned but not cached.
func TestFileChangedWhileHashingIsNotCached(t *testing.T) {
	requireGit(t)
	t.Run("changed", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("b")))
		changed := false
		hooked := hookLauncher{Launcher: launcher, onStart: func() {
			if !changed {
				changed = true
				settle(t, dir, "a.go", "a, changed while git read it\n")
			}
		}}
		reader := cachingReader(hooked, newTestClock())
		wantSame(t, observeWithStarts(t, reader, launcher, 1, dir, "a.go"), presentAs(fakeHash("a")))
		if cacheLen(reader) != 0 {
			t.Fatal("a file changed while hashing was cached")
		}
		wantSame(t, observeWithStarts(t, reader, launcher, 2, dir, "a.go"), presentAs(fakeHash("b")))
		wantSame(t, observeWithStarts(t, reader, launcher, 2, dir, "a.go"), presentAs(fakeHash("b")))
	})
	t.Run("lookup failed", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
		reader := cachingReader(launcher, newTestClock())
		lstat, calls := reader.files.lstat, 0
		reader.files.lstat = func(name string) (fs.FileInfo, error) {
			if filepath.Base(name) == "a.go" {
				calls++
				if calls == 2 {
					return nil, errors.New("lookup failed")
				}
			}
			return lstat(name)
		}
		observeWithStarts(t, reader, launcher, 1, dir, "a.go")
		observeWithStarts(t, reader, launcher, 2, dir, "a.go")
	})
}

// infoWithoutSys hides the platform facts of a FileInfo.
type infoWithoutSys struct{ fs.FileInfo }

func (infoWithoutSys) Sys() any { return nil }

// Without platform facts the fingerprint is unknown: the cache is neither
// consulted nor filled.
func TestUnknownFingerprintIsNotCached(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	settle(t, dir, "a.go", "a\n")
	launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
	reader := cachingReader(launcher, newTestClock())
	lstat := reader.files.lstat
	reader.files.lstat = func(name string) (fs.FileInfo, error) {
		info, err := lstat(name)
		if err != nil {
			return nil, err
		}
		return infoWithoutSys{info}, nil
	}
	observeWithStarts(t, reader, launcher, 1, dir, "a.go")
	observeWithStarts(t, reader, launcher, 2, dir, "a.go")
}

// Only present results are cached: missing, unreadable, a failed call and a
// cancelled call leave nothing behind.
func TestOnlyPresentResultsAreCached(t *testing.T) {
	requireGit(t)
	t.Run("missing", func(t *testing.T) {
		dir := workspace(t)
		launcher := scripted(hashing(fakeHash("a")))
		reader := cachingReader(launcher, newTestClock())
		wantSame(t, observeWithStarts(t, reader, launcher, 0, dir, "a.go"), missing)
		settle(t, dir, "a.go", "a\n")
		wantSame(t, observeWithStarts(t, reader, launcher, 1, dir, "a.go"), presentAs(fakeHash("a")))
	})
	t.Run("unreadable", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		// Batch fails, control succeeds: the single path is unreadable.
		launcher := scripted(newScriptedProcess("", 1), hashing(fakeHash("e")), hashing(fakeHash("a")))
		reader := cachingReader(launcher, newTestClock())
		wantSame(t, observeWithStarts(t, reader, launcher, 2, dir, "a.go"), unreadable)
		wantSame(t, observeWithStarts(t, reader, launcher, 3, dir, "a.go"), presentAs(fakeHash("a")))
		wantSame(t, observeWithStarts(t, reader, launcher, 3, dir, "a.go"), presentAs(fakeHash("a")))
	})
	t.Run("retry successes", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		settle(t, dir, "b.go", "b\n")
		// Batch fails, control succeeds, a.go fails alone, b.go succeeds.
		launcher := scripted(newScriptedProcess("", 1), hashing(fakeHash("e")), newScriptedProcess("", 1), hashing(fakeHash("b")))
		reader := cachingReader(launcher, newTestClock())
		wantSame(t, observeWithStarts(t, reader, launcher, 4, dir, "a.go", "b.go"), unreadable, presentAs(fakeHash("b")))
		wantSame(t, observeWithStarts(t, reader, launcher, 4, dir, "b.go"), presentAs(fakeHash("b")))
	})
	t.Run("error", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		settle(t, dir, "b.go", "b\n")
		// One hash for two paths breaks the output contract.
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a"), fakeHash("b")))
		reader := cachingReader(launcher, newTestClock())
		if _, err := reader.ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go", "b.go"}); !errors.Is(err, ErrGitOutput) {
			t.Fatalf("err = %v", err)
		}
		observeWithStarts(t, reader, launcher, 2, dir, "a.go", "b.go")
	})
	t.Run("cancelled", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
		reader := cachingReader(hookLauncher{Launcher: launcher, onStart: cancel}, newTestClock())
		if _, err := reader.ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if cacheLen(reader) != 0 {
			t.Fatal("a cancelled call filled the cache")
		}
		observeWithStarts(t, reader, launcher, 2, dir, "a.go")
	})
	t.Run("cancelled while the cache is locked", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("a")))
		reader := cachingReader(launcher, newTestClock())
		lstat, calls := reader.files.lstat, 0
		reader.files.lstat = func(name string) (fs.FileInfo, error) {
			if filepath.Base(name) == "a.go" {
				if calls++; calls == 2 {
					// The last lookup after git returned; no context check
					// follows before the cache lock. The context is
					// cancelled while another goroutine holds the lock.
					locked, release := make(chan struct{}), make(chan struct{})
					go func() {
						_ = lockscope.WithMutexLock(&reader.cache.mu, func() error {
							close(locked)
							<-release
							return nil
						})
					}()
					<-locked
					cancel()
					close(release)
				}
			}
			return lstat(name)
		}
		result := make(chan error, 1)
		go func() {
			_, err := reader.ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go"})
			result <- err
		}()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if cacheLen(reader) != 0 {
			t.Fatal("a cancelled call filled the cache")
		}
		observeWithStarts(t, reader, launcher, 2, dir, "a.go")
	})
	t.Run("cancelled between lookups after hashing", func(t *testing.T) {
		dir := workspace(t)
		settle(t, dir, "a.go", "a\n")
		settle(t, dir, "b.go", "b\n")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		launcher := scripted(hashing(fakeHash("a"), fakeHash("b")))
		reader := cachingReader(launcher, newTestClock())
		lstat, calls := reader.files.lstat, 0
		reader.files.lstat = func(name string) (fs.FileInfo, error) {
			if filepath.Base(name) == "a.go" {
				if calls++; calls == 2 {
					cancel()
				}
			}
			if filepath.Base(name) == "b.go" && calls == 2 {
				t.Error("b.go was looked up again after cancellation")
			}
			return lstat(name)
		}
		if _, err := reader.ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go", "b.go"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if cacheLen(reader) != 0 {
			t.Fatal("a cancelled call filled the cache")
		}
	})
}

// An entry is trusted for snapshotCacheTTL from storing; an expired entry is
// a miss and is dropped.
func TestCacheEntriesExpire(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	settle(t, dir, "a.go", "a\n")
	clock := newTestClock()
	launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("b")))
	reader := cachingReader(launcher, clock)
	observeWithStarts(t, reader, launcher, 1, dir, "a.go")
	clock.advance(snapshotCacheTTL - time.Nanosecond)
	wantSame(t, observeWithStarts(t, reader, launcher, 1, dir, "a.go"), presentAs(fakeHash("a")))
	clock.advance(time.Nanosecond)
	if _, hit := reader.cached(dir, "a.go", leafOf(t, reader, dir, "a.go")); hit || cacheLen(reader) != 0 {
		t.Fatalf("expired entry hit %v, %d entries left", hit, cacheLen(reader))
	}
	wantSame(t, observeWithStarts(t, reader, launcher, 2, dir, "a.go"), presentAs(fakeHash("b")))
}

// leafOf looks up a path's leaf as ObserveFiles does.
func leafOf(t *testing.T, reader *WorkspaceSnapshots, dir, name string) leafFile {
	t.Helper()
	kind, _, leaf := reader.decidePath(dir, name)
	if kind != memory.SnapshotFileHash || !leaf.known {
		t.Fatalf("%s: %s, fingerprint known %v", name, kind, leaf.known)
	}
	return leaf
}

// Over the capacity the least recently used entry is evicted; a hit counts
// as a use.
func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		settle(t, dir, name, name)
	}
	launcher := scripted(hashing(fakeHash("a"), fakeHash("b")), hashing(fakeHash("c")), hashing(fakeHash("b")), hashing(fakeHash("a")))
	reader := cachingReader(launcher, newTestClock())
	reader.cache = newSnapshotCache(2, snapshotCacheTTL)
	observeWithStarts(t, reader, launcher, 1, dir, "a.go", "b.go")
	// a.go is used again, so b.go is now the least recently used.
	observeWithStarts(t, reader, launcher, 1, dir, "a.go")
	observeWithStarts(t, reader, launcher, 2, dir, "c.go")
	if cacheLen(reader) != 2 {
		t.Fatalf("%d entries over a capacity of 2", cacheLen(reader))
	}
	observeWithStarts(t, reader, launcher, 2, dir, "a.go", "c.go")
	// b.go was evicted; storing it evicts a.go.
	wantSame(t, observeWithStarts(t, reader, launcher, 3, dir, "b.go"), presentAs(fakeHash("b")))
	wantSame(t, observeWithStarts(t, reader, launcher, 3, dir, "c.go", "b.go"), presentAs(fakeHash("c")), presentAs(fakeHash("b")))
	wantSame(t, observeWithStarts(t, reader, launcher, 4, dir, "a.go"), presentAs(fakeHash("a")))
}

// Two workspaces never share entries, even for a hard-linked file whose
// fingerprint is identical in both.
func TestCacheIsPerWorkspaceRoot(t *testing.T) {
	requireGit(t)
	first, second := workspace(t), workspace(t)
	settle(t, first, "a.go", "a\n")
	if err := os.Link(filepath.Join(first, "a.go"), filepath.Join(second, "a.go")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	launcher := scripted(hashing(fakeHash("a")), hashing(fakeHash("b")))
	reader := cachingReader(launcher, newTestClock())
	if leafOf(t, reader, first, "a.go").fingerprint != leafOf(t, reader, second, "a.go").fingerprint {
		t.Fatal("setup: hard links have different fingerprints")
	}
	wantSame(t, observeWithStarts(t, reader, launcher, 1, first, "a.go"), presentAs(fakeHash("a")))
	wantSame(t, observeWithStarts(t, reader, launcher, 2, second, "a.go"), presentAs(fakeHash("b")))
	wantSame(t, observeWithStarts(t, reader, launcher, 2, first, "a.go"), presentAs(fakeHash("a")))
	wantSame(t, observeWithStarts(t, reader, launcher, 2, second, "a.go"), presentAs(fakeHash("b")))
}

// The rules run before the cache: a cached path that becomes a symlink, or
// whose directory now leaves the workspace (the file itself unchanged), is
// unreadable without consulting the cache or starting git.
func TestCacheDoesNotBypassTheRules(t *testing.T) {
	requireGit(t)
	dir, outside := workspace(t), workspace(t)
	settle(t, dir, "a.go", "a\n")
	settle(t, dir, "other.go", "o\n")
	settle(t, dir, "docs/x.md", "x\n")
	launcher := scripted(hashing(fakeHash("a"), fakeHash("b"), fakeHash("c")))
	reader := cachingReader(launcher, newTestClock())
	observeWithStarts(t, reader, launcher, 1, dir, "a.go", "other.go", "docs/x.md")
	if err := os.Remove(filepath.Join(dir, "a.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.go", filepath.Join(dir, "a.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "docs"), filepath.Join(outside, "docs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "docs"), filepath.Join(dir, "docs")); err != nil {
		t.Fatal(err)
	}
	got := observeWithStarts(t, reader, launcher, 1, dir, "a.go", "other.go", "docs/x.md")
	wantSame(t, got, unreadable, presentAs(fakeHash("b")), unreadable)
}

// Hits, misses and paths the rules decide keep the request's order; only the
// misses go to git, in that order.
func TestMixedHitsAndMissesKeepOrder(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		settle(t, dir, name, name)
	}
	launcher := scripted(hashing(fakeHash("a"), fakeHash("c")), hashing(fakeHash("b"), fakeHash("d")))
	reader := cachingReader(launcher, newTestClock())
	observeWithStarts(t, reader, launcher, 1, dir, "a.go", "c.go")
	got := observeWithStarts(t, reader, launcher, 2, dir, "b.go", "a.go", "nope.go", "c.go", "../x.go", "d.go")
	wantSame(t, got, presentAs(fakeHash("b")), presentAs(fakeHash("a")), missing, presentAs(fakeHash("c")), unreadable, presentAs(fakeHash("d")))
}

// Concurrent calls on one reader with real git return the right hashes;
// once cached, a call starts no git process.
func TestConcurrentObserveFiles(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	var paths []string
	var want []compose.FileObservation
	for index := range 6 {
		name := fmt.Sprintf("f%d.txt", index)
		content := fmt.Sprintf("content %d\n", index)
		settle(t, dir, name, content)
		paths = append(paths, name)
		want = append(want, presentAs(sha1Blob(content)))
	}
	launcher := &countingLauncher{}
	reader := cachingReader(launcher, newTestClock())
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for worker := range 8 {
		workers.Go(func() {
			for call := range 4 {
				ordered := slices.Clone(paths)
				expected := slices.Clone(want)
				shift := (worker + call) % len(ordered)
				ordered = append(ordered[shift:], ordered[:shift]...)
				expected = append(expected[shift:], expected[:shift]...)
				got, err := reader.ObserveFiles(context.Background(), compose.SnapshotRef(dir), ordered)
				if err != nil || !slices.Equal(got, expected) {
					failures <- fmt.Errorf("worker %d call %d: %v, err %v", worker, call, got, err)
					return
				}
			}
		})
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	before := launcher.starts()
	if got := observe(t, reader, dir, paths...); !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
	if launcher.starts() != before {
		t.Fatal("git started for cached files")
	}
}

// A store for a key already present replaces its entry.
func TestCacheStoreReplaces(t *testing.T) {
	cache := newSnapshotCache(4, snapshotCacheTTL)
	now := time.Now()
	key := snapshotCacheKey{root: "/w", clean: "a.go"}
	fingerprint := fileFingerprint{size: 1}
	for _, hash := range []string{fakeHash("a"), fakeHash("b")} {
		if err := cache.storeAll(context.Background(), []snapshotCacheEntry{{key: key, fingerprint: fingerprint, hash: hash, stored: now}}); err != nil {
			t.Fatal(err)
		}
	}
	if hash, hit := cache.lookup(key, fingerprint, now); !hit || hash != fakeHash("b") || cache.recency.Len() != 1 {
		t.Fatalf("hash %s hit %v, %d entries", hash, hit, cache.recency.Len())
	}
}

func TestCacheStoreAllOverCapacityKeepsTheNewest(t *testing.T) {
	cache := newSnapshotCache(2, snapshotCacheTTL)
	now := time.Now()
	fingerprint := fileFingerprint{size: 1}
	var entries []snapshotCacheEntry
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		entries = append(entries, snapshotCacheEntry{key: snapshotCacheKey{root: "/w", clean: name}, fingerprint: fingerprint, hash: fakeHash(name), stored: now})
	}
	if err := cache.storeAll(context.Background(), entries); err != nil {
		t.Fatal(err)
	}
	if cache.recency.Len() != 2 || len(cache.entries) != 2 {
		t.Fatalf("%d list and %d map entries over a capacity of 2", cache.recency.Len(), len(cache.entries))
	}
	for _, name := range []string{"d", "e"} {
		if _, hit := cache.lookup(snapshotCacheKey{root: "/w", clean: name}, fingerprint, now); !hit {
			t.Fatalf("%s was evicted", name)
		}
	}
}

func TestDecideSnapshotCacheLookup(t *testing.T) {
	now := time.Now()
	stored := fileFingerprint{size: 3, mode: 0o644, mtime: 7}
	facts := func(edit func(*snapshotCacheLookupFacts)) snapshotCacheLookupFacts {
		f := snapshotCacheLookupFacts{Found: true, Stored: stored, StoredAt: now, Current: stored, Now: now, TTL: time.Minute}
		edit(&f)
		return f
	}
	tests := map[string]struct {
		facts snapshotCacheLookupFacts
		want  snapshotCacheLookupKind
	}{
		"hit":                {facts(func(*snapshotCacheLookupFacts) {}), snapshotCacheHit},
		"hit before the TTL": {facts(func(f *snapshotCacheLookupFacts) { f.Now = now.Add(time.Minute - 1) }), snapshotCacheHit},
		"miss":               {facts(func(f *snapshotCacheLookupFacts) { f.Found = false }), snapshotCacheMiss},
		"expired at the TTL": {facts(func(f *snapshotCacheLookupFacts) { f.Now = now.Add(time.Minute) }), snapshotCacheExpired},
		"clock went back":    {facts(func(f *snapshotCacheLookupFacts) { f.Now = now.Add(-1) }), snapshotCacheExpired},
		"size":               {facts(func(f *snapshotCacheLookupFacts) { f.Current.size++ }), snapshotCacheStale},
		"mode":               {facts(func(f *snapshotCacheLookupFacts) { f.Current.mode = 0o600 }), snapshotCacheStale},
		"mtime":              {facts(func(f *snapshotCacheLookupFacts) { f.Current.mtime++ }), snapshotCacheStale},
		"native":             {facts(func(f *snapshotCacheLookupFacts) { f.Current.native = otherNativeStat() }), snapshotCacheStale},
	}
	for name, tt := range tests {
		if got := decideSnapshotCacheLookup(tt.facts); got.Kind != tt.want || got.Reason == "" {
			t.Errorf("%s: %+v, want %s", name, got, tt.want)
		}
	}
}

func TestDecideSnapshotCacheStore(t *testing.T) {
	start := time.Now()
	settled := fileFingerprint{size: 3, mode: 0o644, mtime: start.Add(-time.Hour).UnixNano()}
	facts := func(edit func(*snapshotCacheStoreFacts)) snapshotCacheStoreFacts {
		f := snapshotCacheStoreFacts{
			Observation: presentAs(fakeHash("a")),
			BeforeKnown: true, Before: settled,
			SameLeaf:   true,
			AfterKnown: true, After: settled,
			CallStart: start,
		}
		edit(&f)
		return f
	}
	symlink := settled
	symlink.mode = fs.ModeSymlink | 0o777
	tests := map[string]struct {
		facts snapshotCacheStoreFacts
		want  snapshotCacheStoreKind
	}{
		"store":          {facts(func(*snapshotCacheStoreFacts) {}), snapshotCacheStore},
		"unreadable":     {facts(func(f *snapshotCacheStoreFacts) { f.Observation = unreadable }), snapshotCacheSkipNotPresent},
		"missing":        {facts(func(f *snapshotCacheStoreFacts) { f.Observation = missing }), snapshotCacheSkipNotPresent},
		"invalid hash":   {facts(func(f *snapshotCacheStoreFacts) { f.Observation = presentAs("not-a-hash") }), snapshotCacheSkipNotPresent},
		"unknown before": {facts(func(f *snapshotCacheStoreFacts) { f.BeforeKnown = false }), snapshotCacheSkipUnknownStat},
		"unknown after":  {facts(func(f *snapshotCacheStoreFacts) { f.AfterKnown = false }), snapshotCacheSkipUnknownStat},
		"racy mtime": {facts(func(f *snapshotCacheStoreFacts) {
			f.Before.mtime = start.Add(-racyMargin).UnixNano()
			f.After = f.Before
		}), snapshotCacheSkipRacy},
		"moved":                   {facts(func(f *snapshotCacheStoreFacts) { f.SameLeaf = false }), snapshotCacheSkipMoved},
		"changed":                 {facts(func(f *snapshotCacheStoreFacts) { f.After.size++ }), snapshotCacheSkipChanged},
		"symlink in the same way": {facts(func(f *snapshotCacheStoreFacts) { f.Before, f.After = symlink, symlink }), snapshotCacheSkipChanged},
	}
	for name, tt := range tests {
		if got := decideSnapshotCacheStore(tt.facts); got.Kind != tt.want || got.Reason == "" {
			t.Errorf("%s: %+v, want %s", name, got, tt.want)
		}
	}
}

// A directory symlink on the path switched while git runs makes git hash the
// other target; the lookup after hashing resolves the new leaf, so nothing is
// cached even though the old leaf is unchanged.
func TestDirectorySymlinkSwitchedWhileHashingIsNotCached(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	settle(t, dir, "A/f.go", "a\n")
	settle(t, dir, "B/f.go", "b\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink("A", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	launcher := scripted(hashing(fakeHash("b")), hashing(fakeHash("b")))
	switched := false
	hooked := hookLauncher{Launcher: launcher, onStart: func() {
		if switched {
			return
		}
		switched = true
		if err := os.Remove(link); err != nil {
			t.Error(err)
		}
		if err := os.Symlink("B", link); err != nil {
			t.Error(err)
		}
	}}
	reader := cachingReader(hooked, newTestClock())
	wantSame(t, observeWithStarts(t, reader, launcher, 1, dir, "link/f.go"), presentAs(fakeHash("b")))
	if cacheLen(reader) != 0 {
		t.Fatal("a hash read through a switched directory symlink was cached")
	}
	wantSame(t, observeWithStarts(t, reader, launcher, 2, dir, "link/f.go"), presentAs(fakeHash("b")))
}

// The failure fallback applies to the paths not served from the cache, so
// after a failed batch the results can depend on the cache: nine misses are
// over maxPerFileRetries and all unreadable, while one hit leaves eight
// misses, which are retried one by one.
func TestFailedBatchFallbackCountsOnlyMisses(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	var paths []string
	for index := range maxPerFileRetries + 1 {
		name := fmt.Sprintf("f%d.go", index)
		settle(t, dir, name, name)
		paths = append(paths, name)
	}
	failedBatch := func(retries int) []scriptedProcess {
		children := []scriptedProcess{newScriptedProcess("", 1), hashing(fakeHash("e"))}
		for range retries {
			children = append(children, hashing(fakeHash("c")))
		}
		return children
	}

	cold := scripted(failedBatch(0)...)
	for index, got := range observeWithStarts(t, cachingReader(cold, newTestClock()), cold, 2, dir, paths...) {
		if got != unreadable {
			t.Fatalf("without the cache, path %d: %v", index, got)
		}
	}

	warm := scripted(append([]scriptedProcess{hashing(fakeHash("a"))}, failedBatch(maxPerFileRetries)...)...)
	reader := cachingReader(warm, newTestClock())
	observeWithStarts(t, reader, warm, 1, dir, paths[0])
	got := observeWithStarts(t, reader, warm, int32(3+maxPerFileRetries), dir, paths...)
	want := []compose.FileObservation{presentAs(fakeHash("a"))}
	for range maxPerFileRetries {
		want = append(want, presentAs(fakeHash("c")))
	}
	wantSame(t, got, want...)
}
