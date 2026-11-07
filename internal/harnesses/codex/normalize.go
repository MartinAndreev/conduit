package codex

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// maxNativeTextBytes bounds text copied from one native item into one event.
const maxNativeTextBytes = 64 * 1024

func (s *session) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "$protocol/malformed":
		s.emit(provider.ProtocolWarningEvent{
			Code: "malformed_json", Detail: boundedtext.Bytes(params, 512),
			OffendingNativeKind: method,
		})
	case "thread/status/changed":
		s.normalizeThreadStatus(params)
	case "turn/started":
		s.normalizeTurnStarted(params)
	case "turn/completed":
		s.normalizeTurnCompleted(params)
	case "item/started", "item/completed":
		s.normalizeItem(method, params)
	case "item/agentMessage/delta":
		s.normalizeAgentMessageDelta(params)
	case "item/reasoning/summaryTextDelta":
		s.normalizeReasoningDelta(params)
	case "thread/tokenUsage/updated":
		s.normalizeUsage(params)
	case "error":
		s.normalizeError(params)
	case "model/rerouted":
		s.normalizeModelRerouted(params)
	case "configWarning":
		s.normalizeConfigWarning(params)
	case "thread/name/updated", "thread/compacted", "turn/plan/updated", "account/rateLimits/updated",
		"thread/started", "thread/settings/updated", "account/updated", "mcpServer/startupStatus/updated",
		"remoteControl/status/changed", "item/commandExecution/outputDelta":
		// Known administrative observations are deliberately not part of the
		// current provider event contract. Command output is taken from the
		// completed item's bounded aggregatedOutput instead of deltas.
	default:
		// Unknown notifications remain visible without failing forward-compatible
		// sessions. Their payload is bounded and never interpreted as a tool call.
		s.warnNativeOnce(method, provider.ProtocolWarningEvent{
			Code: "unknown_notification", Detail: provider.BoundedText{Text: "unsupported native notification"},
			OffendingNativeKind: method,
		})
	}
}

