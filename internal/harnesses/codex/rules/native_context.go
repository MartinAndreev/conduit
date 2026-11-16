package rules

import "github.com/MartinAndreev/conduit/internal/sessions/provider"

// recordedLaunchConfigHash is the hash of the workspace-write launch the live
// recording of 2026-11-16 (Codex 0.155.1, darwin) applies to. It is a literal
// on purpose: computing it from the current launch would match any launch. A
// test in the parent package compares it with the current launch hash, so a
// launch change needs a new recording.
const recordedLaunchConfigHash = "da228043123decff1dff7727b3701e495d02bc4e4efdfd3688876154e08e5c0e"

// NativeContextEvidence is the Codex evidence table, transcribed from
// specs/012-project-memory/evidence.md. Entries must never be added without a
// live recording. mcp, memory, hooks and instructions have no admissible
// proof, so they have no entries and stay possible.
func NativeContextEvidence() provider.NativeContextEvidenceSet {
	return provider.NativeContextEvidenceSet{
		ProtectedPaths: []provider.ProtectedPathsEvidence{{
			ReadDenied: true, WriteDenied: true,
			HarnessVersion: "0.155.1", LaunchConfigHash: recordedLaunchConfigHash, Platform: "darwin",
		}},
	}
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
