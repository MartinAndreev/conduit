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
