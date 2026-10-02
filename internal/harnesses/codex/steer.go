package codex

import (
	"context"
	"encoding/json"
	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
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
	result, sent, callErr := s.deliverSteer(ctx, request, threadID, turnID, inputs)
	if callErr != nil {
		return provider.DeliveryReceipt{}, s.handleSteerDeliveryFailure(request, sent, callErr)
	}
	receipt, err := s.acceptSteer(request, result)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	s.emit(provider.ControlReceiptEvent{Receipt: receipt})
	return receipt, nil
}

func (s *session) acceptSteer(request provider.SteerRequest, result json.RawMessage) (provider.DeliveryReceipt, error) {
	var response struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.TurnID == "" {
		return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, err, "turn/steer returned no turn ID")
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
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	receipt := provider.DeliveryReceipt{
		RequestID: request.RequestID, MessageID: request.MessageID,
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown,
		Targeting: provider.TargetNativeTurnID, DispatchedFor: provider.Some(request.NewAssignment),
		ObservedAt: s.config.Now(),
	}
	return receipt, nil
}

func (s *session) reconcileLateSteer(request provider.SteerRequest, reply rpcResponse) {
	if reply.err != nil {
		s.handleLateSteerDeliveryFailure(request, reply.err)
		return
	}
	receipt, err := s.acceptSteer(request, reply.result)
	if err != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "late_steer_invalid", Detail: provider.BoundedText{Text: "late turn/steer response was invalid"}})
		return
	}
	s.emit(provider.ControlReceiptEvent{Receipt: receipt})
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

// deliverSteer sends one native correction. It does not accept the assignment
// or publish a receipt; an uncertain send remains reserved for reconciliation.
func (s *session) deliverSteer(ctx context.Context, request provider.SteerRequest, threadID, turnID string, inputs []nativeInput) (json.RawMessage, bool, error) {
	params := struct {
		ThreadID       string        `json:"threadId"`
		ExpectedTurnID string        `json:"expectedTurnId"`
		Input          []nativeInput `json:"input"`
	}{ThreadID: threadID, ExpectedTurnID: turnID, Input: inputs}
	late := func(reply rpcResponse) { s.reconcileLateSteer(request, reply) }
	return s.rpc.call(ctx, "turn/steer", params, late)
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
