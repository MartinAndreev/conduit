package opencode

import (
	"context"
	"errors"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestStartTurnRejectionsDoNotAllocateOrLaunch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*session, *provider.StartTurnRequest)
		code   provider.ErrorCode
		detail string
	}{
		{"binding precedence", func(s *session, r *provider.StartTurnRequest) {
			r.Session = provider.SessionRef{}
			s.closed = true
			r.ResultContract.Name = "changed"
		}, provider.ErrorStaleBinding, "session, turn, assignment, or revision binding is stale"},
		{"revision", func(s *session, r *provider.StartTurnRequest) { r.Assignment.Revision++ }, provider.ErrorStaleBinding, "session, turn, assignment, or revision binding is stale"},
		{"closed precedence", func(s *session, r *provider.StartTurnRequest) { s.closed = true; r.ResultContract.Name = "changed" }, provider.ErrorStaleBinding, "session closed or a turn is already active"},
		{"contract", func(s *session, r *provider.StartTurnRequest) { r.ResultContract.Name = "changed" }, provider.ErrorProtocolViolation, "result contract changed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := &fakeLauncher{}
			allocated := false
			s := &session{ref: provider.SessionRef{AdapterSessionID: "s"}, assignment: provider.AssignmentRef{AssignmentID: "a", Revision: 1}, config: Config{Launcher: launcher, NewID: func(string) (string, error) { allocated = true; return "id", nil }}}
			r := provider.StartTurnRequest{Session: s.ref, Assignment: s.assignment, ResultContract: s.contract.Contract}
			tt.mutate(s, &r)
			beforeAssignment, beforeState := s.assignment, s.state
			_, err := s.StartTurn(context.Background(), r)
			var pe *provider.ProviderError
			if !errors.As(err, &pe) || pe.Code != tt.code || pe.Detail != tt.detail || pe.Operation != provider.OperationStartTurn || pe.RemoteEffect != provider.RemoteEffectNotSent {
				t.Fatalf("error = %#v", err)
			}
			if allocated || launcher.starts != 0 || s.turn.Present || s.process != nil || s.assignment != beforeAssignment || s.state != beforeState {
				t.Fatal("rejection allocated, launched or mutated state")
			}
		})
	}
}
