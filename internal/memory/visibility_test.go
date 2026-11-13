package memory

import "testing"

func TestEffectiveVisibility(t *testing.T) {
	tests := []struct {
		name     string
		scope    Scope
		approved bool
		want     Visibility
	}{
		{"approved attempt observation", Scope{Visibility: VisibilityAttempt, Origin: OriginAttempt}, true, VisibilityProject},
		{"unapproved attempt observation", Scope{Visibility: VisibilityAttempt, Origin: OriginAttempt}, false, VisibilityAttempt},
		{"approved human version in attempt scope", Scope{Visibility: VisibilityAttempt, Origin: OriginHuman}, true, VisibilityAttempt},
		{"approved project observation", Scope{Visibility: VisibilityProject, Origin: OriginAttempt}, true, VisibilityProject},
		{"unapproved human project version", Scope{Visibility: VisibilityProject, Origin: OriginHuman}, false, VisibilityProject},
		{"unknown visibility", Scope{Visibility: "team", Origin: OriginAttempt}, true, "team"},
		{"unknown origin", Scope{Visibility: VisibilityAttempt, Origin: "import"}, true, VisibilityAttempt},
	}
	for _, tt := range tests {
		if got := EffectiveVisibility(tt.scope, tt.approved); got != tt.want {
			t.Fatalf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}
