package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/claude/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// activeTurn is the adapter's lease on one dispatched native user message.
// inputs holds the initial message UUID and every dispatched steer UUID; a
// native result ends the turn only when it references one of them.
type activeTurn struct {
	ref       provider.TurnRef
	inputUUID string
	inputs    map[string]struct{}
	delivered bool
	// ending is set when the terminal result was decided, before the turn is
	// cleared, so no correction can be dispatched into a finished turn.
	ending bool
}

// pendingSteer is a dispatched correction without delivery evidence. While it
// exists, the current assignment revision is unresolved and events carry no
// attribution. It survives the end of its turn: late evidence still resolves
// it, and until then no new turn or correction can be attributed.
type pendingSteer struct {
	request provider.SteerRequest
	uuid    string
}

// sessionBinding is the immutable launch binding of one session.
type sessionBinding struct {
	ref           provider.SessionRef
	nativeSession string
	workspace     string
	assignment    provider.AssignmentRef
	model         provider.ModelSelection
	contract      provider.ResultContractNegotiation
}

type session struct {
	config        Config
	ref           provider.SessionRef
	nativeSession string
	workspace     string
	model         provider.ModelSelection
	// capabilities is replaced by initialize before the session is returned
	// to the caller and is read-only afterwards.
	capabilities provider.CapabilitySnapshot
	process      processruntime.Process
	transport    *transport
	broker       *eventstream.Broker

	mu            sync.Mutex
	state         provider.SessionState
	assignment    provider.AssignmentRef
	contract      provider.ResultContractNegotiation
	turn          *activeTurn
	steer         *pendingSteer
	closing       bool
	observedModel string
	closeResult   provider.CloseSessionResult
	closeDone     chan struct{}
	closeOnce     sync.Once
	exitDone      chan struct{}
	exitOnce      sync.Once

	eventMu  sync.Mutex
	eventSeq uint64
	warnMu   sync.Mutex
	warned   map[string]struct{}

	// normalization is owned by the transport reader goroutine.
	normalization normalizationState
}

func newSession(config Config, binding sessionBinding, proc processruntime.Process) *session {
	return &session{
		config:        config,
		ref:           binding.ref,
		nativeSession: binding.nativeSession,
		workspace:     binding.workspace,
		model:         binding.model,
		capabilities:  claudeCapabilities(binding.model, "", config.Now()),
		process:       proc,
		transport:     newTransport(proc, config.MaxLineBytes, config.StderrTailBytes),
		broker:        eventstream.New(config.EventBuffer),
		state:         provider.SessionOpening,
		assignment:    binding.assignment,
		contract:      binding.contract,
		closeDone:     make(chan struct{}),
		exitDone:      make(chan struct{}),
		warned:        make(map[string]struct{}),
		normalization: newNormalizationState(),
	}
}

// start begins draining and observing the owned process. The broker already
// exists, so startup events wait for a late subscriber.
func (s *session) start() {
	s.transport.onMessage = s.handleMessage
	s.transport.onControlRequest = s.handleControlRequest
	s.transport.onClosed = s.handleTransportClosed
	s.transport.start()
	go s.observeProcessExit()
	s.emit(provider.SessionStateEvent{Previous: provider.SessionStateUnknown, Current: provider.SessionOpening})
}

func (s *session) Ref() provider.SessionRef { return s.ref }

func (s *session) Capabilities() provider.CapabilitySnapshot { return s.capabilities }

// initializeResponse is the evidence-bearing subset of the initialize reply.
type initializeResponse struct {
	Account *struct {
		TokenSource string `json:"tokenSource"`
		APIProvider string `json:"apiProvider"`
	} `json:"account"`
	Models []struct {
		Value         string `json:"value"`
		ResolvedModel string `json:"resolvedModel"`
	} `json:"models"`
	ClaudeCodeVersion     string `json:"claude_code_version"`
	CurrentPermissionMode string `json:"current_permission_mode"`
	SessionState          string `json:"session_state"`
}

