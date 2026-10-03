// Package rules contains deterministic configuration eligibility decisions.
package rules

type UseKind string

const (
	UseExisting         UseKind = "use_existing"
	DiscoverForCreation UseKind = "discover_for_creation"
	RejectConfiguration UseKind = "reject_configuration"
)

type UseFacts struct {
	Missing bool
	Valid   bool
}
type UseDecision struct {
	Kind   UseKind
	Reason string
}

func DecideUse(f UseFacts) UseDecision {
	if f.Missing && !f.Valid {
		return UseDecision{DiscoverForCreation, "Configuration is absent."}
	}
	if !f.Missing && f.Valid {
		return UseDecision{UseExisting, "Existing configuration is valid."}
	}
	return UseDecision{RejectConfiguration, "Configuration cannot be used."}
}

type CreationKind string

const (
	CreateInitial    CreationKind = "create_initial"
	PreserveExisting CreationKind = "preserve_existing"
	RejectCreation   CreationKind = "reject_creation"
)

type CreationFacts struct {
	Missing           bool
	DiscoveryComplete bool
}
type CreationDecision struct {
	Kind   CreationKind
	Reason string
}

func DecideCreation(f CreationFacts) CreationDecision {
	if !f.Missing {
		return CreationDecision{PreserveExisting, "User configuration is preserved."}
	}
	if f.DiscoveryComplete {
		return CreationDecision{CreateInitial, "Discovery completed before initial creation."}
	}
	return CreationDecision{RejectCreation, "Initial creation requires completed discovery."}
}

type ReadinessKind string

const (
	Ready             ReadinessKind = "ready"
	ReadyWithWarnings ReadinessKind = "ready_with_warnings"
	Blocked           ReadinessKind = "blocked"
)

type ReadinessFacts struct {
	Valid    bool
	Warnings bool
}
type ReadinessDecision struct {
	Kind   ReadinessKind
	Reason string
}

func DecideReadiness(f ReadinessFacts) ReadinessDecision {
	if !f.Valid {
		return ReadinessDecision{Blocked, "Final configuration could not be confirmed."}
	}
	if f.Warnings {
		return ReadinessDecision{ReadyWithWarnings, "Configuration is ready; harness observations have warnings."}
	}
	return ReadinessDecision{Ready, "Configuration and local discovery are ready; account and model capabilities remain unknown."}
}
