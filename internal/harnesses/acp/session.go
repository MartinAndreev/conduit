package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// boundTurn is a turn's identity frozen with the assignment it was started
// for. Its own events (activation, usage, terminal) always carry this
// assignment, even if a later turn has already been reserved.
type boundTurn struct {
	ref        provider.TurnRef
	assignment provider.AssignmentRef
}

// turnState is the one unresolved session/prompt. The native turn ends only
// with its response. settled closes once StartTurn has published (or
// abandoned) the activation; a reply waits for it, so a fast reply never
// precedes "active". ending is the lease held from the reply until its
// terminal events are published: the turn still occupies the session, so no
// new prompt can be reserved meanwhile.
type turnState struct {
	bound           boundTurn
	phase           provider.TurnState
	dispatchSettled bool
	settled         chan struct{}
	ending          bool
}

type session struct {
	adapter      *Adapter
	config       Config
	descriptor   Descriptor
	ref          provider.SessionRef
	workspace    string
	capabilities provider.CapabilitySnapshot
	process      processruntime.Process
	rpc          *rpcClient
	broker       *eventstream.Broker

	mu             sync.Mutex
	state          provider.SessionState
	nativeSession  string
	closeSupported bool
	assignment     provider.AssignmentRef
	model          provider.ModelSelection
	contract       provider.ResultContractNegotiation
	turn           *turnState
	closing        bool
	closeResult    provider.CloseSessionResult
	closeDone      chan struct{}
	exitResult     processruntime.Result
	stopKillErr    error
	exitDone       chan struct{}
	exitOnce       sync.Once

	// turnMu serializes turn lifecycle publication (activation, interrupt
	// mark, terminal) so each check and its events stay in order. It is
	// taken before s.mu and eventMu and never while holding them.
	turnMu sync.Mutex
	hooks  testHooks

	eventMu  sync.Mutex
	eventSeq uint64
	textMu   sync.Mutex
	texts    map[string]textCursor
	toolMu   sync.Mutex
	tools    map[string]*toolCall
	warnMu   sync.Mutex
	warned   map[string]struct{}
}

func (s *session) Ref() provider.SessionRef { return s.ref }

// Capabilities is fixed when OpenSession returns.
func (s *session) Capabilities() provider.CapabilitySnapshot { return s.capabilities }

func (s *session) startTransport() {
	s.rpc = newRPCClient(s.process, s.config.MaxLineBytes)
	s.rpc.callbackGrace = s.config.StopGrace
	s.rpc.onNotification = s.handleNotification
	s.rpc.onRequest = s.handleAgentRequest
	s.rpc.onStopping = s.terminateOnTransportStop
	s.rpc.onClosed = s.handleTransportClosed
	s.rpc.start()
	go s.observeProcessExit()
}

type initializeParams struct {
	ProtocolVersion    int `json:"protocolVersion"`
	ClientCapabilities struct {
		FS struct {
			ReadTextFile  bool `json:"readTextFile"`
			WriteTextFile bool `json:"writeTextFile"`
		} `json:"fs"`
		Terminal bool `json:"terminal"`
	} `json:"clientCapabilities"`
	ClientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

type initializeResult struct {
	ProtocolVersion   *int `json:"protocolVersion"`
	AgentCapabilities struct {
		LoadSession         bool `json:"loadSession"`
		SessionCapabilities struct {
			Close json.RawMessage `json:"close"`
		} `json:"sessionCapabilities"`
		Meta json.RawMessage `json:"_meta"`
	} `json:"agentCapabilities"`
	AgentInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agentInfo"`
}

// open runs initialize, session/new and the advertised model selection.
func (s *session) open(ctx context.Context, request provider.OpenSessionRequest) error {
	facts, err := s.initialize(ctx, request.RequestID)
	if err != nil {
		return err
	}
	s.capabilities = s.adapter.capabilities(request.Model, facts, s.config.Now())
	created, err := s.newNativeSession(ctx, request.RequestID)
	if err != nil {
		return err
	}
	options, err := s.selectModel(ctx, request, created)
	if err != nil {
		return err
	}
	if err := s.applySessionSettings(ctx, request, options); err != nil {
		return err
	}
	s.at(hookBeforeOpenTransition)
	// The transport may have stopped right after the last opening reply; a
	// lost session is never resurrected as open.
	err = lockscope.WithMutexLock(&s.mu, func() error {
		if s.state != provider.SessionOpening {
			return adaptererror.New(provider.OperationOpenSession, provider.ErrorSessionLost, provider.RemoteEffectAccepted, nil, "agent session was lost while opening")
		}
		s.state = provider.SessionOpen
		return nil
	})
	if err != nil {
		return err
	}
	s.emit(provider.SessionStateEvent{Previous: provider.SessionOpening, Current: provider.SessionOpen, NativeStatus: "session_created"})
	return nil
}

func (s *session) initialize(ctx context.Context, requestID provider.RequestID) (AgentFacts, error) {
	var params initializeParams
	params.ProtocolVersion = ProtocolVersion
	params.ClientInfo.Name = "conduit"
	params.ClientInfo.Version = s.descriptor.AdapterVersion
	raw, sent, err := s.rpc.call(ctx, "initialize", params)
	if err != nil {
		return AgentFacts{}, nativeCallFailure(provider.OperationOpenSession, requestID, sent, err)
	}
	var result initializeResult
	if err := json.Unmarshal(raw, &result); err != nil || result.ProtocolVersion == nil {
		return AgentFacts{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "initialize returned no protocol version")
	}
	if *result.ProtocolVersion != ProtocolVersion {
		return AgentFacts{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, nil, "agent negotiated an unsupported ACP protocol version")
	}
	closeSupported := presentJSON(result.AgentCapabilities.SessionCapabilities.Close)
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.closeSupported = closeSupported
		return nil
	})
	return AgentFacts{
		Observed: true, ProtocolVersion: *result.ProtocolVersion,
		AgentName: result.AgentInfo.Name, AgentVersion: result.AgentInfo.Version,
		LoadSession: result.AgentCapabilities.LoadSession, CloseSupported: closeSupported,
		Meta: result.AgentCapabilities.Meta,
	}, nil
}

