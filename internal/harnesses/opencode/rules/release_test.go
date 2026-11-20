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

func TestDecideClientClaim(t *testing.T) {
	cases := []struct {
		name           string
		facts          ClientClaimFacts
		wantKind       ClientClaimKind
		wantNext       ClientOwner
		wantStartsKill bool
	}{
		{"none + run reserves killed", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientActorRun}, ClientClaimKill, ClientOwnerKilled, true},
		{"none + kill reserves killed", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientActorKill}, ClientClaimKill, ClientOwnerKilled, true},
		{"none + close reserves close", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientActorClose}, ClientClaimKill, ClientOwnerClose, true},
		{"killed + run shares kill", ClientClaimFacts{Current: ClientOwnerKilled, Actor: ClientActorRun}, ClientClaimAwaitKill, ClientOwnerKilled, true},
		{"killed + kill shares kill", ClientClaimFacts{Current: ClientOwnerKilled, Actor: ClientActorKill}, ClientClaimAwaitKill, ClientOwnerKilled, true},
		{"killed + close shares kill", ClientClaimFacts{Current: ClientOwnerKilled, Actor: ClientActorClose}, ClientClaimAwaitKill, ClientOwnerKilled, true},
		{"close + run yields to close", ClientClaimFacts{Current: ClientOwnerClose, Actor: ClientActorRun}, ClientClaimYieldToClose, ClientOwnerClose, false},
		{"close + kill shares close kill", ClientClaimFacts{Current: ClientOwnerClose, Actor: ClientActorKill}, ClientClaimAwaitKill, ClientOwnerClose, true},
		{"close + close awaits repeated close release", ClientClaimFacts{Current: ClientOwnerClose, Actor: ClientActorClose}, ClientClaimAwaitKill, ClientOwnerClose, true},
		{"invalid current owner", ClientClaimFacts{Current: ClientOwner("bogus"), Actor: ClientActorClose}, ClientClaimInvalid, ClientOwner("bogus"), false},
		{"invalid actor", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientClaimActor("bogus")}, ClientClaimInvalid, ClientOwnerNone, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideClientClaim(tc.facts)
			if decision.Kind != tc.wantKind || decision.NextOwner != tc.wantNext || decision.Reason == "" {
				t.Fatalf("decision = %#v, want kind=%s next=%s", decision, tc.wantKind, tc.wantNext)
			}
			if decision.StartsKill() != tc.wantStartsKill {
				t.Fatalf("StartsKill=%t, want %t", decision.StartsKill(), tc.wantStartsKill)
			}
		})
	}
}

func TestDecideClientCloseRelease(t *testing.T) {
	confirmedRelease := ReleaseFacts{Finished: true, RootReaped: true}
	cases := []struct {
		name  string
		facts ClientCloseReleaseFacts
		want  ReleaseKind
	}{
		{"pending kill", ClientCloseReleaseFacts{KillFinished: false, KillConfirmed: false, TreeKillSupported: true, Release: confirmedRelease}, ReleasePending},
		{"confirmed", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: true, TreeKillSupported: true, Release: confirmedRelease}, ReleaseConfirmed},
		{"unconfirmed kill fails even if release finished", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: false, TreeKillSupported: true, Release: confirmedRelease}, ReleaseFailed},
		{"unconfirmed kill precedes pending release", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: false, TreeKillSupported: true, Release: ReleaseFacts{}}, ReleaseFailed},
		{"tree kill unsupported fails", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: true, TreeKillSupported: false, Release: confirmedRelease}, ReleaseFailed},
		{"pending release", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{}}, ReleasePending},
		{"failed release", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{Finished: true, RootReaped: true, ReleaseFailed: true}}, ReleaseFailed},
		{"pipe close failed", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{Finished: true, RootReaped: true, PipeCloseFailed: true}}, ReleaseFailed},
		{"finished without reap", ClientCloseReleaseFacts{KillFinished: true, KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{Finished: true}}, ReleaseFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideClientCloseRelease(tc.facts)
			if decision.Kind != tc.want || decision.Reason == "" || decision.Confirmed() != (tc.want == ReleaseConfirmed) {
				t.Fatalf("decision = %#v, want %s", decision, tc.want)
			}
		})
	}
}

func TestDecideCancelledControlRelease(t *testing.T) {
	confirmedRelease := ReleaseFacts{Finished: true, RootReaped: true}
	cases := []struct {
		name  string
		facts CancelledControlReleaseFacts
		want  ReleaseKind
	}{
		{"pending cleanup", CancelledControlReleaseFacts{TreeKillSupported: true, Release: ReleaseFacts{}}, ReleasePending},
		{"pending precedes kill failure", CancelledControlReleaseFacts{TreeKillFailed: true, TreeKillSupported: true, Release: ReleaseFacts{}}, ReleasePending},
		{"confirmed", CancelledControlReleaseFacts{TreeKillSupported: true, Release: confirmedRelease}, ReleaseConfirmed},
		{"refused kill fails even after reap and pipes", CancelledControlReleaseFacts{TreeKillFailed: true, TreeKillSupported: true, Release: confirmedRelease}, ReleaseFailed},
		{"tree kill unsupported fails", CancelledControlReleaseFacts{TreeKillSupported: false, Release: confirmedRelease}, ReleaseFailed},
		{"failed release", CancelledControlReleaseFacts{TreeKillSupported: true, Release: ReleaseFacts{Finished: true, RootReaped: true, ReleaseFailed: true}}, ReleaseFailed},
		{"pipe close failed", CancelledControlReleaseFacts{TreeKillSupported: true, Release: ReleaseFacts{Finished: true, RootReaped: true, PipeCloseFailed: true}}, ReleaseFailed},
		{"finished without reap", CancelledControlReleaseFacts{TreeKillSupported: true, Release: ReleaseFacts{Finished: true}}, ReleaseFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideCancelledControlRelease(tc.facts)
			if decision.Kind != tc.want || decision.Reason == "" || decision.Confirmed() != (tc.want == ReleaseConfirmed) {
				t.Fatalf("decision = %#v, want %s", decision, tc.want)
			}
		})
	}
}

func TestDecidePlatformRetention(t *testing.T) {
	cases := []struct {
		name  string
		facts PlatformRetentionFacts
		want  PlatformRetentionKind
	}{
		{"supported", PlatformRetentionFacts{RootRetentionSupported: true}, PlatformRetentionSupported},
		{"unsupported", PlatformRetentionFacts{RootRetentionSupported: false}, PlatformRetentionUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecidePlatformRetention(tc.facts)
			if decision.Kind != tc.want || decision.Reason == "" {
				t.Fatalf("decision = %#v, want %s", decision, tc.want)
			}
		})
	}
}
