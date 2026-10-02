package codex

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"
	"io"
	"os"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type pendingSteer struct {
	request provider.SteerRequest
}

type session struct {
	config       Config
	ref          provider.SessionRef
	model        provider.ModelSelection
	contract     provider.ResultContractNegotiation
	capabilities provider.CapabilitySnapshot
	process      processruntime.Process
	rpc          *rpcClient
	broker       *eventstream.Broker

	mu           sync.Mutex
	nativeThread string
	state        provider.SessionState
	active       provider.Optional[provider.TurnRef]
	assignment   provider.AssignmentRef
	turnPending  bool
	steerPending *pendingSteer
	closing      bool
	closeResult  provider.CloseSessionResult
	closeDone    chan struct{}
	closeOnce    sync.Once
	exitResult   processruntime.Result
	exitDone     chan struct{}
	exitOnce     sync.Once

	eventMu      sync.Mutex
	eventSeq     uint64
	textMu       sync.Mutex
	textVersions map[string]uint64
	textBytes    map[string]uint64
	warnMu       sync.Mutex
	warnedNative map[string]struct{}
}

func (s *session) Ref() provider.SessionRef { return s.ref }

func (s *session) Capabilities() provider.CapabilitySnapshot { return s.capabilities }

func (s *session) initialize(ctx context.Context, request provider.OpenSessionRequest) error {
	_, sent, err := s.rpc.call(ctx, "initialize", struct {
		ClientInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
		Capabilities struct {
			ExperimentalAPI bool `json:"experimentalApi"`
		} `json:"capabilities"`
	}{
		ClientInfo: struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{Name: "conduit", Version: adapterVersion},
		Capabilities: struct {
			ExperimentalAPI bool `json:"experimentalApi"`
		}{ExperimentalAPI: true},
	}, nil)
	if err != nil {
		return adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, sent, err)
	}
	if sent, err := s.rpc.notify(ctx, "initialized", struct{}{}); err != nil {
		return adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, sent, err)
	}
	params := struct {
		CWD                        string   `json:"cwd"`
		Model                      string   `json:"model,omitempty"`
		AllowProviderModelFallback bool     `json:"allowProviderModelFallback"`
		Sandbox                    string   `json:"sandbox"`
		ApprovalPolicy             string   `json:"approvalPolicy"`
		Ephemeral                  bool     `json:"ephemeral"`
		RuntimeWorkspaceRoots      []string `json:"runtimeWorkspaceRoots"`
	}{
		CWD: request.Workspace.Path, Model: request.Model.ModelID,
		AllowProviderModelFallback: false, Sandbox: string(s.config.Sandbox),
		ApprovalPolicy: string(s.config.Approval), Ephemeral: s.config.Ephemeral,
		RuntimeWorkspaceRoots: []string{request.Workspace.Path},
	}
	result, sent, err := s.rpc.call(ctx, "thread/start", params, nil)
	if err != nil {
		return adaptererror.Delivery(provider.OperationOpenSession, request.RequestID, sent, err)
	}
	var opened struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &opened); err != nil || opened.Thread.ID == "" {
		return adaptererror.New(provider.OperationOpenSession, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "thread/start returned no thread ID")
	}
	var previous provider.SessionState
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.nativeThread = opened.Thread.ID
		previous = s.state
		s.state = provider.SessionOpen
		return nil
	})
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionOpen, NativeStatus: "thread_started"})
	return nil
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
	if err := s.checkSessionReference(provider.OperationObserveState, request.Session); err != nil {
		return provider.SessionSnapshot{}, err
	}
	var snapshot provider.SessionSnapshot
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		snapshot = provider.SessionSnapshot{
			Knowledge: provider.SnapshotPartial, SessionState: s.state,
			ActiveTurn: s.active, Native: provider.NativeIDs{Session: provider.Some(provider.NativeSessionID(s.nativeThread))},
		}
		return nil
	})
	snapshot.ObservedAt = s.config.Now()
	return snapshot, nil
}

func (s *session) StartTurn(ctx context.Context, request provider.StartTurnRequest) (provider.StartTurnResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.StartTurnResult{}, adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, false, err)
	}
	actual, err := negotiateResultContract(request.ResultContract)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	inputs, err := codexInputs(request.Input)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	var threadID string
	err = lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideStartTurn(rules.StartTurnFacts{
			Closing: s.closing, TurnPending: s.turnPending, ActiveTurn: s.active.Present,
			SessionMatches:            request.Session == s.ref,
			AssignmentIdentityMatches: request.Assignment.SameIdentity(s.assignment),
		})
		switch decision.Kind {
		case rules.StartTurnClosing:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnOccupied:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnBindingDiffers:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		s.turnPending = true
		threadID = s.nativeThread
		return nil
	})
	if err != nil {
		return provider.StartTurnResult{}, err
	}

	params := struct {
		ThreadID     string          `json:"threadId"`
		Input        []nativeInput   `json:"input"`
		Model        string          `json:"model,omitempty"`
		Effort       string          `json:"effort,omitempty"`
		OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	}{ThreadID: threadID, Input: inputs, Model: s.model.ModelID, Effort: string(s.config.Reasoning)}
	if actual == provider.SchemaNative {
		params.OutputSchema = request.ResultContract.Schema
	}
	late := func(reply rpcResponse) { s.reconcileLateTurn(request, actual, reply) }
	result, sent, callErr := s.rpc.call(ctx, "turn/start", params, late)
	if callErr != nil {
		return provider.StartTurnResult{}, s.handleTurnStartDeliveryFailure(request, sent, callErr)
	}
	return s.acceptTurnStart(request, actual, result)
}

type nativeInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func codexInputs(parts []provider.InputPart) ([]nativeInput, error) {
	result := make([]nativeInput, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case provider.InputInstruction, provider.InputContext:
			result = append(result, nativeInput{Type: "text", Text: part.Text})
		default:
			return nil, adaptererror.New(provider.OperationStartTurn, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "only text instruction/context input is fixture-backed")
		}
	}
	return result, nil
}

func (s *session) acceptTurnStart(request provider.StartTurnRequest, actual provider.SchemaEnforcement, result json.RawMessage) (provider.StartTurnResult, error) {
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &started); err != nil || started.Turn.ID == "" {
		s.releaseTurnStartReservation()
		return provider.StartTurnResult{}, adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "turn/start returned no turn ID")
	}
	turn := provider.TurnRef{AdapterTurnID: provider.AdapterTurnID(started.Turn.ID)}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.turnPending = false
		s.active = provider.Some(turn)
		s.assignment = request.Assignment
		s.contract = provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual}
		return nil
	})
	return provider.StartTurnResult{
		Turn: turn, Assignment: request.Assignment, TargetingStrength: provider.TargetNativeTurnID,
		ResultEnforcement: actual,
		Native:            provider.NativeIDs{Session: provider.Some(provider.NativeSessionID(s.nativeThread)), Turn: provider.Some(provider.NativeTurnID(started.Turn.ID))},
	}, nil
}

func (s *session) reconcileLateTurn(request provider.StartTurnRequest, actual provider.SchemaEnforcement, reply rpcResponse) {
	if reply.err != nil {
		failure := classifyDeliveryFailure(provider.OperationStartTurn, request.RequestID, true, reply.err)
		if failure.NativeRejected {
			s.releaseTurnStartReservation()
			s.emit(provider.ProtocolWarningEvent{Code: "late_turn_start_rejected", Detail: provider.BoundedText{Text: "late turn/start response rejected"}})
			return
		}
		s.emit(provider.ProtocolWarningEvent{Code: "turn_start_delivery_unknown", Detail: provider.BoundedText{Text: "turn/start transport ended before reconciliation"}})
		return
	}
	if _, err := s.acceptTurnStart(request, actual, reply.result); err != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "late_turn_start_invalid", Detail: provider.BoundedText{Text: "late turn/start response was invalid"}})
	}
}

func (s *session) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationRespondPermission, provider.ErrorCapabilityUnknown, provider.RemoteEffectNotSent, nil, "permission round-trip is not fixture-backed")
}

func (s *session) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationAnswerQuestion, provider.ErrorCapabilityUnknown, provider.RemoteEffectNotSent, nil, "question round-trip is not fixture-backed")
}

func (s *session) InterruptTurn(ctx context.Context, request provider.InterruptRequest) (provider.InterruptResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.InterruptResult{}, adaptererror.Delivery(provider.OperationInterruptTurn, request.RequestID, false, err)
	}
	var threadID, turnID string
	err := lockscope.WithMutexLock(&s.mu, func() error {
		if request.Session != s.ref || !s.active.Present || s.active.Value != request.ExpectedActiveTurn || request.Assignment != s.assignment {
			return adaptererror.New(provider.OperationInterruptTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, "interrupt target differs from active binding")
		}
		threadID = s.nativeThread
		turnID = string(s.active.Value.AdapterTurnID)
		return nil
	})
	if err != nil {
		return provider.InterruptResult{}, err
	}
	params := struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
	}{ThreadID: threadID, TurnID: turnID}
	_, sent, err := s.rpc.call(ctx, "turn/interrupt", params, nil)
	if err != nil {
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
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideClose(rules.CloseFacts{
			SessionMatches:         request.Session == s.ref,
			Closing:                s.closing,
			RequireQuiescent:       request.Policy == provider.CloseRequireQuiescent,
			ActiveOrUnresolvedWork: s.active.Present || s.turnPending || s.steerPending != nil,
			ExplicitDetach:         request.Policy == provider.CloseExplicitDetach,
		})
		switch decision.Kind {
		case rules.CloseSessionDiffers, rules.CloseWorkUnresolved:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.CloseAlreadyClosing:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.CloseDetachUnsupported:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		s.closing = true
		previous = s.state
		s.state = provider.SessionClosing
		return nil
	})
	if err != nil {
		return provider.CloseSessionResult{}, err
	}
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionClosing})
	_ = s.rpc.closeInput()
	_ = s.process.Interrupt()
	go s.terminateOwnedProcess()
	select {
	case <-s.closeDone:
		var result provider.CloseSessionResult
		_ = lockscope.WithMutexLock(&s.mu, func() error { result = s.closeResult; return nil })
		return result, nil
	case <-ctx.Done():
		return provider.CloseSessionResult{}, adaptererror.Delivery(provider.OperationCloseSession, request.RequestID, true, ctx.Err())
	}
}

