package rules

import (
	"strings"
	"unicode"
)

// SandboxFacts describe whether the OpenCode server can be launched under the
// macOS Seatbelt sandbox that denies the protected Conduit directory.
type SandboxFacts struct {
	// Darwin is true when the host OS is macOS.
	Darwin bool
	// SandboxExecFound is true when the sandbox-exec executable was found.
	SandboxExecFound bool
	// HomeResolved is true when the user home directory was resolved.
	HomeResolved bool
	// ProtectedPath is the absolute protected directory (home + ".conduit"),
	// to be embedded in the Seatbelt profile as a string literal.
	ProtectedPath string
	// ResolvedProtectedPath is ProtectedPath with symlinks resolved, when it
	// differs from ProtectedPath. Seatbelt matches resolved paths, so a
	// symlinked home or ~/.conduit needs this second denial. Empty when the
	// literal path is already resolved.
	ResolvedProtectedPath string
	// SymlinkResolutionFailed is true when the protected path could not be
	// resolved for a reason other than its absence.
	SymlinkResolutionFailed bool
}

type SandboxKind string

const (
	// SandboxApply launches the server under sandbox-exec.
	SandboxApply SandboxKind = "apply"
	// SandboxUnsupportedPlatform launches unsandboxed: no OS sandbox for
	// OpenCode exists on this platform.
	SandboxUnsupportedPlatform SandboxKind = "unsupported_platform"
	// SandboxExecMissing launches unsandboxed: sandbox-exec was not found.
	SandboxExecMissing SandboxKind = "sandbox_exec_missing"
	// SandboxHomeUnresolved launches unsandboxed: the protected directory
	// cannot be derived without the user home.
	SandboxHomeUnresolved SandboxKind = "home_unresolved"
	// SandboxPathInadmissible launches unsandboxed: the protected path cannot
	// be embedded safely in the profile.
	SandboxPathInadmissible SandboxKind = "path_inadmissible"
	// SandboxPathUnresolved launches unsandboxed: symlinks in the protected
	// path could not be resolved, so a denial of the literal path alone might
	// not match what Seatbelt sees.
	SandboxPathUnresolved SandboxKind = "path_unresolved"
)

type SandboxDecision struct {
	Kind   SandboxKind
	Reason string
}

// Applied reports whether the server is launched under the sandbox.
func (d SandboxDecision) Applied() bool { return d.Kind == SandboxApply }

// DecideSandbox answers whether the OpenCode server is launched under
// sandbox-exec. Every refusal launches unsandboxed instead of failing the
// session (every harness gets project memory, Martin 2026-11-09); the caller
// reports protected paths as unknown with the reason.
//
// Both the literal and the symlink-resolved protected path are denied, so
// both must be admissible. A path is admissible only when it is an absolute POSIX path (Seatbelt is
// macOS only, so this does not depend on the host OS) and contains no `"`, `\` or
// control character: those are refused rather than escaped, so the Seatbelt
// string literal is always the path verbatim.
func DecideSandbox(f SandboxFacts) SandboxDecision {
	switch {
	case !f.Darwin:
		return SandboxDecision{Kind: SandboxUnsupportedPlatform, Reason: "no OS sandbox for OpenCode on this platform"}
	case !f.SandboxExecFound:
		return SandboxDecision{Kind: SandboxExecMissing, Reason: "sandbox-exec was not found; OpenCode runs without an OS sandbox"}
	case !f.HomeResolved:
		return SandboxDecision{Kind: SandboxHomeUnresolved, Reason: "user home directory is unresolved; OpenCode runs without an OS sandbox"}
	case f.SymlinkResolutionFailed:
		return SandboxDecision{Kind: SandboxPathUnresolved, Reason: "symlinks in the protected directory path could not be resolved; OpenCode runs without an OS sandbox"}
	case !admissibleProfilePath(f.ProtectedPath) || (f.ResolvedProtectedPath != "" && !admissibleProfilePath(f.ResolvedProtectedPath)):
		return SandboxDecision{Kind: SandboxPathInadmissible, Reason: "protected directory path cannot be embedded in a Seatbelt profile; OpenCode runs without an OS sandbox"}
	default:
		return SandboxDecision{Kind: SandboxApply, Reason: "OpenCode runs under sandbox-exec with the protected directory denied"}
	}
}

func admissibleProfilePath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, `"\`) {
		return false
	}
	return !strings.ContainsFunc(path, unicode.IsControl)
}
