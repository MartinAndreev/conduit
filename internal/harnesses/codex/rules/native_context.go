package rules

import "github.com/MartinAndreev/conduit/internal/sessions/provider"

// NativeContextEvidence is the Codex evidence table. It is empty until live
// recordings exist (task 1.1); entries must never be added without one.
func NativeContextEvidence() provider.NativeContextEvidenceSet {
	return provider.NativeContextEvidenceSet{}
}

// DecideNativeContextPolicy answers what is proven about one native context
// source for this launch of codex.
func DecideNativeContextPolicy(f provider.NativeContextFacts) provider.NativeContextDecision {
	return provider.DecideNativeContext(f)
}

// DecideProtectedPathsPolicy answers whether codex is proven unable to reach
// Conduit's protected directories for this launch.
func DecideProtectedPathsPolicy(f provider.ProtectedPathsFacts) provider.ProtectedPathsDecision {
	return provider.DecideProtectedPaths(f)
}
