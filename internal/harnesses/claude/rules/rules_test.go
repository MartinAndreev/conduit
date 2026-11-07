package rules

import "testing"

func TestDecideResultTerminal(t *testing.T) {
	tests := []struct {
		name  string
		facts ResultTerminalFacts
		want  ResultTerminalKind
	}{
		{"no active turn is foreign", ResultTerminalFacts{IntersectsActiveInputs: true, TerminalReason: "completed"}, ResultForeign},
		{"foreign uuids precede final reason", ResultTerminalFacts{ActiveTurn: true, TerminalReason: "completed"}, ResultForeign},
		{"foreign uuids precede aborted reason", ResultTerminalFacts{ActiveTurn: true, TerminalReason: TerminalReasonAbortedStreaming}, ResultForeign},
		{"aborted streaming is intermediate", ResultTerminalFacts{ActiveTurn: true, IntersectsActiveInputs: true, TerminalReason: TerminalReasonAbortedStreaming}, ResultIntermediate},
		{"aborted tools is intermediate", ResultTerminalFacts{ActiveTurn: true, IntersectsActiveInputs: true, TerminalReason: TerminalReasonAbortedTools}, ResultIntermediate},
		{"completed is terminal", ResultTerminalFacts{ActiveTurn: true, IntersectsActiveInputs: true, TerminalReason: "completed"}, ResultTerminal},
		{"api error is terminal", ResultTerminalFacts{ActiveTurn: true, IntersectsActiveInputs: true, TerminalReason: "api_error"}, ResultTerminal},
		{"absent reason is terminal", ResultTerminalFacts{ActiveTurn: true, IntersectsActiveInputs: true}, ResultTerminal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideResultTerminal(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideResultOutcome(t *testing.T) {
	tests := []struct {
		facts ResultOutcomeFacts
		want  ResultOutcomeKind
	}{
		{ResultOutcomeFacts{Subtype: "success"}, ResultSucceeded},
		{ResultOutcomeFacts{Subtype: "success", IsError: true}, ResultFailed},
		{ResultOutcomeFacts{Subtype: "error_during_execution"}, ResultFailed},
		{ResultOutcomeFacts{Subtype: "error_max_turns", IsError: true}, ResultFailed},
		{ResultOutcomeFacts{Subtype: "error"}, ResultSucceeded},
	}
	for _, tt := range tests {
		if got := DecideResultOutcome(tt.facts); got.Kind != tt.want || got.Reason == "" {
			t.Fatalf("facts %+v decision %+v, want %s", tt.facts, got, tt.want)
		}
	}
}

func TestDecideResultCandidate(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		facts := ResultCandidateFacts{Terminal: mask&1 != 0, NativeSchemaNegotiated: mask&2 != 0, OutputPresent: mask&4 != 0, JSONObject: mask&8 != 0}
		want := ResultCandidateEmit
		switch {
		case !facts.OutputPresent:
			want = ResultCandidateAbsent
		case !facts.Terminal || !facts.NativeSchemaNegotiated:
			want = ResultCandidateNotEligible
		case !facts.JSONObject:
			want = ResultCandidateInvalid
		}
		if got := DecideResultCandidate(facts); got.Kind != want || got.Reason == "" {
			t.Fatalf("facts %+v decision %+v, want %s", facts, got, want)
		}
	}
}

func TestDecideStartTurnPrecedence(t *testing.T) {
	ok := StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, ResultContractMatches: true}
	tests := []struct {
		name  string
		facts StartTurnFacts
		want  StartTurnKind
	}{
		{"closing precedes occupied", StartTurnFacts{Closing: true, ActiveTurn: true}, StartTurnClosing},
		{"active precedes binding", StartTurnFacts{ActiveTurn: true}, StartTurnOccupied},
		{"unresolved steer precedes binding", StartTurnFacts{SteerUnresolved: true}, StartTurnOccupied},
		{"session differs", StartTurnFacts{AssignmentIdentityMatches: true, ResultContractMatches: true}, StartTurnBindingDiffers},
		{"assignment differs", StartTurnFacts{SessionMatches: true, ResultContractMatches: true}, StartTurnBindingDiffers},
		{"contract differs", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true}, StartTurnContractChanged},
		{"allowed", ok, StartTurnAllowed},
		{"closing precedes release", StartTurnFacts{Closing: true, ReleaseRequested: true}, StartTurnClosing},
		{"release with nothing unresolved", StartTurnFacts{ReleaseRequested: true, SessionMatches: true, AssignmentIdentityMatches: true, ResultContractMatches: true}, StartTurnReleaseNothingUnresolved},
		{"release mismatch precedes active turn", StartTurnFacts{ReleaseRequested: true, SteerUnresolved: true, ActiveTurn: true}, StartTurnReleaseMismatch},
		{"matching release still refused by active turn", StartTurnFacts{ReleaseRequested: true, SteerUnresolved: true, ReleaseMatchesUnresolvedSteer: true, ActiveTurn: true}, StartTurnOccupied},
		{"matching release still refused by binding", StartTurnFacts{ReleaseRequested: true, SteerUnresolved: true, ReleaseMatchesUnresolvedSteer: true}, StartTurnBindingDiffers},
		{"matching release allowed", StartTurnFacts{ReleaseRequested: true, SteerUnresolved: true, ReleaseMatchesUnresolvedSteer: true, SessionMatches: true, AssignmentIdentityMatches: true, ResultContractMatches: true}, StartTurnAllowedReleasingSteer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideStartTurn(tt.facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideSteerPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		facts SteerFacts
		want  SteerKind
	}{
		{"session before mode", SteerFacts{}, SteerSessionDiffers},
		{"mode before binding", SteerFacts{SessionMatches: true}, SteerModeUnsupported},
		{"missing turn", SteerFacts{SessionMatches: true, ActiveTurnMode: true}, SteerBindingUnresolved},
		{"ending turn", SteerFacts{SessionMatches: true, ActiveTurnMode: true, ActiveTurnMatches: true, TurnEnding: true}, SteerBindingUnresolved},
		{"unresolved steer", SteerFacts{SessionMatches: true, ActiveTurnMode: true, ActiveTurnMatches: true, SteerUnresolved: true}, SteerBindingUnresolved},
		{"allowed", SteerFacts{SessionMatches: true, ActiveTurnMode: true, ActiveTurnMatches: true}, SteerAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideSteer(tt.facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideClosePrecedence(t *testing.T) {
	tests := []struct {
		name  string
		facts CloseFacts
		want  CloseKind
	}{
		{"foreign session precedes closing", CloseFacts{Closing: true}, CloseSessionDiffers},
		{"closing precedes detach", CloseFacts{SessionMatches: true, Closing: true, ExplicitDetach: true}, CloseAlreadyClosing},
		{"detach refused with active work", CloseFacts{SessionMatches: true, ExplicitDetach: true, ActiveOrUnresolvedWork: true}, CloseDetachUnsupported},
		{"detach refused when idle", CloseFacts{SessionMatches: true, ExplicitDetach: true}, CloseDetachUnsupported},
		{"require quiescent refused with work", CloseFacts{SessionMatches: true, RequireQuiescent: true, ActiveOrUnresolvedWork: true}, CloseWorkUnresolved},
		{"require quiescent allowed when idle", CloseFacts{SessionMatches: true, RequireQuiescent: true}, CloseAllowed},
		{"interrupt first with work", CloseFacts{SessionMatches: true, InterruptThenClose: true, ActiveOrUnresolvedWork: true}, CloseInterruptFirst},
		{"interrupt policy when idle", CloseFacts{SessionMatches: true, InterruptThenClose: true}, CloseAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideClose(tt.facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestOwnedProcessEvidenceRequiresAllProof(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		facts := QuiescenceFacts{RootReaped: mask&1 != 0, TreeKillSupported: mask&2 != 0, KillAttempted: mask&4 != 0, KillConfirmed: mask&8 != 0}
		want := QuiescenceUnproven
		if mask == 15 {
			want = QuiescenceConfirmed
		}
		if d := DecideQuiescence(facts); d.Kind != want {
			t.Fatalf("facts %#v decision %#v", facts, d)
		}
	}
	for _, tc := range []struct {
		reaped, supported bool
		kind              OwnedProcessTerminationKind
	}{{false, false, OwnedProcessKillRequired}, {false, true, OwnedProcessKillRequired}, {true, true, OwnedProcessKillRequired}, {true, false, OwnedProcessRootAlreadyReaped}} {
		if d := DecideOwnedProcessTermination(tc.reaped, tc.supported); d.Kind != tc.kind {
			t.Fatalf("decision %#v", d)
		}
	}
}

func TestDecideResultEnforcementPrecedence(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		facts := ResultEnforcementFacts{SchemaPresent: mask&1 != 0, NativeAllowed: mask&2 != 0, SchemaValid: mask&4 != 0, NoneAllowed: mask&8 != 0}
		want := ResultEnforcementUnsupported
		if facts.SchemaPresent && facts.NativeAllowed {
			want = ResultEnforcementNative
			if !facts.SchemaValid {
				want = ResultEnforcementInvalidSchema
			}
		} else if facts.NoneAllowed {
			want = ResultEnforcementNone
		}
		if d := DecideResultEnforcement(facts); d.Kind != want || d.Reason == "" {
			t.Fatalf("facts %#v decision %#v", facts, d)
		}
	}
}

func TestDecideAuthentication(t *testing.T) {
	for source, want := range map[string]AuthenticationKind{
		"none": AuthenticationMissing, "": AuthenticationUnknown,
		"claude.ai": AuthenticationPresent, "apiKeyHelper": AuthenticationPresent,
	} {
		if d := DecideAuthentication(source); d.Kind != want || d.Reason == "" {
			t.Fatalf("token source %q decision %+v, want %s", source, d, want)
		}
	}
}

func TestDecideStatusTransition(t *testing.T) {
	for _, tc := range []struct {
		facts StatusFacts
		want  StatusKind
	}{{StatusFacts{}, StatusApply}, {StatusFacts{Closing: true}, StatusIgnoredWhileEnded}, {StatusFacts{Ended: true}, StatusIgnoredWhileEnded}} {
		if d := DecideStatusTransition(tc.facts); d.Kind != tc.want || d.Reason == "" {
			t.Fatalf("facts %+v decision %+v", tc.facts, d)
		}
	}
}

func TestDecideToolOutcome(t *testing.T) {
	for _, tc := range []struct {
		facts ToolOutcomeFacts
		want  ToolOutcomeKind
	}{
		{ToolOutcomeFacts{}, ToolOutcomeSucceeded},
		{ToolOutcomeFacts{IsError: true}, ToolOutcomeFailed},
		{ToolOutcomeFacts{Interrupted: true}, ToolOutcomeCancelled},
		{ToolOutcomeFacts{IsError: true, Interrupted: true}, ToolOutcomeCancelled},
	} {
		if d := DecideToolOutcome(tc.facts); d.Kind != tc.want || d.Reason == "" {
			t.Fatalf("facts %+v decision %+v", tc.facts, d)
		}
	}
}

func TestDecideUnobservedSteer(t *testing.T) {
	tests := []struct {
		name  string
		facts UnobservedSteerFacts
		want  UnobservedSteerKind
	}{
		{"nothing unresolved", UnobservedSteerFacts{QueuedTurnCountKnown: true}, UnobservedSteerNone},
		{"no lifecycle and zero queued is not observed", UnobservedSteerFacts{SteerUnresolved: true, Dispatch: SteerWritten, QueuedTurnCountKnown: true}, UnobservedSteerNotObserved},
		{"queued turns keep doubt", UnobservedSteerFacts{SteerUnresolved: true, Dispatch: SteerWritten, QueuedTurnCountKnown: true, QueuedTurnCount: 1}, UnobservedSteerDoubtRemains},
		{"absent count keeps doubt", UnobservedSteerFacts{SteerUnresolved: true, Dispatch: SteerWritten}, UnobservedSteerDoubtRemains},
		{"reserved only is withdrawn", UnobservedSteerFacts{SteerUnresolved: true, Dispatch: SteerReserved, QueuedTurnCountKnown: true}, UnobservedSteerWithdrawn},
		{"write in flight keeps doubt despite zero queued", UnobservedSteerFacts{SteerUnresolved: true, Dispatch: SteerDispatching, QueuedTurnCountKnown: true}, UnobservedSteerDoubtRemains},
		{"lifecycle keeps doubt despite zero queued", UnobservedSteerFacts{SteerUnresolved: true, Dispatch: SteerWritten, LifecycleObserved: true, QueuedTurnCountKnown: true}, UnobservedSteerDoubtRemains},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideUnobservedSteer(tt.facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideResultFinality(t *testing.T) {
	for reason, want := range map[string]ResultFinalityKind{
		TerminalReasonAbortedStreaming: ResultFinalIntermediate,
		TerminalReasonAbortedTools:     ResultFinalIntermediate,
		"completed":                    ResultFinal,
		"api_error":                    ResultFinal,
		"":                             ResultFinal,
	} {
		if got := DecideResultFinality(reason); got.Kind != want || got.Reason == "" {
			t.Fatalf("reason %q decision %+v, want %s", reason, got, want)
		}
	}
}
