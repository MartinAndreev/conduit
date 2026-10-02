package rules

import "testing"

func TestResultEnforcementPrecedence(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		facts := ResultEnforcementFacts{
			SchemaPresent: mask&1 != 0,
			NativeAllowed: mask&2 != 0,
			SchemaValid:   mask&4 != 0,
			NoneAllowed:   mask&8 != 0,
		}
		want := ResultEnforcementUnsupported
		if facts.SchemaPresent && facts.NativeAllowed {
			want = ResultEnforcementNative
			if !facts.SchemaValid {
				want = ResultEnforcementInvalidSchema
			}
		} else if facts.NoneAllowed {
			want = ResultEnforcementNone
		}
		decision := DecideResultEnforcement(facts)
		if decision.Kind != want || decision.Reason == "" {
			t.Fatalf("facts = %#v, decision = %#v", facts, decision)
		}
	}
}
