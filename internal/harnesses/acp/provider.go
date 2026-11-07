package acp

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Adapter implements the provider contract for one ACP harness descriptor.
type Adapter struct {
	descriptor Descriptor
	config     Config
	hooks      testHooks
}

// New composes the shared ACP client with a harness descriptor.
func New(descriptor Descriptor, config Config) *Adapter {
	descriptor.Args = slices.Clone(descriptor.Args)
	descriptor.Env = slices.Clone(descriptor.Env)
	descriptor.SessionSettings = slices.Clone(descriptor.SessionSettings)
	return &Adapter{descriptor: descriptor, config: withDefaults(descriptor, config)}
}

// Inspect reports the descriptor's static evidence. It does not start the
// executable, so HarnessVersion stays unknown until a session initializes.
func (a *Adapter) Inspect(_ context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	capabilities := a.capabilities(request.Model, AgentFacts{}, a.config.Now())
	return provider.ProviderDescriptor{
		ProviderName: a.descriptor.ProviderName, AdapterVersion: a.descriptor.AdapterVersion,
		HarnessVersion: capabilities.HarnessVersion, ProtocolVersion: protocolName,
		Capabilities: capabilities,
	}, nil
}

// OpenSession starts the owned agent, initializes ACP and creates a native
// session. Any failure after launch terminates and reaps the agent.
func (a *Adapter) OpenSession(ctx context.Context, request provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.OpenSessionResult{}, adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, false, err)
	}
	if request.Workspace.Path == "" {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, "workspace path is required")
	}
	if request.PermissionMode != provider.PermissionDeny || !request.Workspace.Writable {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "ACP adapter supports a writable workspace with deny permission mode only")
	}
	actual, err := negotiateResultContract(provider.OperationOpenSession, request.ResultContract)
	if err != nil {
		return provider.OpenSessionResult{}, err
	}
	adapterSessionID, err := a.config.NewID(a.descriptor.ProviderName + "-session")
	if err != nil {
		return provider.OpenSessionResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate adapter session ID")
	}
	spec := processruntime.Spec{Path: a.config.ExecutablePath, Args: slices.Clone(a.descriptor.Args), Dir: request.Workspace.Path}
	if len(a.descriptor.Env) != 0 {
		spec.Env = slices.Clone(a.descriptor.Env)
	}
	proc, err := a.config.Launcher.Start(spec)
	if err != nil {
		return provider.OpenSessionResult{}, &provider.ProviderError{
			Code: provider.ErrorExecutableMissing, Operation: provider.OperationOpenSession,
			RequestID: provider.Some(request.RequestID), Retry: provider.RetryNever,
			RemoteEffect: provider.RemoteEffectNotSent, Cause: err,
		}
	}
	s := newSession(a, proc, provider.SessionRef{AdapterSessionID: provider.AdapterSessionID(adapterSessionID)}, request, actual)
	s.startTransport()
	s.emit(provider.SessionStateEvent{Previous: provider.SessionStateUnknown, Current: provider.SessionOpening})
	if err := s.open(ctx, request); err != nil {
		return provider.OpenSessionResult{}, s.abortOpen(ctx, err)
	}
	return provider.OpenSessionResult{Session: s, Contract: s.contract}, nil
}

func newSession(a *Adapter, proc processruntime.Process, ref provider.SessionRef, request provider.OpenSessionRequest, actual provider.SchemaEnforcement) *session {
	return &session{
		adapter:      a,
		hooks:        a.hooks,
		config:       a.config,
		descriptor:   a.descriptor,
		ref:          ref,
		assignment:   request.Assignment,
		model:        request.Model,
		workspace:    request.Workspace.Path,
		contract:     provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual},
		capabilities: a.capabilities(request.Model, AgentFacts{}, a.config.Now()),
		process:      proc,
		broker:       eventstream.New(a.config.EventBuffer),
		state:        provider.SessionOpening,
		exitDone:     make(chan struct{}),
		closeDone:    make(chan struct{}),
		texts:        make(map[string]textCursor),
		tools:        make(map[string]*toolCall),
		warned:       make(map[string]struct{}),
	}
}

func negotiateResultContract(operation provider.Operation, contract provider.ResultContractRef) (provider.SchemaEnforcement, error) {
	decision := rules.DecideResultEnforcement(rules.ResultEnforcementFacts{
		SchemaPresent:   len(contract.Schema) != 0,
		SchemaValidJSON: json.Valid(contract.Schema),
		AllowsNone:      contract.Allows(provider.SchemaNone),
	})
	switch decision.Kind {
	case rules.ResultEnforcementNone:
		return provider.SchemaNone, nil
	case rules.ResultEnforcementInvalidSchema:
		return "", adaptererror.New(operation, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, decision.Reason)
	default:
		return "", adaptererror.New(operation, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, decision.Reason)
	}
}

var _ provider.Provider = (*Adapter)(nil)
