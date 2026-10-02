package opencode

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const adapterVersion = "opencode-cli/1"

type Adapter struct{ config Config }

func New(config Config) *Adapter { return &Adapter{config: withDefaults(config)} }

func (a *Adapter) Inspect(_ context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	capabilities := capabilities(request.Model, a.config.Now())
	return provider.ProviderDescriptor{ProviderName: "opencode", AdapterVersion: adapterVersion, ProtocolVersion: "cli-jsonl+api-v2", Capabilities: capabilities}, nil
}

func (a *Adapter) OpenSession(ctx context.Context, request provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	if request.Model.ProviderID == "" || request.Model.ModelID == "" || request.Workspace.Path == "" {
		return provider.OpenSessionResult{}, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "workspace directory and exact model are required", nil)
	}
	if !request.Workspace.Writable || request.PermissionMode != provider.PermissionDeny {
		return provider.OpenSessionResult{}, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "OpenCode adapter requires a writable workspace and deny permission mode", nil)
	}
	actual, err := negotiate(request.ResultContract)
	if err != nil {
		return provider.OpenSessionResult{}, err
	}
	adapterSessionID, err := a.config.NewID("opencode-session")
	if err != nil {
		return provider.OpenSessionResult{}, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "could not allocate adapter session ID", err)
	}
	created, sent, err := a.config.Control.CreateSession(ctx, CreateSessionRequest{Directory: request.Workspace.Path, Model: request.Model, Title: "Conduit assignment " + string(request.Assignment.AssignmentID)})
	if err != nil {
		return provider.OpenSessionResult{}, adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, sent, err)
	}
	if created.ModelVerified && created.Model != request.Model {
		return provider.OpenSessionResult{}, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, "OpenCode did not verify the exact requested model", nil)
	}
	s := &session{config: a.config, ref: provider.SessionRef{AdapterSessionID: provider.AdapterSessionID(adapterSessionID)}, nativeSession: created.SessionID, assignment: request.Assignment, model: request.Model, workspace: request.Workspace.Path, contract: provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual}, capabilities: capabilities(request.Model, a.config.Now()), broker: eventstream.New(a.config.EventBuffer), state: provider.SessionOpen}
	s.emit(provider.SessionStateEvent{Previous: provider.SessionOpening, Current: provider.SessionOpen})
	return provider.OpenSessionResult{Session: s, Contract: s.contract}, nil
}

func negotiate(contract provider.ResultContractRef) (provider.SchemaEnforcement, error) {
	if len(contract.Schema) > 0 && !json.Valid(contract.Schema) {
		return "", adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "result schema is not valid JSON", nil)
	}
	if contract.Allows(provider.SchemaNone) {
		return provider.SchemaNone, nil
	}
	return "", adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "OpenCode native result schema is unproven", nil)
}

func capabilities(model provider.ModelSelection, at time.Time) provider.CapabilitySnapshot {
	items := []provider.CapabilityObservation{
		{Capability: provider.CapabilityEventText, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved},
		{Capability: provider.CapabilityEventTool, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved},
		{Capability: provider.CapabilityEventCommand, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified},
		{Capability: provider.CapabilityEventFile, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified},
		{Capability: provider.CapabilityEventReasoning, Support: provider.SupportUnknown, Evidence: provider.EvidenceLiveObserved, Detail: "reasoning text exists but is not a proven summary"},
		{Capability: provider.CapabilitySteerActiveTurn, Support: provider.SupportSupported, Evidence: provider.EvidenceLiveObserved, Targeting: provider.TargetSessionLease},
		{Capability: provider.CapabilitySteerNextBoundary, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityTurnInterrupt, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilitySessionResume, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilitySessionObserveState, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: "adapter-local partial observation"},
		{Capability: provider.CapabilityPermissionRoundTrip, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityQuestionRoundTrip, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityResultNativeSchema, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilityEventsNativeReplay, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
	}
	return provider.CapabilitySnapshot{ProviderName: "opencode", AdapterVersion: adapterVersion, ProtocolVersion: "cli-jsonl+api-v2", Model: model, ObservedAt: at, Items: items}
}

var _ provider.Provider = (*Adapter)(nil)
var _ processruntime.Launcher = processruntime.ExecLauncher{}