// initialize sends the first control request and turns its reply into
// capability evidence. A missing login is a typed refusal.
func (s *session) initialize(ctx context.Context, request provider.OpenSessionRequest) error {
	raw, sent, err := s.transport.control(ctx, struct {
		Subtype string `json:"subtype"`
		Hooks   any    `json:"hooks"`
	}{Subtype: "initialize"})
	if err != nil {
		var rejected *nativeControlError
		if errors.As(err, &rejected) {
			return adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "claude rejected initialize")
		}
		return adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, sent, err)
	}
	var response initializeResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "initialize returned an invalid response")
	}
	tokenSource := ""
	if response.Account != nil {
		tokenSource = response.Account.TokenSource
	}
	authentication := rules.DecideAuthentication(tokenSource)
	if authentication.Kind == rules.AuthenticationMissing {
		// The contract has no harness_unauthenticated code; auth_failed is the
		// closest existing one. The user must log in with `claude` first.
		return adaptererror.New(provider.OperationOpenSession, provider.ErrorAuthFailed, provider.RemoteEffectAccepted, nil, authentication.Reason)
	}
	s.recordInitializeEvidence(request.Model, response)
	if authentication.Kind == rules.AuthenticationUnknown {
		s.emit(provider.ProtocolWarningEvent{Code: "account_unknown", Detail: provider.BoundedText{Text: authentication.Reason}, OffendingNativeKind: "control_response/initialize"})
	}
	if response.CurrentPermissionMode != "" && response.CurrentPermissionMode != permissionModeArgument {
		s.emit(provider.ProtocolWarningEvent{
			Code: "permission_mode_differs", Detail: boundedtext.String("claude reports permission mode "+response.CurrentPermissionMode, maxNativeErrorBytes),
			OffendingNativeKind: "control_response/initialize",
		})
	}
	if !modelListed(request.Model.ModelID, response) {
		s.emit(provider.ProtocolWarningEvent{
			Code: "model_unverified", Detail: provider.BoundedText{Text: "requested model is not in claude's initialize model list"},
			OffendingNativeKind: "control_response/initialize",
		})
	}
	var previous provider.SessionState
	opened := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.closing || s.state == provider.SessionLost {
			return nil
		}
		previous = s.state
		s.state = provider.SessionOpen
		opened = true
		return nil
	})
	if !opened {
		return adaptererror.New(provider.OperationOpenSession, provider.ErrorSessionLost, provider.RemoteEffectAccepted, s.transport.terminalErr(), "claude stopped during initialize")
	}
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionOpen, NativeStatus: response.SessionState})
	return nil
}

func (s *session) recordInitializeEvidence(model provider.ModelSelection, response initializeResponse) {
	s.capabilities = claudeCapabilities(model, response.ClaudeCodeVersion, s.config.Now())
}

func modelListed(model string, response initializeResponse) bool {
	for _, listed := range response.Models {
		if listed.Value == model || listed.ResolvedModel == model {
			return true
		}
	}
	return false
}

// abortOpen stops the owned process after a failed open: it closes stdin,
// kills the process group and waits up to StopGrace for the reap. A reap that
// is still pending is reported with the open failure; the exit observer stays
// the single owner that completes it.
func (s *session) abortOpen(cause error) error {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.closing = true
		return nil
	})
	_ = s.transport.closeInput()
	killErr := s.process.KillTree()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		cause = errors.Join(cause, fmt.Errorf("terminate owned claude process: %w", killErr))
	}
	timer := time.NewTimer(s.config.StopGrace)
	defer timer.Stop()
	select {
	case <-s.exitDone:
	case <-timer.C:
		cause = errors.Join(cause, errors.New("owned claude process reaping remains pending"))
	}
	if tail := s.transport.stderrTail.String(); tail != "" {
		cause = errors.Join(cause, &stderrTail{Text: tail})
	}
	s.broker.Finish(io.EOF)
	return cause
}

