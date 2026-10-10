package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestSteerRuleRejectionsDoNotDispatch(t *testing.T) {
	cases := []struct {
		name   string
		change func(*session, *provider.SteerRequest)
	}{
		{"skipped revision", func(_ *session, r *provider.SteerRequest) { r.NewAssignment.Revision = 3 }},
		{"identity changed", func(_ *session, r *provider.SteerRequest) { r.NewAssignment.AttemptID = "other" }},
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
			s := normalizationSession(t)
			s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
			r := provider.SteerRequest{RequestID: "steer", Session: s.ref, ExpectedActiveTurn: s.active.Value, CurrentAssignment: s.assignment, ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn}
			tc.change(s, &r)
			before := s.assignment
			_, err := s.Steer(context.Background(), r)
			assertProviderCode(t, err, provider.ErrorStaleBinding)
			if s.steerPending != nil || s.assignment != before {
				t.Fatal("rejection reserved or changed assignment")
			}
		})
	}
}

func TestSteerReservationUncertaintyAndLateReceipt(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	defer serverInput.Close()
	defer clientInput.Close()
	defer clientOutput.Close()
	defer serverOutput.Close()
	s.rpc = newRPCClient(newRPCProcess(clientInput, clientOutput))
	s.rpc.start(nil, nil)
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	request := provider.SteerRequest{RequestID: "steer-1", MessageID: "message-1", Session: s.ref, ExpectedActiveTurn: s.active.Value, CurrentAssignment: s.assignment, ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Steer(ctx, request); done <- err }()
	// Receiving the request proves the reservation exists and delivery is outside
	// the session lock. A concurrent correction must reject before another send.
	line, err := bufio.NewReader(serverInput).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var rpcRequest struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(line, &rpcRequest); err != nil {
		t.Fatal(err)
	}
	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	_, err = s.Steer(secondCtx, request)
	assertProviderCode(t, err, provider.ErrorStaleBinding)
	cancel()
	select {
	case err = <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation remained blocked")
	}
	assertProviderCode(t, err, provider.ErrorCallerCancelled)
	s.mu.Lock()
	pending, assignment := s.steerPending, s.assignment
	s.mu.Unlock()
	if pending == nil || assignment != testAssignment(1) {
		t.Fatal("uncertain delivery lost its reservation or advanced assignment")
	}
	_, err = s.Steer(context.Background(), request)
	assertProviderCode(t, err, provider.ErrorStaleBinding)
	if _, err := serverOutput.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonNumber(rpcRequest.ID) + `,"result":{"turnId":"turn-1"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	eventCtx, eventCancel := context.WithTimeout(context.Background(), time.Second)
	defer eventCancel()
	event, err := stream.Receive(eventCtx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, ok := event.Body().(provider.ControlReceiptEvent)
	if !ok || receipt.Receipt.DispatchedFor.Value != testAssignment(2) || receipt.Receipt.Disposition != provider.DeliveryTransportAccepted {
		t.Fatalf("late event=%#v", event.Body())
	}
	s.mu.Lock()
	pending, assignment = s.steerPending, s.assignment
	s.mu.Unlock()
	if pending != nil || assignment != testAssignment(2) {
		t.Fatal("correlated late reply failed to reconcile assignment")
	}
}

func TestSteerPreCancelledDoesNotReserve(t *testing.T) {
	s := normalizationSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Steer(ctx, provider.SteerRequest{})
	assertProviderCode(t, err, provider.ErrorCallerCancelled)
	if s.steerPending != nil {
		t.Fatal("cancelled steer reserved operation")
	}
}

func TestSteerDeliveryFailurePreservesEffectAndReservation(t *testing.T) {
	native := &rpcError{Code: 123, Message: "rejected"}
	transport := io.ErrUnexpectedEOF
	cases := []struct {
		name    string
		sent    bool
		cause   error
		code    provider.ErrorCode
		effect  provider.RemoteEffect
		retry   provider.RetryClass
		pending bool
	}{
		{"native rejected", true, native, provider.ErrorDeliveryRejected, provider.RemoteEffectNotSent, provider.RetryNever, false},
		{"proven unsent", false, transport, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, provider.RetryNever, false},
		{"transport uncertain", true, transport, provider.ErrorDeliveryUnknown, provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile, true},
		{"cancelled wait uncertain", true, context.Canceled, provider.ErrorCallerCancelled, provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := normalizationSession(t)
			r := provider.SteerRequest{RequestID: "steer-failure"}
			s.steerPending = &pendingSteer{request: r}
			before := s.assignment
			err := s.handleSteerDeliveryFailure(r, tc.sent, tc.cause)
			var e *provider.ProviderError
			if !errors.As(err, &e) || e.Code != tc.code || e.RemoteEffect != tc.effect || e.Retry != tc.retry || e.Cause != tc.cause || e.RequestID != provider.Some(r.RequestID) {
				t.Fatalf("failure=%#v", e)
			}
			if (s.steerPending != nil) != tc.pending || s.assignment != before {
				t.Fatal("failure changed reservation/assignment incorrectly")
			}
			if !s.mu.TryLock() {
				t.Fatal("failure left mutex locked")
			}
			s.mu.Unlock()
		})
	}
}

func TestLateSteerRejectionClearsOnlyMatchingReservation(t *testing.T) {
	for _, matching := range []bool{true, false} {
		t.Run(fmt.Sprint(matching), func(t *testing.T) {
			s := normalizationSession(t)
			r := provider.SteerRequest{RequestID: "late"}
			pendingRequest := r
			if !matching {
				pendingRequest.RequestID = "other"
			}
			s.steerPending = &pendingSteer{request: pendingRequest}
			before := s.assignment
			s.reconcileLateSteer(r, rpcResponse{err: &rpcError{Code: 123, Message: "rejected"}}, steerCommit{})
			if (s.steerPending == nil) != matching || s.assignment != before {
				t.Fatal("late rejection cleared unrelated reservation or changed assignment")
			}
			stream, err := s.broker.Subscribe()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			event, err := stream.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			warning, ok := event.Body().(provider.ProtocolWarningEvent)
			if !ok || warning.Code != "late_steer_rejected" {
				t.Fatalf("warning=%#v", event.Body())
			}
		})
	}
}

func TestLateSteerTransportFailureKeepsUncertainty(t *testing.T) {
	s := normalizationSession(t)
	r := provider.SteerRequest{RequestID: "late"}
	pending := &pendingSteer{request: r}
	s.steerPending = pending
	before := s.assignment
	s.reconcileLateSteer(r, rpcResponse{err: io.ErrUnexpectedEOF}, steerCommit{})
	if s.steerPending != pending || s.assignment != before {
		t.Fatal("late transport failure cleared uncertainty or changed assignment")
	}
}
