package execution

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

var observedAt = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func startResult() provider.StartTurnResult {
	return provider.StartTurnResult{
		Turn:              provider.TurnRef{AdapterTurnID: "turn-a"},
		Assignment:        assignment(3),
		TargetingStrength: provider.TargetNativeTurnID,
		ResultEnforcement: provider.SchemaNative,
		Native:            provider.NativeIDs{Turn: provider.Some[provider.NativeTurnID]("native-turn")},
	}
}

// codexReceipt is a Codex-like synchronous success: native receipt known and
// the assignment already moved.
func codexReceipt() provider.DeliveryReceipt {
	return provider.DeliveryReceipt{
		RequestID: "intent-steer", MessageID: "intent-steer",
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown,
		Targeting:       provider.TargetNativeTurnCAS,
		DispatchedFor:   provider.Some(assignment(4)),
		NativeReceiptID: provider.Some[provider.NativeReceiptID]("native-receipt"),
		ObservedAt:      observedAt.Add(-time.Hour),
	}
}

// claudeReceipt is a Claude-like success: written locally, no native receipt.
func claudeReceipt() provider.DeliveryReceipt {
	return provider.DeliveryReceipt{
		RequestID: "intent-steer", MessageID: "intent-steer",
		Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNone,
		Targeting: provider.TargetSessionLease,
	}
}

func providerError(operation provider.Operation, effect provider.RemoteEffect) *provider.ProviderError {
	return &provider.ProviderError{Code: provider.ErrorDeliveryUnknown, Operation: operation, RemoteEffect: effect, Retry: provider.RetryNever, Detail: "detail", Cause: errors.New("cause")}
}

func decideStart(result provider.StartTurnResult, err error) OutcomeDecision {
	return DecideDispatchOutcome(OutcomeFacts{Command: startCommand(), StartResult: result, Err: err, ObservedAt: observedAt})
}

func decideSteer(receipt provider.DeliveryReceipt, err error) OutcomeDecision {
	return DecideDispatchOutcome(OutcomeFacts{Command: steerCommand(), Receipt: receipt, Err: err, ObservedAt: observedAt})
}

func expectOutcome(t *testing.T, got OutcomeDecision, observation ObservationKind, outcome Outcome, class Classification, invalid InvalidField) {
	t.Helper()
	r := got.Record
	if r.Observation != observation || r.Outcome != outcome || r.Classification != class || r.InvalidField != invalid ||
		!r.ObservedAt.Equal(observedAt) || got.Reason == "" || r.Divergence != "" {
		t.Fatalf("decision = %+v", got)
	}
	advance := class == ClassAdvanced
	if (got.Kind == OutcomeSettleAndAdvance) != advance || r.Advanced != advance {
		t.Fatalf("advance: decision = %+v", got)
	}
}

func TestDecideDispatchOutcomeStartResult(t *testing.T) {
	got := decideStart(startResult(), nil)
	expectOutcome(t, got, ObservedStartResult, OutcomeAccepted, ClassTurnStarted, "")
	if got.Record.AdapterTurnID != "turn-a" || got.Record.Targeting != provider.TargetNativeTurnID || got.Record.ResultEnforcement != provider.SchemaNative {
		t.Fatalf("record = %+v", got.Record)
	}
	fallback := startResult()
	fallback.ResultEnforcement = provider.SchemaPromptOnly
	expectOutcome(t, decideStart(fallback, nil), ObservedStartResult, OutcomeAccepted, ClassTurnStarted, "")

	for name, tc := range map[string]struct {
		change func(*provider.StartTurnResult)
		field  InvalidField
	}{
		"no turn":                {func(r *provider.StartTurnResult) { r.Turn.AdapterTurnID = "" }, InvalidTurn},
		"long turn":              {func(r *provider.StartTurnResult) { r.Turn.AdapterTurnID = provider.AdapterTurnID(fill(257)) }, InvalidTurn},
		"unknown targeting":      {func(r *provider.StartTurnResult) { r.TargetingStrength = "guess" }, InvalidTargeting},
		"unknown enforcement":    {func(r *provider.StartTurnResult) { r.ResultEnforcement = "strict" }, InvalidResultEnforcement},
		"disallowed enforcement": {func(r *provider.StartTurnResult) { r.ResultEnforcement = provider.SchemaNone }, InvalidResultEnforcement},
	} {
		t.Run(name, func(t *testing.T) {
			r := startResult()
			tc.change(&r)
			expectOutcome(t, decideStart(r, nil), ObservedInvalid, OutcomeUnknown, ClassInvalidObservation, tc.field)
		})
	}
	other := startResult()
	other.Assignment.Revision = 2
	got = decideStart(other, nil)
	expectOutcome(t, got, ObservedStartResult, OutcomeUnknown, ClassCorrelationMismatch, "")
	if got.Record.AdapterTurnID != "turn-a" {
		t.Fatalf("correlation mismatch keeps the observation: %+v", got.Record)
	}
	// An invalid observation keeps only the valid targeting, turn and
	// enforcement.
	invalid := startResult()
	invalid.TargetingStrength = "guess"
	got = decideStart(invalid, nil)
	if got.Record.Targeting != "" || got.Record.AdapterTurnID != "turn-a" || got.Record.ResultEnforcement != provider.SchemaNative {
		t.Fatalf("invalid record = %+v", got.Record)
	}
}