// stderrTail carries bounded native diagnostics in an error cause only; it is
// never placed in a caller-visible Detail.
type stderrTail struct{ Text string }

func (e *stderrTail) Error() string { return "claude stderr tail: " + e.Text }

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
			ActiveTurn: s.activeTurnRef(),
			Native:     provider.NativeIDs{Session: provider.Some(provider.NativeSessionID(s.nativeSession))},
		}
		return nil
	})
	snapshot.ObservedAt = s.config.Now()
	return snapshot, nil
}

// activeTurnRef must be called with s.mu held.
func (s *session) activeTurnRef() provider.Optional[provider.TurnRef] {
	if s.turn == nil {
		return provider.None[provider.TurnRef]()
	}
	return provider.Some(s.turn.ref)
}

// nativeUserMessage is the stream-json user input. Priority "now" marks a steer.
type nativeUserMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
	SessionID       string  `json:"session_id"`
	UUID            string  `json:"uuid"`
	Priority        string  `json:"priority,omitempty"`
}

func (s *session) userMessage(content, uuid, priority string) nativeUserMessage {
	message := nativeUserMessage{Type: "user", SessionID: s.nativeSession, UUID: uuid, Priority: priority}
	message.Message.Role = "user"
	message.Message.Content = content
	return message
}

// messageContent joins text parts. Artifact references are not fixture-backed.
func messageContent(operation provider.Operation, parts []provider.InputPart) (string, error) {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case provider.InputInstruction, provider.InputContext:
			values = append(values, part.Text)
		default:
			return "", adaptererror.New(operation, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "only text instruction/context input is supported")
		}
	}
	return strings.Join(values, "\n\n"), nil
}

func (s *session) StartTurn(ctx context.Context, request provider.StartTurnRequest) (provider.StartTurnResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.StartTurnResult{}, adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, false, err)
	}
	content, err := messageContent(provider.OperationStartTurn, request.Input)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	turnID, err := s.config.NewID("claude-turn")
	if err != nil {
		return provider.StartTurnResult{}, adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate adapter turn ID")
	}
	inputUUID, err := s.config.NewUUID()
	if err != nil {
		return provider.StartTurnResult{}, adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate native message UUID")
	}
	turn := &activeTurn{
		ref:       provider.TurnRef{AdapterTurnID: provider.AdapterTurnID(turnID)},
		inputUUID: inputUUID,
		inputs:    map[string]struct{}{inputUUID: {}},
	}
	previousAssignment, actual, err := s.reserveTurn(request, turn)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	sent, err := s.transport.send(ctx, s.userMessage(content, inputUUID, ""))
	if err != nil {
		return provider.StartTurnResult{}, s.handleTurnDispatchFailure(request, turn, previousAssignment, sent, err)
	}
	return provider.StartTurnResult{
		Turn: turn.ref, Assignment: request.Assignment, TargetingStrength: provider.TargetSessionLease,
		ResultEnforcement: actual,
		Native: provider.NativeIDs{
			Session: provider.Some(provider.NativeSessionID(s.nativeSession)),
			Turn:    provider.Some(provider.NativeTurnID(inputUUID)),
		},
	}, nil
}

// reserveTurn evaluates eligibility and installs the turn lease in one critical
// section, before dispatch, so the reader can correlate an immediate echo.
func (s *session) reserveTurn(request provider.StartTurnRequest, turn *activeTurn) (provider.AssignmentRef, provider.SchemaEnforcement, error) {
	var previous provider.AssignmentRef
	var actual provider.SchemaEnforcement
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideStartTurn(rules.StartTurnFacts{
			Closing:                   s.closing || s.state == provider.SessionLost,
			ActiveTurn:                s.turn != nil,
			SteerUnresolved:           s.steer != nil,
			SessionMatches:            request.Session == s.ref,
			AssignmentIdentityMatches: request.Assignment.SameIdentity(s.assignment),
			ResultContractMatches:     sameResultContract(request.ResultContract, s.contract.Contract),
		})
		switch decision.Kind {
		case rules.StartTurnClosing:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnOccupied, rules.StartTurnBindingDiffers:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnContractChanged:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		previous = s.assignment
		actual = s.contract.Actual
		s.turn = turn
		s.assignment = request.Assignment
		return nil
	})
	return previous, actual, err
}

