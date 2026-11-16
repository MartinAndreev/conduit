package rules

import (
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestDecideNativeContextPolicy(t *testing.T) {
	evidence := []provider.NativeContextEvidence{{
		Source: provider.NativeContextMCP, State: provider.NativeContextBlockedVerified,
		Proof: provider.NativeContextProofSideEffect, HarnessVersion: "1.0.0", LaunchConfigHash: "hash",
	}}
	tests := []struct {
		name     string
		version  string
		hash     string
		evidence []provider.NativeContextEvidence
		want     provider.NativeContextState
	}{
		{"matching evidence", "1.0.0", "hash", evidence, provider.NativeContextBlockedVerified},
		{"different version", "1.0.1", "hash", evidence, provider.NativeContextPossible},
		{"different config hash", "1.0.0", "other", evidence, provider.NativeContextPossible},
		{"no evidence", "1.0.0", "hash", nil, provider.NativeContextPossible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideNativeContextPolicy(provider.NativeContextFacts{
				Source: provider.NativeContextMCP, HarnessVersion: tt.version, LaunchConfigHash: tt.hash, Evidence: tt.evidence,
			})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideProtectedPathsPolicy(t *testing.T) {
	evidence := []provider.ProtectedPathsEvidence{{ReadDenied: true, WriteDenied: true, HarnessVersion: "1.0.0", LaunchConfigHash: "hash", Platform: "darwin"}}
	tests := []struct {
		name     string
		version  string
		hash     string
		evidence []provider.ProtectedPathsEvidence
		want     provider.ProtectedPathsState
	}{
		{"matching evidence", "1.0.0", "hash", evidence, provider.ProtectedPathsEnforced},
		{"different version", "1.0.1", "hash", evidence, provider.ProtectedPathsUnknown},
		{"different config hash", "1.0.0", "other", evidence, provider.ProtectedPathsUnknown},
		{"no evidence", "1.0.0", "hash", nil, provider.ProtectedPathsUnknown},
	}
	// Every case runs on the recorded platform; another platform is covered by
	// provider.DecideProtectedPaths.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideProtectedPathsPolicy(provider.ProtectedPathsFacts{HarnessVersion: tt.version, LaunchConfigHash: tt.hash, Platform: "darwin", Evidence: tt.evidence})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideLaunchProtectedPaths(t *testing.T) {
	evidence := []provider.ProtectedPathsEvidence{{ReadDenied: true, WriteDenied: true, HarnessVersion: "1.0.0", LaunchConfigHash: "hash", Platform: "darwin"}}
	facts := provider.ProtectedPathsFacts{HarnessVersion: "1.0.0", LaunchConfigHash: "hash", Platform: "darwin", Evidence: evidence}
	applied := SandboxDecision{Kind: SandboxApply, Reason: "applied"}
	accepted := ServerInfoDecision{Kind: ServerInfoAccepted, Reason: "accepted", Version: "1.0.0"}
	if got := DecideLaunchProtectedPaths(applied, accepted, facts); got.Kind != provider.ProtectedPathsEnforced {
		t.Fatalf("sandboxed matching launch = %+v", got)
	}
	refused := DecideSandbox(SandboxFacts{Darwin: true})
	got := DecideLaunchProtectedPaths(refused, accepted, facts)
	if got.Kind != provider.ProtectedPathsUnknown || got.Reason != refused.Reason {
		t.Fatalf("unsandboxed launch = %+v, want unknown with %q", got, refused.Reason)
	}
	unidentified := DecideServerInfo(ServerInfoFacts{QueryFailed: true})
	if got := DecideLaunchProtectedPaths(applied, unidentified, facts); got.Kind != provider.ProtectedPathsUnknown || got.Reason != unidentified.Reason {
		t.Fatalf("unidentified server = %+v, want unknown with %q", got, unidentified.Reason)
	}
	if got := DecideLaunchProtectedPaths(refused, unidentified, facts); got.Reason != refused.Reason {
		t.Fatalf("sandbox refusal must take precedence: %+v", got)
	}
	facts.LaunchConfigHash = "other"
	if got := DecideLaunchProtectedPaths(applied, accepted, facts); got.Kind != provider.ProtectedPathsUnknown {
		t.Fatalf("other launch = %+v", got)
	}
}
