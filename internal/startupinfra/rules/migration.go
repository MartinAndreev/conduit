package rules

type BackupKind string

const (
	BackupPresent  BackupKind = "backup_present"
	BackupConflict BackupKind = "backup_conflict"
)

// BackupFacts describe an already existing v1 backup, compared with the v1
// file (identity and bytes) the startup operation read and decided on.
type BackupFacts struct {
	Readable  bool
	SameFile  bool
	SameBytes bool
}
type BackupDecision struct {
	Kind   BackupKind
	Reason string
}

// DecideExistingBackup never permits overwriting a backup. Only an existing
// backup that is a hard link to the very file that was read (same identity,
// same bytes; left by an earlier or concurrent attempt) lets migration proceed.
// A separate copy, even byte-identical, would not capture an in-place edit
// made to the original before the rename, so it is a conflict.
func DecideExistingBackup(f BackupFacts) BackupDecision {
	if f.Readable && f.SameFile && f.SameBytes {
		return BackupDecision{BackupPresent, "An existing backup is a link to the read original."}
	}
	return BackupDecision{BackupConflict, "An existing backup is not a link to the read original and is never overwritten."}
}

type ReplacementKind string

const (
	ReplaceAllowed   ReplacementKind = "replace_allowed"
	SourceChanged    ReplacementKind = "source_changed"
	SourceUnreadable ReplacementKind = "source_unreadable"
)

// ReplacementFacts compare the target, re-read immediately before replacement,
// with the file the startup operation read and decided on.
type ReplacementFacts struct {
	Absent    bool
	Readable  bool
	SameFile  bool
	SameBytes bool
}
type ReplacementDecision struct {
	Kind   ReplacementKind
	Reason string
}

// DecideReplacement permits the rename only when the target is still the same
// file with the same bytes. A changed or vanished target is an expected
// concurrent outcome, not a failure: the caller re-reads and decides again. A
// technical read failure is reported as such and is never treated as changed.
func DecideReplacement(f ReplacementFacts) ReplacementDecision {
	if f.Absent || (f.Readable && (!f.SameFile || !f.SameBytes)) {
		return ReplacementDecision{SourceChanged, "Configuration changed after it was read; nothing was written."}
	}
	if !f.Readable {
		return ReplacementDecision{SourceUnreadable, "Configuration could not be re-read before replacement; nothing was written."}
	}
	return ReplacementDecision{ReplaceAllowed, "Configuration is unchanged since it was read."}
}
