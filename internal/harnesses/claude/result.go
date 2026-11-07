package claude

import (
	"bytes"
	"encoding/json"

	"github.com/MartinAndreev/conduit/internal/harnesses/claude/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// nativeResult is the subset of a stream-json `result` message the adapter
// interprets. It is a native observation; the adapter never invents one.
type nativeResult struct {
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	TerminalReason   string          `json:"terminal_reason"`
	UserMessageUUID  string          `json:"user_message_uuid"`
	UserMessageUUIDs []string        `json:"user_message_uuids"`
	Usage            *nativeUsage    `json:"usage"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Origin           *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
}

func (r nativeResult) inputUUIDs() []string {
	uuids := append([]string(nil), r.UserMessageUUIDs...)
	if r.UserMessageUUID != "" {
		uuids = append(uuids, r.UserMessageUUID)
	}
	return uuids
}

// reason is the native terminal reason, or the subtype when it is absent.
func (r nativeResult) reason() string {
	if r.TerminalReason != "" {
		return r.TerminalReason
	}
	return r.Subtype
}

// nativeUsage mirrors result.usage. Pointers keep an absent counter unknown
// instead of a reported zero.
type nativeUsage struct {
	InputTokens              *uint64 `json:"input_tokens"`
	OutputTokens             *uint64 `json:"output_tokens"`
	CacheCreationInputTokens *uint64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *uint64 `json:"cache_read_input_tokens"`
	OutputTokensDetails      *struct {
		ThinkingTokens *uint64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

// counters maps native usage. Cost is left unknown: total_cost_usd is not
// proven to be scoped to this turn.
func (u nativeUsage) counters() provider.UsageCounters {
	counters := provider.UsageCounters{
		InputTokens:      optionalCount(u.InputTokens),
		OutputTokens:     optionalCount(u.OutputTokens),
		CacheReadTokens:  optionalCount(u.CacheReadInputTokens),
		CacheWriteTokens: optionalCount(u.CacheCreationInputTokens),
	}
	if u.OutputTokensDetails != nil {
		counters.ReasoningTokens = optionalCount(u.OutputTokensDetails.ThinkingTokens)
	}
	return counters
}

func optionalCount(value *uint64) provider.Optional[uint64] {
	if value == nil {
		return provider.None[uint64]()
	}
	return provider.Some(*value)
}

// resultFacts are read with the terminal decision in one critical section.
type resultFacts struct {
	decision rules.ResultTerminalDecision
	turn     *activeTurn
	contract provider.ResultContractNegotiation
	model    provider.ModelSelection
}

func (s *session) normalizeResult(line []byte) {
	var result nativeResult
	if json.Unmarshal(line, &result) != nil {
		s.warnNativeOnce("result:invalid", provider.ProtocolWarningEvent{
			Code: "invalid_result", Detail: provider.BoundedText{Text: "ignored a result with an unexpected shape"}, OffendingNativeKind: "result",
		})
		return
	}
	// A result that names an input proves claude received it, including a
	// correction whose echo was not observed.
	for _, uuid := range result.inputUUIDs() {
		s.observeDeliveryEvidence(uuid)
	}
	facts := s.decideResult(result)
	switch facts.decision.Kind {
	case rules.ResultForeign:
		s.reportForeignResult(result)
	case rules.ResultIntermediate:
		s.reportIntermediateResult(result, facts)
	case rules.ResultTerminal:
		s.completeTurn(result, facts)
	}
}

// decideResult evaluates DecideResultTerminal and, for a terminal result,
// marks the turn ending in the same critical section, so no correction can be
// dispatched into it while its terminal events are published.
func (s *session) decideResult(result nativeResult) resultFacts {
	var facts resultFacts
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		active := s.turn != nil && !s.turn.ending
		intersects := false
		if active {
			for _, uuid := range result.inputUUIDs() {
				if _, ok := s.turn.inputs[uuid]; ok {
					intersects = true
					break
				}
			}
		}
		facts.decision = rules.DecideResultTerminal(rules.ResultTerminalFacts{
			ActiveTurn: active, IntersectsActiveInputs: intersects, TerminalReason: result.TerminalReason,
		})
		if facts.decision.Kind == rules.ResultForeign {
			return nil
		}
		if facts.decision.Kind == rules.ResultTerminal {
			s.turn.ending = true
		}
		facts.turn = s.turn
		facts.contract = s.contract
		facts.model = s.model
		if s.observedModel != "" {
			facts.model.ModelID = s.observedModel
		}
		return nil
	})
	return facts
}

// reportForeignResult keeps the active turn: a result for other inputs, such
// as a background task wake, is a warning only.
func (s *session) reportForeignResult(result nativeResult) {
	code := "uncorrelated_result"
	if result.Origin != nil && result.Origin.Kind == "task-notification" {
		code = "native_wake"
	}
	s.emit(provider.ProtocolWarningEvent{
		Code:                code,
		Detail:              boundedtext.String("result does not reference the active turn (terminal_reason "+result.reason()+")", maxNativeErrorBytes),
		OffendingNativeKind: "result",
	})
}

// reportIntermediateResult reports a steer-aborted result; the turn continues.
// Its usage scope is not proven, so the measurement is unknown.
func (s *session) reportIntermediateResult(result nativeResult, facts resultFacts) {
	s.emit(provider.ProtocolWarningEvent{
		Code:                "native_intermediate_result",
		Detail:              boundedtext.String("intermediate result ("+result.reason()+"); the turn continues", maxNativeErrorBytes),
		OffendingNativeKind: "result",
	})
	if result.Usage != nil {
		s.emit(provider.UsageEvent{
			Scope: provider.UsagePrimaryTurn, ScopeID: string(facts.turn.ref.AdapterTurnID),
			Measurement: provider.UsageMeasurementUnknown, Model: facts.model, Counters: result.Usage.counters(),
		})
	}
}

// completeTurn publishes the terminal observations in order (failure warning,
// usage, result candidate, turn state) and then clears the turn. A correction
// still lacking evidence keeps attribution unresolved.
func (s *session) completeTurn(result nativeResult, facts resultFacts) {
	outcome := rules.DecideResultOutcome(rules.ResultOutcomeFacts{Subtype: result.Subtype, IsError: result.IsError})
	if outcome.Kind == rules.ResultFailed {
		s.emit(provider.ProtocolWarningEvent{
			Code:                "native_turn_error",
			Detail:              boundedtext.String(result.reason()+": "+result.Result, maxNativeErrorBytes),
			OffendingNativeKind: "result",
		})
	}
	if result.Usage != nil {
		s.emit(provider.UsageEvent{
			Scope: provider.UsagePrimaryTurn, ScopeID: string(facts.turn.ref.AdapterTurnID),
			Measurement: provider.UsageCumulativeSnapshot, Model: facts.model, Counters: result.Usage.counters(),
		})
	}
	s.emitResultCandidate(result, facts)
	s.emit(provider.TurnStateEvent{Previous: provider.TurnActive, Current: provider.TurnNativeTerminal, TerminalReason: result.reason()})

	steerUnresolved := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.turn == facts.turn {
			s.turn = nil
		}
		steerUnresolved = s.steer != nil
		return nil
	})
	s.normalization.resetTurnItems()
	if steerUnresolved {
		s.emit(provider.ProtocolWarningEvent{
			Code:                "steer_delivery_unknown",
			Detail:              provider.BoundedText{Text: "the turn ended without evidence that claude received the correction"},
			OffendingNativeKind: "result",
		})
	}
}

// emitResultCandidate publishes structured_output as an untrusted candidate
// when DecideResultCandidate allows it.
func (s *session) emitResultCandidate(result nativeResult, facts resultFacts) {
	output := bytes.TrimSpace(result.StructuredOutput)
	present := len(output) != 0 && !bytes.Equal(output, []byte("null"))
	decision := rules.DecideResultCandidate(rules.ResultCandidateFacts{
		Terminal:               true,
		NativeSchemaNegotiated: facts.contract.Actual == provider.SchemaNative,
		OutputPresent:          present,
		JSONObject:             isJSONObject(output),
	})
	switch decision.Kind {
	case rules.ResultCandidateAbsent:
		return
	case rules.ResultCandidateNotEligible:
		s.emit(provider.ProtocolWarningEvent{Code: "result_candidate_not_eligible", Detail: provider.BoundedText{Text: decision.Reason}, OffendingNativeKind: "result"})
		return
	case rules.ResultCandidateInvalid:
		s.emit(provider.ProtocolWarningEvent{Code: "result_candidate_invalid_json", Detail: provider.BoundedText{Text: decision.Reason}, OffendingNativeKind: "result"})
		return
	}
	submissionID, err := s.config.NewID("claude-result")
	if err != nil {
		s.failObservation(err, "could not allocate result submission ID")
		return
	}
	payload := append(json.RawMessage(nil), output...)
	s.emitEvent(func(binding eventBinding) (provider.EventBody, bool) {
		// Recheck correlation and enforcement against the facts that also bind
		// the envelope.
		if !binding.Turn.Present || binding.Turn.Value != facts.turn.ref || binding.Contract.Actual != provider.SchemaNative {
			return nil, false
		}
		return provider.ResultCandidateEvent{Candidate: provider.ResultCandidate{
			SubmissionID: provider.SubmissionID(submissionID), Assignment: binding.Assignment,
			Source: provider.CandidateNativeStructured, Contract: binding.Contract.Contract, Enforcement: binding.Contract.Actual,
			Payload: payload, Native: binding.Native,
		}}, true
	})
}

func isJSONObject(raw []byte) bool {
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil
}
