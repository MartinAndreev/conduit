package acp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type sessionNewResult struct {
	SessionID     string         `json:"sessionId"`
	Models        *sessionModels `json:"models"`
	ConfigOptions []configOption `json:"configOptions"`
}

// sessionModels is the session/set_model advertisement in session/new.
type sessionModels struct {
	CurrentModelID  string `json:"currentModelId"`
	AvailableModels []struct {
		ModelID string `json:"modelId"`
	} `json:"availableModels"`
}

func (m *sessionModels) lists(modelID string) bool {
	if m == nil {
		return false
	}
	for _, model := range m.AvailableModels {
		if model.ModelID == modelID {
			return true
		}
	}
	return false
}

// configOption is a session config option. Select options list values
// directly or in groups ({"group":..., "options":[...]}).
type configOption struct {
	ID           string              `json:"id"`
	CurrentValue json.RawMessage     `json:"currentValue"`
	Category     string              `json:"category"`
	Type         string              `json:"type"`
	Options      []configOptionValue `json:"options"`
}

type configOptionValue struct {
	Value   string              `json:"value"`
	Options []configOptionValue `json:"options"`
}

func (o configOption) lists(value string) bool {
	var walk func([]configOptionValue) bool
	walk = func(values []configOptionValue) bool {
		return slices.ContainsFunc(values, func(candidate configOptionValue) bool {
			return candidate.Value == value || walk(candidate.Options)
		})
	}
	return walk(o.Options)
}

// modelOptionsListing returns the model-category options that list value.
func modelOptionsListing(options []configOption, value string) []configOption {
	var matches []configOption
	for _, option := range options {
		if option.Category == "model" && option.lists(value) {
			matches = append(matches, option)
		}
	}
	return matches
}

// selectModel applies the caller's model selection in a fixed order, each
// step against the options the agent reported last: provider (through the
// descriptor's ProviderConfigID option, because it can change the model
// option's values), model (session/set_model or the model config option),
// then variant (through the descriptor's VariantConfigID option). It returns
// the config options as last reported by the agent.
func (s *session) selectModel(ctx context.Context, request provider.OpenSessionRequest, created sessionNewResult) ([]configOption, error) {
	options, err := s.applyConfigValue(ctx, request.RequestID, "provider", s.descriptor.ProviderConfigID, request.Model.ProviderID, created.ConfigOptions)
	if err != nil {
		return nil, err
	}
	modelOptions := modelOptionsListing(options, request.Model.ModelID)
	modelDecision := rules.DecideSelection(rules.SelectionFacts{
		Requested:           request.Model.ModelID,
		SetModelListed:      created.Models.lists(request.Model.ModelID),
		ConfigOptionMatches: len(modelOptions),
	})
	refreshed, err := s.applySelection(ctx, request, modelDecision, modelOptions, request.Model.ModelID)
	if err != nil {
		return nil, err
	}
	if refreshed != nil {
		options = refreshed
	}
	return s.applyConfigValue(ctx, request.RequestID, "variant", s.descriptor.VariantConfigID, request.Model.Variant, options)
}

// applySelection performs one selection decision. It returns the refreshed
// config options when the agent reported them.
func (s *session) applySelection(ctx context.Context, request provider.OpenSessionRequest, decision rules.SelectionDecision, matches []configOption, value string) ([]configOption, error) {
	native := s.nativeSessionID()
	switch decision.Kind {
	case rules.SelectionNotRequested:
		return nil, nil
	case rules.SelectionSetModel:
		params := struct {
			SessionID string `json:"sessionId"`
			ModelID   string `json:"modelId"`
		}{SessionID: native, ModelID: value}
		_, sent, err := s.rpc.call(ctx, "session/set_model", params)
		if err != nil {
			return nil, nativeCallFailure(provider.OperationOpenSession, request.RequestID, sent, err)
		}
		return nil, nil
	case rules.SelectionConfigOption:
		return s.setConfigOption(ctx, request.RequestID, matches[0].ID, value)
	default:
		// session/new was accepted natively; the owned agent is terminated by
		// the caller, so no selection is left half-applied.
		return nil, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, nil, decision.Reason)
	}
}

func (s *session) nativeSessionID() string {
	var id string
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		id = s.nativeSession
		return nil
	})
	return id
}

