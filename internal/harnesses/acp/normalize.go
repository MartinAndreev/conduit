package acp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func (s *session) handleNotification(method string, params json.RawMessage) {
	if method == "session/update" {
		s.handleSessionUpdate(params)
		return
	}
	if slices.Contains(s.descriptor.RecognizedNotifications, method) {
		return
	}
	s.warnOnce("notification:"+method, provider.ProtocolWarningEvent{
		Code: "unknown_notification", Detail: provider.BoundedText{Text: "unsupported native notification"},
		OffendingNativeKind: method,
	})
}

type sessionUpdateNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

func (s *session) handleSessionUpdate(raw json.RawMessage) {
	var notification sessionUpdateNotification
	var kind struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if json.Unmarshal(raw, &notification) != nil || json.Unmarshal(notification.Update, &kind) != nil || kind.SessionUpdate == "" {
		s.warnOnce("session_update:invalid", provider.ProtocolWarningEvent{Code: "invalid_session_update", Detail: provider.BoundedText{Text: "ignored malformed session/update"}, OffendingNativeKind: "session/update"})
		return
	}
	if native := s.nativeSessionID(); native == "" || notification.SessionID != native {
		s.warnOnce("session_update:uncorrelated", provider.ProtocolWarningEvent{Code: "uncorrelated_session_update", Detail: provider.BoundedText{Text: "ignored session/update for another session"}, OffendingNativeKind: kind.SessionUpdate})
		return
	}
	decision := rules.DecideSessionUpdate(rules.SessionUpdateFacts{SessionUpdate: kind.SessionUpdate})
	switch decision.Kind {
	case rules.SessionUpdateText, rules.SessionUpdateReasoning:
		s.normalizeChunk(decision.Kind, kind.SessionUpdate, notification.Update)
	case rules.SessionUpdateTool:
		s.normalizeToolCall(kind.SessionUpdate, notification.Update)
	case rules.SessionUpdateUsage:
		s.normalizeUsageUpdate(notification.Update)
	case rules.SessionUpdateEcho, rules.SessionUpdateAdministrative:
	default:
		s.warnOnce("session_update:"+kind.SessionUpdate, provider.ProtocolWarningEvent{
			Code: "unknown_session_update", Detail: provider.BoundedText{Text: decision.Reason},
			OffendingNativeKind: kind.SessionUpdate,
		})
	}
}

type chunkUpdate struct {
	MessageID string `json:"messageId"`
	Content   struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (s *session) normalizeChunk(kind rules.SessionUpdateKind, nativeKind string, raw json.RawMessage) {
	var chunk chunkUpdate
	if json.Unmarshal(raw, &chunk) != nil {
		return
	}
	if chunk.Content.Type != "text" {
		s.warnOnce("content:"+nativeKind+":"+chunk.Content.Type, provider.ProtocolWarningEvent{
			Code: "unsupported_content_block", Detail: boundedtext.String(chunk.Content.Type, 128),
			OffendingNativeKind: nativeKind,
		})
		return
	}
	item := s.chunkItemID(nativeKind, chunk.MessageID)
	if kind == rules.SessionUpdateText {
		s.emit(provider.AssistantTextEvent{Phase: provider.AssistantPhaseUnknown, Update: s.textDelta(item, "text", chunk.Content.Text)})
		return
	}
	s.emit(provider.ReasoningSummaryEvent{Availability: provider.ReasoningAvailable, Update: s.textDelta(item, "thought", chunk.Content.Text)})
}

// chunkItemID prefers the native messageId. Without one, chunks of a kind are
// one item per turn (or per session when no turn is active).
func (s *session) chunkItemID(nativeKind, messageID string) string {
	if messageID != "" {
		return "message:" + messageID
	}
	scope := "session"
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.turn != nil {
			scope = "turn:" + string(s.turn.bound.ref.AdapterTurnID)
		}
		return nil
	})
	return scope + ":" + nativeKind
}

type usageUpdate struct {
	Used *uint64 `json:"used"`
	Size *uint64 `json:"size"`
	Cost *struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	} `json:"cost"`
}

// normalizeUsageUpdate reports the session-wide usage observation. ACP's
// used/size describe the context window, which the provider counters cannot
// express, so token counters stay unknown; only a native cost is carried.
func (s *session) normalizeUsageUpdate(raw json.RawMessage) {
	var update usageUpdate
	if json.Unmarshal(raw, &update) != nil {
		return
	}
	var counters provider.UsageCounters
	if update.Cost != nil {
		counters.Cost = provider.Some(update.Cost.Amount)
		counters.Currency = update.Cost.Currency
	}
	var model provider.ModelSelection
	var native string
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		model = s.model
		native = s.nativeSession
		return nil
	})
	s.emit(provider.UsageEvent{
		Scope: provider.UsageOther, ScopeID: "session:" + native,
		Measurement: provider.UsageCumulativeSnapshot, Model: model, Counters: counters,
	})
}

// handleAgentRequest answers every agent→client request automatically on the
// reader goroutine. The answer write is bounded by StopGrace; a failed write
// stops the transport, which loses the session visibly.
func (s *session) handleAgentRequest(id json.RawMessage, method string, params json.RawMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.StopGrace)
	defer cancel()
	decision := rules.DecideAgentRequest(rules.AgentRequestFacts{Method: method})
	switch decision.Kind {
	case rules.AgentRequestAnswerPermission:
		s.answerPermission(ctx, id, params)
	case rules.AgentRequestDeclineElicitation:
		_ = s.rpc.respond(ctx, id, struct {
			Action string `json:"action"`
		}{Action: "decline"})
		s.emit(provider.ProtocolWarningEvent{Code: "elicitation_declined", Detail: provider.BoundedText{Text: decision.Reason}, OffendingNativeKind: method})
	default:
		_ = s.rpc.respondError(ctx, id, -32601, "method not supported by this client")
		s.warnOnce("agent_request:"+method, provider.ProtocolWarningEvent{
			Code: "unsupported_agent_request", Detail: provider.BoundedText{Text: decision.Reason},
			OffendingNativeKind: method,
		})
	}
}

type permissionRequest struct {
	ToolCall struct {
		ToolCallID string `json:"toolCallId"`
		Title      string `json:"title"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

func (s *session) answerPermission(ctx context.Context, id json.RawMessage, params json.RawMessage) {
	var request permissionRequest
	_ = json.Unmarshal(params, &request)
	options := make([]rules.PermissionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, rules.PermissionOption{OptionID: option.OptionID, Kind: option.Kind})
	}
	decision := rules.DecidePermissionAnswer(rules.PermissionAnswerFacts{Options: options})
	type outcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId,omitempty"`
	}
	answer := struct {
		Outcome outcome `json:"outcome"`
	}{Outcome: outcome{Outcome: "cancelled"}}
	code := "permission_auto_cancelled"
	if decision.Kind == rules.PermissionAnswerRejectOnce {
		answer.Outcome = outcome{Outcome: "selected", OptionID: decision.OptionID}
		code = "permission_auto_rejected"
	}
	_ = s.rpc.respond(ctx, id, answer)
	detail := decision.Reason
	if request.ToolCall.Title != "" {
		detail += ": " + request.ToolCall.Title
	}
	s.emit(provider.ProtocolWarningEvent{Code: code, Detail: boundedtext.String(detail, maxNativeErrorBytes), OffendingNativeKind: "session/request_permission"})
}

// joinText concatenates text parts with a newline separator.
func joinText(parts []string) string {
	return strings.Join(parts, "\n")
}
