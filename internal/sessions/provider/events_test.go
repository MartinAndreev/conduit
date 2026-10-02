package provider_test

import (
	"errors"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestProviderEventDerivesKindFromClosedBody(t *testing.T) {
	body := provider.AssistantTextEvent{
		Phase: provider.AssistantCommentary,
		Update: provider.TextSegmentUpdate{
			ItemID:      "item-1",
			SegmentID:   "segment-1",
			Mode:        provider.UpdateDelta,
			BaseVersion: provider.Some(uint64(2)),
			Version:     3,
			ByteOffset:  4,
			Text:        "work",
		},
	}

	event, err := provider.NewProviderEvent(provider.EventEnvelope{}, body)
	if err != nil {
		t.Fatalf("NewProviderEvent returned error: %v", err)
	}
	if event.Kind() != provider.EventAssistantText {
		t.Fatalf("Kind() = %q, want %q", event.Kind(), provider.EventAssistantText)
	}
	if _, ok := event.Body().(provider.AssistantTextEvent); !ok {
		t.Fatalf("Body() type = %T, want provider.AssistantTextEvent", event.Body())
	}
}

func TestEveryEventBodyHasAStableKind(t *testing.T) {
	tests := []struct {
		body provider.EventBody
		kind provider.EventKind
	}{
		{provider.SessionStateEvent{}, provider.EventSessionState},
		{provider.TurnStateEvent{}, provider.EventTurnState},
		{provider.AssistantTextEvent{}, provider.EventAssistantText},
		{provider.ReasoningSummaryEvent{}, provider.EventReasoningSummary},
		{provider.ToolStartedEvent{}, provider.EventToolStarted},
		{provider.ToolUpdatedEvent{}, provider.EventToolUpdated},
		{provider.ToolCompletedEvent{}, provider.EventToolCompleted},
		{provider.CommandStartedEvent{}, provider.EventCommandStarted},
		{provider.CommandOutputEvent{}, provider.EventCommandOutput},
		{provider.CommandCompletedEvent{}, provider.EventCommandCompleted},
		{provider.FileActivityEvent{}, provider.EventFileActivity},
		{provider.PermissionRequestedEvent{}, provider.EventPermissionRequested},
		{provider.QuestionAskedEvent{}, provider.EventQuestionAsked},
		{provider.ControlReceiptEvent{}, provider.EventControlReceipt},
		{provider.ResultCandidateEvent{}, provider.EventResultCandidate},
		{provider.UsageEvent{}, provider.EventUsage},
		{provider.EventGapEvent{}, provider.EventGap},
		{provider.ProtocolWarningEvent{}, provider.EventProtocolWarning},
	}

	for _, test := range tests {
		event, err := provider.NewProviderEvent(provider.EventEnvelope{}, test.body)
		if err != nil {
			t.Fatalf("NewProviderEvent(%T) returned error: %v", test.body, err)
		}
		if event.Kind() != test.kind {
			t.Errorf("NewProviderEvent(%T).Kind() = %q, want %q", test.body, event.Kind(), test.kind)
		}
	}
}

func TestProviderEventRejectsPointerAndTypedNilBodies(t *testing.T) {
	for name, body := range map[string]provider.EventBody{
		"pointer":   &provider.AssistantTextEvent{},
		"typed nil": (*provider.AssistantTextEvent)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := provider.NewProviderEvent(provider.EventEnvelope{}, body)
			if !errors.Is(err, provider.ErrInvalidEventBody) {
				t.Fatalf("error = %v, want ErrInvalidEventBody", err)
			}
		})
	}
}

func TestOptionalDistinguishesUnknownFromKnownZero(t *testing.T) {
	unknown := provider.None[uint64]()
	zero := provider.Some(uint64(0))
	if unknown.Present {
		t.Fatal("None value reported present")
	}
	if !zero.Present || zero.Value != 0 {
		t.Fatalf("Some(0) = %+v, want present zero", zero)
	}
}