// handleTurnDispatchFailure releases a lease whose message was never handed to
// the writer. A possibly sent message keeps its lease, because a later native
// result may still reference it; the caller sees delivery_unknown.
func (s *session) handleTurnDispatchFailure(request provider.StartTurnRequest, turn *activeTurn, previous provider.AssignmentRef, sent bool, cause error) error {
	if !sent {
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.turn == turn {
				s.turn = nil
				s.assignment = previous
			}
			return nil
		})
	}
	return adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, sent, cause)
}

func sameResultContract(a, b provider.ResultContractRef) bool {
	if a.Name != b.Name || a.Version != b.Version || a.Requested != b.Requested || string(a.Schema) != string(b.Schema) || len(a.AllowedFallbacks) != len(b.AllowedFallbacks) {
		return false
	}
	for index := range a.AllowedFallbacks {
		if a.AllowedFallbacks[index] != b.AllowedFallbacks[index] {
			return false
		}
	}
	return true
}

func (s *session) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationRespondPermission, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "permission round-trip (can_use_tool) is deferred")
}

func (s *session) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationAnswerQuestion, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "question round-trip is unsupported")
}

// InterruptTurn sends a native interrupt. Its control response is only a
// receipt; the turn ends when a correlated result is observed.
func (s *session) InterruptTurn(ctx context.Context, request provider.InterruptRequest) (provider.InterruptResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.InterruptResult{}, adaptererror.Delivery(provider.OperationInterruptTurn, request.RequestID, false, err)
	}
	err := lockscope.WithMutexLock(&s.mu, func() error {
		if request.Session != s.ref || s.turn == nil || s.turn.ref != request.ExpectedActiveTurn || request.Assignment != s.assignment {
			return adaptererror.New(provider.OperationInterruptTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, "interrupt target differs from active binding")
		}
		return nil
	})
	if err != nil {
		return provider.InterruptResult{}, err
	}
	_, sent, err := s.transport.control(ctx, struct {
		Subtype string `json:"subtype"`
	}{Subtype: "interrupt"})
	if err != nil {
		var rejected *nativeControlError
		if errors.As(err, &rejected) {
			return provider.InterruptResult{}, &provider.ProviderError{
				Code: provider.ErrorDeliveryRejected, Operation: provider.OperationInterruptTurn, RequestID: provider.Some(request.RequestID),
				Retry: provider.RetryNever, RemoteEffect: provider.RemoteEffectNotSent, Cause: err,
			}
		}
		return provider.InterruptResult{}, adaptererror.Delivery(provider.OperationInterruptTurn, request.RequestID, sent, err)
	}
	s.emit(provider.TurnStateEvent{Previous: provider.TurnActive, Current: provider.TurnInterruptRequested})
	return provider.InterruptResult{
		StopOutcome: provider.StopUnknown, ObservedUsability: provider.SessionUsabilityUnknown,
		Quiescence: provider.QuiescenceUnknown,
	}, nil
}

