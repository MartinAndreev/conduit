package codex

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

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
	case "thread/name/updated", "thread/compacted", "turn/plan/updated", "account/rateLimits/updated", "model/rerouted":
		// Known administrative observations are deliberately not part of the
		// current provider event contract.
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
	default:
		return
	}
	var previous provider.SessionState
	_ = lockscope.WithMutexLock(&s.mu, func() error { previous = s.state; s.state = next; return nil })
	s.emit(provider.SessionStateEvent{Previous: previous, Current: next, NativeStatus: message.Status.Type})
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
			ID     string `json:"id"`
			Status string `json:"status"`
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
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
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
			s.emit(provider.CommandStartedEvent{CommandID: commandID, Display: provider.BoundedText{Text: item.Command}})
			return
		}
		exit := provider.None[int]()
		if item.ExitCode != nil {
			exit = provider.Some(*item.ExitCode)
		}
		s.emit(provider.CommandCompletedEvent{CommandID: commandID, ExitCode: exit, Output: provider.BoundedText{Text: item.AggregatedOutput}})
	case "fileChange":
		if started {
			return
		}
		if len(item.Changes) == 0 {
			s.emit(provider.FileActivityEvent{Operation: provider.FileUnknown})
			return
		}
		for _, change := range item.Changes {
			s.emit(provider.FileActivityEvent{Operation: fileOperation(change.Kind), NativePath: change.Path})
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
		TurnID     string `json:"turnId"`
		TokenUsage struct {
			Total struct {
				InputTokens           uint64 `json:"inputTokens"`
				OutputTokens          uint64 `json:"outputTokens"`
				ReasoningOutputTokens uint64 `json:"reasoningOutputTokens"`
				CachedInputTokens     uint64 `json:"cachedInputTokens"`
				CacheWriteInputTokens uint64 `json:"cacheWriteInputTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	if message.TurnID != "" && !s.matchesActive("", message.TurnID) {
		return
	}
	total := message.TokenUsage.Total
	s.emit(provider.UsageEvent{
		Scope: provider.UsagePrimaryTurn, ScopeID: message.TurnID,
		Measurement: provider.UsageCumulativeSnapshot, Model: s.model,
		Counters: provider.UsageCounters{
			InputTokens: provider.Some(total.InputTokens), OutputTokens: provider.Some(total.OutputTokens),
			ReasoningTokens: provider.Some(total.ReasoningOutputTokens), CacheReadTokens: provider.Some(total.CachedInputTokens),
			CacheWriteTokens: provider.Some(total.CacheWriteInputTokens),
		},
	})
}

func (s *session) currentEnforcement() provider.SchemaEnforcement {
	var enforcement provider.SchemaEnforcement
	_ = lockscope.WithMutexLock(&s.mu, func() error { enforcement = s.contract.Actual; return nil })
	return enforcement
}

func (s *session) textSnapshot(item, segment, text string) provider.TextSegmentUpdate {
	bounded := boundedtext.String(text, 64*1024)
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
	bounded := boundedtext.String(text, 64*1024)
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
