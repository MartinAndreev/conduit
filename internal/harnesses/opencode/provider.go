package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const adapterVersion = "opencode-cli/1"

type Adapter struct{ config Config }

func New(config Config) *Adapter { return &Adapter{config: withDefaults(config)} }

func (a *Adapter) Inspect(_ context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	capabilities := capabilities(request.Model, a.config.Now(), noLaunch())
	return provider.ProviderDescriptor{ProviderName: "opencode", AdapterVersion: adapterVersion, ProtocolVersion: "cli-jsonl+api-v2", Capabilities: capabilities}, nil
}

// OpenSession validates the request, launches the session's own server,
// creates the native session on it and returns the session. Any failure after
// the launch stops the server before returning, so no server outlives a
// failed open; an unconfirmed stop is joined into the error's cause.
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
	server, err := a.launchServer(ctx, request.RequestID, request.Workspace.Path)
	if err != nil {
		return provider.OpenSessionResult{}, err
	}
	info, infoErr := a.identifyServer(ctx, server, request.Workspace.Path)
	if !info.ProvesOwnServer() {
		// Without proof that the clients talk to this session's server,
		// nothing is sent to it.
		return provider.OpenSessionResult{}, abortServer(ctx, server, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, info.Reason, infoErr))
	}
	created, sent, err := a.config.Control.CreateSession(ctx, CreateSessionRequest{Server: server.endpoint, Directory: request.Workspace.Path, Model: request.Model, Title: "Conduit assignment " + string(request.Assignment.AssignmentID)})
	if errors.Is(err, errSecretInNativeID) {
		// OpenCode created a session whose ID cannot be used safely.
		return provider.OpenSessionResult{}, abortServer(ctx, server, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, errSecretInNativeID.Error(), nil))
	}
	if err != nil {
		return provider.OpenSessionResult{}, abortServer(ctx, server, adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, sent, err))
	}
	if created.ModelVerified && created.Model != request.Model {
		return provider.OpenSessionResult{}, abortServer(ctx, server, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, "OpenCode did not verify the exact requested model", nil))
	}
	s := &session{config: a.config, ref: provider.SessionRef{AdapterSessionID: provider.AdapterSessionID(adapterSessionID)}, nativeSession: created.SessionID, assignment: request.Assignment, model: request.Model, workspace: request.Workspace.Path, server: server, endpoint: server.endpoint, contract: provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual}, capabilities: capabilities(request.Model, a.config.Now(), launchReport(server, info, a.config.host.goos)), broker: eventstream.New(a.config.EventBuffer), state: provider.SessionOpen}
	// SessionOpen is published before the server is watched, so a server loss
	// is always published after it.
	s.emit(provider.SessionStateEvent{Previous: provider.SessionOpening, Current: provider.SessionOpen})
	go s.watchServer(server)
	return provider.OpenSessionResult{Session: s, Contract: s.contract}, nil
}

// launchServer plans the server launch from the host facts, starts it and
// waits for readiness. A failed launch is mapped to an open error; a server
// that started but did not become ready is stopped first.
func (a *Adapter) launchServer(ctx context.Context, requestID provider.RequestID, workspace string) (*ownedServer, error) {
	if err := ctx.Err(); err != nil {
		return nil, adaptererror.Delivery(provider.OperationOpenSession, requestID, false, err)
	}
	password, err := a.config.newPassword()
	if err != nil {
		return nil, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "could not generate the OpenCode server password", err)
	}
	launch := planServerLaunch(a.config.ExecutablePath, workspace, password, a.config.host.sandboxFacts())
	server, err := startOwnedServer(ctx, a.config.serverLauncher, launch, password, a.config.ServerReadyTimeout, a.config.StopGrace)
	switch {
	case err == nil:
		return server, nil
	case server == nil && ctx.Err() != nil:
		// Cancelled after the host queries: nothing was started.
		return nil, adaptererror.Delivery(provider.OperationOpenSession, requestID, false, err)
	case server == nil:
		return nil, adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorExecutableMissing, provider.RemoteEffectNotSent, "could not start the OpenCode server", err)
	}
	code := provider.ErrorProtocolViolation
	switch {
	case errors.Is(err, context.Canceled):
		code = provider.ErrorCallerCancelled
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, errServerReadyTimeout):
		code = provider.ErrorDeadline
	}
	return nil, abortServer(ctx, server, adaptererror.NewWithDetail(provider.OperationOpenSession, code, provider.RemoteEffectNotSent, "OpenCode server did not become ready", err))
}

// serverInfoTimeout bounds the server.info call of OpenSession.
const serverInfoTimeout = 10 * time.Second

// identifyServer reads server.info of the session's own server once and
// decides whether it proves the server is ours and whether its version is the
// harness version. The query error is returned so a failed open keeps its
// cause, including a still pending cleanup of the control command.
func (a *Adapter) identifyServer(ctx context.Context, server *ownedServer, workspace string) (rules.ServerInfoDecision, error) {
	infoCtx, cancel := context.WithTimeout(ctx, serverInfoTimeout)
	defer cancel()
	observation, err := a.config.Control.ServerInfo(infoCtx, ServerInfoRequest{Server: server.endpoint, Directory: workspace})
	return rules.DecideServerInfo(rules.ServerInfoFacts{
		QueryFailed: err != nil,
		Version:     observation.Version,
		URLs:        observation.URLs,
		PID:         observation.PID,
		ExpectedURL: server.endpoint.URL,
		ExpectedPID: server.proc.PID(),
	}), err
}