func (s *session) Close(ctx context.Context, request provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.CloseSessionResult{}, adaptererror.Delivery(provider.OperationCloseSession, request.RequestID, false, err)
	}
	var previous provider.SessionState
	interrupt := false
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideClose(rules.CloseFacts{
			SessionMatches:         request.Session == s.ref,
			Closing:                s.closing,
			ActiveOrUnresolvedWork: s.turn != nil || s.steer != nil,
			RequireQuiescent:       request.Policy == provider.CloseRequireQuiescent,
			ExplicitDetach:         request.Policy == provider.CloseExplicitDetach,
			InterruptThenClose:     request.Policy == provider.CloseInterruptThenClose,
		})
		switch decision.Kind {
		case rules.CloseSessionDiffers, rules.CloseWorkUnresolved:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.CloseAlreadyClosing:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.CloseDetachUnsupported:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		interrupt = decision.Kind == rules.CloseInterruptFirst
		s.closing = true
		previous = s.state
		s.state = provider.SessionClosing
		return nil
	})
	if err != nil {
		return provider.CloseSessionResult{}, err
	}
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionClosing})
	go s.terminateOwnedProcess(interrupt)
	select {
	case <-s.closeDone:
		var result provider.CloseSessionResult
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			result = s.closeResult
			return nil
		})
		return result, nil
	case <-ctx.Done():
		return provider.CloseSessionResult{}, adaptererror.Delivery(provider.OperationCloseSession, request.RequestID, true, ctx.Err())
	}
}

// terminateOwnedProcess is the single close owner. It optionally interrupts
// active work, closes stdin, waits StopGrace for an exit, kills the process
// group, waits StopGrace for the reap, and publishes the copied outcome.
func (s *session) terminateOwnedProcess(interrupt bool) {
	if interrupt {
		s.sendCloseInterrupt()
	}
	_ = s.transport.closeInput()
	reaped := s.waitExit()

	// A reaped root does not prove that descendants in its process group are
	// gone, so the group is always killed where tree kill is supported.
	termination := rules.DecideOwnedProcessTermination(reaped, s.process.TreeKillSupported())
	killAttempted := termination.Kind == rules.OwnedProcessKillRequired
	var killErr error
	if killAttempted {
		killErr = s.process.KillTree()
	}
	if !reaped {
		reaped = s.waitExit()
	}
	treeKillSupported := reaped && s.process.TreeKillSupported()
	killConfirmed := treeKillSupported && killAttempted && (killErr == nil || errors.Is(killErr, os.ErrProcessDone))
	quiescence := rules.DecideQuiescence(rules.QuiescenceFacts{
		RootReaped: reaped, TreeKillSupported: treeKillSupported,
		KillAttempted: killAttempted, KillConfirmed: killConfirmed,
	})
	result := provider.CloseSessionResult{HandleClosed: true, ExecutionState: provider.ExecutionUnknown}
	if quiescence.Kind == rules.QuiescenceConfirmed {
		result.ExecutionState = provider.ExecutionNone
		result.ResourcesReleased = true
	}
	var previous provider.SessionState
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		previous = s.state
		s.state = provider.SessionClosed
		s.turn = nil
		s.closeResult = result
		return nil
	})
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionClosed})
	s.broker.Finish(nil)
	s.closeOnce.Do(func() { close(s.closeDone) })
}

// sendCloseInterrupt writes the interrupt control request without waiting for
// its receipt; the bounded exit wait follows regardless of the outcome.
func (s *session) sendCloseInterrupt() {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.StopGrace)
	defer cancel()
	interrupt := struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}{Type: "control_request", RequestID: "conduit_close_interrupt"}
	interrupt.Request.Subtype = "interrupt"
	_, _ = s.transport.send(ctx, interrupt)
}

func (s *session) waitExit() bool {
	timer := time.NewTimer(s.config.StopGrace)
	defer timer.Stop()
	select {
	case <-s.exitDone:
		return true
	case <-timer.C:
		return false
	}
}

func (s *session) observeProcessExit() {
	result := s.process.Wait()
	s.exitOnce.Do(func() {
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
		close(s.exitDone)
		if lost {
			s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "claude_process_exit"})
			s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, result.Err, "claude process exited"))
		}
	})
}

// handleTransportClosed loses the session when stdout ends, a line is malformed
// or above MaxLineBytes, or a write fails. No native result is invented; the
// owned process group is killed so it cannot block on a full pipe.
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
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "claude_transport_closed"})
	s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, cause, "claude stream-json transport closed"))
	_ = s.process.KillTree()
}

