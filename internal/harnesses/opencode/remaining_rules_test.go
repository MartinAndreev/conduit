package opencode

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestSteerTechnicalAndBusinessRejectionPrecedence(t *testing.T) {
	for _, stage := range []string{"lease", "correction", "mode"} {
		t.Run(stage, func(t *testing.T) {
			c := &fakeControl{}
			s, r := steerTestSession(c)
			r.Mode = "unsupported"
			if stage != "mode" {
				r.NewAssignment.Revision = 3
			}
			if stage == "lease" {
				r.Session = provider.SessionRef{AdapterSessionID: "wrong"}
			}
			_, err := s.Steer(context.Background(), r)
			var e *provider.ProviderError
			if !errors.As(err, &e) || e.Operation != provider.OperationSteer || e.RemoteEffect != provider.RemoteEffectNotSent || e.Retry != provider.RetryNever || e.Cause != nil {
				t.Fatalf("error=%#v", e)
			}
			want := provider.ErrorStaleBinding
			if stage == "mode" {
				want = provider.ErrorCapabilityUnsupported
			}
			if e.Code != want || (stage == "lease" && e.Detail != rules.StaleBindingReason) {
				t.Fatalf("precedence=%#v", e)
			}
			if c.promptCalls != 0 || s.steerPending || s.assignment != r.CurrentAssignment {
				t.Fatal("rejection changed state or dispatched")
			}
		})
	}
}

func TestCloseRuleAndRefusalLeaveHandleOpen(t *testing.T) {
	cases := []struct {
		name  string
		facts rules.CloseFacts
		want  rules.CloseKind
	}{
		{"stale precedes active", rules.CloseFacts{ActiveTurn: true}, rules.CloseBindingStale},
		{"active requires detach", rules.CloseFacts{SessionMatches: true, ActiveTurn: true}, rules.CloseActiveWorkRequiresDetach},
		{"detach", rules.CloseFacts{SessionMatches: true, ActiveTurn: true, ExplicitDetach: true}, rules.CloseAllowed},
		{"idle", rules.CloseFacts{SessionMatches: true}, rules.CloseAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := rules.DecideClose(tc.facts)
			s, _ := steerTestSession(&fakeControl{})
			if !tc.facts.ActiveTurn {
				s.turn = provider.None[provider.TurnRef]()
			}
			r := provider.CloseSessionRequest{Session: s.ref}
			if !tc.facts.SessionMatches {
				r.Session = provider.SessionRef{AdapterSessionID: "wrong"}
			}
			if tc.facts.ExplicitDetach {
				r.Policy = provider.CloseExplicitDetach
			}
			got, err := s.Close(context.Background(), r)
			if tc.want != rules.CloseAllowed {
				var e *provider.ProviderError
				if !errors.As(err, &e) || e.Detail != d.Reason || s.closed || s.state == provider.SessionClosed {
					t.Fatalf("refusal=%#v state=%s", e, s.state)
				}
				if _, err = s.broker.Subscribe(); err != nil {
					t.Fatalf("refusal finished broker: %v", err)
				}
			} else if err != nil || !got.HandleClosed || got.ExecutionState != provider.ExecutionUnknown || got.ResourcesReleased || !s.closed {
				t.Fatalf("close=%#v err=%v", got, err)
			}
		})
	}
}

func TestResultEnforcementRulesAndOpenRefusal(t *testing.T) {
	cases := []struct {
		name     string
		contract provider.ResultContractRef
		want     rules.ResultEnforcementKind
		code     provider.ErrorCode
	}{
		{"invalid precedes unsupported", provider.ResultContractRef{Schema: []byte("{"), Requested: provider.SchemaNative}, rules.ResultEnforcementInvalidSchema, provider.ErrorProtocolViolation},
		{"invalid precedes none", provider.ResultContractRef{Schema: []byte("{"), Requested: provider.SchemaNone}, rules.ResultEnforcementInvalidSchema, provider.ErrorProtocolViolation},
		{"none empty", provider.ResultContractRef{Requested: provider.SchemaNone}, rules.ResultEnforcementNone, ""},
		{"none valid", provider.ResultContractRef{Schema: []byte("{}"), Requested: provider.SchemaNone}, rules.ResultEnforcementNone, ""},
		{"unsupported", provider.ResultContractRef{Schema: []byte("{}"), Requested: provider.SchemaNative}, rules.ResultEnforcementUnsupported, provider.ErrorCapabilityUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := negotiate(tc.contract)
			if tc.want == rules.ResultEnforcementNone {
				if err != nil || actual != provider.SchemaNone {
					t.Fatalf("actual=%s err=%v", actual, err)
				}
				return
			}
			c := &fakeControl{}
			ids := 0
			a := New(Config{Control: c, NewID: func(string) (string, error) { ids++; return "id", nil }})
			_, err = a.OpenSession(context.Background(), provider.OpenSessionRequest{Model: provider.ModelSelection{ProviderID: "p", ModelID: "m"}, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, PermissionMode: provider.PermissionDeny, ResultContract: tc.contract})
			var e *provider.ProviderError
			if !errors.As(err, &e) || e.Code != tc.code || e.Operation != provider.OperationOpenSession || e.RemoteEffect != provider.RemoteEffectNotSent || e.Retry != provider.RetryNever || e.Cause != nil {
				t.Fatalf("error=%#v", e)
			}
			if c.create.Directory != "" || ids != 0 {
				t.Fatal("negotiation refusal allocated or created session")
			}
		})
	}

}

func TestAcceptSteerClearsPendingBeforeStaleReceiptRefusal(t *testing.T) {
	for _, name := range []string{"turn absent", "revision changed"} {
		t.Run(name, func(t *testing.T) {
			s, r := steerTestSession(&fakeControl{})
			s.steerPending = true
			if name == "turn absent" {
				s.turn = provider.None[provider.TurnRef]()
			} else {
				s.assignment.Revision++
			}
			before := s.assignment
			_, err := s.acceptSteer(r, PromptResult{Accepted: true, ReceiptID: "receipt"})
			var e *provider.ProviderError
			if !errors.As(err, &e) || e.Code != provider.ErrorDeliveryUnknown || e.Operation != provider.OperationSteer || e.RemoteEffect != provider.RemoteEffectAccepted || e.Retry != provider.RetryNever || e.Cause != nil {
				t.Fatalf("receipt error=%#v", e)
			}
			if s.steerPending || s.assignment != before || s.deliveryUnknown {
				t.Fatal("stale receipt changed assignment, uncertainty or pending release")
			}
		})
	}
}
