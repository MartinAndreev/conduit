package claude

import (
	"context"
	"encoding/json"
	"errors"
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
// attribution. It survives the end of its turn unless the terminal result
// proves it was not observed (rules.DecideUnobservedSteer): late evidence still
// resolves it, and otherwise only Conduit's explicit
// StartTurnRequest.ReleaseUnresolvedSteer naming request.MessageID clears it.
type pendingSteer struct {
	request provider.SteerRequest
	uuid    string
	// lifecycleObserved is set when any command_lifecycle named uuid.
	lifecycleObserved bool
	// dispatch is the local write progress (rules.SteerReserved until Steer
	// claims the write). withdrawn is set when a terminal result withdrew a
	// steer that was never handed to the writer; Steer then must not send it.
	dispatch  rules.SteerDispatch
	withdrawn bool
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

	mu         sync.Mutex
	state      provider.SessionState
	assignment provider.AssignmentRef
	contract   provider.ResultContractNegotiation
	turn       *activeTurn
	steer      *pendingSteer
	// released holds the UUIDs of steers Conduit explicitly released. Such a
	// steer may still be queued in claude and run later; until a result names
	// it, output is attributed only when it is positively correlated to the
	// active turn (activeInput is one of its inputs).
	released map[string]struct{}
	// activeInput is the input UUID claude most recently started (lifecycle
	// started or replay echo), which identifies the input that later output
	// belongs to.
	activeInput string
	// afterSteerReserved is a test seam between steer reservation and the
	// dispatch claim; it is nil in production.
	afterSteerReserved func()
	// beforeTransportLoss is a test seam at the start of
	// handleTransportClosed; it is nil in production.
	beforeTransportLoss func()
	closing             bool
	observedModel       string
	closeResult         provider.CloseSessionResult
	closeDone           chan struct{}
	closeOnce           sync.Once
	exitDone            chan struct{}
	exitOnce            sync.Once
	// lossKillOnce guards the single group kill after an unexpected stop.
	lossKillOnce sync.Once
	// rootExited is set (under mu) once the root's exit was observed.
	rootExited bool

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
		released:      make(map[string]struct{}),
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

// abortOpen hands the process of a failed open to one retained cleanup owner
// (stdin close, group kill, pipe close, root reap and reader drain) and waits
// for it within what remains of the caller's context, at most StopGrace. It
// returns cause with any cleanup failure joined; when the wait ends first, the
// joined CleanupPendingError keeps the owner observable until it completes.
func (s *session) abortOpen(ctx context.Context, cause error) error {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.closing = true
		return nil
	})
	owner := processruntime.NewCleanupOwnerWithDrain(s.process, s.transport.readersDone)
	owner.BeginCleanup()
	waitCtx, cancel := context.WithTimeout(ctx, s.config.StopGrace)
	defer cancel()
	state, waitErr := owner.Await(waitCtx)
	s.broker.Finish(io.EOF)
	var cleanup error
	if waitErr != nil {
		cleanup = &CleanupPendingError{Owner: owner, Cause: waitErr}
	} else {
		cleanup = errors.Join(state.TreeTerminationErr, state.PipeCloseErr, state.StdinCloseErr, state.RootResult.ReleaseErr)
	}
	if tail := s.transport.stderrTail.String(); tail != "" {
		cleanup = errors.Join(cleanup, &stderrTail{Text: tail})
	}
	return withCleanupFailure(cause, cleanup)
}

// CleanupPendingError reports that an aborted open returned before its owned
// claude process was fully cleaned up. Owner keeps the cleanup observable:
// Done closes on completion and Latest reports its state. Cause is the ended
// wait.
type CleanupPendingError struct {
	Owner *processruntime.CleanupOwner
	Cause error
}

func (e *CleanupPendingError) Error() string { return "owned claude process cleanup is still pending" }
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

// outputCorrelatedLocked reports whether current output can be attributed:
// always when no released steer may still run, otherwise only when claude
// most recently started an input of the active turn. s.mu must be held.
func (s *session) outputCorrelatedLocked() bool {
	if len(s.released) == 0 {
		return true
	}
	if s.turn == nil {
		return false
	}
	_, ok := s.turn.inputs[s.activeInput]
	return ok
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
	reservation, err := s.reserveTurn(request, turn)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	sent, err := s.transport.send(ctx, s.userMessage(content, inputUUID, ""))
	if err != nil {
		return provider.StartTurnResult{}, s.handleTurnDispatchFailure(request, turn, reservation, sent, err)
	}
	return provider.StartTurnResult{
		Turn: turn.ref, Assignment: request.Assignment, TargetingStrength: provider.TargetSessionLease,
		ResultEnforcement: reservation.actual,
		Native: provider.NativeIDs{
			Session: provider.Some(provider.NativeSessionID(s.nativeSession)),
			Turn:    provider.Some(provider.NativeTurnID(inputUUID)),
		},
	}, nil
}