// handleControlRequest rejects control requests sent by claude (for example
// can_use_tool), which this adapter does not support, so claude never waits.
func (s *session) handleControlRequest(requestID, subtype string) {
	s.emit(provider.ProtocolWarningEvent{
		Code: "unsupported_control_request", Detail: provider.BoundedText{Text: "native control request rejected"},
		OffendingNativeKind: "control_request/" + boundedtext.String(subtype, 64).Text,
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.transport.rejectControlRequest(ctx, requestID, "Conduit adapter does not support this control request")
}

// eventBinding freezes correlation and attribution for one event.
type eventBinding struct {
	Turn       provider.Optional[provider.TurnRef]
	Assignment provider.Optional[provider.AssignmentRef]
	Native     provider.NativeIDs
	Contract   provider.ResultContractNegotiation
}

// snapshotEventBinding is called under eventMu and takes the session mutex
// briefly; no caller holds the session mutex across event publication.
func (s *session) snapshotEventBinding() eventBinding {
	var binding eventBinding
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		binding = eventBinding{
			Turn:     s.activeTurnRef(),
			Native:   provider.NativeIDs{Session: provider.Some(provider.NativeSessionID(s.nativeSession))},
			Contract: s.contract,
		}
		// While a correction is unresolved, the revision the agent works on is
		// unknown, so attribution stays absent (provider contract).
		if s.steer == nil {
			binding.Assignment = provider.Some(s.assignment)
		}
		if s.turn != nil {
			binding.Native.Turn = provider.Some(provider.NativeTurnID(s.turn.inputUUID))
		}
		return nil
	})
	return binding
}

func (s *session) emit(body provider.EventBody) {
	s.emitEvent(func(eventBinding) (provider.EventBody, bool) { return body, true })
}

// emitEvent serializes event construction and publication. The body builder
// and the envelope consume the same binding snapshot.
func (s *session) emitEvent(buildBody func(eventBinding) (provider.EventBody, bool)) {
	var idErr error
	err := lockscope.WithMutexLock(&s.eventMu, func() error {
		binding := s.snapshotEventBinding()
		body, publish := buildBody(binding)
		if !publish {
			return nil
		}
		var eventID string
		eventID, idErr = s.config.NewID("claude-event")
		if idErr != nil {
			return idErr
		}
		s.eventSeq++
		envelope := provider.EventEnvelope{
			EventID: provider.EventID(eventID), Session: s.ref,
			Turn: binding.Turn, Assignment: binding.Assignment, AdapterSeq: s.eventSeq,
			Native: binding.Native, ObservedAt: s.config.Now(),
		}
		event, err := provider.NewProviderEvent(envelope, body)
		if err != nil {
			return err
		}
		return s.broker.Publish(event)
	})
	if idErr != nil {
		s.failObservation(idErr, "could not allocate event ID")
		return
	}
	if err != nil && !errors.Is(err, eventstream.ErrConsumerClosed) {
		// The stream already ended (overflow or terminal); stop the owned
		// process instead of letting it run unobserved.
		_ = s.process.KillTree()
	}
}

// failObservation marks the session lost after a local identity failure, ends
// the event stream and stops the owned process, after every lock is released.
func (s *session) failObservation(cause error, detail string) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if !s.closing {
			s.state = provider.SessionLost
		}
		return nil
	})
	s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, cause, detail))
	_ = s.process.KillTree()
}

// warnNativeOnce publishes a warning at most once per key.
func (s *session) warnNativeOnce(key string, warning provider.ProtocolWarningEvent) {
	var exists bool
	_ = lockscope.WithMutexLock(&s.warnMu, func() error {
		_, exists = s.warned[key]
		if !exists {
			s.warned[key] = struct{}{}
		}
		return nil
	})
	if !exists {
		s.emit(warning)
	}
}

var _ provider.Session = (*session)(nil)
