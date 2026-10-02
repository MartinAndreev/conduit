package opencode

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func steerTestSession(control Control) (*session, provider.SteerRequest) {
	a := provider.AssignmentRef{ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", Revision: 1}
	next := a
	next.Revision++
	s := &session{config: withDefaults(Config{Control: control}), ref: provider.SessionRef{AdapterSessionID: "session"}, nativeSession: "native", workspace: "/work", assignment: a, turn: provider.Some(provider.TurnRef{AdapterTurnID: "turn"}), broker: eventstream.New(16)}
	return s, provider.SteerRequest{Session: s.ref, ExpectedActiveTurn: s.turn.Value, CurrentAssignment: a, ExpectedRevision: 1, NewAssignment: next, Mode: provider.DeliverActiveTurn}
}

func TestSteerRuleRejectionsDoNotDispatch(t *testing.T) {
	cases := []struct {
		name   string
		change func(*session, *provider.SteerRequest)
	}{
		{"skipped revision", func(_ *session, r *provider.SteerRequest) { r.NewAssignment.Revision = 3 }},
		{"identity changed", func(_ *session, r *provider.SteerRequest) { r.NewAssignment.RunID = "other" }},
		{"stale current", func(_ *session, r *provider.SteerRequest) { r.CurrentAssignment.Revision = 0 }},
		{"stale expected", func(_ *session, r *provider.SteerRequest) { r.ExpectedRevision = 0 }},
		{"overflow", func(s *session, r *provider.SteerRequest) {
			s.assignment.Revision = ^uint64(0)
			r.CurrentAssignment = s.assignment
			r.ExpectedRevision = s.assignment.Revision
			r.NewAssignment = s.assignment
			r.NewAssignment.Revision = 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			control := &fakeControl{}
			s, r := steerTestSession(control)
			tc.change(s, &r)
			before := s.assignment
			_, err := s.Steer(context.Background(), r)
			var e *provider.ProviderError
			if !errors.As(err, &e) || e.Code != provider.ErrorStaleBinding || e.RemoteEffect != provider.RemoteEffectNotSent {
				t.Fatalf("rejection=%v", err)
			}
			if control.promptCalls != 0 || s.steerPending || s.assignment != before {
				t.Fatal("rejection dispatched, reserved or changed assignment")
			}
		})
	}
}

// A native accepted receipt arriving after the active lease changed must not
// attribute the correction to that new lease or advance its assignment.
func TestSteerAcceptedReceiptRechecksBinding(t *testing.T) {
	control := &blockingControl{entered: make(chan struct{}), release: make(chan struct{})}
	s, r := steerTestSession(control)
	done := make(chan error, 1)
	go func() { _, err := s.Steer(context.Background(), r); done <- err }()
	<-control.entered
	s.mu.Lock()
	s.turn = provider.Some(provider.TurnRef{AdapterTurnID: "replacement"})
	s.mu.Unlock()
	close(control.release)
	err := <-done
	var e *provider.ProviderError
	if !errors.As(err, &e) || e.Code != provider.ErrorDeliveryUnknown || e.RemoteEffect != provider.RemoteEffectAccepted {
		t.Fatalf("receipt error=%v", err)
	}
	if s.assignment != r.CurrentAssignment || s.steerPending {
		t.Fatal("stale accepted receipt advanced assignment or left pending")
	}
}

func TestSteerUnsentFailureReleasesReservation(t *testing.T) {
	control := &fakeControl{promptErr: context.Canceled, promptSent: false}
	s, r := steerTestSession(control)
	_, err := s.Steer(context.Background(), r)
	var e *provider.ProviderError
	if !errors.As(err, &e) || e.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("failure=%v", err)
	}
	if s.steerPending || s.deliveryUnknown || s.assignment != r.CurrentAssignment {
		t.Fatal("unsent failure left uncertainty or changed assignment")
	}
	control.promptErr = nil
	receipt, err := s.Steer(context.Background(), r)
	if err != nil || receipt.DispatchedFor.Value != r.NewAssignment || control.promptCalls != 2 {
		t.Fatalf("replacement attempt receipt=%#v err=%v", receipt, err)
	}
}

type rejectingSteerControl struct{ fakeControl }

func (c *rejectingSteerControl) Prompt(context.Context, PromptRequest) (PromptResult, bool, error) {
	c.promptCalls++
	return PromptResult{Accepted: false}, true, nil
}

func TestSteerNativeRejectionReleasesReservation(t *testing.T) {
	control := &rejectingSteerControl{}
	s, r := steerTestSession(control)
	_, err := s.Steer(context.Background(), r)
	var e *provider.ProviderError
	if !errors.As(err, &e) || e.Code != provider.ErrorDeliveryRejected || e.RemoteEffect != provider.RemoteEffectNotSent || e.Retry != provider.RetryNever || e.Cause != nil {
		t.Fatalf("rejection=%#v", e)
	}
	if s.steerPending || s.deliveryUnknown || s.assignment != r.CurrentAssignment || control.promptCalls != 1 {
		t.Fatal("native rejection left pending/uncertain state or changed assignment")
	}
	if !s.mu.TryLock() {
		t.Fatal("native rejection left mutex locked")
	}
	s.mu.Unlock()
}

func TestSteerTransportFailureRemainsUncertain(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	control := &fakeControl{promptSent: true, promptErr: cause}
	s, r := steerTestSession(control)
	_, err := s.Steer(context.Background(), r)
	var e *provider.ProviderError
	if !errors.As(err, &e) || e.Code != provider.ErrorDeliveryUnknown || e.RemoteEffect != provider.RemoteEffectPossiblySent || e.Retry != provider.RetryAfterReconcile || e.Cause != cause {
		t.Fatalf("failure=%#v", e)
	}
	if s.steerPending || !s.deliveryUnknown || s.assignment != r.CurrentAssignment {
		t.Fatal("transport failure lost uncertainty or changed assignment")
	}
	if !s.mu.TryLock() {
		t.Fatal("failure left mutex locked")
	}
	s.mu.Unlock()
	_, _ = s.Steer(context.Background(), r)
	if control.promptCalls != 1 {
		t.Fatal("uncertain prompt was retried")
	}
}