func (s *session) handleServerRequest(id json.RawMessage, method string, _ json.RawMessage) {
	s.emit(provider.ProtocolWarningEvent{
		Code: "unsupported_server_request", Detail: provider.BoundedText{Text: "native server request rejected"},
		OffendingNativeKind: method,
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.rpc.rejectRequest(ctx, id, -32601, "Conduit adapter does not support this server request")
}

func (s *session) normalizeThreadStatus(raw json.RawMessage) {
	var message struct {
		ThreadID string `json:"threadId"`
		Status   struct {
			Type string `json:"type"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &message) != nil || !s.matchesThread(message.ThreadID) {
		return
	}
	var next provider.SessionState
	switch message.Status.Type {
	case "idle":
		next = provider.SessionIdleObserved
	case "active":
		next = provider.SessionOpen
	case "systemError":
		// The App Server still answers and its process is owned, so this is not
		// SessionLost (reserved for lost observation/control). The native
		// thread reports an internal failure; surface it without inventing a
		// local session or turn transition.
		s.emit(provider.ProtocolWarningEvent{
			Code: "native_thread_system_error", Detail: provider.BoundedText{Text: "Codex reported thread status systemError"},
			OffendingNativeKind: "thread/status/changed",
		})
		return
	default:
		return
	}
	var previous provider.SessionState
	applied := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideThreadStatusTransition(rules.ThreadStatusFacts{
			Closing: s.closing,
			Ended:   s.state == provider.SessionLost || s.state == provider.SessionClosed,
		})
		if decision.Kind != rules.ThreadStatusApply {
			return nil
		}
		previous = s.state
		s.state = next
		applied = true
		return nil
	})
	if applied {
		s.emit(provider.SessionStateEvent{Previous: previous, Current: next, NativeStatus: message.Status.Type})
	}
}

func (s *session) normalizeTurnStarted(raw json.RawMessage) {
	var message struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &message) != nil || message.Turn.ID == "" || !s.matchesThread(message.ThreadID) {
		s.warnNativeOnce("turn/started:invalid", provider.ProtocolWarningEvent{Code: "uncorrelated_turn_started", Detail: provider.BoundedText{Text: "ignored malformed or foreign turn/started"}, OffendingNativeKind: "turn/started"})
		return
	}
	var active provider.Optional[provider.TurnRef]
	_ = lockscope.WithMutexLock(&s.mu, func() error { active = s.active; return nil })
	if !active.Present || string(active.Value.AdapterTurnID) != message.Turn.ID {
		s.warnNativeOnce("turn/started:stale", provider.ProtocolWarningEvent{Code: "uncorrelated_turn_started", Detail: provider.BoundedText{Text: "ignored turn/started for a different turn"}, OffendingNativeKind: "turn/started"})
		return
	}
	s.emit(provider.TurnStateEvent{Previous: provider.TurnStarting, Current: provider.TurnActive})
}

func (s *session) normalizeTurnCompleted(raw json.RawMessage) {
	var message struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string           `json:"id"`
			Status string           `json:"status"`
			Error  *nativeTurnError `json:"error"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &message) != nil || message.Turn.ID == "" || !s.matchesThread(message.ThreadID) {
		s.warnNativeOnce("turn/completed:invalid", provider.ProtocolWarningEvent{Code: "uncorrelated_turn_completed", Detail: provider.BoundedText{Text: "ignored malformed or foreign turn/completed"}, OffendingNativeKind: "turn/completed"})
		return
	}
	var matches bool
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		matches = s.active.Present && string(s.active.Value.AdapterTurnID) == message.Turn.ID
		return nil
	})
	if !matches {
		s.warnNativeOnce("turn/completed:stale", provider.ProtocolWarningEvent{Code: "uncorrelated_turn_completed", Detail: provider.BoundedText{Text: "ignored turn/completed for a different turn"}, OffendingNativeKind: "turn/completed"})
		return
	}
	// TerminalReason stays the native status; the contract has no dedicated
	// failure-reason field, so the native error travels in a warning first.
	if message.Turn.Error != nil {
		s.reportTurnError(message.Turn.ID, "turn/completed", *message.Turn.Error)
	}
	s.emit(provider.TurnStateEvent{Previous: provider.TurnActive, Current: provider.TurnNativeTerminal, TerminalReason: message.Turn.Status})
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.active.Present && string(s.active.Value.AdapterTurnID) == message.Turn.ID {
			s.active = provider.None[provider.TurnRef]()
		}
		return nil
	})
}

type itemNotification struct {
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
	Item     json.RawMessage `json:"item"`
}

type nativeItem struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"`
	Phase            string   `json:"phase"`
	Text             string   `json:"text"`
	Status           string   `json:"status"`
	Command          string   `json:"command"`
	AggregatedOutput string   `json:"aggregatedOutput"`
	ExitCode         *int     `json:"exitCode"`
	Summary          []string `json:"summary"`
	Changes          []struct {
		Path string          `json:"path"`
		Kind patchChangeKind `json:"kind"`
	} `json:"changes"`
}

// patchChangeKind accepts Codex's PatchChangeKind object ({"type":"update"})
// and the bare string form seen in older traces. Any other shape decodes as
// an unknown kind instead of discarding the whole item.
type patchChangeKind string

func (k *patchChangeKind) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*k = patchChangeKind(text)
		return nil
	}
	var object struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &object) == nil {
		*k = patchChangeKind(object.Type)
	}
	return nil
}