func TestTextUpdatesRepresentTruncationAndOriginalLengthKnowledge(t *testing.T) {
	assistant := provider.AssistantTextEvent{Update: provider.TextSegmentUpdate{
		Truncated:     true,
		OriginalBytes: provider.Some(uint64(12)),
	}}
	reasoning := provider.ReasoningSummaryEvent{Update: provider.TextSegmentUpdate{
		Truncated:     true,
		OriginalBytes: provider.None[uint64](),
	}}
	emptyKnown := provider.AssistantTextEvent{Update: provider.TextSegmentUpdate{
		OriginalBytes: provider.Some(uint64(0)),
	}}

	if !assistant.Update.Truncated || !assistant.Update.OriginalBytes.Present {
		t.Fatalf("assistant truncation metadata = %+v", assistant.Update)
	}
	if !reasoning.Update.Truncated || reasoning.Update.OriginalBytes.Present {
		t.Fatalf("reasoning truncation metadata = %+v", reasoning.Update)
	}
	if !emptyKnown.Update.OriginalBytes.Present || emptyKnown.Update.OriginalBytes.Value != 0 {
		t.Fatalf("known empty original length = %+v", emptyKnown.Update.OriginalBytes)
	}
}

func TestUsageObservationDistinguishesDeltaFromSnapshot(t *testing.T) {
	delta := provider.UsageEvent{
		Scope:       provider.UsagePrimaryTurn,
		ScopeID:     "turn-1",
		Measurement: provider.UsageDelta,
	}
	snapshot := delta
	snapshot.Measurement = provider.UsageCumulativeSnapshot
	if delta.Measurement == snapshot.Measurement {
		t.Fatal("usage delta and cumulative snapshot are indistinguishable")
	}
}

func TestCandidateCanRemainUnattributed(t *testing.T) {
	candidate := provider.ResultCandidate{
		Assignment: provider.None[provider.AssignmentRef](),
		Source:     provider.CandidateAssistantFinal,
	}
	if candidate.Assignment.Present {
		t.Fatal("unattributed candidate unexpectedly has an assignment")
	}
}

func TestResultContractAllowsOnlyKnownExplicitEnforcement(t *testing.T) {
	unknown := provider.SchemaEnforcement("future_mode")
	tests := []struct {
		name     string
		contract provider.ResultContractRef
		actual   provider.SchemaEnforcement
		want     bool
	}{
		{
			name:     "zero value is not negotiated",
			contract: provider.ResultContractRef{},
			actual:   "",
		},
		{
			name:     "unknown requested mode is rejected",
			contract: provider.ResultContractRef{Requested: unknown},
			actual:   unknown,
		},
		{
			name: "unknown fallback is rejected",
			contract: provider.ResultContractRef{
				Requested:        provider.SchemaNative,
				AllowedFallbacks: []provider.SchemaEnforcement{unknown},
			},
			actual: unknown,
		},
		{
			name:     "requested native is accepted",
			contract: provider.ResultContractRef{Requested: provider.SchemaNative},
			actual:   provider.SchemaNative,
			want:     true,
		},
		{
			name: "explicit prompt fallback is accepted",
			contract: provider.ResultContractRef{
				Requested:        provider.SchemaNative,
				AllowedFallbacks: []provider.SchemaEnforcement{provider.SchemaPromptOnly},
			},
			actual: provider.SchemaPromptOnly,
			want:   true,
		},
		{
			name: "explicit none fallback is accepted",
			contract: provider.ResultContractRef{
				Requested:        provider.SchemaNative,
				AllowedFallbacks: []provider.SchemaEnforcement{provider.SchemaNone},
			},
			actual: provider.SchemaNone,
			want:   true,
		},
		{
			name:     "unlisted known fallback is rejected",
			contract: provider.ResultContractRef{Requested: provider.SchemaNative},
			actual:   provider.SchemaNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.contract.Allows(test.actual); got != test.want {
				t.Fatalf("Allows(%q) = %v, want %v", test.actual, got, test.want)
			}
		})
	}
}
