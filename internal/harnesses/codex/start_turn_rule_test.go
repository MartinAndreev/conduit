package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestStartTurnRejectionsDoNotReserveOrDispatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*session, *provider.StartTurnRequest)
		code   provider.ErrorCode
		detail string
	}{
		{"closing precedence", func(s *session, r *provider.StartTurnRequest) {
			s.closing = true
			s.active = provider.Some(provider.TurnRef{})
			r.Session = provider.SessionRef{}
		}, provider.ErrorSessionLost, "session is closing"},
		{"occupied precedence", func(s *session, r *provider.StartTurnRequest) {
			s.active = provider.Some(provider.TurnRef{})
			r.Session = provider.SessionRef{}
		}, provider.ErrorStaleBinding, "session already has an active or unresolved turn"},
		{"wrong session", func(s *session, r *provider.StartTurnRequest) { r.Session = provider.SessionRef{} }, provider.ErrorStaleBinding, "session or assignment binding differs"},
		{"wrong identity", func(s *session, r *provider.StartTurnRequest) { r.Assignment.AssignmentID = "different" }, provider.ErrorStaleBinding, "session or assignment binding differs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := normalizationSession(t) // No RPC client: any dispatch would panic.
			r := provider.StartTurnRequest{Session: s.ref, Assignment: s.assignment, ResultContract: s.contract.Contract}
			tt.mutate(s, &r)
			beforeAssignment, beforeActive := s.assignment, s.active
			_, err := s.StartTurn(context.Background(), r)
			var pe *provider.ProviderError
			if !errors.As(err, &pe) || pe.Code != tt.code || pe.Detail != tt.detail || pe.Operation != provider.OperationStartTurn || pe.RemoteEffect != provider.RemoteEffectNotSent {
				t.Fatalf("error = %#v", err)
			}
			if s.turnPending || s.assignment != beforeAssignment || s.active != beforeActive {
				t.Fatal("rejection mutated reservation or binding")
			}
		})
	}
}

func TestStartTurnAllowsChangedRevisionWithSameIdentity(t *testing.T) {
	s := normalizationSession(t)
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	defer serverInput.Close()
	defer clientInput.Close()
	defer clientOutput.Close()
	defer serverOutput.Close()
	s.rpc = newRPCClient(newRPCProcess(clientInput, clientOutput))
	s.rpc.start(nil, nil)
	done := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(serverInput).ReadBytes('\n')
		if err != nil {
			done <- err
			return
		}
		var request struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		if err = json.Unmarshal(line, &request); err != nil {
			done <- err
			return
		}
		if request.Method != "turn/start" {
			done <- errors.New("unexpected native method")
			return
		}
		_, err = serverOutput.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonNumber(request.ID) + `,"result":{"turn":{"id":"native-turn"}}}` + "\n"))
		done <- err
	}()
	assignment := s.assignment
	assignment.Revision++
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := s.StartTurn(ctx, provider.StartTurnRequest{Session: s.ref, Assignment: assignment, ResultContract: s.contract.Contract})
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if result.Assignment != assignment || s.assignment != assignment || !s.active.Present || s.turnPending {
		t.Fatalf("allowed start did not accept binding: result=%+v", result)
	}
}