func (s *session) normalizeItem(method string, raw json.RawMessage) {
	var notification itemNotification
	if json.Unmarshal(raw, &notification) != nil {
		return
	}
	if !s.matchesActive(notification.ThreadID, notification.TurnID) {
		s.warnNativeOnce(method+":uncorrelated", provider.ProtocolWarningEvent{Code: "uncorrelated_item", Detail: provider.BoundedText{Text: "ignored item event outside the active thread and turn"}, OffendingNativeKind: method})
		return
	}
	var item nativeItem
	if json.Unmarshal(notification.Item, &item) != nil {
		return
	}
	started := method == "item/started"
	switch item.Type {
	case "agentMessage":
		if started {
			return
		}
		phase := provider.AssistantPhaseUnknown
		switch item.Phase {
		case "commentary":
			phase = provider.AssistantCommentary
		case "final_answer":
			phase = provider.AssistantFinal
		}
		update := s.textSnapshot(item.ID, "text", item.Text)
		s.emit(provider.AssistantTextEvent{Phase: phase, Update: update})
		if phase == provider.AssistantFinal && s.currentEnforcement() == provider.SchemaNative {
			if !json.Valid([]byte(item.Text)) {
				// A candidate payload must be JSON; invalid bytes cannot travel as
				// json.RawMessage and would break journal encoding downstream.
				s.emit(provider.ProtocolWarningEvent{
					Code: "result_candidate_invalid_json", Detail: provider.BoundedText{Text: "final answer under native schema enforcement was not valid JSON"},
					OffendingNativeKind: method,
				})
				return
			}
			submissionID, err := s.config.NewID("codex-result")
			if err != nil {
				_ = lockscope.WithMutexLock(&s.mu, func() error {
					if !s.closing {
						s.state = provider.SessionLost
					}
					return nil
				})
				s.broker.Finish(adaptererror.New(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, err, "could not allocate result submission ID"))
				_ = s.process.KillTree()
				return
			}
			s.emitEvent(func(binding eventBinding) (provider.EventBody, bool) {
				// Recheck correlation and negotiated enforcement against the same facts
				// used for both the candidate and its envelope.
				if !binding.Turn.Present || string(binding.Turn.Value.AdapterTurnID) != notification.TurnID ||
					(notification.ThreadID != "" && string(binding.Native.Session.Value) != notification.ThreadID) || binding.Contract.Actual != provider.SchemaNative {
					return nil, false
				}
				return provider.ResultCandidateEvent{Candidate: provider.ResultCandidate{
					SubmissionID: provider.SubmissionID(submissionID), Assignment: binding.Assignment,
					Source: provider.CandidateNativeStructured, Contract: binding.Contract.Contract, Enforcement: binding.Contract.Actual,
					Payload: json.RawMessage(item.Text), Native: binding.Native,
				}}, true
			})
		}
	case "reasoning":
		if started {
			return
		}
		text := strings.Join(item.Summary, "\n")
		availability := provider.ReasoningUnavailable
		if text != "" {
			availability = provider.ReasoningAvailable
		}
		s.emit(provider.ReasoningSummaryEvent{Availability: availability, Update: s.textSnapshot(item.ID, "summary", text)})
	case "commandExecution":
		commandID := provider.NativeCommandID(item.ID)
		if started {
			s.emit(provider.CommandStartedEvent{CommandID: commandID, Display: boundedtext.String(item.Command, maxNativeTextBytes)})
			return
		}
		exit := provider.None[int]()
		if item.ExitCode != nil {
			exit = provider.Some(*item.ExitCode)
		}
		// CommandCompletedEvent has no outcome field. A failed command with an
		// exit code is fully described by it; a declined command never ran and
		// a failure without an exit code is otherwise indistinguishable from
		// success, so both carry a warning with the native status.
		switch item.Status {
		case "declined":
			exit = provider.None[int]()
			s.emit(provider.ProtocolWarningEvent{Code: "command_declined", Detail: provider.BoundedText{Text: "native command was declined and did not run"}, OffendingNativeKind: method})
		case "failed":
			if item.ExitCode == nil {
				s.emit(provider.ProtocolWarningEvent{Code: "command_failed", Detail: provider.BoundedText{Text: "native command failed without an exit code"}, OffendingNativeKind: method})
			}
		}
		s.emit(provider.CommandCompletedEvent{CommandID: commandID, ExitCode: exit, Output: boundedtext.String(item.AggregatedOutput, maxNativeTextBytes)})
	case "fileChange":
		if started {
			return
		}
		// Only a completed patch is file activity. Failed, declined or still
		// in-progress patches were not applied; report them without paths. An
		// absent status predates the field and keeps the archived behaviour.
		if item.Status != "" && item.Status != "completed" {
			s.emit(provider.ProtocolWarningEvent{
				Code: "file_change_not_applied", Detail: boundedtext.String("native file change status "+item.Status, 256),
				OffendingNativeKind: method,
			})
			return
		}
		if len(item.Changes) == 0 {
			s.emit(provider.FileActivityEvent{Operation: provider.FileUnknown})
			return
		}
		for _, change := range item.Changes {
			s.emit(provider.FileActivityEvent{Operation: fileOperation(string(change.Kind)), NativePath: change.Path})
		}
	}
}

