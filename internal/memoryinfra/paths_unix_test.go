//go:build !windows

package memoryinfra

import "testing"

// Names Windows refuses (quotes) and Unix ENOTDIR semantics: decoy files a
// quoted path would reach if git unquoted it (`"/etc/passwd"` would become
// the real /etc/passwd), and a regular file used as a directory.
func TestQuotedPathsWithDecoysAndFilesAsDirectories(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, `"name"`, "decoy\n")
	write(t, dir, `"x"`, "decoy\n")
	write(t, dir, `"/etc/passwd"`, "decoy\n")
	launcher := &countingLauncher{}
	wantObservations(t, NewWorkspaceSnapshots(launcher), launcher, dir, []pathCase{
		{"quote brought to the front by cleaning", `./"name"`, unreadable},
		{"quote after a parent segment", `a/../"x"`, unreadable},
		{"quoted absolute path with a local decoy", `./"/etc/passwd"`, unreadable},
		{"file in place of a directory", "a.go/x", unreadable},
		{"file under a missing directory under a file", "a.go/x/y", unreadable},
	})
}