func (s *session) terminateOwnedProcess() {
	reaped := false
	select {
	case <-s.exitDone:
		reaped = true
	case <-time.After(s.config.CloseGrace):
	}

	// A reaped root process is not proof that descendants in its owned process
	// group are gone. On supported platforms, always terminate the group. On
	// unsupported platforms, KillTree still terminates the root process only.
	termination := rules.DecideOwnedProcessTermination(reaped, s.process.TreeKillSupported())
	killAttempted := termination.Kind == rules.OwnedProcessKillRequired
	var killErr error
	if killAttempted {
		killErr = s.process.KillTree()
	}
	if !reaped {
		select {
		case <-s.exitDone:
			reaped = true
		case <-time.After(s.config.CloseGrace):
		}
	}

	result := provider.CloseSessionResult{HandleClosed: true, ExecutionState: provider.ExecutionUnknown}
	// Preserve the original evidence-read and cause-check short circuit order.
	treeKillSupported := reaped && s.process.TreeKillSupported()
	killConfirmed := treeKillSupported && killAttempted && (killErr == nil || errors.Is(killErr, os.ErrProcessDone))
	quiescence := rules.DecideQuiescence(rules.QuiescenceFacts{
		RootReaped:        reaped,
		TreeKillSupported: treeKillSupported,
		KillAttempted:     killAttempted,
		KillConfirmed:     killConfirmed,
	})
	if quiescence.Kind == rules.QuiescenceConfirmed {
		result.ExecutionState = provider.ExecutionNone
		result.ResourcesReleased = true
	}
	var previous provider.SessionState
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		previous = s.state
		s.state = provider.SessionClosed
		s.active = provider.None[provider.TurnRef]()
		s.closeResult = result
		return nil
	})
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionClosed})
	s.broker.Finish(nil)
	s.closeOnce.Do(func() { close(s.closeDone) })
}

func (s *session) abortOpen() {
	_ = lockscope.WithMutexLock(&s.mu, func() error { s.closing = true; return nil })
	_ = s.rpc.closeInput()
	_ = s.process.KillTree()
	<-s.exitDone
	s.broker.Finish(io.EOF)
}

func (s *session) observeProcessExit() {
	result := s.process.Wait()
	s.exitOnce.Do(func() {
		var closing bool
		var previous provider.SessionState
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			s.exitResult = result
			closing = s.closing
			previous = s.state
			if !closing {
				s.state = provider.SessionLost
			}
			return nil
		})
		close(s.exitDone)
		if !closing {
			s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "app_server_process_exit"})
			s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorSessionLost, provider.RemoteEffectUnknown, result.Err, "App Server process exited"))
		}
	})
}

func (s *session) emit(body provider.EventBody) {
	s.emitEvent(func(_ eventBinding) (provider.EventBody, bool) { return body, true })
}

// emitEvent serializes all event construction/publication. The mandatory body
// builder and envelope consume one immutable binding snapshot, never two reads.
func (s *session) emitEvent(buildBody func(eventBinding) (provider.EventBody, bool)) {
	var idErr error
	err := lockscope.WithMutexLock(&s.eventMu, func() error {
		binding := s.snapshotEventBinding()
		body, publish := buildBody(binding)
		if !publish {
			return nil
		}
		var eventID string
		eventID, idErr = s.config.NewID("codex-event")
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
		s.handleEventIDFailure(idErr)
		return
	}
	if err != nil {
		_ = s.process.KillTree()
	}
}

// handleEventIDFailure marks the local observation session lost, ends its event
// stream and stops the owned process after all event/session locks are released.
func (s *session) handleEventIDFailure(cause error) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if !s.closing {
			s.state = provider.SessionLost
		}
		return nil
	})
	s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, cause, "could not allocate event ID"))
	_ = s.process.KillTree()
}

func optionalAssignment(assignment provider.AssignmentRef, unresolved bool) provider.Optional[provider.AssignmentRef] {
	if unresolved {
		return provider.None[provider.AssignmentRef]()
	}
	return provider.Some(assignment)
}

var _ provider.Session = (*session)(nil)

// handleTurnStartDeliveryFailure clears a proven rejection/unsent start. A sent
// transport failure retains the pending start for late native reconciliation.
func (s *session) handleTurnStartDeliveryFailure(request provider.StartTurnRequest, sent bool, cause error) error {
	failure := classifyDeliveryFailure(provider.OperationStartTurn, request.RequestID, sent, cause)
	if failure.NativeRejected || !sent {
		s.releaseTurnStartReservation()
	}
	return failure.Err
}

func (s *session) releaseTurnStartReservation() {
	_ = lockscope.WithMutexLock(&s.mu, func() error { s.turnPending = false; return nil })
}