type textDeltaNotification struct {
	ThreadID     string `json:"threadId"`
	TurnID       string `json:"turnId"`
	ItemID       string `json:"itemId"`
	Delta        string `json:"delta"`
	SummaryIndex int    `json:"summaryIndex"`
}

func (s *session) normalizeAgentMessageDelta(raw json.RawMessage) {
	var message textDeltaNotification
	if json.Unmarshal(raw, &message) != nil || message.ItemID == "" || !s.matchesActive(message.ThreadID, message.TurnID) {
		s.warnNativeOnce("item/agentMessage/delta:uncorrelated", provider.ProtocolWarningEvent{Code: "uncorrelated_text_delta", Detail: provider.BoundedText{Text: "ignored malformed or foreign assistant text delta"}, OffendingNativeKind: "item/agentMessage/delta"})
		return
	}
	s.emit(provider.AssistantTextEvent{Phase: provider.AssistantPhaseUnknown, Update: s.textDelta(message.ItemID, "text", message.Delta)})
}

func (s *session) normalizeReasoningDelta(raw json.RawMessage) {
	var message textDeltaNotification
	if json.Unmarshal(raw, &message) != nil || message.ItemID == "" || !s.matchesActive(message.ThreadID, message.TurnID) {
		s.warnNativeOnce("item/reasoning/summaryTextDelta:uncorrelated", provider.ProtocolWarningEvent{Code: "uncorrelated_reasoning_delta", Detail: provider.BoundedText{Text: "ignored malformed or foreign reasoning summary delta"}, OffendingNativeKind: "item/reasoning/summaryTextDelta"})
		return
	}
	segment := "summary-" + strconv.Itoa(message.SummaryIndex)
	s.emit(provider.ReasoningSummaryEvent{Availability: provider.ReasoningAvailable, Update: s.textDelta(message.ItemID, segment, message.Delta)})
}

