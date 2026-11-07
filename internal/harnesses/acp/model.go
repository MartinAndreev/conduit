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

// selectModel applies the requested provider and model only through methods
// session/new advertised. The provider is applied first because it can change
// the model option's values; the refreshed options are used for the model.
// It returns the config options as last reported by the agent.
func (s *session) selectModel(ctx context.Context, request provider.OpenSessionRequest, created sessionNewResult) ([]configOption, error) {
	if request.Model.Variant != "" {
		return nil, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, nil, "model variant selection is unsupported")
	}
	options := created.ConfigOptions
	providerOptions := modelOptionsListing(options, request.Model.ProviderID)
	providerDecision := rules.DecideSelection(rules.SelectionFacts{
		Requested:           request.Model.ProviderID,
		ConfigOptionMatches: len(providerOptions),
	})
	refreshed, err := s.applySelection(ctx, request, providerDecision, providerOptions, request.Model.ProviderID)
	if err != nil {
		return nil, err
	}
	if refreshed != nil {
		options = refreshed
	}
	modelOptions := modelOptionsListing(options, request.Model.ModelID)
	modelDecision := rules.DecideSelection(rules.SelectionFacts{
		Requested:           request.Model.ModelID,
		SetModelListed:      created.Models.lists(request.Model.ModelID),
		ConfigOptionMatches: len(modelOptions),
	})
	refreshed, err = s.applySelection(ctx, request, modelDecision, modelOptions, request.Model.ModelID)
	if err != nil {
		return nil, err
	}
	if refreshed != nil {
		options = refreshed
	}
	return options, nil
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

// setConfigOption sends session/set_config_option and returns the refreshed
// options the agent reported (nil when it reported none). A JSON-RPC
// rejection is classified like any other native call failure.
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
		ConfigOptions []configOption `json:"configOptions"`
	}
	if json.Unmarshal(raw, &response) == nil && response.ConfigOptions != nil {
		return response.ConfigOptions, nil
	}
	return nil, nil
}

// currentIs reports whether the option's current value is value. Non-string
// current values (for example booleans) never match a string setting.
func (o configOption) currentIs(value string) bool {
	var current string
	return json.Unmarshal(o.CurrentValue, &current) == nil && current == value
}

// applySessionSettings enforces the descriptor's required session config
// values in order, each against the options last reported by the agent. Any
// setting that cannot be honoured, including a native rejection of the set
// call, fails the open as capability_unsupported with the cause preserved;
// the caller then aborts and reaps the agent.
func (s *session) applySessionSettings(ctx context.Context, request provider.OpenSessionRequest, options []configOption) error {
	for _, setting := range s.descriptor.SessionSettings {
		index := slices.IndexFunc(options, func(option configOption) bool { return option.ID == setting.ConfigID })
		facts := rules.SessionSettingFacts{OptionAdvertised: index >= 0}
		if index >= 0 {
			facts.ValueListed = options[index].lists(setting.Value)
			facts.AlreadyCurrent = options[index].currentIs(setting.Value)
		}
		decision := rules.DecideSessionSetting(facts)
		switch decision.Kind {
		case rules.SessionSettingAlreadyCurrent:
			continue
		case rules.SessionSettingUnsupported:
			return adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, nil, decision.Reason+": "+setting.ConfigID)
		}
		refreshed, err := s.setConfigOption(ctx, request.RequestID, setting.ConfigID, setting.Value)
		if err != nil {
			return sessionSettingFailure(setting, err)
		}
		if refreshed != nil {
			options = refreshed
		}
	}
	return nil
}

// sessionSettingFailure maps a native rejection of a required setting to
// capability_unsupported; transport and caller failures keep their delivery
// classification. The original error stays the cause in both cases.
func sessionSettingFailure(setting ConfigSetting, err error) error {
	var nativeErr *rpcError
	if errors.As(err, &nativeErr) {
		return adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectAccepted, err, "agent rejected required session config option "+setting.ConfigID)
	}
	return err
}
