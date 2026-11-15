package memory

import "testing"

func TestDecideSnapshotPath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		want   SnapshotPathKind
		reason string
		clean  string
	}{
		{"plain", "a.go", SnapshotPathAdmitted, "path is relative to the workspace", "a.go"},
		{"nested", "internal/memory/a.go", SnapshotPathAdmitted, "path is relative to the workspace", "internal/memory/a.go"},
		{"cleaned dot segments", "./docs//x/../a.md", SnapshotPathAdmitted, "path is relative to the workspace", "docs/a.md"},
		{"dotfile", ".gitattributes", SnapshotPathAdmitted, "path is relative to the workspace", ".gitattributes"},
		{"quote inside", `a"b.go`, SnapshotPathAdmitted, "path is relative to the workspace", `a"b.go`},
		{"empty", "", SnapshotPathRefused, "path is empty", ""},
		{"NUL", "a\x00.go", SnapshotPathRefused, "path contains a NUL byte", ""},
		{"NUL precedes line break", "a\x00\n", SnapshotPathRefused, "path contains a NUL byte", ""},
		{"newline", "a\n.go", SnapshotPathRefused, "path contains a line break", ""},
		{"carriage return", "a.go\r", SnapshotPathRefused, "path contains a line break", ""},
		{"line break precedes backslash", "\\a\n", SnapshotPathRefused, "path contains a line break", ""},
		{"backslash", `docs\a.md`, SnapshotPathRefused, "path contains a backslash or colon", ""},
		{"drive", "C:/a.go", SnapshotPathRefused, "path contains a backslash or colon", ""},
		{"stream", "a.go:stream", SnapshotPathRefused, "path contains a backslash or colon", ""},
		{"backslash precedes quote", `"a\b`, SnapshotPathRefused, "path contains a backslash or colon", ""},
		{"leading quote", `"a.go"`, SnapshotPathRefused, "path starts with a double quote", ""},
		{"quote precedes absolute", `"/a`, SnapshotPathRefused, "path starts with a double quote", ""},
		{"quote after cleaning a dot", `./"name"`, SnapshotPathRefused, "path starts with a double quote", ""},
		{"quote after cleaning a parent", `a/../"x"`, SnapshotPathRefused, "path starts with a double quote", ""},
		{"quoted absolute after cleaning", `./"/etc/passwd"`, SnapshotPathRefused, "path starts with a double quote", ""},
		{"quote precedes leaving", `"/../..`, SnapshotPathRefused, "path starts with a double quote", ""},
		{"absolute", "/etc/passwd", SnapshotPathRefused, "path is absolute", ""},
		{"absolute with dot dot", "/../a", SnapshotPathRefused, "path is absolute", ""},
		{"parent", "..", SnapshotPathRefused, "path leaves the workspace", ""},
		{"leaves", "../a.go", SnapshotPathRefused, "path leaves the workspace", ""},
		{"leaves after cleaning", "docs/../../a.go", SnapshotPathRefused, "path leaves the workspace", ""},
		{"dot dot name is not a segment", "..a/b..", SnapshotPathAdmitted, "path is relative to the workspace", "..a/b.."},
		{"root", ".", SnapshotPathRefused, "path names the workspace root", ""},
		{"root after cleaning", "docs/..", SnapshotPathRefused, "path names the workspace root", ""},
		{"trailing slash", "docs/", SnapshotPathAdmitted, "path is relative to the workspace", "docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSnapshotPath(tt.path)
			if got.Kind != tt.want || got.Reason != tt.reason || got.Clean != tt.clean {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestDecideSnapshotFile(t *testing.T) {
	file := SnapshotFileFacts{Parent: SnapshotInsideRoot, Lookup: SnapshotLookupFound, Regular: true, Size: 10, MaxBytes: 10}
	with := func(change func(*SnapshotFileFacts)) SnapshotFileFacts {
		f := file
		change(&f)
		return f
	}
	tests := []struct {
		name   string
		facts  SnapshotFileFacts
		want   SnapshotFileKind
		reason string
	}{
		{"regular file at the bound", file, SnapshotFileHash, "regular file inside the workspace within the size bound"},
		{"empty file", with(func(f *SnapshotFileFacts) { f.Size = 0 }), SnapshotFileHash, "regular file inside the workspace within the size bound"},
		{"parent outside", with(func(f *SnapshotFileFacts) { f.Parent = SnapshotOutsideRoot }), SnapshotFileUnreadable, "file's directory resolves outside the workspace"},
		{"parent outside precedes missing", SnapshotFileFacts{Parent: SnapshotOutsideRoot, Lookup: SnapshotLookupMissing}, SnapshotFileUnreadable, "file's directory resolves outside the workspace"},
		{"parent unresolved", with(func(f *SnapshotFileFacts) { f.Parent = SnapshotUnresolved }), SnapshotFileUnreadable, "file's directory could not be resolved"},
		{"parent unresolved precedes missing", SnapshotFileFacts{Parent: SnapshotUnresolved, Lookup: SnapshotLookupMissing}, SnapshotFileUnreadable, "file's directory could not be resolved"},
		{"unknown parent", with(func(f *SnapshotFileFacts) { f.Parent = "" }), SnapshotFileUnreadable, "file's directory could not be resolved"},
		{"missing", SnapshotFileFacts{Parent: SnapshotInsideRoot, Lookup: SnapshotLookupMissing}, SnapshotFileMissing, "file does not exist in the workspace"},
		{"missing precedes the other facts", with(func(f *SnapshotFileFacts) { f.Lookup, f.Symlink, f.Size = SnapshotLookupMissing, true, 99 }), SnapshotFileMissing, "file does not exist in the workspace"},
		{"lookup failed", with(func(f *SnapshotFileFacts) { f.Lookup = SnapshotLookupFailed }), SnapshotFileUnreadable, "file could not be looked up"},
		{"unknown lookup", with(func(f *SnapshotFileFacts) { f.Lookup = "" }), SnapshotFileUnreadable, "file could not be looked up"},
		{"failed lookup precedes symlink", with(func(f *SnapshotFileFacts) { f.Lookup, f.Symlink = SnapshotLookupFailed, true }), SnapshotFileUnreadable, "file could not be looked up"},
		{"symlink", with(func(f *SnapshotFileFacts) { f.Symlink, f.Regular = true, false }), SnapshotFileUnreadable, "file is a symlink"},
		{"symlink precedes not regular", with(func(f *SnapshotFileFacts) { f.Symlink = true }), SnapshotFileUnreadable, "file is a symlink"},
		{"directory", with(func(f *SnapshotFileFacts) { f.Regular = false }), SnapshotFileUnreadable, "file is not a regular file"},
		{"not regular precedes size", with(func(f *SnapshotFileFacts) { f.Regular, f.Size = false, 99 }), SnapshotFileUnreadable, "file is not a regular file"},
		{"no size bound", with(func(f *SnapshotFileFacts) { f.MaxBytes = 0 }), SnapshotFileUnreadable, "no file size bound"},
		{"negative size bound", with(func(f *SnapshotFileFacts) { f.MaxBytes = -1 }), SnapshotFileUnreadable, "no file size bound"},
		{"over the bound", with(func(f *SnapshotFileFacts) { f.Size = 11 }), SnapshotFileUnreadable, "file is over the size bound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSnapshotFile(tt.facts)
			if got.Kind != tt.want || got.Reason != tt.reason {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestMaxSnapshotFileBytes(t *testing.T) {
	if MaxSnapshotFileBytes != 32*1024*1024 {
		t.Fatalf("MaxSnapshotFileBytes = %d", MaxSnapshotFileBytes)
	}
}