// abortServer stops the server of a failed open and returns cause. The stop
// is bounded by StopGrace, not by the possibly ended ctx of the failed open.
// An unconfirmed stop is joined into the provider error's cause: while the
// cleanup is still running, as a *CleanupPendingError whose Owner keeps it
// observable (Done, Latest), as the Codex adapter does for a failed open;
// once it ended, as its reason and termination error.
func abortServer(ctx context.Context, server *ownedServer, cause error) error {
	return abortServerOutcome(server, server.stopOutcome(context.WithoutCancel(ctx)), cause)
}

func abortServerOutcome(server *ownedServer, outcome serverStopOutcome, cause error) error {
	if outcome.stop.Confirmed() && outcome.release.Confirmed() {
		return cause
	}
	var stopErr error
	select {
	case <-server.cleanup.Done():
		// Cleanup may have finished right after Await timed out; recompute
		// both decisions from the final snapshot so ReleaseErr and pipe
		// close errors are never lost.
		outcome = server.latestOutcome()
		if outcome.stop.Confirmed() && outcome.release.Confirmed() {
			return cause
		}
		if !outcome.stop.Confirmed() {
			stopErr = errors.Join(errors.New(outcome.stop.Reason), outcome.state.TreeTerminationErr)
		}
		if !outcome.release.Confirmed() {
			stopErr = errors.Join(stopErr, errors.New(outcome.release.Reason), outcome.state.RootResult.ReleaseErr, outcome.state.PipeCloseErr, outcome.state.StdinCloseErr)
		}
	default:
		stopErr = &CleanupPendingError{Owner: server.cleanup, Cause: errors.New(outcome.stop.Reason)}
	}
	var providerErr *provider.ProviderError
	if errors.As(cause, &providerErr) {
		joined := *providerErr
		joined.Cause = errors.Join(providerErr.Cause, stopErr)
		return &joined
	}
	return errors.Join(cause, stopErr)
}

// CleanupPendingError reports that a failed open returned before its owned
// OpenCode server was confirmed stopped (or a cancelled control command before
// its cleanup finished). Owner keeps the cleanup observable: Done closes on
// completion and Latest reports its state. Cause is the stop decision's reason.
type CleanupPendingError struct {
	Owner                *processruntime.CleanupOwner
	Cause                error
	treeKillNotSupported bool
}

func (e *CleanupPendingError) Error() string {
	return "owned OpenCode server cleanup is still pending"
}
func (e *CleanupPendingError) Unwrap() error { return e.Cause }

func negotiate(contract provider.ResultContractRef) (provider.SchemaEnforcement, error) {
	decision := rules.DecideResultEnforcement(rules.ResultEnforcementFacts{
		SchemaPresent:   len(contract.Schema) > 0,
		SchemaValidJSON: json.Valid(contract.Schema),
		AllowsNone:      contract.Allows(provider.SchemaNone),
	})
	switch decision.Kind {
	case rules.ResultEnforcementNone:
		return provider.SchemaNone, nil
	case rules.ResultEnforcementInvalidSchema:
		return "", adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, decision.Reason, nil)
	default:
		return "", adaptererror.NewWithDetail(provider.OperationOpenSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, decision.Reason, nil)
	}
}

func capabilities(model provider.ModelSelection, at time.Time, report launchFacts) provider.CapabilitySnapshot {
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
	return provider.CapabilitySnapshot{ProviderName: "opencode", AdapterVersion: adapterVersion, HarnessVersion: report.harnessVersion, ProtocolVersion: "cli-jsonl+api-v2", Model: model, ObservedAt: at, Items: items, NativeContext: nativeContextReport(rules.NativeContextEvidence(), report)}
}

// launchFacts is what the native context report is bound to: the actual
// server launch of a session, or no launch (Inspect).
type launchFacts struct {
	launched bool
	sandbox  rules.SandboxDecision
	// info decides the harness version from the server's server.info.
	info rules.ServerInfoDecision
	// harnessVersion is the accepted server.info version, empty otherwise, so
	// no evidence can match.
	harnessVersion string
	configHash     string
	platform       string
}

// launchReport takes the facts from the session's own server launch; the
// platform is the one the sandbox was decided for.
func launchReport(server *ownedServer, info rules.ServerInfoDecision, platform string) launchFacts {
	return launchFacts{launched: true, sandbox: server.sandbox, info: info, harnessVersion: info.Version, configHash: server.configHash, platform: platform}
}

// noLaunch is reported before a session exists. Its empty hash matches no
// evidence.
func noLaunch() launchFacts { return launchFacts{platform: runtime.GOOS} }

// nativeContextReport derives the snapshot's native context state from the
// evidence, bound to the launch's harness version, hash and platform. A
// launched server without the sandbox reports the sandbox refusal.
func nativeContextReport(evidence provider.NativeContextEvidenceSet, launch launchFacts) provider.NativeContextReport {
	paths := provider.ProtectedPathsFacts{
		HarnessVersion: launch.harnessVersion, LaunchConfigHash: launch.configHash, Platform: launch.platform, Evidence: evidence.ProtectedPaths,
	}
	pathsDecision := rules.DecideProtectedPathsPolicy(paths)
	if launch.launched {
		pathsDecision = rules.DecideLaunchProtectedPaths(launch.sandbox, launch.info, paths)
	}
	return provider.NewNativeContextReport(
		func(source provider.NativeContextSource) provider.NativeContextDecision {
			return rules.DecideNativeContextPolicy(provider.NativeContextFacts{
				Source: source, HarnessVersion: launch.harnessVersion, LaunchConfigHash: launch.configHash, Evidence: evidence.Sources,
			})
		},
		pathsDecision,
	)
}

var _ provider.Provider = (*Adapter)(nil)
var _ processruntime.Launcher = processruntime.ExecLauncher{}
