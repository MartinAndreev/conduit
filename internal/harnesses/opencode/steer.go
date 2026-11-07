package opencode

import (
	"context"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func (s *session) Steer(ctx context.Context, request provider.SteerRequest) (provider.DeliveryReceipt, error) {
	messageID, err := s.newNativeMessageID()
	if err != nil {
		return provider.DeliveryReceipt{}, adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "could not allocate native message ID", err)
	}
	native, err := s.reserveSteer(request)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	result, sent, err := s.deliverSteer(ctx, native, messageID, request.Parts)
	if err != nil {
		return provider.DeliveryReceipt{}, s.handleSteerDeliveryFailure(request, messageID, sent, err)
	}
	if !result.Accepted {
		return provider.DeliveryReceipt{}, s.handleSteerRejection()
	}
	receipt, err := s.acceptSteer(request, result)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	s.emit(provider.ControlReceiptEvent{Receipt: receipt})
	return receipt, nil
}

// reserveSteer keeps assignment policy and adapter lease reservation atomic.
func (s *session) reserveSteer(request provider.SteerRequest) (provider.NativeSessionID, error) {
	var native provider.NativeSessionID
	err := lockscope.WithMutexLock(&s.mu, func() error {
		eligibility := rules.DecideSteerEligibility(rules.SteerEligibilityFacts{
			SessionMatches:  request.Session == s.ref,
			ActiveTurn:      s.turn.Present,
			TurnMatches:     request.ExpectedActiveTurn == s.turn.Value,
			SteerPending:    s.steerPending,
			DeliveryUnknown: s.deliveryUnresolved(),
		})
		if eligibility.Kind == rules.SteerBindingStale {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, eligibility.Reason, nil)
		}
		decision := sessions.DecideCorrection(s.assignment, request.CurrentAssignment, request.ExpectedRevision, request.NewAssignment)
		if decision.Kind != sessions.CorrectionAllowed {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, decision.Reason, nil)
		}
		mode := rules.DecideSteerMode(request.Mode == provider.DeliverActiveTurn)
		if mode.Kind == rules.SteerModeUnsupported {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, mode.Reason, nil)
		}
		s.steerPending = true
		native = s.nativeSession
		return nil
	})
	return native, err
}

// deliverSteer performs only the native session prompt effect, admitted under
// the adapter-chosen native message ID.
func (s *session) deliverSteer(ctx context.Context, native provider.NativeSessionID, messageID string, parts []provider.InputPart) (PromptResult, bool, error) {
	return s.config.Control.Prompt(ctx, PromptRequest{SessionID: native, Directory: s.workspace, MessageID: messageID, Text: joinParts(parts), Delivery: "steer"})
}

// acceptSteer advances only a still-matching lease after transport acceptance.
func (s *session) acceptSteer(request provider.SteerRequest, result PromptResult) (provider.DeliveryReceipt, error) {
	err := lockscope.WithMutexLock(&s.mu, func() error {
		s.steerPending = false
		decision := rules.DecideSteerReceipt(rules.SteerReceiptFacts{
			ActiveTurn:      s.turn.Present,
			TurnMatches:     s.turn.Value == request.ExpectedActiveTurn,
			RevisionMatches: s.assignment.Revision == request.ExpectedRevision,
		})
		if decision.Kind == rules.SteerReceiptBindingChanged {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorDeliveryUnknown, provider.RemoteEffectAccepted, decision.Reason, nil)
		}
		s.assignment = request.NewAssignment
		return nil
	})
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	receipt := provider.DeliveryReceipt{RequestID: request.RequestID, MessageID: request.MessageID, Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown, Targeting: provider.TargetSessionLease, DispatchedFor: provider.Some(request.NewAssignment), NativeReceiptID: provider.Some(result.ReceiptID), ObservedAt: s.config.Now()}
	return receipt, nil
}

// handleSteerDeliveryFailure ends the local reservation and records the steer
// as unresolved when the native prompt may have been sent. It never advances
// the assignment. If the turn already ended, native reconciliation starts in
// the background, because the turn-end trigger has already passed.
func (s *session) handleSteerDeliveryFailure(request provider.SteerRequest, messageID string, sent bool, cause error) error {
	turnEnded := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.steerPending = false
		if sent {
			s.unresolvedSteer = &unresolvedSteer{request: request, nativeMessageID: messageID}
			turnEnded = !s.turn.Present
		}
		return nil
	})
	if turnEnded {
		go s.reconcileUnresolvedSteer()
	}
	return adaptererror.Delivery(provider.OperationSteer, request.RequestID, sent, cause)
}

// handleSteerRejection releases a natively rejected prompt reservation.
func (s *session) handleSteerRejection() error {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.steerPending = false
		return nil
	})
	return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorDeliveryRejected, provider.RemoteEffectNotSent, "OpenCode rejected steer", nil)
}
