package rules

import "testing"

func TestDecisions(t *testing.T) {
	for _, c := range []struct {
		f UseFacts
		k UseKind
	}{{UseFacts{true, false}, DiscoverForCreation}, {UseFacts{false, true}, UseExisting}, {UseFacts{false, false}, RejectConfiguration}, {UseFacts{true, true}, RejectConfiguration}} {
		d := DecideUse(c.f)
		if d.Kind != c.k || d.Reason == "" {
			t.Fatal(d)
		}
	}
	for _, c := range []struct {
		f CreationFacts
		k CreationKind
	}{{CreationFacts{true, true}, CreateInitial}, {CreationFacts{false, false}, PreserveExisting}, {CreationFacts{true, false}, RejectCreation}} {
		d := DecideCreation(c.f)
		if d.Kind != c.k || d.Reason == "" {
			t.Fatal(d)
		}
	}
	for _, c := range []struct {
		f ReadinessFacts
		k ReadinessKind
	}{{ReadinessFacts{true, false}, Ready}, {ReadinessFacts{true, true}, ReadyWithWarnings}, {ReadinessFacts{false, true}, Blocked}} {
		d := DecideReadiness(c.f)
		if d.Kind != c.k || d.Reason == "" {
			t.Fatal(d)
		}
	}
}
