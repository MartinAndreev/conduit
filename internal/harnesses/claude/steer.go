package claude

import (
	"context"

	"github.com/MartinAndreev/conduit/internal/harnesses/claude/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Steer dispatches a `priority:"now"` user message into the active turn. The
// returned receipt proves only the local write. Native acceptance (replay echo,
// command_lifecycle started, or a result naming the UUID) later publishes a
// ControlReceiptEvent and advances the assignment; until then attribution is
// absent. Neither proves that the agent applied the correction.
func (s *session) Steer(ctx context.Context, request provider.SteerRequest) (provider.DeliveryReceipt, error) {
	if err := ctx.Err(); err != nil {
		return provider.DeliveryReceipt{}, adaptererror.Delivery(provider.OperationSteer, request.RequestID, false, err)
	}
	content, err := messageContent(provider.OperationSteer, request.Parts)
	if err != nil {
		return provider.DeliveryReceipt{}, err
	}
	uuid, err := s.config.NewUUID()
	if err != nil {
		return provider.DeliveryReceipt{}, adaptererror.New(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate native message UUID")
	}
	steer := &pendingSteer{request: request, uuid: uuid, dispatch: rules.SteerReserved}
	if err := s.reserveSteer(request, steer); err != nil {
		return provider.DeliveryReceipt{}, err
	}
	if s.afterSteerReserved != nil {
		s.afterSteerReserved()
	}
	if err := s.claimSteerDispatch(steer); err != nil {
		return provider.DeliveryReceipt{}, err
	}
	sent, err := s.transport.send(ctx, s.userMessage(content, uuid, "now"))
	if err != nil {
		return provider.DeliveryReceipt{}, s.handleSteerDispatchFailure(request, steer, sent, err)
	}
	s.markSteerWritten(steer)
	return provider.DeliveryReceipt{
		RequestID: request.RequestID, MessageID: request.MessageID,
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNone,
		Targeting: provider.TargetSessionLease, DispatchedFor: provider.Some(request.NewAssignment),
		ObservedAt: s.config.Now(),
	}, nil
}

// reserveSteer checks adapter facts and the domain correction rule, then adds
// the UUID to the active turn's input set, all in one critical section and
// before dispatch so the reader can correlate an immediate echo.
func (s *session) reserveSteer(request provider.SteerRequest, steer *pendingSteer) error {
	return lockscope.WithMutexLock(&s.mu, func() error {
		technical := rules.DecideSteer(rules.SteerFacts{
			SessionMatches:    request.Session == s.ref,
			ActiveTurnMode:    request.Mode == provider.DeliverActiveTurn,
			ActiveTurnMatches: s.turn != nil && s.turn.ref == request.ExpectedActiveTurn,
			TurnEnding:        s.turn != nil && s.turn.ending,
			SteerUnresolved:   s.steer != nil,
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
		s.steer = steer
		s.turn.inputs[steer.uuid] = struct{}{}
		return nil
	})
}

// claimSteerDispatch moves a reserved steer to dispatching, unless the turn's
// terminal result already withdrew it: then nothing is sent and the caller
// gets stale_binding with RemoteEffectNotSent. Withdrawal and the claim are
// decided under the session mutex, so exactly one of them wins.
func (s *session) claimSteerDispatch(steer *pendingSteer) error {
	return lockscope.WithMutexLock(&s.mu, func() error {
		if steer.withdrawn {
			return adaptererror.New(provider.OperationSteer, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, "the turn ended before the correction was sent; it was withdrawn")
		}
		steer.dispatch = rules.SteerDispatching
		return nil
	})
}

// markSteerWritten records that the steer's write completed. The terminal
// result reader judges "not observed" only for a steer marked written before
// it read the result.
func (s *session) markSteerWritten(steer *pendingSteer) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		steer.dispatch = rules.SteerWritten
		return nil
	})
}

// handleSteerDispatchFailure releases a reservation whose message never reached
// the writer. A possibly sent correction stays pending for native evidence.
func (s *session) handleSteerDispatchFailure(request provider.SteerRequest, steer *pendingSteer, sent bool, cause error) error {
	if !sent {
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.steer == steer {
				s.steer = nil
			}
			if s.turn != nil {
				delete(s.turn.inputs, steer.uuid)
			}
			return nil
		})
	}
	return adaptererror.Delivery(provider.OperationSteer, request.RequestID, sent, cause)
}

// steerAcceptance is a correction resolved by native delivery evidence.
type steerAcceptance struct {
	request provider.SteerRequest
	uuid    string
}

// observeDeliveryEvidence handles native proof that claude received the input
// with this UUID: the first evidence for the turn's own message activates the
// turn; evidence for the pending correction advances the assignment. Events
// publish after the session mutex is released.
func (s *session) observeDeliveryEvidence(uuid string) {
	activated := false
	var accepted *steerAcceptance
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.turn != nil && s.turn.inputUUID == uuid && !s.turn.delivered {
			s.turn.delivered = true
			activated = true
		}
		if s.steer != nil && s.steer.uuid == uuid {
			accepted = &steerAcceptance{request: s.steer.request, uuid: uuid}
			s.assignment = s.steer.request.NewAssignment
			s.steer = nil
		}
		return nil
	})
	if activated {
		s.emit(provider.TurnStateEvent{Previous: provider.TurnStarting, Current: provider.TurnActive})
	}
	if accepted != nil {
		s.emit(provider.ControlReceiptEvent{Receipt: provider.DeliveryReceipt{
			RequestID: accepted.request.RequestID, MessageID: accepted.request.MessageID,
			Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown,
			Targeting: provider.TargetSessionLease, DispatchedFor: provider.Some(accepted.request.NewAssignment),
			NativeReceiptID: provider.Some(provider.NativeReceiptID(accepted.uuid)), ObservedAt: s.config.Now(),
		}})
	}
}

// observeSteerLifecycle records that claude reported a command lifecycle for
// the unresolved correction. It is not delivery evidence by itself.
// observeInputStarted records the input claude started processing (lifecycle
// started or replay echo); output that follows belongs to it.
func (s *session) observeInputStarted(uuid string) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.activeInput = uuid
		return nil
	})
}

// observeInputsFinished forgets released steers that a result named: once
// claude reported their result, they can no longer produce output.
func (s *session) observeInputsFinished(uuids []string) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		for _, uuid := range uuids {
			delete(s.released, uuid)
		}
		return nil
	})
}

func (s *session) observeSteerLifecycle(uuid string) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.steer != nil && s.steer.uuid == uuid {
			s.steer.lifecycleObserved = true
		}
		return nil
	})
}