func TestDecideDispatchOutcomeReceipts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*provider.DeliveryReceipt)
		outcome Outcome
		class   Classification
	}{
		{"codex-like", func(*provider.DeliveryReceipt) {}, OutcomeAccepted, ClassAdvanced},
		{"without dispatched for", func(r *provider.DeliveryReceipt) { r.DispatchedFor = provider.None[provider.AssignmentRef]() }, OutcomeUnknown, ClassInsufficientEvidence},
		{"no native receipt", func(r *provider.DeliveryReceipt) { r.Receipt = provider.ReceiptNone }, OutcomeUnknown, ClassTransportOnly},
		{"unknown receipt knowledge", func(r *provider.DeliveryReceipt) { r.Receipt = provider.ReceiptUnknown }, OutcomeUnknown, ClassTransportOnly},
		{"queued", func(r *provider.DeliveryReceipt) { r.Disposition = provider.DeliveryQueuedByHarness }, OutcomeUnknown, ClassTransportOnly},
		{"rejected", func(r *provider.DeliveryReceipt) { r.Disposition = provider.DeliveryRejected }, OutcomeRejected, ClassRejectedReceipt},
		{"delivery unknown", func(r *provider.DeliveryReceipt) { r.Disposition = provider.DeliveryUnknown }, OutcomeUnknown, ClassDeliveryUnknown},
		{"other request", func(r *provider.DeliveryReceipt) { r.RequestID = "other" }, OutcomeUnknown, ClassCorrelationMismatch},
		{"other message", func(r *provider.DeliveryReceipt) { r.MessageID = "other" }, OutcomeUnknown, ClassCorrelationMismatch},
		{"dispatched for other revision", func(r *provider.DeliveryReceipt) { r.DispatchedFor = provider.Some(assignment(3)) }, OutcomeUnknown, ClassCorrelationMismatch},
		{"absent native receipt id", func(r *provider.DeliveryReceipt) { r.NativeReceiptID = provider.None[provider.NativeReceiptID]() }, OutcomeAccepted, ClassAdvanced},
		{"256-byte native receipt id", func(r *provider.DeliveryReceipt) {
			r.NativeReceiptID = provider.Some(provider.NativeReceiptID(fill(256)))
		}, OutcomeAccepted, ClassAdvanced},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := codexReceipt()
			tc.change(&r)
			got := decideSteer(r, nil)
			expectOutcome(t, got, ObservedReceipt, tc.outcome, tc.class, "")
			if got.Record.Disposition != r.Disposition || got.Record.ReceiptKnowledge != r.Receipt || got.Record.Targeting != r.Targeting {
				t.Fatalf("record = %+v", got.Record)
			}
		})
	}
	got := decideSteer(claudeReceipt(), nil)
	expectOutcome(t, got, ObservedReceipt, OutcomeUnknown, ClassTransportOnly, "")
	if got.Record.DispatchedFor != DispatchedForAbsent || got.Record.NativeReceiptID != "" {
		t.Fatalf("claude record = %+v", got.Record)
	}
	got = decideSteer(codexReceipt(), nil)
	if got.Record.DispatchedFor != DispatchedForMatches || got.Record.NativeReceiptID != "native-receipt" {
		t.Fatalf("codex record = %+v", got.Record)
	}
	for name, tc := range map[string]struct {
		change func(*provider.DeliveryReceipt)
		field  InvalidField
	}{
		"unknown disposition":   {func(r *provider.DeliveryReceipt) { r.Disposition = "sent" }, InvalidDisposition},
		"unknown knowledge":     {func(r *provider.DeliveryReceipt) { r.Receipt = "maybe" }, InvalidReceiptKnowledge},
		"unknown targeting":     {func(r *provider.DeliveryReceipt) { r.Targeting = "" }, InvalidTargeting},
		"present empty receipt": {func(r *provider.DeliveryReceipt) { r.NativeReceiptID = provider.Some[provider.NativeReceiptID]("") }, InvalidNativeReceiptID},
		"257-byte native receipt": {func(r *provider.DeliveryReceipt) {
			r.NativeReceiptID = provider.Some(provider.NativeReceiptID(fill(257)))
		}, InvalidNativeReceiptID},
		"disposition before knowledge": {func(r *provider.DeliveryReceipt) {
			r.Disposition, r.Receipt = "sent", "maybe"
		}, InvalidDisposition},
	} {
		t.Run(name, func(t *testing.T) {
			r := codexReceipt()
			tc.change(&r)
			got := decideSteer(r, nil)
			expectOutcome(t, got, ObservedInvalid, OutcomeUnknown, ClassInvalidObservation, tc.field)
			if got.Record.Disposition != "" || got.Record.ReceiptKnowledge != "" || got.Record.DispatchedFor != "" || got.Record.NativeReceiptID != "" {
				t.Fatalf("invalid receipt keeps receipt fields: %+v", got.Record)
			}
		})
	}
}

func TestDecideDispatchOutcomeErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		outcome Outcome
		class   Classification
	}{
		{"not sent", providerError(provider.OperationSteer, provider.RemoteEffectNotSent), OutcomeRejected, ClassNotSent},
		{"possibly sent", providerError(provider.OperationSteer, provider.RemoteEffectPossiblySent), OutcomeUnknown, ClassPossiblySent},
		{"unknown effect", providerError(provider.OperationSteer, provider.RemoteEffectUnknown), OutcomeUnknown, ClassPossiblySent},
		{"accepted without result", providerError(provider.OperationSteer, provider.RemoteEffectAccepted), OutcomeUnknown, ClassAcceptedWithoutResult},
		{"wrapped", fmt.Errorf("adapter: %w", providerError(provider.OperationSteer, provider.RemoteEffectNotSent)), OutcomeRejected, ClassNotSent},
		{"other operation", providerError(provider.OperationStartTurn, provider.RemoteEffectNotSent), OutcomeUnknown, ClassCorrelationMismatch},
		{"other request", func() error {
			e := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
			e.RequestID = provider.Some[provider.RequestID]("other")
			return e
		}(), OutcomeUnknown, ClassCorrelationMismatch},
		{"same request", func() error {
			e := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
			e.RequestID = provider.Some[provider.RequestID]("intent-steer")
			e.Session = provider.Some(steerCommand().Session)
			e.Turn = provider.Some(steerCommand().ExpectedTurn)
			return e
		}(), OutcomeRejected, ClassNotSent},
		{"other session", func() error {
			e := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
			e.Session = provider.Some(provider.SessionRef{AdapterSessionID: "other"})
			return e
		}(), OutcomeUnknown, ClassCorrelationMismatch},
		{"other turn", func() error {
			e := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
			e.Turn = provider.Some(provider.TurnRef{AdapterTurnID: "other"})
			return e
		}(), OutcomeUnknown, ClassCorrelationMismatch},
		{"retry advice is ignored", func() error {
			e := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
			e.Retry = "whenever"
			return e
		}(), OutcomeRejected, ClassNotSent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decideSteer(provider.DeliveryReceipt{}, tc.err)
			expectOutcome(t, got, ObservedProviderError, tc.outcome, tc.class, "")
			if got.Record.ErrorCode != provider.ErrorDeliveryUnknown || got.Record.RemoteEffect == "" {
				t.Fatalf("record = %+v", got.Record)
			}
		})
	}
	expectOutcome(t, decideSteer(provider.DeliveryReceipt{}, errors.New("pipe closed")), ObservedOtherError, OutcomeUnknown, ClassUnclassifiedError, "")
	var typedNil *provider.ProviderError
	expectOutcome(t, decideSteer(provider.DeliveryReceipt{}, typedNil), ObservedOtherError, OutcomeUnknown, ClassUnclassifiedError, "")
	// A start has no turn yet: a turn named on its error contradicts it.
	startErr := providerError(provider.OperationStartTurn, provider.RemoteEffectNotSent)
	startErr.Turn = provider.Some(provider.TurnRef{AdapterTurnID: "native"})
	expectOutcome(t, decideStart(provider.StartTurnResult{}, startErr), ObservedProviderError, OutcomeUnknown, ClassCorrelationMismatch, "")
	expectOutcome(t, decideStart(provider.StartTurnResult{}, providerError(provider.OperationStartTurn, provider.RemoteEffectNotSent)), ObservedProviderError, OutcomeRejected, ClassNotSent, "")

	for name, tc := range map[string]struct {
		change func(*provider.ProviderError)
		field  InvalidField
	}{
		"unknown operation": {func(e *provider.ProviderError) { e.Operation = "dance" }, InvalidOperation},
		"unknown code":      {func(e *provider.ProviderError) { e.Code = "oops" }, InvalidErrorCode},
		"unknown effect":    {func(e *provider.ProviderError) { e.RemoteEffect = "maybe" }, InvalidRemoteEffect},
		"operation first":   {func(e *provider.ProviderError) { e.Operation, e.Code = "dance", "oops" }, InvalidOperation},
	} {
		t.Run(name, func(t *testing.T) {
			e := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
			tc.change(e)
			got := decideSteer(provider.DeliveryReceipt{}, e)
			expectOutcome(t, got, ObservedInvalid, OutcomeUnknown, ClassInvalidObservation, tc.field)
			if got.Record.ErrorCode != "" || got.Record.RemoteEffect != "" {
				t.Fatalf("invalid error keeps fields: %+v", got.Record)
			}
		})
	}
}

