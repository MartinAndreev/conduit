package rules

import "github.com/MartinAndreev/conduit/internal/sessions/provider"

// recordedLaunchConfigHash is the hash of the launch configuration the live
// recording applies to. It is a literal on purpose: computing it from the
// current launch would match any launch. A test in the parent package compares
// it with the current launch hash, so a launch change needs a new recording.
const recordedLaunchConfigHash = "9e7cac6dd98e7c21ec1929386d6f1a816ed31f2299544e5816ee895fdf015ad0"

// NativeContextEvidence is the Claude evidence table, transcribed from
// specs/012-project-memory/evidence.md. Entries must never be added without a
// live recording. mcp, memory, hooks and instructions have no admissible proof
// there, so they have no entries and stay possible.
func NativeContextEvidence() provider.NativeContextEvidenceSet {
	return provider.NativeContextEvidenceSet{
		ProtectedPaths: []provider.ProtectedPathsEvidence{{
			ReadDenied: true, WriteDenied: true,
			HarnessVersion: "2.1.316", LaunchConfigHash: recordedLaunchConfigHash, Platform: "darwin",
		}},
	}
}

// DecideNativeContextPolicy answers what is proven about one native context
// source for this launch of claude.
func DecideNativeContextPolicy(f provider.NativeContextFacts) provider.NativeContextDecision {
	return provider.DecideNativeContext(f)
}

// DecideProtectedPathsPolicy answers whether claude is proven unable to reach
// Conduit's protected directories for this launch.
func DecideProtectedPathsPolicy(f provider.ProtectedPathsFacts) provider.ProtectedPathsDecision {
	return provider.DecideProtectedPaths(f)
}
