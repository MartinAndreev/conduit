package rules

import "testing"

func TestDecideExistingBackup(t *testing.T) {
	for _, tc := range []struct {
		facts BackupFacts
		want  BackupKind
	}{
		{BackupFacts{Readable: true, SameFile: true, SameBytes: true}, BackupPresent},
		{BackupFacts{Readable: true, SameBytes: true}, BackupConflict},
		{BackupFacts{Readable: true, SameFile: true}, BackupConflict},
		{BackupFacts{SameFile: true, SameBytes: true}, BackupConflict},
		{BackupFacts{}, BackupConflict},
	} {
		d := DecideExistingBackup(tc.facts)
		if d.Kind != tc.want || d.Reason == "" {
			t.Fatal(tc.facts, d)
		}
	}
}

func TestDecideReplacement(t *testing.T) {
	for name, tc := range map[string]struct {
		facts ReplacementFacts
		want  ReplacementKind
	}{
		"unchanged":     {ReplacementFacts{Readable: true, SameFile: true, SameBytes: true}, ReplaceAllowed},
		"bytes changed": {ReplacementFacts{Readable: true, SameFile: true}, SourceChanged},
		"other file":    {ReplacementFacts{Readable: true, SameBytes: true}, SourceChanged},
		"absent":        {ReplacementFacts{Absent: true}, SourceChanged},
		"read failure":  {ReplacementFacts{}, SourceUnreadable},
	} {
		t.Run(name, func(t *testing.T) {
			d := DecideReplacement(tc.facts)
			if d.Kind != tc.want || d.Reason == "" {
				t.Fatal(d)
			}
		})
	}
}