func TestDecideDispatchOutcomeContradictory(t *testing.T) {
	err := providerError(provider.OperationStartTurn, provider.RemoteEffectNotSent)
	expectOutcome(t, decideStart(startResult(), err), ObservedInvalid, OutcomeUnknown, ClassInvalidObservation, InvalidContradictory)
	expectOutcome(t, decideSteer(codexReceipt(), errors.New("x")), ObservedInvalid, OutcomeUnknown, ClassInvalidObservation, InvalidContradictory)
	got := DecideDispatchOutcome(OutcomeFacts{Command: startCommand(), Receipt: codexReceipt(), Err: err, ObservedAt: observedAt})
	expectOutcome(t, got, ObservedInvalid, OutcomeUnknown, ClassInvalidObservation, InvalidContradictory)
}

func TestDecideDispatchOutcomeInvalidFacts(t *testing.T) {
	for name, facts := range map[string]OutcomeFacts{
		"no time":      {Command: startCommand(), StartResult: startResult()},
		"unknown kind": {Command: Command{IntentID: "x", Kind: "interrupt"}, ObservedAt: observedAt},
		"no intent id": {Command: Command{Kind: IntentStartTurn}, ObservedAt: observedAt},
	} {
		if got := DecideDispatchOutcome(facts); got.Kind != OutcomeInvalidFacts || got.Reason == "" {
			t.Fatalf("%s = %+v", name, got)
		}
	}
}

