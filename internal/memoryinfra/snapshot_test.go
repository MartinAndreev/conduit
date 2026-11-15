package memoryinfra

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/process"
)

// requireGit skips a test that needs real git when it is not on PATH.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH; the workspace snapshot reader hashes with git")
	}
}

// gitIn runs git in dir for test setup, with a fixed identity and no global
// configuration.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// workspace returns a fresh resolved temporary directory.
func workspace(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// newRepo initialises a git repository with extra init arguments.
func newRepo(t *testing.T, initArgs ...string) string {
	t.Helper()
	dir := workspace(t)
	gitIn(t, dir, append([]string{"init", "-q"}, initArgs...)...)
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stagedHash is the blob hash git stored for a committed path.
func stagedHash(t *testing.T, dir, name string) string {
	t.Helper()
	fields := strings.Fields(gitIn(t, dir, "ls-files", "-s", "--", name))
	if len(fields) < 2 {
		t.Fatalf("%s is not staged", name)
	}
	return fields[1]
}

// sha1Blob is git's SHA-1 blob hash of content, without filters.
func sha1Blob(content string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content)))
	return hex.EncodeToString(sum[:])
}

func observe(t *testing.T, reader *WorkspaceSnapshots, dir string, paths ...string) []compose.FileObservation {
	t.Helper()
	observed, err := reader.ObserveFiles(context.Background(), compose.SnapshotRef(dir), paths)
	if err != nil {
		t.Fatalf("ObserveFiles: %v", err)
	}
	if len(observed) != len(paths) {
		t.Fatalf("%d observations for %d paths", len(observed), len(paths))
	}
	return observed
}

func presentAs(hash string) compose.FileObservation {
	return compose.FileObservation{Observation: memory.SourcePresent, Hash: hash}
}

var (
	missing    = compose.FileObservation{Observation: memory.SourceMissing}
	unreadable = compose.FileObservation{Observation: memory.SourceUnreadable}
)

// countingLauncher records the git processes it starts.
type countingLauncher struct {
	mu      sync.Mutex
	started []process.Process
	args    [][]string
	env     [][]string
}

func (l *countingLauncher) Start(spec process.Spec) (process.Process, error) {
	child, err := process.ExecLauncher{}.Start(spec)
	if err == nil {
		_ = lockscope.WithMutexLock(&l.mu, func() error {
			l.started = append(l.started, child)
			l.args = append(l.args, spec.Args)
			l.env = append(l.env, spec.Env)
			return nil
		})
	}
	return child, err
}

func (l *countingLauncher) starts() (count int) {
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		count = len(l.started)
		return nil
	})
	return count
}

func TestCommittedAndModifiedFilesInSHA1Repository(t *testing.T) {
	requireGit(t)
	dir := newRepo(t, "--object-format=sha1")
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "docs/b.md", "# b\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	committedA, committedB := stagedHash(t, dir, "a.go"), stagedHash(t, dir, "docs/b.md")
	if committedA != gitIn(t, dir, "hash-object", "a.go") || len(committedA) != 40 {
		t.Fatalf("setup: staged %s", committedA)
	}

	launcher := &countingLauncher{}
	reader := NewWorkspaceSnapshots(launcher)
	got := observe(t, reader, dir, "a.go", "docs/b.md", "./docs/../a.go")
	want := []compose.FileObservation{presentAs(committedA), presentAs(committedB), presentAs(committedA)}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	write(t, dir, "a.go", "package a\n\nconst changed = true\n")
	got = observe(t, reader, dir, "a.go", "docs/b.md")
	modified := gitIn(t, dir, "hash-object", "a.go")
	if modified == committedA || !slices.Equal(got, []compose.FileObservation{presentAs(modified), presentAs(committedB)}) {
		t.Fatalf("uncommitted change: got %v, want %s", got, modified)
	}
	if launcher.starts() != 2 {
		t.Fatalf("started %d git processes for two calls", launcher.starts())
	}
	if !slices.Equal(launcher.args[0], []string{"hash-object", "--stdin-paths"}) {
		t.Fatalf("args = %v", launcher.args[0])
	}
}

func TestSHA256Repository(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	cmd := exec.Command("git", "init", "-q", "--object-format=sha256")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("installed git cannot create a SHA-256 repository: %v %s", err, out)
	}
	write(t, dir, "a.go", "package a\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	committed := stagedHash(t, dir, "a.go")
	if len(committed) != 64 {
		t.Fatalf("setup: staged %s", committed)
	}
	// A parent GIT_DIR pointing elsewhere would make git hash as SHA-1
	// outside any repository; it must not reach the child.
	t.Setenv("GIT_DIR", filepath.Join(workspace(t), "elsewhere"))
	got := observe(t, NewWorkspaceSnapshots(nil), dir, "a.go")
	if !slices.Equal(got, []compose.FileObservation{presentAs(committed)}) {
		t.Fatalf("got %v, want %s", got, committed)
	}
}

