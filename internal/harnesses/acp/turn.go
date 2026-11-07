package acp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

func promptBlocks(parts []provider.InputPart) ([]contentBlock, error) {
	blocks := make([]contentBlock, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Kind == provider.InputInstruction || part.Kind == provider.InputContext:
			blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
		case part.Kind == provider.InputArtifactRef && part.URI != "":
			// resource_link is part of every ACP agent's baseline prompt support.
			blocks = append(blocks, contentBlock{Type: "resource_link", URI: part.URI, Name: part.URI, MimeType: part.MediaType})
		default:
			return nil, adaptererror.New(provider.OperationStartTurn, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, nil, "input part kind is unsupported")
		}
	}
	if len(blocks) == 0 {
		return nil, adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, nil, "turn input is empty")
	}
	return blocks, nil
}

// StartTurn sends session/prompt and returns once the request is written. The
// turn stays active until the session/prompt response, which is its native
// terminal; StartTurn does not wait for it.
func (s *session) StartTurn(ctx context.Context, request provider.StartTurnRequest) (provider.StartTurnResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.StartTurnResult{}, adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, false, err)
	}
	actual, err := negotiateResultContract(provider.OperationStartTurn, request.ResultContract)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	blocks, err := promptBlocks(request.Input)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	turnID, err := s.config.NewID(s.descriptor.ProviderName + "-turn")
	if err != nil {
		return provider.StartTurnResult{}, adaptererror.New(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, err, "could not allocate adapter turn ID")
	}
	turn := provider.TurnRef{AdapterTurnID: provider.AdapterTurnID(turnID)}
	native, err := s.reserveTurn(request, turn, actual)
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	params := struct {
		SessionID string         `json:"sessionId"`
		Prompt    []contentBlock `json:"prompt"`
	}{SessionID: native, Prompt: blocks}
	sent, sendErr := s.rpc.send(ctx, "session/prompt", params, func(reply rpcResponse) { s.handlePromptReply(turn, reply) })
	if err := s.settleTurnDispatch(request, turn, sent, sendErr); err != nil {
		return provider.StartTurnResult{}, err
	}
	return provider.StartTurnResult{
		Turn: turn, Assignment: request.Assignment, TargetingStrength: provider.TargetSessionLease,
		ResultEnforcement: actual,
		Native:            provider.NativeIDs{Session: optionalNativeSession(native)},
	}, nil
}

// reserveTurn evaluates eligibility and reserves the turn in one critical
// section, so concurrent starts cannot both send session/prompt.
func (s *session) reserveTurn(request provider.StartTurnRequest, turn provider.TurnRef, actual provider.SchemaEnforcement) (string, error) {
	var native string
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideStartTurn(rules.StartTurnFacts{
			SessionMatches:            request.Session == s.ref,
			AssignmentIdentityMatches: request.Assignment.SameIdentity(s.assignment),
			Closing:                   s.closing,
			SessionLost:               s.state == provider.SessionLost,
			TurnUnresolved:            s.turn != nil,
		})
		switch decision.Kind {
		case rules.StartTurnBindingDiffers, rules.StartTurnOccupied:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		case rules.StartTurnUnavailable:
			return adaptererror.New(provider.OperationStartTurn, provider.ErrorSessionLost, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		s.turn = &turnState{ref: turn, phase: provider.TurnStarting}
		s.assignment = request.Assignment
		s.contract = provider.ResultContractNegotiation{Contract: request.ResultContract, Actual: actual}
		native = s.nativeSession
		return nil
	})
	return native, err
}

// settleTurnDispatch records the dispatch outcome. A proven unsent prompt
// releases the reservation. A written prompt becomes active. A possibly sent
// prompt stays unresolved until its reply or transport loss. A reply that
// arrived before settlement is processed now, after the activation event.
func (s *session) settleTurnDispatch(request provider.StartTurnRequest, turn provider.TurnRef, sent bool, sendErr error) error {
	var deferred *rpcResponse
	var previous provider.TurnState
	activated := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		current := s.turn
		if current == nil || current.ref != turn {
			return nil
		}
		current.dispatchSettled = true
		if !sent {
			s.turn = nil
			return nil
		}
		if sendErr == nil && current.phase == provider.TurnStarting {
			current.phase = provider.TurnActive
			activated = true
		}
		deferred = current.deferred
		current.deferred = nil
		if deferred != nil {
			previous = current.phase
			s.turn = nil
		}
		return nil
	})
	if activated {
		s.emitTurn(turn, provider.TurnStateEvent{Previous: provider.TurnStarting, Current: provider.TurnActive})
	}
	if deferred != nil {
		s.finishTurn(turn, previous, *deferred)
	}
	if sendErr != nil {
		return adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, sent, sendErr)
	}
	return nil
}

