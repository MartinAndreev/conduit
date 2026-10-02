package rules

import "testing"

func TestResultEnforcementFacts(t *testing.T) {
	// Explicit facts table also covers an absent schema whose validity fact is false.
	for _, f := range []ResultEnforcementFacts{{false, false, true}, {true, false, true}, {true, false, false}, {true, true, false}, {true, true, true}} {
		d := DecideResultEnforcement(f)
		want := ResultEnforcementUnsupported
		if f.SchemaPresent && !f.SchemaValidJSON {
			want = ResultEnforcementInvalidSchema
		} else if f.AllowsNone {
			want = ResultEnforcementNone
		}
		if d.Kind != want || d.Reason == "" {
			t.Fatalf("decision=%#v", d)
		}
	}
}
