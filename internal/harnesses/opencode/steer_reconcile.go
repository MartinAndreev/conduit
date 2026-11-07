package opencode

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// steerQueryTimeout bounds the native delivery query of one unresolved steer.
const steerQueryTimeout = 10 * time.Second

// reconcileUnresolvedSteer resolves a steer whose delivery is unknown after
// its turn ended: decide whether to query, query native delivery outside the
// session mutex, then decide and apply the outcome against the state read
// again, and publish after unlocking. A failed query keeps the doubt; only an
// explicit Conduit release can then unblock the session.
func (s *session) reconcileUnresolvedSteer() {
	steer, query, ok := s.beginSteerReconcile()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), steerQueryTimeout)
	observation, queryErr := s.config.Control.QuerySteerDelivery(ctx, query)
	cancel()
	decision := s.applySteerReconcile(steer, observation, queryErr != nil)
	s.reportSteerReconcile(steer, decision)
	if s.steerReconcileApplied != nil {
		s.steerReconcileApplied(decision.Kind)
	}
}

// settleReleasedSteers runs after a turn ended while explicitly released
// steers may still run. Each released message is queried on its own, outside
// the session mutex; rules.DecideReleasedSteerSettled settles it only when
// OpenCode proves its work completed or that it was never admitted. Output
// stays unattributed until every released message is settled. A failed or
// inconclusive query is repeated at the next turn end.
func (s *session) settleReleasedSteers() {
	for _, pending := range s.beginReleasedSteerQueries() {
		ctx, cancel := context.WithTimeout(context.Background(), steerQueryTimeout)
		observation, queryErr := s.config.Control.QueryReleasedSteer(ctx, pending.query)
		cancel()
		s.applyReleasedSteerSettlement(pending.released, observation, queryErr != nil)
	}
}

type releasedSteerQuery struct {
	released *releasedSteer
	query    SteerDeliveryQuery
}

// beginReleasedSteerQueries marks every released steer the trigger rule
// allows (a turn has ended, no query for it is running) as being queried.
func (s *session) beginReleasedSteerQueries() []releasedSteerQuery {
	var queries []releasedSteerQuery
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		for _, released := range s.released {
			decision := rules.DecideSteerReconcileTrigger(rules.SteerReconcileTriggerFacts{
				SteerUnresolved: true,
				Reconciling:     released.querying,
				ActiveTurn:      s.turn.Present,
				Unavailable:     s.closed || s.state == provider.SessionLost,
			})
			if decision.Kind != rules.SteerReconcileStart {
				continue
			}
			released.querying = true
			queries = append(queries, releasedSteerQuery{
				released: released,
				query:    SteerDeliveryQuery{SessionID: s.nativeSession, Directory: s.workspace, MessageID: released.nativeMessageID},
			})
		}
		return nil
	})
	return queries
}

// applyReleasedSteerSettlement decides one released message's query outcome
// against current state, applies it in the same critical section and
// publishes after unlocking.
func (s *session) applyReleasedSteerSettlement(released *releasedSteer, observation ReleasedSteerObservation, queryFailed bool) {
	var decision rules.ReleasedSteerDecision
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		decision = rules.DecideReleasedSteerSettled(rules.ReleasedSteerFacts{
			SameReleased:     s.released[released.nativeMessageID] == released,
			QueryFailed:      queryFailed,
			InHistory:        observation.InHistory,
			IdleAfterMessage: observation.IdleAfterMessage,
			InInbox:          observation.InInbox,
		})
		switch decision.Kind {
		case rules.ReleasedSteerFinished:
			delete(s.released, released.nativeMessageID)
		case rules.ReleasedSteerMayRun:
			released.querying = false
		}
		return nil
	})
	detail := boundedtext.String(released.nativeMessageID+": "+decision.Reason, s.config.MaxTextBytes)
	switch decision.Kind {
	case rules.ReleasedSteerFinished:
		s.emit(provider.ProtocolWarningEvent{Code: "released_steer_settled", Detail: detail})
	case rules.ReleasedSteerMayRun:
		s.emit(provider.ProtocolWarningEvent{Code: "released_steer_pending", Detail: detail})
	}
}

