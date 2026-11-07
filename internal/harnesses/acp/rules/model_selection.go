package rules

// SelectionKind is how one requested model selection value is applied.
type SelectionKind string

const (
	SelectionNotRequested SelectionKind = "not_requested"
	SelectionSetModel     SelectionKind = "set_model"
	SelectionConfigOption SelectionKind = "config_option"
	SelectionUnsupported  SelectionKind = "unsupported"
	SelectionAmbiguous    SelectionKind = "ambiguous"
)

type SelectionFacts struct {
	// Requested is the value to select (a provider or model ID); empty means
	// the harness default is kept.
	Requested string
	// SetModelListed is true when session/new advertised session/set_model
	// models and this value is one of them. Only model IDs may use it.
	SetModelListed bool
	// ConfigOptionMatches counts model-category config options from
	// session/new that list this value.
	ConfigOptionMatches int
}

type SelectionDecision struct {
	Kind   SelectionKind
	Reason string
}

// DecideSelection applies a value only through a method the agent advertised.
// session/set_model takes precedence; a config option must match exactly one
// model-category option so the value cannot be applied to the wrong selector.
func DecideSelection(f SelectionFacts) SelectionDecision {
	if f.Requested == "" {
		return SelectionDecision{
			Kind:   SelectionNotRequested,
			Reason: "no value requested; harness default is kept",
		}
	}
	if f.SetModelListed {
		return SelectionDecision{
			Kind:   SelectionSetModel,
			Reason: "agent advertised session/set_model with this model",
		}
	}
	switch {
	case f.ConfigOptionMatches == 1:
		return SelectionDecision{
			Kind:   SelectionConfigOption,
			Reason: "exactly one model-category config option lists this value",
		}
	case f.ConfigOptionMatches > 1:
		return SelectionDecision{
			Kind:   SelectionAmbiguous,
			Reason: "several model-category config options list this value",
		}
	default:
		return SelectionDecision{
			Kind:   SelectionUnsupported,
			Reason: "agent advertised no model selection for this value",
		}
	}
}

// SessionSettingKind is how one descriptor-required session config setting
// is reached.
type SessionSettingKind string

const (
	SessionSettingApply          SessionSettingKind = "apply"
	SessionSettingAlreadyCurrent SessionSettingKind = "already_current"
	SessionSettingUnsupported    SessionSettingKind = "unsupported"
)

type SessionSettingFacts struct {
	// OptionAdvertised is true when the agent's current config options
	// contain the setting's config ID.
	OptionAdvertised bool
	// ValueListed is true when that option lists the required value.
	ValueListed bool
	// AlreadyCurrent is true when the option's current value is the
	// required value.
	AlreadyCurrent bool
}

type SessionSettingDecision struct {
	Kind   SessionSettingKind
	Reason string
}

// DecideSessionSetting requires every descriptor setting to hold before the
// session is usable. A missing option or unlisted value is unsupported; the
// session never continues under the agent's default.
func DecideSessionSetting(f SessionSettingFacts) SessionSettingDecision {
	if !f.OptionAdvertised {
		return SessionSettingDecision{
			Kind:   SessionSettingUnsupported,
			Reason: "agent does not advertise the required session config option",
		}
	}
	if f.AlreadyCurrent {
		return SessionSettingDecision{
			Kind:   SessionSettingAlreadyCurrent,
			Reason: "required session config value is already current",
		}
	}
	if !f.ValueListed {
		return SessionSettingDecision{
			Kind:   SessionSettingUnsupported,
			Reason: "agent does not list the required session config value",
		}
	}
	return SessionSettingDecision{
		Kind:   SessionSettingApply,
		Reason: "required session config value is set through session/set_config_option",
	}
}
