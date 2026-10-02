package rules

type ResultEnforcementKind string

const (
	ResultEnforcementNone          ResultEnforcementKind = "none"
	ResultEnforcementInvalidSchema ResultEnforcementKind = "invalid_schema"
	ResultEnforcementUnsupported   ResultEnforcementKind = "unsupported"
)

type ResultEnforcementFacts struct {
	SchemaPresent   bool
	SchemaValidJSON bool
	AllowsNone      bool
}
type ResultEnforcementDecision struct {
	Kind   ResultEnforcementKind
	Reason string
}

// DecideResultEnforcement preserves schema validity precedence over enforcement support.
func DecideResultEnforcement(f ResultEnforcementFacts) ResultEnforcementDecision {
	if f.SchemaPresent && !f.SchemaValidJSON {
		return ResultEnforcementDecision{
			Kind:   ResultEnforcementInvalidSchema,
			Reason: "result schema is not valid JSON",
		}
	}
	if f.AllowsNone {
		return ResultEnforcementDecision{
			Kind:   ResultEnforcementNone,
			Reason: "result contract permits no native schema enforcement",
		}
	}
	return ResultEnforcementDecision{
		Kind:   ResultEnforcementUnsupported,
		Reason: "OpenCode native result schema is unproven",
	}
}