func presentJSON(raw json.RawMessage) bool {
	return len(raw) != 0 && string(raw) != "null"
}

func (s *session) newNativeSession(ctx context.Context, requestID provider.RequestID) (sessionNewResult, error) {
	params := struct {
		CWD        string `json:"cwd"`
		MCPServers []any  `json:"mcpServers"`
	}{CWD: s.workspace, MCPServers: []any{}}
	// The session ID is committed on the reader goroutine so session/update
	// notifications that immediately follow the reply correlate with it.
	var created sessionNewResult
	var decodeErr error
	commit := func(reply rpcResponse) {
		if reply.err != nil {
			return
		}
		if decodeErr = json.Unmarshal(reply.result, &created); decodeErr != nil || created.SessionID == "" {
			return
		}
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			s.nativeSession = created.SessionID
			return nil
		})
	}
	_, sent, err := s.rpc.callCommitting(ctx, "session/new", params, commit)
	if err != nil {
		return sessionNewResult{}, nativeCallFailure(provider.OperationOpenSession, requestID, sent, err)
	}
	if decodeErr != nil || created.SessionID == "" {
		return sessionNewResult{}, adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, decodeErr, "session/new returned no session ID")
	}
	return created, nil
}

// nativeCallFailure separates a native JSON-RPC rejection from transport
// uncertainty. ACP's -32000 is "authentication required".
func nativeCallFailure(operation provider.Operation, requestID provider.RequestID, sent bool, cause error) error {
	var nativeErr *rpcError
	if errors.As(cause, &nativeErr) {
		code := provider.ErrorDeliveryRejected
		if nativeErr.Code == -32000 {
			code = provider.ErrorAuthFailed
		}
		return &provider.ProviderError{
			Code: code, Operation: operation, RequestID: provider.Some(requestID),
			Retry: provider.RetryNever, RemoteEffect: provider.RemoteEffectNotSent, Cause: cause,
		}
	}
	return adaptererror.Delivery(operation, requestID, sent, cause)
}

func (s *session) Events(_ context.Context, cursor provider.EventCursor) (provider.EventStream, error) {
	if cursor.AdapterCursor != "" {
		return nil, adaptererror.New(provider.OperationSubscribeEvents, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "native replay is unsupported")
	}
	stream, err := s.broker.Subscribe()
	if err != nil {
		return nil, adaptererror.New(provider.OperationSubscribeEvents, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "event subscription failed")
	}
	return stream, nil
}

func (s *session) ObserveState(ctx context.Context, request provider.ObserveStateRequest) (provider.SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return provider.SessionSnapshot{}, adaptererror.Delivery(provider.OperationObserveState, "", false, err)
	}
	if request.Session != s.ref {
		return provider.SessionSnapshot{}, adaptererror.New(provider.OperationObserveState, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, "session reference differs")
	}
	var snapshot provider.SessionSnapshot
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		snapshot = provider.SessionSnapshot{
			Knowledge: provider.SnapshotPartial, SessionState: s.state,
			Native: provider.NativeIDs{Session: optionalNativeSession(s.nativeSession)},
		}
		if s.turn != nil {
			snapshot.ActiveTurn = provider.Some(s.turn.bound.ref)
		}
		return nil
	})
	snapshot.ObservedAt = s.config.Now()
	return snapshot, nil
}

func optionalNativeSession(id string) provider.Optional[provider.NativeSessionID] {
	if id == "" {
		return provider.None[provider.NativeSessionID]()
	}
	return provider.Some(provider.NativeSessionID(id))
}

func (s *session) Steer(context.Context, provider.SteerRequest) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationSteer, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "ACP steering is unsupported")
}

func (s *session) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationRespondPermission, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "permission requests are answered automatically")
}

func (s *session) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationAnswerQuestion, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "question round-trip is unsupported")
}