// Every classification and every invalid field is reachable, so the stored
// lists and the rule cannot drift apart.
func TestOutcomeRuleReachesEveryValue(t *testing.T) {
	classes := map[Classification]bool{}
	fields := map[InvalidField]bool{}
	note := func(d OutcomeDecision) {
		classes[d.Record.Classification] = true
		if d.Record.InvalidField != "" {
			fields[d.Record.InvalidField] = true
		}
	}
	note(decideStart(startResult(), nil))
	note(decideStart(provider.StartTurnResult{}, nil))
	r := startResult()
	r.TargetingStrength = ""
	note(decideStart(r, nil))
	r = startResult()
	r.ResultEnforcement = ""
	note(decideStart(r, nil))
	note(decideStart(startResult(), errors.New("x")))
	for _, disposition := range append(DeliveryDispositions(), "x") {
		for _, knowledge := range append(ReceiptKnowledges(), "x") {
			receipt := codexReceipt()
			receipt.Disposition, receipt.Receipt = disposition, knowledge
			note(decideSteer(receipt, nil))
			receipt.DispatchedFor = provider.None[provider.AssignmentRef]()
			note(decideSteer(receipt, nil))
		}
	}
	receipt := codexReceipt()
	receipt.Targeting = ""
	note(decideSteer(receipt, nil))
	receipt = codexReceipt()
	receipt.NativeReceiptID = provider.Some[provider.NativeReceiptID]("")
	note(decideSteer(receipt, nil))
	receipt = codexReceipt()
	receipt.MessageID = "x"
	note(decideSteer(receipt, nil))
	for _, effect := range append(RemoteEffects(), "x") {
		note(decideSteer(provider.DeliveryReceipt{}, providerError(provider.OperationSteer, effect)))
	}
	bad := providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
	bad.Operation = "x"
	note(decideSteer(provider.DeliveryReceipt{}, bad))
	bad = providerError(provider.OperationSteer, provider.RemoteEffectNotSent)
	bad.Code = "x"
	note(decideSteer(provider.DeliveryReceipt{}, bad))
	note(decideSteer(provider.DeliveryReceipt{}, errors.New("x")))
	for _, class := range Classifications() {
		if !classes[class] {
			t.Errorf("classification %s unreachable", class)
		}
	}
	for _, field := range InvalidFields() {
		if !fields[field] {
			t.Errorf("invalid field %s unreachable", field)
		}
	}
}

// The provider enumerations the outcome stores name every constant of the
// provider contract.
func TestOutcomeEnumerations(t *testing.T) {
	for name, values := range map[string][]string{
		"outcomes":       texts(Outcomes()),
		"observations":   texts(ObservationKinds()),
		"dispatched for": texts(DispatchedFors()),
		"divergences":    texts(Divergences()),
		"states":         texts(IntentStates()),
		"kinds":          texts(IntentKinds()),
	} {
		if len(values) == 0 || len(slices.Compact(slices.Sorted(slices.Values(values)))) != len(values) {
			t.Fatalf("%s = %v", name, values)
		}
	}
}

func texts[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func TestIntentDispatchStateCoversEveryState(t *testing.T) {
	want := map[IntentState]memory.DispatchState{
		StateRecorded:    memory.DispatchIntentRecorded,
		StateDispatching: memory.DispatchDeliveryUnknown,
		StateUnknown:     memory.DispatchDeliveryUnknown,
		StateAccepted:    memory.DispatchDelivered,
		StateDiverged:    memory.DispatchDelivered,
		StateRejected:    memory.DispatchNotDispatched,
		StateInvalidated: memory.DispatchNotDispatched,
	}
	for _, state := range IntentStates() {
		got, ok := IntentDispatchState(state)
		if !ok || got != want[state] {
			t.Fatalf("%s = %s, %v", state, got, ok)
		}
	}
	if _, ok := IntentDispatchState("sent"); ok {
		t.Fatal("unknown state mapped")
	}
	if !slices.Equal(OpenIntentStates(), []IntentState{StateRecorded, StateDispatching, StateUnknown, StateDiverged}) {
		t.Fatalf("open states = %v", OpenIntentStates())
	}
}
