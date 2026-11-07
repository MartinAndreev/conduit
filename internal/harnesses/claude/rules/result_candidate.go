package rules

// ResultCandidateFacts describe `result.structured_output` for one native result.
type ResultCandidateFacts struct {
	// Terminal is true when DecideResultTerminal chose ResultTerminal.
	Terminal bool
	// NativeSchemaNegotiated is true when the session launched with --json-schema.
	NativeSchemaNegotiated bool
	// OutputPresent is true when structured_output is present and not null.
	OutputPresent bool
	// JSONObject is true when structured_output decodes as a JSON object.
	JSONObject bool
}

type ResultCandidateKind string

const (
	ResultCandidateEmit        ResultCandidateKind = "emit"
	ResultCandidateAbsent      ResultCandidateKind = "absent"
	ResultCandidateNotEligible ResultCandidateKind = "not_eligible"
	ResultCandidateInvalid     ResultCandidateKind = "invalid"
)

type ResultCandidateDecision struct {
	Kind   ResultCandidateKind
	Reason string
}

// DecideResultCandidate answers whether structured output may travel as an
// untrusted ResultCandidate. Only the active turn's terminal result under a
// negotiated native schema qualifies, and only a JSON object is accepted.
func DecideResultCandidate(f ResultCandidateFacts) ResultCandidateDecision {
	if !f.OutputPresent {
		return ResultCandidateDecision{
			Kind:   ResultCandidateAbsent,
			Reason: "result carries no structured output",
		}
	}
	if !f.Terminal || !f.NativeSchemaNegotiated {
		return ResultCandidateDecision{
			Kind:   ResultCandidateNotEligible,
			Reason: "structured output is not from the active turn's terminal result under a native schema",
		}
	}
	if !f.JSONObject {
		return ResultCandidateDecision{
			Kind:   ResultCandidateInvalid,
			Reason: "structured output is not a JSON object",
		}
	}
	return ResultCandidateDecision{
		Kind:   ResultCandidateEmit,
		Reason: "structured output is an untrusted candidate for the active turn",
	}
}
