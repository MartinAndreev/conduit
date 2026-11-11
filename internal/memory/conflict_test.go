package memory

import "testing"

func TestDecideConflictLabel(t *testing.T) {
	counterpart := func(status RecordStatus, evaluated, eligible bool) ConflictCounterpart {
		return ConflictCounterpart{RecordID: "r2", Status: status, Evaluated: evaluated, Eligible: eligible}
	}
	tests := []struct {
		name         string
		counterparts []ConflictCounterpart
		want         ConflictKind
	}{
		{"no relations", nil, ConflictNone},
		{"evaluated eligible", []ConflictCounterpart{counterpart(StatusActive, true, true)}, ConflictLabelled},
		{"evaluated eligible regardless of stored status", []ConflictCounterpart{counterpart(StatusSuperseded, true, true)}, ConflictLabelled},
		{"evaluated ineligible", []ConflictCounterpart{counterpart(StatusActive, true, false)}, ConflictNone},
		{"unevaluated active", []ConflictCounterpart{counterpart(StatusActive, false, false)}, ConflictLabelled},
		{"unevaluated needs verification", []ConflictCounterpart{counterpart(StatusNeedsVerification, false, false)}, ConflictLabelled},
		{"unevaluated proposed", []ConflictCounterpart{counterpart(StatusProposed, false, false)}, ConflictNone},
		{"unevaluated superseded", []ConflictCounterpart{counterpart(StatusSuperseded, false, false)}, ConflictNone},
		{"one of several", []ConflictCounterpart{counterpart(StatusArchived, false, false), {RecordID: "r3", Status: StatusActive, Evaluated: true, Eligible: true}}, ConflictLabelled},

		{"empty counterpart id", []ConflictCounterpart{{Status: StatusActive}}, ConflictInvalidFacts},
		{"self relation", []ConflictCounterpart{{RecordID: "r1", Status: StatusActive}}, ConflictInvalidFacts},
		{"unknown status", []ConflictCounterpart{counterpart("draft", true, true)}, ConflictInvalidFacts},
		{"eligible without evaluation", []ConflictCounterpart{counterpart(StatusActive, false, true)}, ConflictInvalidFacts},
		{"invalid after a labelling one", []ConflictCounterpart{counterpart(StatusActive, true, true), {Status: StatusActive}}, ConflictInvalidFacts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideConflictLabel(ConflictFacts{RecordID: "r1", Counterparts: tt.counterparts})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want kind %q with a reason", got, tt.want)
			}
		})
	}
	for name, tt := range map[string]struct {
		counterparts []ConflictCounterpart
		want         ConflictKind
	}{
		"truncated without read counterparts":        {nil, ConflictLabelled},
		"truncated with only ineligible ones":        {[]ConflictCounterpart{counterpart(StatusActive, true, false), counterpart(StatusArchived, false, false)}, ConflictLabelled},
		"truncated with an invalid counterpart":      {[]ConflictCounterpart{{Status: StatusActive}}, ConflictInvalidFacts},
		"truncated with an eligible counterpart too": {[]ConflictCounterpart{counterpart(StatusActive, true, true)}, ConflictLabelled},
	} {
		got := DecideConflictLabel(ConflictFacts{RecordID: "r1", Counterparts: tt.counterparts, CounterpartsTruncated: true})
		if got.Kind != tt.want || got.Reason == "" {
			t.Fatalf("%s: decision = %+v, want kind %q", name, got, tt.want)
		}
	}
	if got := DecideConflictLabel(ConflictFacts{CounterpartsTruncated: true}); got.Kind != ConflictInvalidFacts {
		t.Fatalf("truncated without record id: decision = %+v", got)
	}
	if got := DecideConflictLabel(ConflictFacts{}); got.Kind != ConflictInvalidFacts {
		t.Fatalf("missing record id: decision = %+v", got)
	}
}
