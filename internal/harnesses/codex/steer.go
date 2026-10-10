package codex

import (
	"context"
	"encoding/json"
	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func (s *session) Steer(ctx context.Context, request provider.SteerRequest) (provider.DeliveryReceipt, error) {
	if err := ctx.Err(); err != nil {
		return provider.DeliveryReceipt{}, adaptererror.Delivery(provider.OperationSteer, request.RequestID, false, err)
	}
	inputs, err := codexInputs(request.Parts)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	threadID, turnID, err := s.reserveSteer(request)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	committed, sent, callErr := s.deliverSteer(ctx, request, threadID, turnID, inputs)
	if callErr != nil {
		return provider.DeliveryReceipt{}, s.handleSteerDeliveryFailure(request, sent, callErr)
	}
	if !committed.done {
		return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, nil, "turn/steer reply was not committed")
	}
	if committed.err != nil {
		return provider.DeliveryReceipt{}, committed.err
	}
	receipt := s.steerReceipt(request)
	s.emit(provider.ControlReceiptEvent{Receipt: receipt})
	return receipt, nil
}

// steerCommit is written by the reader goroutine before the reply reaches the
// waiting caller or late callback.
type steerCommit struct {
	done bool
	err  error
}

// commitSteer advances the assignment while the reader is positioned right
// after the native reply, before any later turn/completed can clear the turn.
func (s *session) commitSteer(request provider.SteerRequest, result json.RawMessage) steerCommit {
	var response struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.TurnID == "" {
		return steerCommit{done: true, err: adaptererror.New(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "turn/steer returned no turn ID")}
	}
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideSteerReceipt(rules.SteerReceiptFacts{
			PendingRequestMatches: s.steerPending != nil && s.steerPending.request.RequestID == request.RequestID,
			ActiveTurnMatches:     s.active.Present && s.active.Value == request.ExpectedActiveTurn,
			NativeTurnMatches:     response.TurnID == string(request.ExpectedActiveTurn.AdapterTurnID),
		})
		if decision.Kind != rules.SteerReceiptAccepted {
			return adaptererror.New(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, nil, decision.Reason)
		}
		s.assignment = request.NewAssignment
		s.steerPending = nil
		return nil
	})
	return steerCommit{done: true, err: err}
}

func (s *session) steerReceipt(request provider.SteerRequest) provider.DeliveryReceipt {
	return provider.DeliveryReceipt{
		RequestID: request.RequestID, MessageID: request.MessageID,
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown,
		Targeting: provider.TargetNativeTurnID, DispatchedFor: provider.Some(request.NewAssignment),
		ObservedAt: s.config.Now(),
	}
}

func (s *session) reconcileLateSteer(request provider.SteerRequest, reply rpcResponse, committed steerCommit) {
	if reply.err != nil {
		s.handleLateSteerDeliveryFailure(request, reply.err)
		return
	}
	if committed.err != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "late_steer_invalid", Detail: provider.BoundedText{Text: "late turn/steer response was invalid"}})
		return
	}
	s.emit(provider.ControlReceiptEvent{Receipt: s.steerReceipt(request)})
}

// reserveSteer checks adapter facts and the domain rule under one lock, then
// reserves delivery before any other caller can inspect those same facts.
func (s *session) reserveSteer(request provider.SteerRequest) (string, string, error) {
	var threadID, turnID string
	err := lockscope.WithMutexLock(&s.mu, func() error {
		technical := rules.DecideSteer(rules.SteerFacts{
			SessionMatches:    request.Session == s.ref,
			ActiveTurnMode:    request.Mode == provider.DeliverActiveTurn,
			ActiveTurnMatches: s.active.Present && s.active.Value == request.ExpectedActiveTurn,
			SteerPending:      s.steerPending != nil,
			TurnPending:       s.turnPending,
		})
		switch technical.Kind {
		case rules.SteerSessionDiffers, rules.SteerBindingUnresolved:
			return adaptererror.New(provider.OperationSteer, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, technical.Reason)
		case rules.SteerModeUnsupported:
			return adaptererror.New(provider.OperationSteer, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, technical.Reason)
		}
		decision := sessions.DecideCorrection(s.assignment, request.CurrentAssignment, request.ExpectedRevision, request.NewAssignment)
		if decision.Kind != sessions.CorrectionAllowed {
			return adaptererror.New(provider.OperationSteer, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		s.steerPending = &pendingSteer{request: request}
		threadID = s.nativeThread
		turnID = string(s.active.Value.AdapterTurnID)
		return nil
	})
	return threadID, turnID, err
}

// deliverSteer sends one native correction. A successful reply is committed on
// the reader goroutine; an uncertain send remains reserved for reconciliation.
func (s *session) deliverSteer(ctx context.Context, request provider.SteerRequest, threadID, turnID string, inputs []nativeInput) (steerCommit, bool, error) {
	params := struct {
		ThreadID       string        `json:"threadId"`
		ExpectedTurnID string        `json:"expectedTurnId"`
		Input          []nativeInput `json:"input"`
	}{ThreadID: threadID, ExpectedTurnID: turnID, Input: inputs}
	var committed steerCommit
	commit := func(reply rpcResponse) {
		if reply.err == nil {
			committed = s.commitSteer(request, reply.result)
		}
	}
	late := func(reply rpcResponse) { s.reconcileLateSteer(request, reply, committed) }
	_, sent, err := s.rpc.callCommitting(ctx, "turn/steer", params, commit, late)
	if err != nil {
		// The reader may still commit a late reply; do not read it here.
		return steerCommit{}, sent, err
	}
	return committed, sent, nil
}

// handleSteerDeliveryFailure releases a proven rejected/unsent reservation. A
// possibly sent correction stays pending until native reconciliation resolves it.
func (s *session) handleSteerDeliveryFailure(request provider.SteerRequest, sent bool, cause error) error {
	failure := classifyDeliveryFailure(provider.OperationSteer, request.RequestID, sent, cause)
	if failure.NativeRejected || !sent {
		s.releaseSteerReservation()
	}
	return failure.Err
}

func (s *session) releaseSteerReservation() {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.steerPending = nil
		return nil
	})
}

// handleLateSteerDeliveryFailure clears only the matching rejected reservation;
// a transport failure preserves uncertainty. Warnings publish after unlocking.
func (s *session) handleLateSteerDeliveryFailure(request provider.SteerRequest, cause error) {
	failure := classifyDeliveryFailure(provider.OperationSteer, request.RequestID, true, cause)
	if failure.NativeRejected {
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.steerPending != nil && s.steerPending.request.RequestID == request.RequestID {
				s.steerPending = nil
			}
			return nil
		})
		s.emit(provider.ProtocolWarningEvent{Code: "late_steer_rejected", Detail: provider.BoundedText{Text: "late turn/steer response rejected"}})
		return
	}
	s.emit(provider.ProtocolWarningEvent{Code: "steer_delivery_unknown", Detail: provider.BoundedText{Text: "turn/steer transport ended before reconciliation"}})
}