// handlePromptReply runs on the reader goroutine (or on transport stop). It
// ends the turn it belongs to; a reply for another turn is ignored. A native
// reply racing StartTurn is deferred until the activation is published; a
// transport failure is published at once, before the stream ends with
// session_lost.
func (s *session) handlePromptReply(turn provider.TurnRef, reply rpcResponse) {
	var transportErr *transportError
	transportStopped := errors.As(reply.err, &transportErr)
	var previous provider.TurnState
	finish := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		current := s.turn
		if current == nil || current.ref != turn {
			return nil
		}
		if !current.dispatchSettled && !transportStopped {
			current.deferred = &reply
			return nil
		}
		previous = current.phase
		s.turn = nil
		finish = true
		return nil
	})
	if finish {
		s.finishTurn(turn, previous, reply)
	}
}

type promptResult struct {
	StopReason string       `json:"stopReason"`
	Usage      *promptUsage `json:"usage"`
}

// promptUsage accepts the ACP field names and fx's reasoningTokens alias.
type promptUsage struct {
	InputTokens       *uint64 `json:"inputTokens"`
	OutputTokens      *uint64 `json:"outputTokens"`
	ThoughtTokens     *uint64 `json:"thoughtTokens"`
	ReasoningTokens   *uint64 `json:"reasoningTokens"`
	CachedReadTokens  *uint64 `json:"cachedReadTokens"`
	CachedWriteTokens *uint64 `json:"cachedWriteTokens"`
}

// finishTurn publishes the native terminal of one turn: usage and warnings
// first, then the turn state. Transport loss reports a lost turn instead.
func (s *session) finishTurn(turn provider.TurnRef, previous provider.TurnState, reply rpcResponse) {
	var transportErr *transportError
	var nativeErr *rpcError
	var result promptResult
	decoded := reply.err == nil && json.Unmarshal(reply.result, &result) == nil
	decision := rules.DecidePromptOutcome(rules.PromptOutcomeFacts{
		TransportStopped: errors.As(reply.err, &transportErr),
		NativeError:      errors.As(reply.err, &nativeErr),
		ResultDecoded:    decoded,
		StopReason:       result.StopReason,
	})
	s.resetTurnObservations()
	switch decision.Kind {
	case rules.PromptTransportLost:
		s.emitTurn(turn, provider.TurnStateEvent{Previous: previous, Current: provider.TurnLost, TerminalReason: "transport_lost"})
		return
	case rules.PromptFailed:
		s.emitTurn(turn, provider.ProtocolWarningEvent{Code: "native_turn_error", Detail: nativeErrorDetail(nativeErr), OffendingNativeKind: "session/prompt"})
		s.emitTurn(turn, provider.TurnStateEvent{Previous: previous, Current: provider.TurnNativeTerminal, TerminalReason: "error"})
		return
	case rules.PromptMalformed:
		s.emitTurn(turn, provider.ProtocolWarningEvent{Code: "malformed_prompt_response", Detail: provider.BoundedText{Text: decision.Reason}, OffendingNativeKind: "session/prompt"})
		s.emitTurn(turn, provider.TurnStateEvent{Previous: previous, Current: provider.TurnNativeTerminal, TerminalReason: "unknown"})
		return
	case rules.PromptCompletedWithWarning:
		s.emitTurn(turn, provider.ProtocolWarningEvent{Code: "native_stop_reason", Detail: boundedtext.String(result.StopReason, maxNativeErrorBytes), OffendingNativeKind: "session/prompt"})
	}
	if usage, ok := s.turnUsage(turn, result.Usage); ok {
		s.emitTurn(turn, usage)
	}
	s.emitTurn(turn, provider.TurnStateEvent{Previous: previous, Current: provider.TurnNativeTerminal, TerminalReason: result.StopReason})
}