// beginSteerReconcile marks the unresolved steer as being reconciled when
// DecideSteerReconcileTrigger allows it, so concurrent triggers query once.
func (s *session) beginSteerReconcile() (*unresolvedSteer, SteerDeliveryQuery, bool) {
	var steer *unresolvedSteer
	var query SteerDeliveryQuery
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideSteerReconcileTrigger(rules.SteerReconcileTriggerFacts{
			SteerUnresolved: s.unresolvedSteer != nil,
			Reconciling:     s.unresolvedSteer != nil && s.unresolvedSteer.reconciling,
			ActiveTurn:      s.turn.Present,
			Unavailable:     s.closed || s.state == provider.SessionLost,
		})
		if decision.Kind != rules.SteerReconcileStart {
			return nil
		}
		steer = s.unresolvedSteer
		steer.reconciling = true
		query = SteerDeliveryQuery{SessionID: s.nativeSession, Directory: s.workspace, MessageID: steer.nativeMessageID}
		return nil
	})
	return steer, query, steer != nil
}

// applySteerReconcile decides the query outcome against current state and
// applies it in the same critical section: delivered advances the assignment,
// not delivered releases the binding without advancing it, and a remaining
// doubt leaves the steer unresolved for an explicit release.
func (s *session) applySteerReconcile(steer *unresolvedSteer, observation SteerDeliveryObservation, queryFailed bool) rules.SteerReconcileDecision {
	var decision rules.SteerReconcileDecision
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		decision = rules.DecideSteerReconcile(rules.SteerReconcileFacts{
			SameSteerUnresolved: s.unresolvedSteer == steer,
			QueryFailed:         queryFailed,
			InHistory:           observation.InHistory,
			InInbox:             observation.InInbox,
			RevisionMatches:     s.assignment.Revision == steer.request.ExpectedRevision,
		})
		switch decision.Kind {
		case rules.SteerReconcileDelivered:
			s.assignment = steer.request.NewAssignment
			s.unresolvedSteer = nil
		case rules.SteerReconcileNotDelivered:
			s.unresolvedSteer = nil
		case rules.SteerReconcileDoubtRemains:
			steer.reconciling = false
		}
		return nil
	})
	return decision
}

// reportSteerReconcile publishes the outcome after the session mutex is
// released. Delivered uses the accepted-receipt semantics of Steer; not
// delivered is a rejected, receipt-less receipt naming no dispatched
// assignment.
func (s *session) reportSteerReconcile(steer *unresolvedSteer, decision rules.SteerReconcileDecision) {
	request := steer.request
	switch decision.Kind {
	case rules.SteerReconcileDelivered:
		s.emit(provider.ControlReceiptEvent{Receipt: provider.DeliveryReceipt{
			RequestID: request.RequestID, MessageID: request.MessageID,
			Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown,
			Targeting: provider.TargetSessionLease, DispatchedFor: provider.Some(request.NewAssignment),
			NativeReceiptID: provider.Some(provider.NativeReceiptID(steer.nativeMessageID)), ObservedAt: s.config.Now(),
		}})
	case rules.SteerReconcileNotDelivered:
		s.emit(provider.ProtocolWarningEvent{Code: "steer_not_delivered", Detail: provider.BoundedText{Text: decision.Reason}})
		s.emit(provider.ControlReceiptEvent{Receipt: provider.DeliveryReceipt{
			RequestID: request.RequestID, MessageID: request.MessageID,
			Disposition: provider.DeliveryRejected, Receipt: provider.ReceiptNone,
			Targeting: provider.TargetSessionLease, DispatchedFor: provider.None[provider.AssignmentRef](),
			ObservedAt: s.config.Now(),
		}})
	case rules.SteerReconcileDoubtRemains:
		s.emit(provider.ProtocolWarningEvent{Code: "steer_delivery_unknown", Detail: provider.BoundedText{Text: decision.Reason}})
	}
}

// newNativeMessageID allocates an OpenCode message ID before dispatch, in
// OpenCode's own shape: "msg_", 12 hex digits of the millisecond time shifted
// by 12 bits (so IDs sort by creation time), then 14 alphanumeric characters
// taken from the adapter's random ID source.
func (s *session) newNativeMessageID() (string, error) {
	entropy, err := s.config.NewID("opencode-message")
	if err != nil {
		return "", err
	}
	var suffix strings.Builder
	for _, r := range entropy {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			suffix.WriteRune(r)
		}
	}
	random := suffix.String()
	if len(random) > 14 {
		random = random[len(random)-14:]
	}
	random = strings.Repeat("0", 14-len(random)) + random
	stamp := uint64(s.config.Now().UnixMilli()) << 12 & (1<<48 - 1)
	hex := strconv.FormatUint(stamp, 16)
	return "msg_" + strings.Repeat("0", 12-len(hex)) + hex + random, nil
}