// setConfigOption sends session/set_config_option and verifies the reply
// (rules.DecideSettingConfirmation): it returns the refreshed options only
// when they report value as the option's current value. A reply reporting
// another value fails as capability_unsupported; a reply without decodable
// configOptions fails as protocol_violation. Either way the caller aborts
// and reaps the agent. A JSON-RPC rejection is a native call failure.
func (s *session) setConfigOption(ctx context.Context, requestID provider.RequestID, configID, value string) ([]configOption, error) {
	params := struct {
		SessionID string `json:"sessionId"`
		ConfigID  string `json:"configId"`
		Value     string `json:"value"`
	}{SessionID: s.nativeSessionID(), ConfigID: configID, Value: value}
	raw, sent, err := s.rpc.call(ctx, "session/set_config_option", params)
	if err != nil {
		return nil, nativeCallFailure(provider.OperationOpenSession, requestID, sent, err)
	}
	var response struct {
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	var options []configOption
	facts := rules.SettingConfirmationFacts{
		OptionsReported: json.Unmarshal(raw, &response) == nil && presentJSON(response.ConfigOptions) &&
			json.Unmarshal(response.ConfigOptions, &options) == nil,
	}
	if index := slices.IndexFunc(options, func(option configOption) bool { return option.ID == configID }); index >= 0 {
		facts.OptionPresent = true
		facts.CurrentMatches = options[index].currentIs(value)
	}
	decision := rules.DecideSettingConfirmation(facts)
	switch decision.Kind {
	case rules.SettingUnverifiable:
		return nil, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, nil, decision.Reason+": "+configID)
	case rules.SettingNotApplied:
		return nil, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, nil, decision.Reason+": "+configID)
	}
	return options, nil
}

// currentIs reports whether the option's current value is value. Non-string
// current values (for example booleans) never match a string setting.
func (o configOption) currentIs(value string) bool {
	var current string
	return json.Unmarshal(o.CurrentValue, &current) == nil && current == value
}

// applySessionSettings enforces the descriptor's required session config
// values in order, each against the options last reported by the agent.
func (s *session) applySessionSettings(ctx context.Context, request provider.OpenSessionRequest, options []configOption) error {
	var err error
	for _, setting := range s.descriptor.SessionSettings {
		if options, err = s.applyConfigValue(ctx, request.RequestID, setting.ConfigID, setting.ConfigID, setting.Value, options); err != nil {
			return err
		}
	}
	return nil
}

// applyConfigValue sets one required value through the named session config
// option (rules.DecideSessionSetting) and returns the options as last
// reported by the agent. A value that cannot be honoured, including a native
// rejection of the set call, fails the open as capability_unsupported with
// the cause preserved; the caller then aborts and reaps the agent. There is
// no fallback to the agent's default.
// label names the value in errors (the descriptor may map no config ID).
func (s *session) applyConfigValue(ctx context.Context, requestID provider.RequestID, label, configID, value string, options []configOption) ([]configOption, error) {
	index := slices.IndexFunc(options, func(option configOption) bool { return option.ID == configID })
	facts := rules.SessionSettingFacts{Requested: value != "", Mapped: configID != "", OptionAdvertised: configID != "" && index >= 0}
	if facts.OptionAdvertised {
		facts.ValueListed = options[index].lists(value)
		facts.AlreadyCurrent = options[index].currentIs(value)
	}
	decision := rules.DecideSessionSetting(facts)
	switch decision.Kind {
	case rules.SessionSettingNotRequested, rules.SessionSettingAlreadyCurrent:
		return options, nil
	case rules.SessionSettingUnsupported:
		return nil, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, nil, decision.Reason+": "+label)
	}
	refreshed, err := s.setConfigOption(ctx, requestID, configID, value)
	if err != nil {
		return nil, configValueFailure(label, err)
	}
	return refreshed, nil
}

// configValueFailure maps a native rejection of a required value to
// capability_unsupported; transport and caller failures keep their delivery
// classification. The original error stays the cause in both cases.
func configValueFailure(label string, err error) error {
	var nativeErr *rpcError
	if errors.As(err, &nativeErr) {
		return adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, err, "agent rejected required session config value: "+label)
	}
	return err
}
