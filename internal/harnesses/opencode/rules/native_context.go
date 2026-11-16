package rules

import "github.com/MartinAndreev/conduit/internal/sessions/provider"

// RecordedLaunchConfigHash is the launch hash of the sandboxed macOS
// `opencode serve` of this adapter without a symlinked home: sandbox-exec with
// the Seatbelt profile denying "<conduit-dir>", wrapping "<opencode>" serve
// --hostname 127.0.0.1 --port 0. It is a literal on purpose: computing it from
// the current launch would match any launch. A test in the parent package
// compares it with the current launch hash, so a launch change needs a new
// recording. It binds the live recording of 2026-11-16 in evidence.md.
const RecordedLaunchConfigHash = "d5d6c4cedfc5d7e023ceee293f974d4ce0b3c8993d7520c9af65956c0ed40c36"

// NativeContextEvidence is the OpenCode evidence table, transcribed from
// specs/012-project-memory/evidence.md. Entries must never be added without a
// live recording through the adapter's own-server launch. Native sources have
// no admissible proof there, so they have no entries and stay possible.
func NativeContextEvidence() provider.NativeContextEvidenceSet {
	return provider.NativeContextEvidenceSet{
		ProtectedPaths: []provider.ProtectedPathsEvidence{{
			ReadDenied: true, WriteDenied: true,
			HarnessVersion: "2.0.22", LaunchConfigHash: RecordedLaunchConfigHash, Platform: "darwin",
		}},
	}
}

// DecideNativeContextPolicy answers what is proven about one native context
// source for this launch of opencode.
func DecideNativeContextPolicy(f provider.NativeContextFacts) provider.NativeContextDecision {
	return provider.DecideNativeContext(f)
}

// DecideProtectedPathsPolicy answers whether opencode is proven unable to reach
// Conduit's protected directories for this launch.
func DecideProtectedPathsPolicy(f provider.ProtectedPathsFacts) provider.ProtectedPathsDecision {
	return provider.DecideProtectedPaths(f)
}

// DecideLaunchProtectedPaths answers protected paths for one actual server
// launch. A server launched without the sandbox is never protected, whatever
// the evidence, and reports the sandbox refusal as its reason; a sandboxed
// server whose version was not accepted from server.info reports that
// refusal; otherwise the evidence bound to version, hash and platform decides.
func DecideLaunchProtectedPaths(sandbox SandboxDecision, info ServerInfoDecision, f provider.ProtectedPathsFacts) provider.ProtectedPathsDecision {
	if !sandbox.Applied() {
		return provider.ProtectedPathsDecision{Kind: provider.ProtectedPathsUnknown, Reason: sandbox.Reason}
	}
	if info.Kind != ServerInfoAccepted {
		return provider.ProtectedPathsDecision{Kind: provider.ProtectedPathsUnknown, Reason: info.Reason}
	}
	return DecideProtectedPathsPolicy(f)
}
