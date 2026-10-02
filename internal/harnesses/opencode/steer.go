package opencode

import (
	"context"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func (s *session) Steer(ctx context.Context, request provider.SteerRequest) (provider.DeliveryReceipt, error) {
	native, err := s.reserveSteer(request)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	result, sent, err := s.deliverSteer(ctx, native, request.Parts)
	if err != nil {
		return provider.DeliveryReceipt{}, s.handleSteerDeliveryFailure(request, sent, err)
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
		if request.Session != s.ref || !s.turn.Present || request.ExpectedActiveTurn != s.turn.Value || s.steerPending || s.deliveryUnknown {
			return s.stale(provider.OperationSteer)
		}
		decision := sessions.DecideCorrection(s.assignment, request.CurrentAssignment, request.ExpectedRevision, request.NewAssignment)
		if decision.Kind != sessions.CorrectionAllowed {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, decision.Reason, nil)
		}
		if request.Mode != provider.DeliverActiveTurn {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "only active-turn steer is supported", nil)
		}
		s.steerPending = true
		native = s.nativeSession
		return nil
	})
	return native, err
}

// deliverSteer performs only the native session prompt effect.
func (s *session) deliverSteer(ctx context.Context, native provider.NativeSessionID, parts []provider.InputPart) (PromptResult, bool, error) {
	return s.config.Control.Prompt(ctx, PromptRequest{SessionID: native, Directory: s.workspace, Text: joinParts(parts), Delivery: "steer"})
}

// acceptSteer advances only a still-matching lease after transport acceptance.
func (s *session) acceptSteer(request provider.SteerRequest, result PromptResult) (provider.DeliveryReceipt, error) {
	err := lockscope.WithMutexLock(&s.mu, func() error {
		s.steerPending = false
		if !s.turn.Present || s.turn.Value != request.ExpectedActiveTurn || s.assignment.Revision != request.ExpectedRevision {
			return adaptererror.NewWithDetail(provider.OperationSteer, provider.ErrorDeliveryUnknown, provider.RemoteEffectAccepted, "binding changed after accepted delivery", nil)
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

// handleSteerDeliveryFailure ends the local reservation and records uncertainty
// when the native prompt may have been sent. It never advances the assignment.
func (s *session) handleSteerDeliveryFailure(request provider.SteerRequest, sent bool, cause error) error {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.steerPending = false
		if sent {
			s.deliveryUnknown = true
		}
		return nil
	})
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