func (s *session) normalizeUsage(raw json.RawMessage) {
	var message struct {
		ThreadID   string `json:"threadId"`
		TurnID     string `json:"turnId"`
		TokenUsage struct {
			Total nativeTokenBreakdown `json:"total"`
			Last  nativeTokenBreakdown `json:"last"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	var event provider.UsageEvent
	publish, regressed := false, false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if message.ThreadID != "" && message.ThreadID != s.nativeThread {
			return nil
		}
		active := message.TurnID != "" && s.active.Present && message.TurnID == string(s.active.Value.AdapterTurnID)
		counters, measurement, wentBack := s.usage.observe(message.TokenUsage.Total, message.TokenUsage.Last, active)
		if !active {
			return nil
		}
		model := s.model
		if s.reroutedModel.Present {
			model = s.reroutedModel.Value
		}
		event = provider.UsageEvent{
			Scope: provider.UsagePrimaryTurn, ScopeID: message.TurnID,
			Measurement: measurement, Model: model, Counters: counters,
		}
		publish, regressed = true, wentBack
		return nil
	})
	if regressed {
		s.warnNativeOnce("usage_total_regressed", provider.ProtocolWarningEvent{
			Code: "usage_total_regressed", Detail: provider.BoundedText{Text: "thread token total fell below the turn baseline; reporting the last response usage"},
			OffendingNativeKind: "thread/tokenUsage/updated",
		})
	}
	if publish {
		s.emit(event)
	}
}

// maxNativeErrorBytes bounds native error and warning text in one event.
const maxNativeErrorBytes = 4 * 1024

// nativeTurnError mirrors Codex TurnError. codexErrorInfo is either a string
// code or an object with one variant key (for example httpConnectionFailed).
type nativeTurnError struct {
	Message        string          `json:"message"`
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
}

func (e nativeTurnError) detail() provider.BoundedText {
	text := e.Message
	if kind := codexErrorKind(e.CodexErrorInfo); kind != "" {
		text = kind + ": " + text
	}
	return boundedtext.String(text, maxNativeErrorBytes)
}

func codexErrorKind(raw json.RawMessage) string {
	var code string
	if json.Unmarshal(raw, &code) == nil {
		return code
	}
	var variant map[string]json.RawMessage
	if json.Unmarshal(raw, &variant) == nil && len(variant) == 1 {
		for key := range variant {
			return key
		}
	}
	return ""
}

// normalizeError handles the native `error` notification. A retried error is
// only a notice, once per turn. A final error is reported once per turn; the
// same reason on the following turn/completed is not repeated.
func (s *session) normalizeError(raw json.RawMessage) {
	var message struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		Error     nativeTurnError `json:"error"`
		WillRetry bool            `json:"willRetry"`
	}
	if json.Unmarshal(raw, &message) != nil || !s.matchesThread(message.ThreadID) {
		s.warnNativeOnce("error:invalid", provider.ProtocolWarningEvent{Code: "uncorrelated_error", Detail: provider.BoundedText{Text: "ignored malformed or foreign error notification"}, OffendingNativeKind: "error"})
		return
	}
	if message.WillRetry {
		s.warnNativeOnce("error:retry:"+message.TurnID, provider.ProtocolWarningEvent{
			Code: "native_error_retrying", Detail: message.Error.detail(), OffendingNativeKind: "error",
		})
		return
	}
	s.reportTurnError(message.TurnID, "error", message.Error)
}

// reportTurnError publishes a native turn failure reason at most once per turn.
func (s *session) reportTurnError(turnID, nativeKind string, failure nativeTurnError) {
	s.warnNativeOnce("turn_error:"+turnID, provider.ProtocolWarningEvent{
		Code: "native_turn_error", Detail: failure.detail(), OffendingNativeKind: nativeKind,
	})
}

// normalizeModelRerouted records that Codex served the turn with another
// model. Usage reported afterwards is attributed to the new model until the
// next turn/start names the configured model again.
func (s *session) normalizeModelRerouted(raw json.RawMessage) {
	var message struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		FromModel string `json:"fromModel"`
		ToModel   string `json:"toModel"`
		Reason    string `json:"reason"`
	}
	if json.Unmarshal(raw, &message) != nil || message.ToModel == "" || !s.matchesThread(message.ThreadID) {
		s.warnNativeOnce("model/rerouted:invalid", provider.ProtocolWarningEvent{Code: "uncorrelated_model_reroute", Detail: provider.BoundedText{Text: "ignored malformed or foreign model/rerouted"}, OffendingNativeKind: "model/rerouted"})
		return
	}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.active.Present && message.TurnID == string(s.active.Value.AdapterTurnID) {
			model := s.model
			model.ModelID = message.ToModel
			s.reroutedModel = provider.Some(model)
		}
		return nil
	})
	s.emit(provider.ProtocolWarningEvent{
		Code:                "model_rerouted",
		Detail:              boundedtext.String("model rerouted from "+message.FromModel+" to "+message.ToModel+" ("+message.Reason+")", maxNativeErrorBytes),
		OffendingNativeKind: "model/rerouted",
	})
}

// normalizeConfigWarning surfaces a Codex configuration warning, once per
// distinct summary.
func (s *session) normalizeConfigWarning(raw json.RawMessage) {
	var message struct {
		Summary string  `json:"summary"`
		Details *string `json:"details"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	text := message.Summary
	if message.Details != nil && *message.Details != "" {
		text += ": " + *message.Details
	}
	s.warnNativeOnce("configWarning:"+boundedtext.String(message.Summary, 256).Text, provider.ProtocolWarningEvent{
		Code: "native_config_warning", Detail: boundedtext.String(text, maxNativeErrorBytes), OffendingNativeKind: "configWarning",
	})
}

func (s *session) currentEnforcement() provider.SchemaEnforcement {
	var enforcement provider.SchemaEnforcement
	_ = lockscope.WithMutexLock(&s.mu, func() error { enforcement = s.contract.Actual; return nil })
	return enforcement
}

func (s *session) textSnapshot(item, segment, text string) provider.TextSegmentUpdate {
	bounded := boundedtext.String(text, maxNativeTextBytes)
	key := item + "\x00" + segment
	var version uint64
	_ = lockscope.WithMutexLock(&s.textMu, func() error {
		version = s.textVersions[key] + 1
		s.textVersions[key] = version
		s.textBytes[key] = bounded.OriginalBytes.Value
		return nil
	})
	return provider.TextSegmentUpdate{
		ItemID: provider.ItemID(item), SegmentID: provider.SegmentID(segment), Text: bounded.Text,
		Mode: provider.UpdateSnapshot, BaseVersion: provider.None[uint64](), Version: version,
		Truncated: bounded.Truncated, OriginalBytes: bounded.OriginalBytes,
	}
}

func (s *session) textDelta(item, segment, text string) provider.TextSegmentUpdate {
	bounded := boundedtext.String(text, maxNativeTextBytes)
	key := item + "\x00" + segment
	var base, offset, version uint64
	_ = lockscope.WithMutexLock(&s.textMu, func() error {
		base = s.textVersions[key]
		offset = s.textBytes[key]
		version = base + 1
		s.textVersions[key] = version
		s.textBytes[key] = offset + bounded.OriginalBytes.Value
		return nil
	})
	return provider.TextSegmentUpdate{
		ItemID: provider.ItemID(item), SegmentID: provider.SegmentID(segment), Text: bounded.Text,
		Mode: provider.UpdateDelta, BaseVersion: provider.Some(base), Version: version,
		ByteOffset: offset, Truncated: bounded.Truncated, OriginalBytes: bounded.OriginalBytes,
	}
}

func (s *session) matchesThread(native string) bool {
	var matches bool
	_ = lockscope.WithMutexLock(&s.mu, func() error { matches = native == "" || native == s.nativeThread; return nil })
	return matches
}

func (s *session) matchesActive(threadID, turnID string) bool {
	var matches bool
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		matches = (threadID == "" || threadID == s.nativeThread) && turnID != "" && s.active.Present && turnID == string(s.active.Value.AdapterTurnID)
		return nil
	})
	return matches
}

func (s *session) warnNativeOnce(key string, warning provider.ProtocolWarningEvent) {
	var exists bool
	_ = lockscope.WithMutexLock(&s.warnMu, func() error {
		_, exists = s.warnedNative[key]
		if !exists {
			s.warnedNative[key] = struct{}{}
		}
		return nil
	})
	if !exists {
		s.emit(warning)
	}
}

func optionalNativeTurn(value string) provider.Optional[provider.NativeTurnID] {
	if value == "" {
		return provider.None[provider.NativeTurnID]()
	}
	return provider.Some(provider.NativeTurnID(value))
}

func fileOperation(value string) provider.FileOperation {
	switch strings.ToLower(value) {
	case "add", "create", "added":
		return provider.FileCreate
	case "delete", "deleted", "remove":
		return provider.FileDelete
	case "update", "modified", "modify":
		return provider.FileUpdate
	default:
		return provider.FileUnknown
	}
}