// turnReservation is what reserveTurn changed, so an unsent dispatch can undo
// exactly that.
type turnReservation struct {
	previous provider.AssignmentRef
	actual   provider.SchemaEnforcement
	// released is the unresolved steer cleared by an explicit release.
	released *pendingSteer
}

// reserveTurn evaluates eligibility (including an explicit steer release) and
// installs the turn lease in one critical section, before dispatch, so the
// reader can correlate an immediate echo.
func (s *session) reserveTurn(request provider.StartTurnRequest, turn *activeTurn) (turnReservation, error) {
	var reservation turnReservation
	err := lockscope.WithMutexLock(&s.mu, func() error {
		release := request.ReleaseUnresolvedSteer
		decision := rules.DecideStartTurn(rules.StartTurnFacts{
			Closing:                       s.closing || s.state == provider.SessionLost,
			ActiveTurn:                    s.turn != nil,
			SteerUnresolved:               s.steer != nil,
			ReleaseRequested:              release.Present,
			ReleaseMatchesUnresolvedSteer: release.Present && s.steer != nil && s.steer.request.MessageID == release.Value,
			SessionMatches:                request.Session == s.ref,
			AssignmentIdentityMatches:     request.Assignment.SameIdentity(s.assignment),
			ResultContractMatches:         sameResultContract(request.ResultContract, s.contract.Contract),
		})
		switch decision.Kind {
		case rules.StartTurnClosing:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnOccupied, rules.StartTurnBindingDiffers, rules.StartTurnReleaseNothingUnresolved, rules.StartTurnReleaseMismatch:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnContractChanged:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnAllowedReleasingSteer:
			// The released steer's delivery stays unknown; the assignment it
			// proposed is never adopted, and earlier output stays unattributed.
			reservation.released = s.steer
			s.released[s.steer.uuid] = struct{}{}
			s.steer = nil
		}
		reservation.previous = s.assignment
		reservation.actual = s.contract.Actual
		s.turn = turn
		s.assignment = request.Assignment
		return nil
	})
	return reservation, err
}

// handleTurnDispatchFailure releases a lease whose message was never handed to
// the writer and restores a steer that this request released, so a not-sent
// StartTurn leaves the session as it was. A possibly sent message keeps its
// lease and the release, because a later native result may still reference it;
// the caller sees delivery_unknown.
func (s *session) handleTurnDispatchFailure(request provider.StartTurnRequest, turn *activeTurn, reservation turnReservation, sent bool, cause error) error {
	if !sent {
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.turn == turn {
				s.turn = nil
				s.assignment = reservation.previous
				if s.steer == nil && reservation.released != nil {
					s.steer = reservation.released
					delete(s.released, reservation.released.uuid)
				}
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
// receipt; the turn ends when a correlated result is observed. The receipt is
// observed on the reader in wire order (observeInterruptReceipt), so it can
// never be published against a later turn.
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
	turn := request.ExpectedActiveTurn
	_, sent, err := s.transport.controlCommitting(ctx, struct {
		Subtype string `json:"subtype"`
	}{Subtype: "interrupt"}, func(reply controlReply) {
		if reply.err == nil {
			s.observeInterruptReceipt(turn)
		}
	})
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
	return provider.InterruptResult{
		StopOutcome: provider.StopUnknown, ObservedUsability: provider.SessionUsabilityUnknown,
		Quiescence: provider.QuiescenceUnknown,
	}, nil
}

// observeInterruptReceipt runs on the transport reader when the interrupt's
// control_response arrives, before any later native line. It publishes
// TurnInterruptRequested only while the requested turn is still the active,
// unfinished turn of the event binding; a turn that already ended (its
// terminal result came first on the wire) suppresses the receipt.
func (s *session) observeInterruptReceipt(turn provider.TurnRef) {
	ending := true
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		ending = s.turn == nil || s.turn.ref != turn || s.turn.ending
		return nil
	})
	if ending {
		return
	}
	s.emitEvent(func(binding eventBinding) (provider.EventBody, bool) {
		if !binding.Turn.Present || binding.Turn.Value != turn {
			return nil, false
		}
		return provider.TurnStateEvent{Previous: provider.TurnActive, Current: provider.TurnInterruptRequested}, true
	})
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

// observeProcessExit reacts to the root's exit as soon as it is reaped, even
// while a descendant still holds stdout or stderr open (process.Wait would
// block on those pipes). Group termination and stream finalization are
// separate: an unexpected exit kills the group at once, which ends the pipes;
// the loss is published only after the reader routed every line it already
// received (a final result, usage, a candidate), bounded by StopGrace, so it
// follows those events. exitDone then closes once Wait returned.
func (s *session) observeProcessExit() {
	rootExited, rootResult := processruntime.ObserveRootExit(s.process)
	<-rootExited
	if closing := s.markRootExited(); !closing {
		s.lossKillOnce.Do(func() { _ = s.process.KillTree() })
		timer := time.NewTimer(s.config.StopGrace)
		select {
		case <-s.transport.routed:
		case <-timer.C:
		}
		timer.Stop()
	}
	s.handleUnexpectedStop(s.markLost(), "claude_process_exit",
		adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, rootResult().Err, "claude process exited"))
	s.process.Wait()
	s.exitOnce.Do(func() { close(s.exitDone) })
}

