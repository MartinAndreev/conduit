package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestSteerForeignSessionDoesNotReserveOrDispatch(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	writer := &recordingWriteCloser{}
	s.rpc = newRPCClient(newRPCProcess(writer, io.NopCloser(&emptyReader{})))
	s.rpc.start(nil, nil)
	request := provider.SteerRequest{RequestID: "steer", Session: provider.SessionRef{AdapterSessionID: "other"}, ExpectedActiveTurn: s.active.Value, CurrentAssignment: s.assignment, ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn}
	_, err := s.Steer(context.Background(), request)
	assertProviderCode(t, err, provider.ErrorStaleBinding)
	if writer.count() != 0 || s.steerPending != nil || s.assignment != testAssignment(1) {
		t.Fatal("foreign session dispatched, reserved, or changed assignment")
	}
}

func TestCandidateAndEnvelopeShareSnapshotAcrossSteerTransition(t *testing.T) {
	for _, pendingAtSnapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "reserve after snapshot", true: "accept after unresolved snapshot"}[pendingAtSnapshot], func(t *testing.T) {
			s := normalizationSession(t)
			s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
			request := provider.SteerRequest{RequestID: "steer-1", Session: s.ref, ExpectedActiveTurn: s.active.Value, CurrentAssignment: s.assignment, ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn}
			if pendingAtSnapshot {
				if _, _, err := s.reserveSteer(request); err != nil {
					t.Fatal(err)
				}
			}
			snapshotted, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			originalNewID := s.config.NewID
			s.config.NewID = func(prefix string) (string, error) {
				if prefix == "codex-event" && calls.Add(1) == 2 {
					close(snapshotted)
					<-release
				}
				return originalNewID(prefix)
			}
			stream, err := s.broker.Subscribe()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.handleNotification("item/completed", notificationJSON("thread-1", "turn-1", map[string]any{"id": "message", "type": "agentMessage", "phase": "final_answer", "text": `{"status":"completed"}`}))
			}()
			// The second event ID allocation follows candidate body construction.
			// Previously emit reread session state after this allocation; the forced
			// transition below therefore exposes the original mixed-snapshot race.
			select {
			case <-snapshotted:
			case <-time.After(time.Second):
				t.Fatal("candidate did not reach publication boundary")
			}
			if pendingAtSnapshot {
				if committed := s.commitSteer(request, json.RawMessage(`{"turnId":"turn-1"}`)); committed.err != nil {
					close(release)
					t.Fatal(committed.err)
				}
			} else {
				if _, _, err := s.reserveSteer(request); err != nil {
					close(release)
					t.Fatal(err)
				}
			}
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("publication stayed blocked")
			}
			first, event := receiveEvent(t, stream), receiveEvent(t, stream)
			if first.Kind() != provider.EventAssistantText || event.Kind() != provider.EventResultCandidate || first.Envelope.AdapterSeq >= event.Envelope.AdapterSeq {
				t.Fatalf("event order first=%s second=%s", first.Kind(), event.Kind())
			}
			candidate := event.Body().(provider.ResultCandidateEvent).Candidate
			if candidate.Assignment != event.Envelope.Assignment {
				t.Fatalf("mixed attribution candidate=%#v envelope=%#v", candidate.Assignment, event.Envelope.Assignment)
			}
			if candidate.Assignment.Present == pendingAtSnapshot || (!pendingAtSnapshot && candidate.Assignment.Value != testAssignment(1)) {
				t.Fatalf("candidate lost snapshot attribution: %#v", candidate.Assignment)
			}
			if candidate.Native != event.Envelope.Native || candidate.Native.Session.Value != "thread-1" || candidate.Native.Turn.Value != "turn-1" || event.Envelope.Turn.Value != request.ExpectedActiveTurn {
				t.Fatalf("mixed native/turn correlation candidate=%#v envelope=%#v", candidate.Native, event.Envelope)
			}
			if candidate.Enforcement != provider.SchemaNative || candidate.Contract.Name != "result" {
				t.Fatal("candidate lost captured contract")
			}
		})
	}
}

