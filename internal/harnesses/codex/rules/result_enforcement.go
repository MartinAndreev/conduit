package rules

type ResultEnforcementFacts struct {
	SchemaPresent bool
	NativeAllowed bool
	SchemaValid   bool
	NoneAllowed   bool
}
type ResultEnforcementKind string

const (
	ResultEnforcementNative        ResultEnforcementKind = "native"
	ResultEnforcementNone          ResultEnforcementKind = "none"
	ResultEnforcementInvalidSchema ResultEnforcementKind = "invalid_schema"
	ResultEnforcementUnsupported   ResultEnforcementKind = "unsupported"
)

type ResultEnforcementDecision struct {
	Kind   ResultEnforcementKind
	Reason string
}

func DecideResultEnforcement(f ResultEnforcementFacts) ResultEnforcementDecision {
	if f.SchemaPresent && f.NativeAllowed {
		if !f.SchemaValid {
			return ResultEnforcementDecision{
				Kind:   ResultEnforcementInvalidSchema,
				Reason: "result schema is not valid JSON",
			}
		}
		return ResultEnforcementDecision{
			Kind:   ResultEnforcementNative,
			Reason: "native result schema enforcement can be negotiated",
		}
	}
	if f.NoneAllowed {
		return ResultEnforcementDecision{
			Kind:   ResultEnforcementNone,
			Reason: "result schema enforcement can be omitted",
		}
	}
	return ResultEnforcementDecision{
		Kind:   ResultEnforcementUnsupported,
		Reason: "requested result enforcement cannot be negotiated",
	}
}