func TestGitattributesNormaliseLineEndings(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	write(t, dir, ".gitattributes", "*.txt text eol=lf\n")
	write(t, dir, "notes.txt", "one\r\ntwo\r\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	lf := sha1Blob("one\ntwo\n")
	if staged := stagedHash(t, dir, "notes.txt"); staged != lf {
		t.Fatalf("setup: staged %s, want %s", staged, lf)
	}
	got := observe(t, NewWorkspaceSnapshots(nil), dir, "notes.txt")
	if !slices.Equal(got, []compose.FileObservation{presentAs(lf)}) {
		t.Fatalf("got %v, want the LF blob %s (CRLF blob %s)", got, lf, sha1Blob("one\r\ntwo\r\n"))
	}
}

// Configuration overrides in the parent environment do not reach git: with
// core.autocrlf=true from GIT_CONFIG_* the CRLF file would hash as LF.
func TestParentGitOverridesDoNotLeak(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	write(t, dir, "crlf.txt", "a\r\nb\r\n")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.autocrlf")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	t.Setenv("GIT_WORK_TREE", workspace(t))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(workspace(t), "index"))
	launcher := &countingLauncher{}
	got := observe(t, NewWorkspaceSnapshots(launcher), dir, "crlf.txt")
	if !slices.Equal(got, []compose.FileObservation{presentAs(sha1Blob("a\r\nb\r\n"))}) {
		t.Fatalf("got %v", got)
	}
	for _, entry := range launcher.env[0] {
		if strings.HasPrefix(entry, "GIT_") && entry != "GIT_TERMINAL_PROMPT=0" && entry != "GIT_OPTIONAL_LOCKS=0" {
			t.Fatalf("child environment has %q", entry)
		}
	}
}

func TestNonRepositoryDirectoryHashesAsSHA1(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	probe := exec.Command("git", "rev-parse", "--git-dir")
	probe.Dir = dir
	if probe.Run() == nil {
		t.Skip("the temporary directory is inside a git repository")
	}
	write(t, dir, "plain.txt", "hello\r\n")
	got := observe(t, NewWorkspaceSnapshots(nil), dir, "plain.txt")
	if !slices.Equal(got, []compose.FileObservation{presentAs(sha1Blob("hello\r\n"))}) {
		t.Fatalf("got %v", got)
	}
}

// pathCase is one declared path and its expected observation.
type pathCase struct {
	name string
	path string
	want compose.FileObservation
}

// wantObservations observes each case alone and checks that git was never
// started for them.
func wantObservations(t *testing.T, reader *WorkspaceSnapshots, launcher *countingLauncher, dir string, cases []pathCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := observe(t, reader, dir, tt.path); !slices.Equal(got, []compose.FileObservation{tt.want}) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	if launcher.starts() != 0 {
		t.Fatalf("git started %d times for paths that are not hashed", launcher.starts())
	}
}

// Paths the rules refuse or that are not regular files inside the workspace
// are never handed to git; when none is admitted, git is not started. Needs
// neither symlinks nor file names Windows refuses.
func TestPathsThatAreNotHashed(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "big.bin", "12345")
	write(t, dir, "docs/x.md", "x\n")
	launcher := &countingLauncher{}
	reader := NewWorkspaceSnapshots(launcher)
	reader.maxBytes = 4
	wantObservations(t, reader, launcher, dir, []pathCase{
		{"missing", "nope.go", missing},
		{"missing directory", "nope/a.go", missing},
		{"missing nested directories", "docs/nope/deeper/x.md", missing},
		{"directory", "docs", unreadable},
		{"quote brought to the front by cleaning", `./"name"`, unreadable},
		{"quote after a parent segment", `a/../"x"`, unreadable},
		{"quoted absolute path", `./"/etc/passwd"`, unreadable},
		{"parent segment", "../a.go", unreadable},
		{"absolute", filepath.ToSlash(filepath.Join(dir, "a.go")), unreadable},
		{"newline", "a.go\nb.go", unreadable},
		{"over the size bound", "big.bin", unreadable},
		{"empty", "", unreadable},
	})
}