func TestNativeRejectionPolicyMatchesStartAndSteer(t *testing.T) {
	for _, operation := range []provider.Operation{provider.OperationStartTurn, provider.OperationSteer} {
		t.Run(string(operation), func(t *testing.T) {
			s := normalizationSession(t)
			serverInput, clientInput := io.Pipe()
			clientOutput, serverOutput := io.Pipe()
			defer serverInput.Close()
			defer clientInput.Close()
			defer clientOutput.Close()
			defer serverOutput.Close()
			s.rpc = newRPCClient(newRPCProcess(clientInput, clientOutput))
			s.rpc.start(nil, nil)
			serverDone := make(chan error, 1)
			go func() {
				line, err := bufio.NewReader(serverInput).ReadBytes('\n')
				if err != nil {
					serverDone <- err
					return
				}
				var request struct {
					ID uint64 `json:"id"`
				}
				if err = json.Unmarshal(line, &request); err != nil {
					serverDone <- err
					return
				}
				_, err = serverOutput.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonNumber(request.ID) + `,"error":{"code":123,"message":"rejected"}}` + "\n"))
				serverDone <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			if operation == provider.OperationStartTurn {
				_, err = s.StartTurn(ctx, provider.StartTurnRequest{RequestID: "request-1", Session: s.ref, Assignment: s.assignment, ResultContract: s.contract.Contract})
			} else {
				s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
				_, err = s.Steer(ctx, provider.SteerRequest{RequestID: "request-1", Session: s.ref, ExpectedActiveTurn: s.active.Value, CurrentAssignment: s.assignment, ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn})
			}
			if serverErr := <-serverDone; serverErr != nil {
				t.Fatal(serverErr)
			}
			var e *provider.ProviderError
			var native *rpcError
			if !errors.As(err, &e) || e.Code != provider.ErrorDeliveryRejected || e.Retry != provider.RetryNever || e.RemoteEffect != provider.RemoteEffectNotSent || e.Operation != operation || e.RequestID != provider.Some(provider.RequestID("request-1")) || !errors.As(e.Cause, &native) {
				t.Fatalf("native rejection=%#v", e)
			}
			if s.turnPending || s.steerPending != nil || s.assignment != testAssignment(1) {
				t.Fatal("native rejection left pending state or changed assignment")
			}
		})
	}
}

func TestStartDeliveryFailureKeepsOnlyUncertainReservation(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsent", true: "possibly sent"}[sent], func(t *testing.T) {
			s := normalizationSession(t)
			s.turnPending = true
			r := provider.StartTurnRequest{RequestID: "start"}
			err := s.handleTurnStartDeliveryFailure(r, sent, context.DeadlineExceeded)
			var e *provider.ProviderError
			effect, retry := provider.RemoteEffectNotSent, provider.RetryNever
			if sent {
				effect, retry = provider.RemoteEffectPossiblySent, provider.RetryAfterReconcile
			}
			if !errors.As(err, &e) || e.Code != provider.ErrorDeadline || e.RemoteEffect != effect || e.Retry != retry || s.turnPending != sent {
				t.Fatalf("failure=%#v pending=%v", e, s.turnPending)
			}
		})
	}
}

func TestLateStartRejectionReleasesPendingButTransportDoesNot(t *testing.T) {
	for _, nativeRejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "native rejection"}[nativeRejected], func(t *testing.T) {
			s := normalizationSession(t)
			s.turnPending = true
			before := s.assignment
			cause := error(io.ErrUnexpectedEOF)
			if nativeRejected {
				cause = &rpcError{Code: 123, Message: "rejected"}
			}
			s.reconcileLateTurn(provider.StartTurnRequest{RequestID: "start"}, rpcResponse{err: cause}, turnStartCommit{})
			if s.turnPending == nativeRejected || s.assignment != before || s.active.Present {
				t.Fatal("late failure changed wrong pending/assignment/active state")
			}
		})
	}
}
