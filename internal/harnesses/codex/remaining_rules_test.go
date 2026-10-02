package codex

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestCloseEligibilityPrecedenceAndNoMutation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*session, *provider.CloseSessionRequest)
		code   provider.ErrorCode
	}{
		{"foreign session before closing", func(s *session, r *provider.CloseSessionRequest) { s.closing = true; r.Session = provider.SessionRef{} }, provider.ErrorStaleBinding},
		{"closing before active work", func(s *session, r *provider.CloseSessionRequest) {
			s.closing = true
			r.Policy = provider.CloseRequireQuiescent
		}, provider.ErrorSessionLost},
		{"unresolved work", func(s *session, r *provider.CloseSessionRequest) { r.Policy = provider.CloseRequireQuiescent }, provider.ErrorStaleBinding},
		{"owned process cannot detach", func(s *session, r *provider.CloseSessionRequest) { r.Policy = provider.CloseExplicitDetach }, provider.ErrorCapabilityUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := normalizationSession(t)
			s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
			r := provider.CloseSessionRequest{Session: s.ref}
			tc.change(s, &r)
			state, closing, active := s.state, s.closing, s.active
			d := rules.DecideClose(rules.CloseFacts{SessionMatches: r.Session == s.ref, Closing: s.closing, RequireQuiescent: r.Policy == provider.CloseRequireQuiescent, ActiveOrUnresolvedWork: s.active.Present, ExplicitDetach: r.Policy == provider.CloseExplicitDetach})
			_, err := s.Close(context.Background(), r)
			var pe *provider.ProviderError
			if !errors.As(err, &pe) || pe.Code != tc.code || pe.Operation != provider.OperationCloseSession || pe.RemoteEffect != provider.RemoteEffectNotSent || pe.Detail != d.Reason {
				t.Fatalf("error %#v", err)
			}
			if s.state != state || s.closing != closing || s.active != active {
				t.Fatal("rejected close mutated session")
			}
		})
	}
}

func TestResultNegotiationPreservesNativeValidationBeforeFallback(t *testing.T) {
	cases := []struct {
		name     string
		contract provider.ResultContractRef
		actual   provider.SchemaEnforcement
		code     provider.ErrorCode
	}{
		{"native valid", provider.ResultContractRef{Schema: json.RawMessage(`{}`), Requested: provider.SchemaNative}, provider.SchemaNative, ""},
		{"native malformed with none fallback", provider.ResultContractRef{Schema: json.RawMessage(`{`), Requested: provider.SchemaNative, AllowedFallbacks: []provider.SchemaEnforcement{provider.SchemaNone}}, "", provider.ErrorProtocolViolation},
		{"none ignores malformed schema", provider.ResultContractRef{Schema: json.RawMessage(`{`), Requested: provider.SchemaNone}, provider.SchemaNone, ""},
		{"empty schema falls back", provider.ResultContractRef{Requested: provider.SchemaNative, AllowedFallbacks: []provider.SchemaEnforcement{provider.SchemaNone}}, provider.SchemaNone, ""},
		{"native requires schema", provider.ResultContractRef{Requested: provider.SchemaNative}, "", provider.ErrorCapabilityUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := negotiateResultContract(tc.contract)
			if actual != tc.actual {
				t.Fatalf("actual %q", actual)
			}
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var pe *provider.ProviderError
			if !errors.As(err, &pe) || pe.Code != tc.code || pe.Operation != provider.OperationOpenSession || pe.Retry != provider.RetryNever || pe.RemoteEffect != provider.RemoteEffectNotSent {
				t.Fatalf("error %#v", err)
			}
		})
	}
}
