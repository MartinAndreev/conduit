package memory

import "testing"

func TestEffectiveTrust(t *testing.T) {
	for _, stored := range Trusts() {
		if got := EffectiveTrust(stored, false); got != stored {
			t.Fatalf("unapproved %s = %s", stored, got)
		}
		if got := EffectiveTrust(stored, true); got != TrustHumanApproved {
			t.Fatalf("approved %s = %s", stored, got)
		}
	}
	if got := EffectiveTrust("backend_score", true); got != "backend_score" {
		t.Fatalf("unknown trust = %s", got)
	}
}
