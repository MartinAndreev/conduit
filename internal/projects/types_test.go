package projects

import "testing"

func TestRegistrationPolicy(t *testing.T) {
	available := Directory{Kind: Available, Name: "a", CanonicalPath: "/a"}
	existing := Project{ID: "existing", Name: "a", CanonicalPath: "/a"}
	for _, tc := range []struct {
		facts RegistrationFacts
		kind  RegistrationKind
	}{
		{RegistrationFacts{Directory: available}, RegistrationAllowed},
		{RegistrationFacts{Directory: available, Existing: &existing}, RegistrationExisting},
		{RegistrationFacts{Directory: Directory{Kind: Missing}, Existing: &existing}, RegistrationRejected},
		{RegistrationFacts{Directory: Directory{Kind: Available, Name: "a"}}, RegistrationRejected},
	} {
		got := DecideRegistration(tc.facts)
		if got.Kind != tc.kind || got.Reason == "" {
			t.Fatalf("%+v", got)
		}
	}
}
func TestOpeningPolicy(t *testing.T) {
	for _, kind := range []AvailabilityKind{Available, Missing, Unreadable, NotDirectory, InvalidPath, CanonicalizationFailed} {
		got := DecideOpening(Directory{Kind: kind})
		want := OpeningUnavailable
		if kind == Available {
			want = OpeningAllowed
		}
		if got.Kind != want || got.Reason == "" {
			t.Fatalf("%+v", got)
		}
	}
}
