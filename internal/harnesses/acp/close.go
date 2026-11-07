package acp

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Close ends the owned agent: session/cancel first when the policy allows
// closing over an unresolved prompt, then session/close when advertised, stdin
// close, StopGrace, group kill and bounded reap. One cleanup owner runs the
// sequence; the caller's context bounds only its wait for the result.
func (s *session) Close(ctx context.Context, request provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.CloseSessionResult{}, adaptererror.Delivery(provider.OperationCloseSession, request.RequestID, false, err)
	}
	decision, previous, native, err := s.beginClose(request)
	if err != nil {
		return provider.CloseSessionResult{}, err
	}
	s.emit(provider.SessionStateEvent{Previous: previous, Current: provider.SessionClosing})
	cancelUnresolved := decision.Kind == rules.CloseCancelThenClose
	go s.shutdown(native, cancelUnresolved)
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

// beginClose decides eligibility and marks the session closing atomically.
func (s *session) beginClose(request provider.CloseSessionRequest) (rules.CloseDecision, provider.SessionState, string, error) {
	var decision rules.CloseDecision
	var previous provider.SessionState
	var native string
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision = rules.DecideClose(rules.CloseFacts{
			SessionMatches:     request.Session == s.ref,
			Closing:            s.closing,
			ActiveWork:         s.turn != nil,
			RequireQuiescent:   request.Policy == provider.CloseRequireQuiescent,
			ExplicitDetach:     request.Policy == provider.CloseExplicitDetach,
			InterruptThenClose: request.Policy == provider.CloseInterruptThenClose,
		})
		switch decision.Kind {
		case rules.CloseBindingStale, rules.CloseWorkUnresolved, rules.CloseUnresolvedNoPolicy:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.CloseAlreadyClosing:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.CloseDetachUnsupported:
			return adaptererror.New(provider.OperationCloseSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		s.closing = true
		previous = s.state
		s.state = provider.SessionClosing
		native = s.nativeSession
		return nil
	})
	return decision, previous, native, err
}

// shutdown is the single cleanup owner of the agent process. Every wait is
// bounded by StopGrace; the result says whether quiescence was proven.
func (s *session) shutdown(native string, cancelUnresolved bool) {
	grace := s.config.StopGrace
	if cancelUnresolved {
		s.requestNativeCancel(native, grace)
	}
	if s.closeAdvertised() {
		s.requestNativeClose(native, grace)
	}
	_ = s.rpc.closeInput()
	reaped := s.waitExit(grace)

	termination := rules.DecideOwnedProcessTermination(rules.OwnedProcessTerminationFacts{
		RootReaped: reaped, TreeKillSupported: s.process.TreeKillSupported(),
	})
	killAttempted := termination.Kind == rules.OwnedProcessKillRequired
	var killErr error
	if killAttempted {
		killErr = s.process.KillTree()
	}
	if !reaped {
		reaped = s.waitExit(grace)
	}
	s.waitTransportStopped(grace)

	treeKill := s.process.TreeKillSupported()
	quiescence := rules.DecideQuiescence(rules.QuiescenceFacts{
		RootReaped: reaped, TreeKillSupported: treeKill, KillAttempted: killAttempted,
		KillConfirmed: killErr == nil || errors.Is(killErr, os.ErrProcessDone),
	})
	s.completeClose(quiescence)
}

func (s *session) closeAdvertised() bool {
	var advertised bool
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		advertised = s.closeSupported
		return nil
	})
	return advertised && !s.rpc.stopped()
}

// requestNativeCancel is best effort: a failure is visible in the close
// outcome only through unproven quiescence.
func (s *session) requestNativeCancel(native string, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	_, _ = s.sendCancel(ctx, native)
}

// requestNativeClose waits at most grace for the session/close reply. A
// rejection or timeout does not stop the remaining owned-process cleanup.
func (s *session) requestNativeClose(native string, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	_, _, err := s.rpc.call(ctx, "session/close", struct {
		SessionID string `json:"sessionId"`
	}{SessionID: native})
	if err != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "native_close_failed", Detail: provider.BoundedText{Text: "session/close did not complete; owned process is terminated"}, OffendingNativeKind: "session/close"})
	}
}

func (s *session) waitExit(bound time.Duration) bool {
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-s.exitDone:
		return true
	case <-timer.C:
		return false
	}
}

// waitTransportStopped lets the reader publish the outcome of a prompt that
// was failed by the process exit before the stream is finished.
func (s *session) waitTransportStopped(bound time.Duration) {
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-s.rpc.finished:
	case <-timer.C:
	}
}

// completeClose publishes the close result once and finishes the stream.
func (s *session) completeClose(quiescence rules.QuiescenceDecision) {
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
	close(s.closeDone)
}