// markRootExited records that the root exited, so the transport stop that the
// group kill causes leaves the loss publication to observeProcessExit. It
// reports whether Close or a failed open owns the shutdown.
func (s *session) markRootExited() bool {
	closing := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.rootExited = true
		closing = s.closing
		return nil
	})
	return closing
}

// handleTransportClosed loses the session when stdout ends, a line is malformed
// or above MaxLineBytes, or a write fails. No native result is invented. After
// a root exit, observeProcessExit owns the publication, so only the kill
// obligation remains here.
func (s *session) handleTransportClosed(cause error) {
	if s.beforeTransportLoss != nil {
		s.beforeTransportLoss()
	}
	// The root-exit check and the loss reservation are one critical section:
	// once the exit observer has recorded the root exit it owns the
	// publication, and otherwise this path reserves the loss before the
	// observer can.
	rootExited := false
	var loss sessionLoss
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		rootExited = s.rootExited
		if !rootExited {
			loss = s.markLostLocked()
		}
		return nil
	})
	if rootExited {
		return
	}
	s.handleUnexpectedStop(loss, "claude_transport_closed",
		adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, cause, "claude stream-json transport closed"))
}

// sessionLoss is the outcome of markLost: whether this caller performed the
// state transition and whether Close or a failed open already owns cleanup.
type sessionLoss struct {
	published bool
	previous  provider.SessionState
	closing   bool
}

// markLost moves an open session to SessionLost once. A closing session is not
// marked lost: its close or abort procedure owns the outcome.
func (s *session) markLost() sessionLoss {
	var loss sessionLoss
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		loss = s.markLostLocked()
		return nil
	})
	return loss
}

// markLostLocked is markLost for a caller that holds s.mu.
func (s *session) markLostLocked() sessionLoss {
	loss := sessionLoss{closing: s.closing}
	if s.closing || s.state == provider.SessionLost {
		return loss
	}
	loss.previous = s.state
	s.state = provider.SessionLost
	loss.published = true
	return loss
}

// handleUnexpectedStop is the single cleanup owner for an unexpected root exit
// and a transport failure, whichever is observed first. State publication is
// deduplicated by markLost; the obligation to kill the owned process group is
// separate and runs once, because a root that exited on its own does not prove
// that its descendants are gone, and a stopped transport leaves the process
// unobserved. Close and a failed open own their own termination.
func (s *session) handleUnexpectedStop(loss sessionLoss, nativeStatus string, terminal error) {
	if loss.published {
		s.emit(provider.SessionStateEvent{Previous: loss.previous, Current: provider.SessionLost, NativeStatus: nativeStatus})
		s.broker.Finish(terminal)
	}
	if !loss.closing {
		s.lossKillOnce.Do(func() { _ = s.process.KillTree() })
	}
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
			Native:   provider.NativeIDs{Session: provider.Some(provider.NativeSessionID(s.nativeSession))},
			Contract: s.contract,
		}
		// Output that may come from a released steer still running in claude
		// belongs to no turn or revision that Conduit knows.
		if !s.outputCorrelatedLocked() {
			return nil
		}
		binding.Turn = s.activeTurnRef()
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
