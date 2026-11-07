package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// turnState is the one unresolved session/prompt. The native turn ends only
// with its response; dispatchSettled orders that terminal after StartTurn has
// published the activation, so a fast reply never precedes "active".
type turnState struct {
	ref             provider.TurnRef
	phase           provider.TurnState
	dispatchSettled bool
	deferred        *rpcResponse
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
	exitDone       chan struct{}
	exitOnce       sync.Once

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
	s.rpc.onNotification = s.handleNotification
	s.rpc.onRequest = s.handleAgentRequest
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
	var previous provider.SessionState
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		previous = s.state
		s.state = provider.SessionOpen
		return nil
	})
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionOpen, NativeStatus: "session_created"})
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
			snapshot.ActiveTurn = provider.Some(s.turn.ref)
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

// observeProcessExit records the root's reap. An exit nobody asked for loses
// the session without inventing a native terminal.
func (s *session) observeProcessExit() {
	result := s.process.Wait()
	s.exitOnce.Do(func() {
		var previous provider.SessionState
		lost := false
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			s.exitResult = result
			previous = s.state
			if !s.closing && s.state != provider.SessionLost {
				s.state = provider.SessionLost
				lost = true
			}
			return nil
		})
		close(s.exitDone)
		if lost {
			s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "agent_process_exit"})
			s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, result.Err, "ACP agent process exited"))
		}
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
	s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, cause, "ACP transport closed"))
	_ = s.process.KillTree()
}

// abortOpen terminates an agent whose open failed and waits for its reap at
// most StopGrace; the exit observer keeps reaping after that bound.
func (s *session) abortOpen() {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.closing = true
		return nil
	})
	_ = s.rpc.closeInput()
	_ = s.process.KillTree()
	s.waitExit(s.config.StopGrace)
	s.broker.Finish(io.EOF)
}

var _ provider.Session = (*session)(nil)
