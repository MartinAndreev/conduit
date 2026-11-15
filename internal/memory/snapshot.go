package memory

import (
	"path"
	"strings"
)

// MaxSnapshotFileBytes bounds the size of a workspace file the snapshot reader
// hashes: 32 MiB. A larger file is unreadable, so a record citing it enters
// needs_verification instead of making composition hash an arbitrarily large
// file. Source files that agents cite are far smaller; the bound only stops
// generated or binary artefacts.
const MaxSnapshotFileBytes int64 = 32 << 20

// SnapshotPathKind is the verdict on a declared source path's syntax.
type SnapshotPathKind string

const (
	// SnapshotPathAdmitted: the path may be looked up in the workspace.
	SnapshotPathAdmitted SnapshotPathKind = "admitted"
	// SnapshotPathRefused: the path is never looked up; the file is
	// unreadable.
	SnapshotPathRefused SnapshotPathKind = "refused"
)

// SnapshotPathDecision carries, for an admitted path, its cleaned form, which
// the reader looks up and hashes.
type SnapshotPathDecision struct {
	Kind   SnapshotPathKind
	Reason string
	Clean  string
}

// DecideSnapshotPath checks a declared file source path before the workspace
// is touched. A path is stored relative to the workspace root with forward
// slashes. Refused, in this order: empty; a NUL byte; a line break, which
// cannot be passed one per line to git; a backslash or colon, which Windows
// reads as a separator, drive or stream; a leading double quote in the
// declared or the cleaned path, which git's --stdin-paths would unquote into
// another path (cleaning can bring one to the front: `./"x` becomes `"x`); an
// absolute path; a ".." segment after cleaning, which leaves the workspace;
// and a path that cleans to the workspace root itself. The byte checks run on
// the declared path: cleaning only removes bytes, so they hold for the cleaned
// path too, and a relative path never cleans to an absolute one.
func DecideSnapshotPath(declared string) SnapshotPathDecision {
	refused := func(reason string) SnapshotPathDecision {
		return SnapshotPathDecision{Kind: SnapshotPathRefused, Reason: reason}
	}
	switch {
	case declared == "":
		return refused("path is empty")
	case strings.ContainsRune(declared, 0):
		return refused("path contains a NUL byte")
	case strings.ContainsAny(declared, "\n\r"):
		return refused("path contains a line break")
	case strings.ContainsAny(declared, `\:`):
		return refused("path contains a backslash or colon")
	}
	clean := path.Clean(declared)
	switch {
	case strings.HasPrefix(declared, `"`) || strings.HasPrefix(clean, `"`):
		return refused("path starts with a double quote")
	case strings.HasPrefix(declared, "/"):
		return refused("path is absolute")
	case clean == ".." || strings.HasPrefix(clean, "../"):
		return refused("path leaves the workspace")
	case clean == ".":
		return refused("path names the workspace root")
	}
	return SnapshotPathDecision{Kind: SnapshotPathAdmitted, Reason: "path is relative to the workspace", Clean: clean}
}

// SnapshotLookup is the result of looking up an admitted path without
// following a final symlink (lstat).
type SnapshotLookup string

const (
	SnapshotLookupFound   SnapshotLookup = "found"
	SnapshotLookupMissing SnapshotLookup = "missing"
	// SnapshotLookupFailed: the lookup failed for another reason than
	// absence, such as permissions or a file in place of a directory.
	SnapshotLookupFailed SnapshotLookup = "failed"
)

// SnapshotResolution is where the directory of an admitted path lies once
// every symlink in it is resolved, relative to the resolved workspace root.
type SnapshotResolution string

const (
	SnapshotInsideRoot  SnapshotResolution = "inside"
	SnapshotOutsideRoot SnapshotResolution = "outside"
	SnapshotUnresolved  SnapshotResolution = "unresolved"
)

// SnapshotFileFacts are what the reader learns about an admitted path without
// reading the file. Parent is where the path's directory resolves; when a
// directory on the way is absent, it is where the nearest existing ancestor
// resolves, and Lookup is missing. The leaf is looked up (lstat) only under a
// parent inside the workspace, so Lookup, Symlink, Regular and Size say
// nothing about files outside it; they matter only when the lookup found the
// path.
type SnapshotFileFacts struct {
	Parent  SnapshotResolution
	Lookup  SnapshotLookup
	Symlink bool
	Regular bool
	Size    int64
	// MaxBytes is the reader's size bound, normally MaxSnapshotFileBytes.
	MaxBytes int64
}

// SnapshotFileKind is the verdict on one admitted path; each kind is one
// SourceObservation, with hash standing for present once git hashed it.
type SnapshotFileKind string

const (
	SnapshotFileHash       SnapshotFileKind = "hash"
	SnapshotFileMissing    SnapshotFileKind = "missing"
	SnapshotFileUnreadable SnapshotFileKind = "unreadable"
)

type SnapshotFileDecision struct {
	Kind   SnapshotFileKind
	Reason string
}

// DecideSnapshotFile decides whether an admitted path is hashed. In order: a
// directory that resolves outside the workspace or cannot be resolved is
// unreadable, whatever the leaf, so the answer never tells whether a file
// outside exists; an absent path is missing; a failed lookup, any symlink
// (even one pointing inside the workspace), anything but a regular file, a
// missing size bound (invalid facts) and a file over the bound are
// unreadable; anything else is hashed.
func DecideSnapshotFile(f SnapshotFileFacts) SnapshotFileDecision {
	unreadable := func(reason string) SnapshotFileDecision {
		return SnapshotFileDecision{Kind: SnapshotFileUnreadable, Reason: reason}
	}
	switch {
	case f.Parent == SnapshotOutsideRoot:
		return unreadable("file's directory resolves outside the workspace")
	case f.Parent != SnapshotInsideRoot:
		return unreadable("file's directory could not be resolved")
	case f.Lookup == SnapshotLookupMissing:
		return SnapshotFileDecision{Kind: SnapshotFileMissing, Reason: "file does not exist in the workspace"}
	case f.Lookup != SnapshotLookupFound:
		return unreadable("file could not be looked up")
	case f.Symlink:
		return unreadable("file is a symlink")
	case !f.Regular:
		return unreadable("file is not a regular file")
	case f.MaxBytes <= 0:
		return unreadable("no file size bound")
	case f.Size > f.MaxBytes:
		return unreadable("file is over the size bound")
	}
	return SnapshotFileDecision{Kind: SnapshotFileHash, Reason: "regular file inside the workspace within the size bound"}
}
