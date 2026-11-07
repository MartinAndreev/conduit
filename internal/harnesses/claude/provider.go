package claude

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/claude/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const (
	adapterVersion  = "claude-code-stream-json/1"
	protocolVersion = "claude-p-stream-json"
	providerName    = "claude"
)

// Adapter implements the harness provider contract by driving the user's own
// `claude` executable in headless stream-json mode.
type Adapter struct {
	config Config
}

func New(config Config) *Adapter {
	return &Adapter{config: withDefaults(config)}
}

// Inspect reports declared and fixture evidence without launching claude.
func (a *Adapter) Inspect(_ context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	return provider.ProviderDescriptor{
		ProviderName:    providerName,
		AdapterVersion:  adapterVersion,
		HarnessVersion:  "unknown (live evidence baseline 2.1.316)",
		ProtocolVersion: protocolVersion,
		Capabilities:    claudeCapabilities(request.Model, "", a.config.Now()),
	}, nil
}

// OpenSession launches one long-lived `claude -p` process, initializes its
// control protocol, and refuses a missing login. Every failure after launch
// kills and reaps the owned process before returning.
func (a *Adapter) OpenSession(ctx context.Context, request provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.OpenSessionResult{}, adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, false, err)
	}
	if request.Workspace.Path == "" || request.Model.ModelID == "" {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, "workspace path and explicit model ID are required")
	}
	if request.PermissionMode != provider.PermissionDeny || !request.Workspace.Writable {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "Claude adapter supports only a writable workspace with deny permission mode")
	}
	actual, err := negotiateResultContract(request.ResultContract)
	if err != nil {
		return provider.OpenSessionResult{}, err
	}
	adapterSessionID, err := a.config.NewID("claude-session")
	if err != nil {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate adapter session ID")
	}
	nativeSessionID, err := a.config.NewUUID()
	if err != nil {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate native session ID")
	}
	var schema json.RawMessage
	if actual == provider.SchemaNative {
		schema = request.ResultContract.Schema
	}
	proc, err := a.config.Launcher.Start(processruntime.Spec{
		Path:       a.config.ExecutablePath,
		Args:       launchArguments(nativeSessionID, request.Model.ModelID, schema),
		Dir:        request.Workspace.Path,
		Env:        launchEnvironment(a.config.Environ()),
		ReplaceEnv: true,
	})
	if err != nil {
		return provider.OpenSessionResult{}, &provider.ProviderError{
			Code: provider.ErrorExecutableMissing, Operation: provider.OperationOpenSession,
			RequestID: provider.Some(request.RequestID), Retry: provider.RetryNever,
			RemoteEffect: provider.RemoteEffectNotSent, Cause: err,
		}
	}
	s := newSession(a.config, sessionBinding{
		ref:           provider.SessionRef{AdapterSessionID: provider.AdapterSessionID(adapterSessionID)},
		nativeSession: nativeSessionID,
		workspace:     request.Workspace.Path,
		assignment:    request.Assignment,
		model:         request.Model,
		contract:      provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual},
	}, proc)
	s.start()

	if err := s.initialize(ctx, request); err != nil {
		return provider.OpenSessionResult{}, s.abortOpen(ctx, err)
	}
	return provider.OpenSessionResult{Session: s, Contract: s.contract}, nil
}

func negotiateResultContract(contract provider.ResultContractRef) (provider.SchemaEnforcement, error) {
	decision := rules.DecideResultEnforcement(rules.ResultEnforcementFacts{
		SchemaPresent: len(contract.Schema) != 0,
		NativeAllowed: contract.Allows(provider.SchemaNative),
		SchemaValid:   json.Valid(contract.Schema),
		NoneAllowed:   contract.Allows(provider.SchemaNone),
	})
	switch decision.Kind {
	case rules.ResultEnforcementNative:
		return provider.SchemaNative, nil
	case rules.ResultEnforcementNone:
		return provider.SchemaNone, nil
	case rules.ResultEnforcementInvalidSchema:
		return "", adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, decision.Reason)
	default:
		return "", adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, decision.Reason)
	}
}

// claudeCapabilities reports evidence honestly. The 2026-11-07 live recording
// (claude 2.1.316, no login) proves only the control protocol, delivery echo,
// interrupt receipt and result shapes. Model output, tools, steer during an
// active turn, structured_output and sandbox behaviour are fixture-only.
func claudeCapabilities(model provider.ModelSelection, harnessVersion string, observedAt time.Time) provider.CapabilitySnapshot {
	const fixtureOnly = "synthesized fixture shapes; not observed live with a logged-in claude"
	items := []provider.CapabilityObservation{
		{Capability: provider.CapabilityEventText, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: fixtureOnly},
		{Capability: provider.CapabilityEventTool, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: fixtureOnly},
		{Capability: provider.CapabilityEventCommand, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: fixtureOnly},
		{Capability: provider.CapabilityEventFile, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: fixtureOnly},
		{Capability: provider.CapabilityEventReasoning, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: fixtureOnly + "; thinking text is not a proven summary"},
		{Capability: provider.CapabilitySteerActiveTurn, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Targeting: provider.TargetSessionLease, Detail: "priority now message; steer during an active turn was not observed live"},
		{Capability: provider.CapabilitySteerNextBoundary, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityTurnInterrupt, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Targeting: provider.TargetSessionLease, Detail: "interrupt control response is a receipt; only a later result proves the stop"},
		{Capability: provider.CapabilitySessionResume, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilitySessionObserveState, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: "adapter-local partial observation"},
		{Capability: provider.CapabilityPermissionRoundTrip, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: "can_use_tool is deferred; equivalence of acceptEdits, sandboxed Bash and disallowed web tools to deny mode is unverified"},
		{Capability: provider.CapabilityQuestionRoundTrip, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityResultNativeSchema, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: "--json-schema structured_output was not observed live"},
		{Capability: provider.CapabilityEventsNativeReplay, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
	}
	return provider.CapabilitySnapshot{
		ProviderName: providerName, AdapterVersion: adapterVersion, HarnessVersion: harnessVersion,
		ProtocolVersion: protocolVersion, Model: model, ObservedAt: observedAt, Items: items,
	}
}

var _ provider.Provider = (*Adapter)(nil)
