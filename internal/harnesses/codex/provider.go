package codex

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const adapterVersion = "codex-app-server/1"

// Adapter implements the harness provider contract using Codex App Server.
type Adapter struct {
	config Config
}

func New(config Config) *Adapter {
	return &Adapter{config: withDefaults(config)}
}

func (a *Adapter) Inspect(_ context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	return provider.ProviderDescriptor{
		ProviderName:    "codex",
		AdapterVersion:  adapterVersion,
		HarnessVersion:  "unknown (archived fixture baseline 0.155.1)",
		ProtocolVersion: "app-server-jsonrpc-v2",
		Capabilities:    codexCapabilities(request.Model, a.config.Now()),
	}, nil
}

func (a *Adapter) OpenSession(ctx context.Context, request provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.OpenSessionResult{}, adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, false, err)
	}
	if request.Workspace.Path == "" || request.Model.ModelID == "" {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, "workspace path and explicit model ID are required")
	}
	if request.PermissionMode != provider.PermissionDeny || a.config.Approval != ApprovalNever {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "this adapter slice supports deny/never permissions only")
	}
	if !request.Workspace.Writable && a.config.Sandbox == SandboxWorkspaceWrite {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, "workspace-write sandbox conflicts with a read-only workspace binding")
	}
	actual, err := negotiateResultContract(request.ResultContract)
	if err != nil {
		return provider.OpenSessionResult{}, err
	}
	adapterSessionID, err := a.config.NewID("codex-session")
	if err != nil {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate adapter session ID")
	}
	proc, err := a.config.Launcher.Start(processruntime.Spec{
		Path: a.config.ExecutablePath,
		Args: []string{"app-server"},
		Dir:  request.Workspace.Path,
	})
	if err != nil {
		return provider.OpenSessionResult{}, &provider.ProviderError{
			Code: provider.ErrorExecutableMissing, Operation: provider.OperationOpenSession,
			RequestID: provider.Some(request.RequestID), Retry: provider.RetryNever,
			RemoteEffect: provider.RemoteEffectNotSent, Cause: err,
		}
	}
	s := &session{
		config:       a.config,
		ref:          provider.SessionRef{AdapterSessionID: provider.AdapterSessionID(adapterSessionID)},
		assignment:   request.Assignment,
		model:        request.Model,
		contract:     provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual},
		capabilities: codexCapabilities(request.Model, a.config.Now()),
		process:      proc,
		broker:       eventstream.New(a.config.EventBuffer),
		state:        provider.SessionOpening,
		exitDone:     make(chan struct{}),
		closeDone:    make(chan struct{}),
		textVersions: make(map[string]uint64),
		textBytes:    make(map[string]uint64),
		warnedNative: make(map[string]struct{}),
	}
	s.rpc = newRPCClient(proc)
	s.rpc.start(s.handleNotification, s.handleServerRequest)
	go s.observeProcessExit()
	s.emit(provider.SessionStateEvent{Previous: provider.SessionStateUnknown, Current: provider.SessionOpening})

	if err := s.initialize(ctx, request); err != nil {
		s.abortOpen()
		return provider.OpenSessionResult{}, err
	}
	return provider.OpenSessionResult{Session: s, Contract: s.contract}, nil
}

func negotiateResultContract(contract provider.ResultContractRef) (provider.SchemaEnforcement, error) {
	if len(contract.Schema) != 0 && contract.Allows(provider.SchemaNative) {
		if !json.Valid(contract.Schema) {
			return "", &provider.ProviderError{
				Code: provider.ErrorProtocolViolation, Operation: provider.OperationOpenSession,
				Retry: provider.RetryNever, RemoteEffect: provider.RemoteEffectNotSent,
				Detail: "result schema is not valid JSON",
			}
		}
		return provider.SchemaNative, nil
	}
	if contract.Allows(provider.SchemaNone) {
		return provider.SchemaNone, nil
	}
	return "", &provider.ProviderError{
		Code: provider.ErrorCapabilityUnsupported, Operation: provider.OperationOpenSession,
		Retry: provider.RetryNever, RemoteEffect: provider.RemoteEffectNotSent,
		Detail: "requested result enforcement cannot be negotiated",
	}
}

func codexCapabilities(model provider.ModelSelection, observedAt time.Time) provider.CapabilitySnapshot {
	fixture := "archived live fixture: Codex CLI 0.155.1 with gpt-5.6-luna; configured executable/model not inspected"
	items := []provider.CapabilityObservation{
		{Capability: provider.CapabilityEventText, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Detail: fixture},
		{Capability: provider.CapabilityEventCommand, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Detail: fixture},
		{Capability: provider.CapabilityEventFile, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Detail: fixture},
		{Capability: provider.CapabilityEventTool, Support: provider.SupportUnknown, Evidence: provider.EvidenceDeclared, Detail: "generic tool events were not exercised by the archived fixture"},
		{Capability: provider.CapabilityEventReasoning, Support: provider.SupportUnknown, Evidence: provider.EvidenceLiveObserved, Detail: fixture + "; reasoning lifecycle was observed but summary text was unavailable"},
		{Capability: provider.CapabilitySteerActiveTurn, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Targeting: provider.TargetNativeTurnID, Detail: fixture},
		{Capability: provider.CapabilitySteerNextBoundary, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityTurnInterrupt, Support: provider.SupportSupported, Evidence: provider.EvidenceDeclared, Targeting: provider.TargetNativeTurnID},
		{Capability: provider.CapabilitySessionResume, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilitySessionObserveState, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: "adapter-local partial observation"},
		{Capability: provider.CapabilityPermissionRoundTrip, Support: provider.SupportUnknown, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityQuestionRoundTrip, Support: provider.SupportUnknown, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityResultNativeSchema, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Detail: fixture},
		{Capability: provider.CapabilityEventsNativeReplay, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
	}
	return provider.CapabilitySnapshot{
		ProviderName: "codex", AdapterVersion: adapterVersion,
		ProtocolVersion: "app-server-jsonrpc-v2", Model: model,
		ObservedAt: observedAt, Items: items,
	}
}

var _ provider.Provider = (*Adapter)(nil)
