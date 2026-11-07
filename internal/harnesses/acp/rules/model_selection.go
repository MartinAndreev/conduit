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

// SessionSettingKind is how one required session config value is reached.
// The same policy covers every value the client sets through a
// descriptor-named config option: the caller's provider and variant (reasoning
// effort) and the descriptor's own SessionSettings. Their semantics are
// identical once requested: the named option must exist and list the value,
// and the agent's default is never kept as a fallback.
type SessionSettingKind string

const (
	SessionSettingNotRequested   SessionSettingKind = "not_requested"
	SessionSettingApply          SessionSettingKind = "apply"
	SessionSettingAlreadyCurrent SessionSettingKind = "already_current"
	SessionSettingUnsupported    SessionSettingKind = "unsupported"
)

type SessionSettingFacts struct {
	// Requested is false when the caller left the value empty, which keeps
	// the harness default. Descriptor SessionSettings are always requested.
	Requested bool
	// Mapped is true when the descriptor names the config option that
	// carries this value.
	Mapped bool
	// OptionAdvertised is true when the agent's current config options
	// contain that config ID.
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

// DecideSessionSetting decides one required session config value. An empty
// request keeps the default without a call; anything requested that the
// descriptor or agent cannot carry is unsupported.
func DecideSessionSetting(f SessionSettingFacts) SessionSettingDecision {
	if !f.Requested {
		return SessionSettingDecision{
			Kind:   SessionSettingNotRequested,
			Reason: "no value requested; harness default is kept",
		}
	}
	if !f.Mapped {
		return SessionSettingDecision{
			Kind:   SessionSettingUnsupported,
			Reason: "harness descriptor maps no session config option for this value",
		}
	}
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

// SettingConfirmationKind says whether a set_config_option reply proves the
// required value now holds.
type SettingConfirmationKind string

const (
	SettingConfirmed    SettingConfirmationKind = "confirmed"
	SettingNotApplied   SettingConfirmationKind = "not_applied"
	SettingUnverifiable SettingConfirmationKind = "unverifiable"
)

type SettingConfirmationFacts struct {
	// OptionsReported is true when the reply carried a decodable
	// configOptions list (ACP requires the full list in the reply).
	OptionsReported bool
	// OptionPresent is true when that list contains the set config ID.
	OptionPresent bool
	// CurrentMatches is true when its currentValue is the requested value.
	CurrentMatches bool
}

type SettingConfirmationDecision struct {
	Kind   SettingConfirmationKind
	Reason string
}

// DecideSettingConfirmation fails closed: a required value counts as applied
// only when the agent's reply reports it as current. A reply without a
// decodable option list cannot prove anything and is a protocol problem; a
// reply that reports another value (or drops the option) means the agent
// did not honour the value.
func DecideSettingConfirmation(f SettingConfirmationFacts) SettingConfirmationDecision {
	if !f.OptionsReported {
		return SettingConfirmationDecision{
			Kind:   SettingUnverifiable,
			Reason: "set_config_option reply carried no decodable configOptions",
		}
	}
	if !f.OptionPresent || !f.CurrentMatches {
		return SettingConfirmationDecision{
			Kind:   SettingNotApplied,
			Reason: "set_config_option reply does not report the required value as current",
		}
	}
	return SettingConfirmationDecision{
		Kind:   SettingConfirmed,
		Reason: "agent reports the required value as current",
	}
}
