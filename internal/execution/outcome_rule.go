package execution

import (
	"errors"
	"slices"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Outcome is what is known about an attempted send.
type Outcome string

const (
	OutcomeAccepted Outcome = "accepted"
	OutcomeRejected Outcome = "rejected"
	OutcomeUnknown  Outcome = "unknown"
)

func Outcomes() []Outcome { return []Outcome{OutcomeAccepted, OutcomeRejected, OutcomeUnknown} }

// Classification is why an observation has its outcome.
type Classification string

const (
	ClassTurnStarted           Classification = "turn_started"
	ClassAdvanced              Classification = "advanced"
	ClassInsufficientEvidence  Classification = "insufficient_evidence"
	ClassTransportOnly         Classification = "transport_only"
	ClassRejectedReceipt       Classification = "rejected_receipt"
	ClassDeliveryUnknown       Classification = "delivery_unknown"
	ClassNotSent               Classification = "not_sent"
	ClassPossiblySent          Classification = "possibly_sent"
	ClassAcceptedWithoutResult Classification = "accepted_without_result"
	ClassUnclassifiedError     Classification = "unclassified_error"
	ClassCorrelationMismatch   Classification = "correlation_mismatch"
	ClassInvalidObservation    Classification = "invalid_observation"
)

func Classifications() []Classification {
	return []Classification{ClassTurnStarted, ClassAdvanced, ClassInsufficientEvidence, ClassTransportOnly,
		ClassRejectedReceipt, ClassDeliveryUnknown, ClassNotSent, ClassPossiblySent, ClassAcceptedWithoutResult,
		ClassUnclassifiedError, ClassCorrelationMismatch, ClassInvalidObservation}
}

// ObservationKind is the one kind a raw adapter observation normalizes to.
type ObservationKind string

const (
	ObservedStartResult   ObservationKind = "start_result"
	ObservedReceipt       ObservationKind = "receipt"
	ObservedProviderError ObservationKind = "provider_error"
	ObservedOtherError    ObservationKind = "other_error"
	ObservedInvalid       ObservationKind = "invalid"
)

func ObservationKinds() []ObservationKind {
	return []ObservationKind{ObservedStartResult, ObservedReceipt, ObservedProviderError, ObservedOtherError, ObservedInvalid}
}

// InvalidField names the first field that made an observation invalid.
type InvalidField string

const (
	InvalidContradictory     InvalidField = "contradictory"
	InvalidTurn              InvalidField = "turn"
	InvalidTargeting         InvalidField = "targeting"
	InvalidResultEnforcement InvalidField = "result_enforcement"
	InvalidDisposition       InvalidField = "disposition"
	InvalidReceiptKnowledge  InvalidField = "receipt_knowledge"
	InvalidNativeReceiptID   InvalidField = "native_receipt_id"
	InvalidOperation         InvalidField = "operation"
	InvalidErrorCode         InvalidField = "error_code"
	InvalidRemoteEffect      InvalidField = "remote_effect"
)

func InvalidFields() []InvalidField {
	return []InvalidField{InvalidContradictory, InvalidTurn, InvalidTargeting, InvalidResultEnforcement,
		InvalidDisposition, InvalidReceiptKnowledge, InvalidNativeReceiptID, InvalidOperation, InvalidErrorCode, InvalidRemoteEffect}
}

// DispatchedFor compares a receipt's DispatchedFor with the Steer's new
// assignment.
type DispatchedFor string

const (
	DispatchedForAbsent  DispatchedFor = "absent"
	DispatchedForMatches DispatchedFor = "matches"
	DispatchedForDiffers DispatchedFor = "differs"
)

func DispatchedFors() []DispatchedFor {
	return []DispatchedFor{DispatchedForAbsent, DispatchedForMatches, DispatchedForDiffers}
}

// Divergence is why a proven advancement could not be recorded locally.
type Divergence string

const (
	DivergenceBindingChanged  Divergence = "binding_changed"
	DivergenceRevisionChanged Divergence = "revision_changed"
	DivergenceSessionMissing  Divergence = "session_missing"
)

func Divergences() []Divergence {
	return []Divergence{DivergenceBindingChanged, DivergenceRevisionChanged, DivergenceSessionMissing}
}

// Enumerations the outcome stores from the provider contract. They mirror
// internal/sessions/provider; storage's CHECK lists are tested against them.
func TargetingStrengths() []provider.TargetingStrength {
	return []provider.TargetingStrength{provider.TargetNativeTurnCAS, provider.TargetNativeTurnID, provider.TargetSessionLease, provider.TargetUnknown}
}

func SchemaEnforcements() []provider.SchemaEnforcement {
	return []provider.SchemaEnforcement{provider.SchemaNative, provider.SchemaPromptOnly, provider.SchemaNone}
}

func DeliveryDispositions() []provider.DeliveryDisposition {
	return []provider.DeliveryDisposition{provider.DeliveryTransportAccepted, provider.DeliveryQueuedByHarness, provider.DeliveryRejected, provider.DeliveryUnknown}
}

func ReceiptKnowledges() []provider.ReceiptKnowledge {
	return []provider.ReceiptKnowledge{provider.ReceiptNativeKnown, provider.ReceiptNone, provider.ReceiptUnknown}
}

func ErrorCodes() []provider.ErrorCode {
	return []provider.ErrorCode{provider.ErrorCapabilityUnsupported, provider.ErrorCapabilityUnknown, provider.ErrorStaleBinding,
		provider.ErrorNativeTargetRejected, provider.ErrorDeliveryRejected, provider.ErrorDeliveryUnknown, provider.ErrorSessionLost,
		provider.ErrorTurnLost, provider.ErrorProtocolViolation, provider.ErrorEventGap, provider.ErrorBackpressureExceeded,
		provider.ErrorAuthFailed, provider.ErrorExecutableMissing, provider.ErrorDeadline, provider.ErrorCallerCancelled}
}

func RemoteEffects() []provider.RemoteEffect {
	return []provider.RemoteEffect{provider.RemoteEffectNotSent, provider.RemoteEffectPossiblySent, provider.RemoteEffectAccepted, provider.RemoteEffectUnknown}
}

func Operations() []provider.Operation {
	return []provider.Operation{provider.OperationInspect, provider.OperationOpenSession, provider.OperationResumeSession,
		provider.OperationSubscribeEvents, provider.OperationReceiveEvent, provider.OperationObserveState, provider.OperationStartTurn,
		provider.OperationSteer, provider.OperationRespondPermission, provider.OperationAnswerQuestion, provider.OperationInterruptTurn,
		provider.OperationCloseSession}
}

// OutcomeRecord is the stored outcome DTO. Empty strings are NULL. It holds no
// native text, error detail, cause or serialized error. ObservedAt is the
// dispatcher's clock, never the adapter's. Advanced is true only when the
// revision moved; Divergence is set only when a proven advancement could not
// be recorded.
type OutcomeRecord struct {
	Outcome           Outcome
	Classification    Classification
	Observation       ObservationKind
	InvalidField      InvalidField
	ResultEnforcement provider.SchemaEnforcement
	Disposition       provider.DeliveryDisposition
	ReceiptKnowledge  provider.ReceiptKnowledge
	Targeting         provider.TargetingStrength
	ErrorCode         provider.ErrorCode
	RemoteEffect      provider.RemoteEffect
	DispatchedFor     DispatchedFor
	NativeReceiptID   provider.NativeReceiptID
	AdapterTurnID     provider.AdapterTurnID
	Advanced          bool
	Divergence        Divergence
	ObservedAt        time.Time
}

// OutcomeFacts are one raw observation of the command's harness call. The
// StartResult is the call's result for StartTurn, Receipt for Steer; Err is
// its error.
type OutcomeFacts struct {
	Command     Command
	StartResult provider.StartTurnResult
	Receipt     provider.DeliveryReceipt
	Err         error
	ObservedAt  time.Time
}

type OutcomeDecisionKind string

const (
	// OutcomeSettle records the outcome without moving the revision.
	OutcomeSettle OutcomeDecisionKind = "settle"
	// OutcomeSettleAndAdvance records an accepted Steer and moves the
	// session's revision to the target in the same transaction.
	OutcomeSettleAndAdvance OutcomeDecisionKind = "settle_and_advance"
	OutcomeInvalidFacts     OutcomeDecisionKind = "invalid_facts"
)

// OutcomeDecision carries Record except for OutcomeInvalidFacts.
type OutcomeDecision struct {
	Kind   OutcomeDecisionKind
	Reason string
	Record OutcomeRecord
}

// DecideDispatchOutcome classifies one observation totally: every input has
// exactly one stored row. It first normalizes the observation to one kind: an
// error together with a non-zero result or receipt is contradictory; a
// non-nil *provider.ProviderError is a provider error, any other error is
// other_error; otherwise the result or receipt of the command's operation.
// Each kind's fields are checked in a fixed order and the first invalid one
// makes the observation invalid with that field; values that are only advice
// (Retry), diagnostics (Detail, Cause), native IDs other than the receipt ID
// and the adapter's ObservedAt are ignored. Correlation follows: a mismatch
// keeps the observation and is unknown/correlation_mismatch. Only a Steer
// receipt that is transport_accepted with native_receipt_known and
// DispatchedFor equal to the new assignment advances the revision.
func DecideDispatchOutcome(f OutcomeFacts) OutcomeDecision {
	c := f.Command
	if !slices.Contains(IntentKinds(), c.Kind) || c.IntentID == "" || f.ObservedAt.IsZero() {
		return OutcomeDecision{Kind: OutcomeInvalidFacts, Reason: "outcome facts need a known operation, an intent id and the dispatcher's time"}
	}
	record := OutcomeRecord{ObservedAt: f.ObservedAt}
	var reason string
	switch {
	case f.Err != nil && (f.StartResult != (provider.StartTurnResult{}) || f.Receipt != (provider.DeliveryReceipt{})):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidContradictory
	case f.Err != nil:
		reason = classifyError(c, f.Err, &record)
	case c.Kind == IntentStartTurn:
		reason = classifyStartResult(c, f.StartResult, &record)
	default:
		reason = classifyReceipt(c, f.Receipt, &record)
	}
	if record.Observation == ObservedInvalid {
		record.Outcome, record.Classification = OutcomeUnknown, ClassInvalidObservation
		reason = "observation is invalid: " + string(record.InvalidField)
	}
	if record.Classification == ClassAdvanced {
		record.Advanced = true
		return OutcomeDecision{Kind: OutcomeSettleAndAdvance, Reason: reason, Record: record}
	}
	return OutcomeDecision{Kind: OutcomeSettle, Reason: reason, Record: record}
}

func classified(record *OutcomeRecord, outcome Outcome, class Classification, reason string) string {
	record.Outcome, record.Classification = outcome, class
	return reason
}

func classifyStartResult(c Command, r provider.StartTurnResult, record *OutcomeRecord) string {
	turnValid := boundedIdentity(string(r.Turn.AdapterTurnID))
	targetingValid := slices.Contains(TargetingStrengths(), r.TargetingStrength)
	enforcementValid := slices.Contains(SchemaEnforcements(), r.ResultEnforcement) && c.ResultContract.Allows(r.ResultEnforcement)
	if turnValid {
		record.AdapterTurnID = r.Turn.AdapterTurnID
	}
	if targetingValid {
		record.Targeting = r.TargetingStrength
	}
	if enforcementValid {
		record.ResultEnforcement = r.ResultEnforcement
	}
	switch {
	case !turnValid:
		record.Observation, record.InvalidField = ObservedInvalid, InvalidTurn
		return ""
	case !targetingValid:
		record.Observation, record.InvalidField = ObservedInvalid, InvalidTargeting
		return ""
	case !enforcementValid:
		record.Observation, record.InvalidField = ObservedInvalid, InvalidResultEnforcement
		return ""
	}
	record.Observation = ObservedStartResult
	if r.Assignment != c.Current {
		return classified(record, OutcomeUnknown, ClassCorrelationMismatch, "start result names another assignment")
	}
	return classified(record, OutcomeAccepted, ClassTurnStarted, "turn started for the intent's assignment")
}

func classifyReceipt(c Command, r provider.DeliveryReceipt, record *OutcomeRecord) string {
	if slices.Contains(TargetingStrengths(), r.Targeting) {
		record.Targeting = r.Targeting
	}
	switch {
	case !slices.Contains(DeliveryDispositions(), r.Disposition):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidDisposition
	case !slices.Contains(ReceiptKnowledges(), r.Receipt):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidReceiptKnowledge
	case record.Targeting == "":
		record.Observation, record.InvalidField = ObservedInvalid, InvalidTargeting
	case r.NativeReceiptID.Present && !boundedIdentity(string(r.NativeReceiptID.Value)):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidNativeReceiptID
	}
	if record.Observation == ObservedInvalid {
		return ""
	}
	record.Observation = ObservedReceipt
	record.Disposition, record.ReceiptKnowledge = r.Disposition, r.Receipt
	if r.NativeReceiptID.Present {
		record.NativeReceiptID = r.NativeReceiptID.Value
	}
	switch {
	case !r.DispatchedFor.Present:
		record.DispatchedFor = DispatchedForAbsent
	case r.DispatchedFor.Value == c.New:
		record.DispatchedFor = DispatchedForMatches
	default:
		record.DispatchedFor = DispatchedForDiffers
	}
	if string(r.RequestID) != string(c.IntentID) || string(r.MessageID) != string(c.IntentID) || record.DispatchedFor == DispatchedForDiffers {
		return classified(record, OutcomeUnknown, ClassCorrelationMismatch, "receipt names another request, message or assignment")
	}
	switch r.Disposition {
	case provider.DeliveryTransportAccepted:
		switch {
		case r.Receipt != provider.ReceiptNativeKnown:
			return classified(record, OutcomeUnknown, ClassTransportOnly, "transport accepted without a native receipt")
		case record.DispatchedFor == DispatchedForAbsent:
			return classified(record, OutcomeUnknown, ClassInsufficientEvidence, "native receipt without the dispatched assignment")
		default:
			return classified(record, OutcomeAccepted, ClassAdvanced, "native receipt for the new assignment")
		}
	case provider.DeliveryQueuedByHarness:
		return classified(record, OutcomeUnknown, ClassTransportOnly, "queued by the harness; delivery is not proven")
	case provider.DeliveryRejected:
		return classified(record, OutcomeRejected, ClassRejectedReceipt, "the harness rejected the steer")
	default:
		return classified(record, OutcomeUnknown, ClassDeliveryUnknown, "the adapter does not know whether the steer was delivered")
	}
}

func classifyError(c Command, err error, record *OutcomeRecord) string {
	var providerError *provider.ProviderError
	if !errors.As(err, &providerError) || providerError == nil {
		record.Observation = ObservedOtherError
		return classified(record, OutcomeUnknown, ClassUnclassifiedError, "the harness call failed without a provider error")
	}
	switch {
	case !slices.Contains(Operations(), providerError.Operation):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidOperation
	case !slices.Contains(ErrorCodes(), providerError.Code):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidErrorCode
	case !slices.Contains(RemoteEffects(), providerError.RemoteEffect):
		record.Observation, record.InvalidField = ObservedInvalid, InvalidRemoteEffect
	}
	if record.Observation == ObservedInvalid {
		return ""
	}
	record.Observation = ObservedProviderError
	record.ErrorCode, record.RemoteEffect = providerError.Code, providerError.RemoteEffect
	if providerError.Operation != expectedOperation(c.Kind) ||
		providerError.RequestID.Present && string(providerError.RequestID.Value) != string(c.IntentID) ||
		providerError.Session.Present && providerError.Session.Value != c.Session ||
		providerError.Turn.Present && !turnCorrelates(c, providerError.Turn.Value) {
		return classified(record, OutcomeUnknown, ClassCorrelationMismatch, "provider error names another operation, request, session or turn")
	}
	switch providerError.RemoteEffect {
	case provider.RemoteEffectNotSent:
		return classified(record, OutcomeRejected, ClassNotSent, "the adapter proved nothing was sent")
	case provider.RemoteEffectAccepted:
		return classified(record, OutcomeUnknown, ClassAcceptedWithoutResult, "the harness accepted the command but the call returned no result")
	default:
		return classified(record, OutcomeUnknown, ClassPossiblySent, "the command may have been sent")
	}
}

func expectedOperation(kind IntentKind) provider.Operation {
	if kind == IntentSteer {
		return provider.OperationSteer
	}
	return provider.OperationStartTurn
}

// turnCorrelates reports whether an error's present turn is the command's:
// the expected turn of a Steer. A StartTurn has no turn before it starts, so
// any turn named on its error contradicts it (no adapter names one; the
// plan's conservative rule, Astra review of 2.1).
func turnCorrelates(c Command, turn provider.TurnRef) bool {
	return c.Kind == IntentSteer && turn == c.ExpectedTurn
}