// nativeErrorDetail bounds the JSON-RPC error message and, when present, the
// agent's data.message diagnostic.
func nativeErrorDetail(nativeErr *rpcError) provider.BoundedText {
	if nativeErr == nil {
		return provider.BoundedText{Text: "native error"}
	}
	text := nativeErr.Message
	var data struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(nativeErr.Data, &data) == nil && data.Message != "" {
		text += ": " + data.Message
	}
	return boundedtext.String(text, maxNativeErrorBytes)
}

func (s *session) turnUsage(turn provider.TurnRef, usage *promptUsage) (provider.UsageEvent, bool) {
	if usage == nil {
		return provider.UsageEvent{}, false
	}
	reasoning := usage.ThoughtTokens
	if reasoning == nil {
		reasoning = usage.ReasoningTokens
	}
	counters := provider.UsageCounters{
		InputTokens: optionalCount(usage.InputTokens), OutputTokens: optionalCount(usage.OutputTokens),
		ReasoningTokens: optionalCount(reasoning), CacheReadTokens: optionalCount(usage.CachedReadTokens),
		CacheWriteTokens: optionalCount(usage.CachedWriteTokens),
	}
	if !counters.InputTokens.Present && !counters.OutputTokens.Present && !counters.ReasoningTokens.Present &&
		!counters.CacheReadTokens.Present && !counters.CacheWriteTokens.Present {
		return provider.UsageEvent{}, false
	}
	var model provider.ModelSelection
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		model = s.model
		return nil
	})
	return provider.UsageEvent{
		Scope: provider.UsagePrimaryTurn, ScopeID: string(turn.AdapterTurnID),
		Measurement: provider.UsageCumulativeSnapshot, Model: model, Counters: counters,
	}, true
}

func optionalCount(value *uint64) provider.Optional[uint64] {
	if value == nil {
		return provider.None[uint64]()
	}
	return provider.Some(*value)
}

// InterruptTurn sends the session/cancel notification. Its receipt only says
// the notification was written; the turn ends with the session/prompt reply.
func (s *session) InterruptTurn(ctx context.Context, request provider.InterruptRequest) (provider.InterruptResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.InterruptResult{}, adaptererror.Delivery(provider.OperationInterruptTurn, request.RequestID, false, err)
	}
	var native string
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideInterrupt(rules.InterruptFacts{
			SessionMatches:    request.Session == s.ref,
			AssignmentMatches: request.Assignment == s.assignment,
			ActiveTurn:        s.turn != nil,
			ActiveTurnMatches: s.turn != nil && s.turn.ref == request.ExpectedActiveTurn,
		})
		if decision.Kind != rules.InterruptAllowed {
			return adaptererror.New(provider.OperationInterruptTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, nil, decision.Reason)
		}
		native = s.nativeSession
		return nil
	})
	if err != nil {
		return provider.InterruptResult{}, err
	}
	sent, err := s.sendCancel(ctx, native)
	if err != nil {
		return provider.InterruptResult{}, adaptererror.Delivery(provider.OperationInterruptTurn, request.RequestID, sent, err)
	}
	s.markInterruptRequested(request.ExpectedActiveTurn)
	return provider.InterruptResult{
		StopOutcome: provider.StopUnknown, ObservedUsability: provider.SessionUsabilityUnknown,
		Quiescence: provider.QuiescenceUnknown,
	}, nil
}

func (s *session) sendCancel(ctx context.Context, native string) (bool, error) {
	return s.rpc.notify(ctx, "session/cancel", struct {
		SessionID string `json:"sessionId"`
	}{SessionID: native})
}

// markInterruptRequested records the cancel for a turn that is still
// unresolved; a turn already ended by its reply is left alone.
func (s *session) markInterruptRequested(turn provider.TurnRef) {
	var previous provider.TurnState
	marked := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.turn == nil || s.turn.ref != turn || s.turn.phase == provider.TurnInterruptRequested {
			return nil
		}
		previous = s.turn.phase
		s.turn.phase = provider.TurnInterruptRequested
		marked = true
		return nil
	})
	if marked {
		s.emitTurn(turn, provider.TurnStateEvent{Previous: previous, Current: provider.TurnInterruptRequested})
	}
}