// Symlinks: never followed as the file itself, followed as a directory only
// when it resolves inside the workspace, and a directory leaving it hides
// whether the file outside exists.
func TestSymlinkedPaths(t *testing.T) {
	requireGit(t)
	outside := workspace(t)
	write(t, outside, "secret.txt", "secret\n")
	dir := newRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "docs/x.md", "x\n")
	if err := os.Symlink("a.go", filepath.Join(dir, "link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for link, target := range map[string]string{"out": outside, "docslink": "docs", "dangling": filepath.Join(outside, "gone")} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	launcher := &countingLauncher{}
	reader := NewWorkspaceSnapshots(launcher)
	wantObservations(t, reader, launcher, dir, []pathCase{
		{"symlink inside the workspace", "link.go", unreadable},
		{"symlinked directory leaving the workspace", "out/secret.txt", unreadable},
		{"missing file behind a directory leaving the workspace", "out/nope.txt", unreadable},
		{"missing directory behind a directory leaving the workspace", "out/nope/x.txt", unreadable},
		{"dangling symlinked directory", "dangling/x.txt", unreadable},
	})
	got := observe(t, reader, dir, "docslink/x.md")
	if !slices.Equal(got, []compose.FileObservation{presentAs(sha1Blob("x\n"))}) {
		t.Fatalf("got %v", got)
	}
}

func TestManyPathsKeepOrderInOneProcess(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	var paths []string
	var want []compose.FileObservation
	for index := range 300 {
		name := fmt.Sprintf("dir%d/file%03d.txt", index%7, index)
		switch index % 10 {
		case 3:
			want = append(want, missing)
		case 6:
			name = "../" + name
			want = append(want, unreadable)
		default:
			content := fmt.Sprintf("content %d\n", index)
			write(t, dir, name, content)
			want = append(want, presentAs(sha1Blob(content)))
		}
		paths = append(paths, name)
	}
	launcher := &countingLauncher{}
	got := observe(t, NewWorkspaceSnapshots(launcher), dir, paths...)
	if !slices.Equal(got, want) {
		t.Fatalf("observations out of order")
	}
	if launcher.starts() != 1 {
		t.Fatalf("started %d git processes", launcher.starts())
	}
}

func TestInvalidSnapshotRef(t *testing.T) {
	dir := workspace(t)
	write(t, dir, "file.txt", "x")
	refs := []string{"", "relative/dir", dir + "/./x", dir + "/", filepath.Join(dir, "missing"), filepath.Join(dir, "file.txt"), dir + "\x00"}
	for _, ref := range refs {
		observed, err := NewWorkspaceSnapshots(nil).ObserveFiles(context.Background(), compose.SnapshotRef(ref), []string{"a.go"})
		if !errors.Is(err, ErrInvalidSnapshot) || observed != nil {
			t.Fatalf("%q: observed %v, err %v", ref, observed, err)
		}
		if ref != "" && strings.Contains(err.Error(), ref) || strings.Contains(err.Error(), dir) {
			t.Fatalf("%q: error repeats the ref: %v", ref, err)
		}
	}
	_, err := NewWorkspaceSnapshots(nil).ObserveFiles(context.Background(), compose.SnapshotRef(filepath.Join(dir, "missing")), nil)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cause lost: %v", err)
	}
}

func TestGitNotFound(t *testing.T) {
	dir := workspace(t)
	write(t, dir, "a.go", "package a\n")
	t.Setenv("PATH", "")
	reader := NewWorkspaceSnapshots(nil)
	observed, err := reader.ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go"})
	if !errors.Is(err, ErrGitUnavailable) || !errors.Is(err, exec.ErrNotFound) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
	// Without an admitted path git is not needed.
	got := observe(t, reader, dir, "missing.go")
	if !slices.Equal(got, []compose.FileObservation{missing}) {
		t.Fatalf("got %v", got)
	}
}

func TestCancelledBeforeReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewWorkspaceSnapshots(nil).ObserveFiles(ctx, compose.SnapshotRef(workspace(t)), []string{"a.go"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// A failing clean filter fails git's whole batch; the control run succeeds,
// so the batch is retried per file and only the filtered file is unreadable.
func TestFailingFilterMarksOnlyItsFileUnreadable(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	gitIn(t, dir, "config", "filter.broken.clean", "false")
	gitIn(t, dir, "config", "filter.broken.required", "true")
	write(t, dir, ".gitattributes", "*.lfs filter=broken\n")
	write(t, dir, "a.go", "a\n")
	write(t, dir, "b.lfs", "b\n")
	write(t, dir, "c.go", "c\n")
	launcher := &countingLauncher{}
	got := observe(t, NewWorkspaceSnapshots(launcher), dir, "a.go", "b.lfs", "c.go")
	want := []compose.FileObservation{presentAs(sha1Blob("a\n")), unreadable, presentAs(sha1Blob("c\n"))}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Batch, control, three single-path retries.
	if launcher.starts() != 5 {
		t.Fatalf("started %d git processes", launcher.starts())
	}

	// A single failing path is unreadable without a retry.
	launcher = &countingLauncher{}
	got = observe(t, NewWorkspaceSnapshots(launcher), dir, "b.lfs")
	if !slices.Equal(got, []compose.FileObservation{unreadable}) || launcher.starts() != 2 {
		t.Fatalf("got %v after %d processes", got, launcher.starts())
	}

	// Over the retry bound the whole batch is unreadable.
	paths := []string{"b.lfs"}
	for index := range maxPerFileRetries {
		name := fmt.Sprintf("f%d.go", index)
		write(t, dir, name, name)
		paths = append(paths, name)
	}
	launcher = &countingLauncher{}
	got = observe(t, NewWorkspaceSnapshots(launcher), dir, paths...)
	for index, observed := range got {
		if observed != unreadable {
			t.Fatalf("path %d: %v", index, observed)
		}
	}
	if launcher.starts() != 2 {
		t.Fatalf("started %d git processes", launcher.starts())
	}
}

// When git cannot work in the workspace at all, the control run fails too
// and the call fails without stderr or paths in the error.
func TestBrokenRepositoryIsAnError(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	write(t, dir, "a.go", "a\n")
	config, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteString("[core\n"); err != nil {
		t.Fatal(err)
	}
	if err := config.Close(); err != nil {
		t.Fatal(err)
	}
	observed, err := NewWorkspaceSnapshots(nil).ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go"})
	if !errors.Is(err, ErrWorkspaceUnusable) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
	if strings.Contains(err.Error(), "config") || strings.Contains(err.Error(), "a.go") || strings.Contains(err.Error(), dir) {
		t.Fatalf("error carries git output or paths: %v", err)
	}
}

func TestParseHashes(t *testing.T) {
	sha1Hash, sha256Hash := strings.Repeat("a", 40), strings.Repeat("b", 64)
	tests := []struct {
		name   string
		stdout string
		want   int
		hashes []string
	}{
		{"one per line", sha1Hash + "\n" + sha256Hash + "\n", 2, []string{sha1Hash, sha256Hash}},
		{"CRLF", sha1Hash + "\r\n", 1, []string{sha1Hash}},
		{"no final newline", sha1Hash, 1, nil},
		{"too few", sha1Hash + "\n", 2, nil},
		{"too many", sha1Hash + "\n" + sha1Hash + "\n", 1, nil},
		{"invalid hash", strings.Repeat("A", 40) + "\n", 1, nil},
		{"empty line", "\n", 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHashes([]byte(tt.stdout), tt.want)
			if tt.hashes == nil {
				if !errors.Is(err, ErrGitOutput) || got != nil {
					t.Fatalf("got %v, err %v", got, err)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.hashes) {
				t.Fatalf("got %v, err %v", got, err)
			}
		})
	}
}

func TestGitEnvironment(t *testing.T) {
	parent := []string{
		"PATH=/bin", "HOME=/home/u", "XDG_CONFIG_HOME=/cfg", "TMPDIR=/tmp", "LANG=C",
		"GIT_DIR=/x", "GIT_WORK_TREE=/y", "GIT_INDEX_FILE=/z", "GIT_CONFIG_COUNT=1", "SECRET_TOKEN=s",
		"=C:=C:\\", "malformed", "SystemRoot=C:\\Windows", "Path=C:\\bin",
	}
	unix := gitEnvironment(parent, false)
	wantUnix := []string{"PATH=/bin", "HOME=/home/u", "XDG_CONFIG_HOME=/cfg", "TMPDIR=/tmp", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}
	if !slices.Equal(unix, wantUnix) {
		t.Fatalf("unix = %q", unix)
	}
	windows := gitEnvironment(parent, true)
	wantWindows := []string{"PATH=/bin", "HOME=/home/u", "XDG_CONFIG_HOME=/cfg", "TMPDIR=/tmp", "SystemRoot=C:\\Windows", "Path=C:\\bin", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}
	if !slices.Equal(windows, wantWindows) {
		t.Fatalf("windows = %q", windows)
	}
	if slices.Contains(keptEnvironment, "SYSTEMROOT") {
		t.Fatal("windows variables leaked into the shared list")
	}
}

func TestBoundedBuffer(t *testing.T) {
	buffer := &boundedBuffer{limit: 4}
	for _, chunk := range []string{"ab", "cde", "f"} {
		if n, err := buffer.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("write %q: %d %v", chunk, n, err)
		}
	}
	data, exceeded := buffer.result()
	if string(data) != "abcd" || !exceeded {
		t.Fatalf("data %q exceeded %v", data, exceeded)
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "w", "root")
	tests := map[string]bool{
		filepath.Join(root, "a"):              true,
		filepath.Join(root, "..a"):            true,
		filepath.Join(root, "..", "rootless"): false,
		filepath.Dir(root):                    false,
		"relative":                            false,
	}
	for path, want := range tests {
		if got := within(root, path); got != want {
			t.Fatalf("within(%q) = %v", path, got)
		}
	}
}
