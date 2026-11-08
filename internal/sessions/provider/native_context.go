package provider

import "slices"

// NativeContextSources lists the source kinds every adapter must report.
func NativeContextSources() []NativeContextSource {
	return []NativeContextSource{NativeContextMemory, NativeContextInstructions, NativeContextMCP, NativeContextHooks}
}

// NativeContextReport states what is proven about a harness's own context
// loading. The zero value reads as the honest default: possible and unknown.
type NativeContextReport struct {
	Sources        []NativeContextObservation
	ProtectedPaths ProtectedPathsState
	// ProtectedPathsReason explains ProtectedPaths for people and logs.
	ProtectedPathsReason string
}

type NativeContextObservation struct {
	Source NativeContextSource
	State  NativeContextState
	Reason string
}

// SourceState returns the reported state, or possible for an unreported source.
func (r NativeContextReport) SourceState(source NativeContextSource) NativeContextState {
	for _, observation := range r.Sources {
		if observation.Source == source && observation.State != "" {
			return observation.State
		}
	}
	return NativeContextPossible
}

// ProtectedPathsState returns the reported state, or unknown when unreported.
func (r NativeContextReport) ProtectedPathsState() ProtectedPathsState {
	if r.ProtectedPaths == "" {
		return ProtectedPathsUnknown
	}
	return r.ProtectedPaths
}

// NewNativeContextReport assembles a report covering every source kind. Adapters
// supply decisions from their own rules, so no adapter can omit a source.
func NewNativeContextReport(decide func(NativeContextSource) NativeContextDecision, paths ProtectedPathsDecision) NativeContextReport {
	sources := NativeContextSources()
	observations := make([]NativeContextObservation, 0, len(sources))
	for _, source := range sources {
		decision := decide(source)
		observations = append(observations, NativeContextObservation{Source: source, State: decision.Kind, Reason: decision.Reason})
	}
	return NativeContextReport{Sources: observations, ProtectedPaths: paths.Kind, ProtectedPathsReason: paths.Reason}
}

// NativeContextEvidenceSet is one harness's recorded proof. Its zero value is
// the correct content until a live recording exists.
type NativeContextEvidenceSet struct {
	Sources        []NativeContextEvidence
	ProtectedPaths []ProtectedPathsEvidence
}

// NativeContextEvidence is one recorded proof, bound to the exact harness
// version and launch configuration it was recorded with.
type NativeContextEvidence struct {
	Source           NativeContextSource
	State            NativeContextState
	Proof            NativeContextProof
	HarnessVersion   string
	LaunchConfigHash string
}

// ProtectedPathsEvidence records real attempts by the harness to use a
// protected directory. Both a read and a write must have been refused.
type ProtectedPathsEvidence struct {
	ReadDenied       bool
	WriteDenied      bool
	HarnessVersion   string
	LaunchConfigHash string
}

type NativeContextFacts struct {
	Source           NativeContextSource
	HarnessVersion   string
	LaunchConfigHash string
	Evidence         []NativeContextEvidence
}

// NativeContextDecision's Kind is the state to report for the source.
type NativeContextDecision struct {
	Kind   NativeContextState
	Reason string
}

type ProtectedPathsFacts struct {
	HarnessVersion   string
	LaunchConfigHash string
	Evidence         []ProtectedPathsEvidence
}

// ProtectedPathsDecision's Kind is the state to report for protected paths.
type ProtectedPathsDecision struct {
	Kind   ProtectedPathsState
	Reason string
}

// DecideNativeContext is the semantics shared by every adapter's
// DecideNativeContextPolicy. Only evidence for this exact version and launch
// configuration counts, and only with a proof admissible for the claimed state.
// Contradictory evidence proves neither state, so it stays possible.
func DecideNativeContext(f NativeContextFacts) NativeContextDecision {
	var blocked, observed bool
	if bindingKnown(f.HarnessVersion, f.LaunchConfigHash) {
		for _, evidence := range f.Evidence {
			if evidence.Source != f.Source || evidence.HarnessVersion != f.HarnessVersion || evidence.LaunchConfigHash != f.LaunchConfigHash {
				continue
			}
			if !proofAdmissible(evidence) {
				continue
			}
			blocked = blocked || evidence.State == NativeContextBlockedVerified
			observed = observed || evidence.State == NativeContextObserved
		}
	}
	switch {
	case blocked && observed:
		return NativeContextDecision{Kind: NativeContextPossible, Reason: "evidence both blocks and observes this source"}
	case blocked:
		return NativeContextDecision{Kind: NativeContextBlockedVerified, Reason: "blocking is proven for this harness version and launch configuration"}
	case observed:
		return NativeContextDecision{Kind: NativeContextObserved, Reason: "the harness reports loading this source and its exact bytes are known"}
	default:
		return NativeContextDecision{Kind: NativeContextPossible, Reason: "no admissible evidence for this harness version and launch configuration"}
	}
}

// proofAdmissible keeps each claim to the proof that can support it. MCP and
// hooks leave a side effect when they run. Memory and instructions do not, so
// they need an observed absence of reads or a documented, tested mechanism.
func proofAdmissible(evidence NativeContextEvidence) bool {
	switch evidence.State {
	case NativeContextObserved:
		return evidence.Proof == NativeContextProofHarnessReportedBytes
	case NativeContextBlockedVerified:
		switch evidence.Source {
		case NativeContextMCP, NativeContextHooks:
			return evidence.Proof == NativeContextProofSideEffect
		case NativeContextMemory, NativeContextInstructions:
			return evidence.Proof == NativeContextProofReadAbsence || evidence.Proof == NativeContextProofDocumentedMechanism
		}
	}
	return false
}

// DecideProtectedPaths is the semantics shared by every adapter's
// DecideProtectedPathsPolicy.
func DecideProtectedPaths(f ProtectedPathsFacts) ProtectedPathsDecision {
	enforced := bindingKnown(f.HarnessVersion, f.LaunchConfigHash) && slices.ContainsFunc(f.Evidence, func(evidence ProtectedPathsEvidence) bool {
		return evidence.ReadDenied && evidence.WriteDenied &&
			evidence.HarnessVersion == f.HarnessVersion && evidence.LaunchConfigHash == f.LaunchConfigHash
	})
	if enforced {
		return ProtectedPathsDecision{Kind: ProtectedPathsEnforced, Reason: "harness read and write attempts were refused for this version and launch configuration"}
	}
	return ProtectedPathsDecision{Kind: ProtectedPathsUnknown, Reason: "no refused read and write recorded for this harness version and launch configuration"}
}

// bindingKnown refuses an empty version or hash, which identify no launch.
func bindingKnown(harnessVersion, launchConfigHash string) bool {
	return harnessVersion != "" && launchConfigHash != ""
}
