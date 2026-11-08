package provider_test

import (
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestDecideNativeContext(t *testing.T) {
	const version, hash = "1.0.0", "hash"
	bound := func(source provider.NativeContextSource, state provider.NativeContextState, proof provider.NativeContextProof) provider.NativeContextEvidence {
		return provider.NativeContextEvidence{Source: source, State: state, Proof: proof, HarnessVersion: version, LaunchConfigHash: hash}
	}
	blockedMCP := bound(provider.NativeContextMCP, provider.NativeContextBlockedVerified, provider.NativeContextProofSideEffect)
	tests := []struct {
		name     string
		source   provider.NativeContextSource
		version  string
		hash     string
		evidence []provider.NativeContextEvidence
		want     provider.NativeContextState
	}{
		{"matching evidence", provider.NativeContextMCP, version, hash, []provider.NativeContextEvidence{blockedMCP}, provider.NativeContextBlockedVerified},
		{"different version", provider.NativeContextMCP, "1.0.1", hash, []provider.NativeContextEvidence{blockedMCP}, provider.NativeContextPossible},
		{"different config hash", provider.NativeContextMCP, version, "other", []provider.NativeContextEvidence{blockedMCP}, provider.NativeContextPossible},
		{"no evidence", provider.NativeContextMCP, version, hash, nil, provider.NativeContextPossible},
		{"other source", provider.NativeContextHooks, version, hash, []provider.NativeContextEvidence{blockedMCP}, provider.NativeContextPossible},
		{"empty version", provider.NativeContextMCP, "", hash, []provider.NativeContextEvidence{{Source: provider.NativeContextMCP, State: provider.NativeContextBlockedVerified, Proof: provider.NativeContextProofSideEffect, LaunchConfigHash: hash}}, provider.NativeContextPossible},
		{"empty hash", provider.NativeContextMCP, version, "", []provider.NativeContextEvidence{{Source: provider.NativeContextMCP, State: provider.NativeContextBlockedVerified, Proof: provider.NativeContextProofSideEffect, HarnessVersion: version}}, provider.NativeContextPossible},
		{"hooks side effect", provider.NativeContextHooks, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextHooks, provider.NativeContextBlockedVerified, provider.NativeContextProofSideEffect)}, provider.NativeContextBlockedVerified},
		{"memory read absence", provider.NativeContextMemory, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextMemory, provider.NativeContextBlockedVerified, provider.NativeContextProofReadAbsence)}, provider.NativeContextBlockedVerified},
		{"instructions documented mechanism", provider.NativeContextInstructions, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextInstructions, provider.NativeContextBlockedVerified, provider.NativeContextProofDocumentedMechanism)}, provider.NativeContextBlockedVerified},
		{"memory behavioral marker stays possible", provider.NativeContextMemory, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextMemory, provider.NativeContextBlockedVerified, provider.NativeContextProofBehavioralMarker)}, provider.NativeContextPossible},
		{"instructions behavioral marker stays possible", provider.NativeContextInstructions, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextInstructions, provider.NativeContextBlockedVerified, provider.NativeContextProofBehavioralMarker)}, provider.NativeContextPossible},
		{"memory side effect is not admissible", provider.NativeContextMemory, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextMemory, provider.NativeContextBlockedVerified, provider.NativeContextProofSideEffect)}, provider.NativeContextPossible},
		{"mcp behavioral marker stays possible", provider.NativeContextMCP, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextMCP, provider.NativeContextBlockedVerified, provider.NativeContextProofBehavioralMarker)}, provider.NativeContextPossible},
		{"observed with reported bytes", provider.NativeContextInstructions, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextInstructions, provider.NativeContextObserved, provider.NativeContextProofHarnessReportedBytes)}, provider.NativeContextObserved},
		{"observed without reported bytes", provider.NativeContextInstructions, version, hash, []provider.NativeContextEvidence{bound(provider.NativeContextInstructions, provider.NativeContextObserved, provider.NativeContextProofBehavioralMarker)}, provider.NativeContextPossible},
		{"contradictory evidence", provider.NativeContextMCP, version, hash, []provider.NativeContextEvidence{blockedMCP, bound(provider.NativeContextMCP, provider.NativeContextObserved, provider.NativeContextProofHarnessReportedBytes)}, provider.NativeContextPossible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.DecideNativeContext(provider.NativeContextFacts{Source: tt.source, HarnessVersion: tt.version, LaunchConfigHash: tt.hash, Evidence: tt.evidence})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideProtectedPaths(t *testing.T) {
	const version, hash = "1.0.0", "hash"
	refused := provider.ProtectedPathsEvidence{ReadDenied: true, WriteDenied: true, HarnessVersion: version, LaunchConfigHash: hash}
	tests := []struct {
		name     string
		version  string
		hash     string
		evidence []provider.ProtectedPathsEvidence
		want     provider.ProtectedPathsState
	}{
		{"read and write refused", version, hash, []provider.ProtectedPathsEvidence{refused}, provider.ProtectedPathsEnforced},
		{"different version", "1.0.1", hash, []provider.ProtectedPathsEvidence{refused}, provider.ProtectedPathsUnknown},
		{"different config hash", version, "other", []provider.ProtectedPathsEvidence{refused}, provider.ProtectedPathsUnknown},
		{"no evidence", version, hash, nil, provider.ProtectedPathsUnknown},
		{"only read refused", version, hash, []provider.ProtectedPathsEvidence{{ReadDenied: true, HarnessVersion: version, LaunchConfigHash: hash}}, provider.ProtectedPathsUnknown},
		{"only write refused", version, hash, []provider.ProtectedPathsEvidence{{WriteDenied: true, HarnessVersion: version, LaunchConfigHash: hash}}, provider.ProtectedPathsUnknown},
		{"empty binding", "", "", []provider.ProtectedPathsEvidence{{ReadDenied: true, WriteDenied: true}}, provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.DecideProtectedPaths(provider.ProtectedPathsFacts{HarnessVersion: tt.version, LaunchConfigHash: tt.hash, Evidence: tt.evidence})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestNativeContextReportZeroValueIsDefault(t *testing.T) {
	var report provider.NativeContextReport
	for _, source := range provider.NativeContextSources() {
		if got := report.SourceState(source); got != provider.NativeContextPossible {
			t.Fatalf("%s = %s, want possible", source, got)
		}
	}
	if got := report.ProtectedPathsState(); got != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", got)
	}
}