// errRootExited stops the transport when the agent's root exited but its
// stdout is still held open (for example by a descendant).
var errRootExited = errors.New("ACP agent root process exited while its output stayed open")

// observeProcessExit watches the root and the owned resources separately.
// Full Wait (root reaped and both pipes ended) is resource completion and
// closes exitDone. Root exit alone starts the StopGrace period: the exit
// normally also ends the transport, whose stop path reports the loss after
// failing the pending prompt (TurnLost before SessionLost). If the transport
// is still running after the grace period, it is stopped through that same
// path, which kills the group (onStopping) and loses the session. No native
// terminal is invented either way.
func (s *session) observeProcessExit() {
	rootExited, _ := processruntime.ObserveRootExit(s.process)
	go s.awaitResourceRelease()
	<-rootExited
	timer := time.NewTimer(s.config.StopGrace)
	defer timer.Stop()
	select {
	case <-s.rpc.finished:
	case <-timer.C:
		s.rpc.finish(errRootExited)
	}
}

// awaitResourceRelease records full reaping of the owned process.
func (s *session) awaitResourceRelease() {
	result := s.process.Wait()
	s.exitOnce.Do(func() {
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			s.exitResult = result
			return nil
		})
		close(s.exitDone)
	})
}

// handleTransportClosed runs once when the JSON-RPC transport stops (EOF,
// read error, line bound, malformed line or failed write). Pending replies have
// already been failed. The session is lost and the owned agent is terminated.
func (s *session) handleTransportClosed(cause error) {
	var previous provider.SessionState
	lost := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.closing || s.state == provider.SessionLost {
			return nil
		}
		previous = s.state
		s.state = provider.SessionLost
		lost = true
		return nil
	})
	if !lost {
		return
	}
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "acp_transport_closed"})
	var killErr error
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		killErr = s.stopKillErr
		return nil
	})
	s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, errors.Join(cause, killErr), "ACP transport closed"))
}

// terminateOnTransportStop runs first when the transport stops, before any
// pending reply is failed. Once the transport is gone the agent can no longer
// be observed or controlled, so its group is killed unconditionally (a close
// in progress kills it anyway). The kill also fails a writer blocked on an
// agent that stopped reading stdin; otherwise a prompt reply callback waiting
// for StartTurn's settlement would wait on that write forever.
//
// Loss settlement must not depend on the OS honouring the kill, so our own
// pipe ends are closed regardless: closing stdin fails a blocked write, and
// closing stdout ends the reader. A kill failure is kept for the loss cause.
func (s *session) terminateOnTransportStop(error) {
	killErr := s.process.KillTree()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	if killErr != nil {
		killErr = fmt.Errorf("terminate owned ACP agent: %w", killErr)
	}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.stopKillErr = killErr
		return nil
	})
	_ = s.rpc.closeInput()
	_ = s.process.Stdout().Close()
	s.at(hookTransportStopping)
}

// abortOpen hands the agent of a failed open to one retained cleanup owner
// (stdin close, group kill, pipe close, root reap and reader drain) and waits
// for it within what remains of the caller's context, at most StopGrace. It
// returns cause with any cleanup failure joined; when the wait ends first,
// the joined CleanupPendingError keeps the owner observable.
func (s *session) abortOpen(ctx context.Context, cause error) error {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.closing = true
		return nil
	})
	owner := processruntime.NewCleanupOwnerWithDrain(s.process, s.rpc.finished)
	owner.BeginCleanup()
	waitCtx, cancel := context.WithTimeout(ctx, s.config.StopGrace)
	defer cancel()
	state, waitErr := owner.Await(waitCtx)
	s.broker.Finish(io.EOF)
	if waitErr != nil {
		return withCleanupFailure(cause, &CleanupPendingError{Owner: owner, Cause: waitErr})
	}
	return withCleanupFailure(cause, errors.Join(state.TreeTerminationErr, state.PipeCloseErr, state.StdinCloseErr, state.RootResult.ReleaseErr))
}

// CleanupPendingError reports that an aborted open returned before its owned
// agent was fully cleaned up. Owner keeps the cleanup observable: Done closes
// on completion and Latest reports its state. Cause is the ended wait.
type CleanupPendingError struct {
	Owner *processruntime.CleanupOwner
	Cause error
}

func (e *CleanupPendingError) Error() string { return "owned ACP agent cleanup is still pending" }
func (e *CleanupPendingError) Unwrap() error { return e.Cause }

// withCleanupFailure joins a cleanup failure into the open failure's cause
// while keeping the open failure's public classification.
func withCleanupFailure(cause, cleanup error) error {
	if cleanup == nil {
		return cause
	}
	var providerErr *provider.ProviderError
	if errors.As(cause, &providerErr) {
		joined := *providerErr
		joined.Cause = errors.Join(providerErr.Cause, cleanup)
		return &joined
	}
	return errors.Join(cause, cleanup)
}

var _ provider.Session = (*session)(nil)
