package rules

import "testing"

func TestDecideRelease(t *testing.T) {
	cases := []struct {
		name  string
		facts ReleaseFacts
		want  ReleaseKind
	}{
		{"running", ReleaseFacts{}, ReleasePending},
		{"running with a reaped root", ReleaseFacts{RootReaped: true}, ReleasePending},
		{"running precedes failure", ReleaseFacts{ReleaseFailed: true}, ReleasePending},
		{"reaped and closed", ReleaseFacts{Finished: true, RootReaped: true}, ReleaseConfirmed},
		{"release failed", ReleaseFacts{Finished: true, ReleaseFailed: true}, ReleaseFailed},
		{"pipe close failed", ReleaseFacts{Finished: true, RootReaped: true, PipeCloseFailed: true}, ReleaseFailed},
		{"finished without reap", ReleaseFacts{Finished: true}, ReleaseFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideRelease(tc.facts)
			if decision.Kind != tc.want || decision.Reason == "" || decision.Confirmed() != (tc.want == ReleaseConfirmed) {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}
