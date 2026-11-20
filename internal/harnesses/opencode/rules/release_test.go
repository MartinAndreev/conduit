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
		wantNeedsKill  bool
		wantNaturalRel bool
	}{
		{"none + natural reserves natural", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientActorNatural}, ClientClaimFinalizeNaturally, ClientOwnerNatural, false, true},
		{"none + kill reserves killed", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientActorKill}, ClientClaimKill, ClientOwnerKilled, true, false},
		{"none + close reserves close", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientActorClose}, ClientClaimKill, ClientOwnerClose, true, false},
		{"natural + natural awaits natural", ClientClaimFacts{Current: ClientOwnerNatural, Actor: ClientActorNatural}, ClientClaimAwaitNatural, ClientOwnerNatural, false, true},
		{"natural + kill skips kill and awaits natural", ClientClaimFacts{Current: ClientOwnerNatural, Actor: ClientActorKill}, ClientClaimAwaitNatural, ClientOwnerNatural, false, true},
		{"natural + close skips kill and awaits natural", ClientClaimFacts{Current: ClientOwnerNatural, Actor: ClientActorClose}, ClientClaimAwaitNatural, ClientOwnerNatural, false, true},
		{"killed + natural awaits post-kill release", ClientClaimFacts{Current: ClientOwnerKilled, Actor: ClientActorNatural}, ClientClaimAwaitKill, ClientOwnerKilled, true, false},
		{"killed + kill shares single kill", ClientClaimFacts{Current: ClientOwnerKilled, Actor: ClientActorKill}, ClientClaimAwaitKill, ClientOwnerKilled, true, false},
		{"killed + close shares single kill and release", ClientClaimFacts{Current: ClientOwnerKilled, Actor: ClientActorClose}, ClientClaimAwaitKill, ClientOwnerKilled, true, false},
		{"close + natural yields to close", ClientClaimFacts{Current: ClientOwnerClose, Actor: ClientActorNatural}, ClientClaimYieldToClose, ClientOwnerClose, false, false},
		{"close + kill shares close kill", ClientClaimFacts{Current: ClientOwnerClose, Actor: ClientActorKill}, ClientClaimAwaitKill, ClientOwnerClose, true, false},
		{"close + close awaits repeated close release", ClientClaimFacts{Current: ClientOwnerClose, Actor: ClientActorClose}, ClientClaimAwaitKill, ClientOwnerClose, true, false},
		{"invalid current owner", ClientClaimFacts{Current: ClientOwner("bogus"), Actor: ClientActorClose}, ClientClaimInvalid, ClientOwner("bogus"), false, false},
		{"invalid actor", ClientClaimFacts{Current: ClientOwnerNone, Actor: ClientClaimActor("bogus")}, ClientClaimInvalid, ClientOwnerNone, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideClientClaim(tc.facts)
			if decision.Kind != tc.wantKind || decision.NextOwner != tc.wantNext || decision.Reason == "" {
				t.Fatalf("decision = %#v, want kind=%s next=%s", decision, tc.wantKind, tc.wantNext)
			}
			if decision.NeedsKill() != tc.wantNeedsKill || decision.NaturalRelease() != tc.wantNaturalRel {
				t.Fatalf("NeedsKill=%t NaturalRelease=%t, want %t %t", decision.NeedsKill(), decision.NaturalRelease(), tc.wantNeedsKill, tc.wantNaturalRel)
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
		{"natural confirmed", ClientCloseReleaseFacts{NaturalRelease: true, Release: confirmedRelease}, ReleaseConfirmed},
		{"natural ignores unconfirmed kill and tree support", ClientCloseReleaseFacts{NaturalRelease: true, KillConfirmed: false, TreeKillSupported: false, Release: confirmedRelease}, ReleaseConfirmed},
		{"natural pending", ClientCloseReleaseFacts{NaturalRelease: true, Release: ReleaseFacts{}}, ReleasePending},
		{"natural failed reap", ClientCloseReleaseFacts{NaturalRelease: true, Release: ReleaseFacts{Finished: true, ReleaseFailed: true}}, ReleaseFailed},
		{"killed confirmed", ClientCloseReleaseFacts{KillConfirmed: true, TreeKillSupported: true, Release: confirmedRelease}, ReleaseConfirmed},
		{"killed unconfirmed fails even if release finished", ClientCloseReleaseFacts{KillConfirmed: false, TreeKillSupported: true, Release: confirmedRelease}, ReleaseFailed},
		{"killed unconfirmed precedes pending", ClientCloseReleaseFacts{KillConfirmed: false, TreeKillSupported: true, Release: ReleaseFacts{}}, ReleaseFailed},
		{"tree kill unsupported fails", ClientCloseReleaseFacts{KillConfirmed: true, TreeKillSupported: false, Release: confirmedRelease}, ReleaseFailed},
		{"killed pending release", ClientCloseReleaseFacts{KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{}}, ReleasePending},
		{"killed failed release", ClientCloseReleaseFacts{KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{Finished: true, RootReaped: true, ReleaseFailed: true}}, ReleaseFailed},
		{"killed pipe close failed", ClientCloseReleaseFacts{KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{Finished: true, RootReaped: true, PipeCloseFailed: true}}, ReleaseFailed},
		{"killed finished without reap", ClientCloseReleaseFacts{KillConfirmed: true, TreeKillSupported: true, Release: ReleaseFacts{Finished: true}}, ReleaseFailed},
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
