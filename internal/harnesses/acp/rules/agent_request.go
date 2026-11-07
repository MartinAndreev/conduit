package rules

// AgentRequestKind is the automatic answer policy for an agent→client request.
type AgentRequestKind string

const (
	AgentRequestAnswerPermission   AgentRequestKind = "answer_permission"
	AgentRequestDeclineElicitation AgentRequestKind = "decline_elicitation"
	AgentRequestReject             AgentRequestKind = "reject"
)

type AgentRequestFacts struct {
	Method string
}

type AgentRequestDecision struct {
	Kind   AgentRequestKind
	Reason string
}

// DecideAgentRequest answers every agent request without a human round-trip.
// fs/* and terminal/* are rejected because the client advertises neither
// capability; unknown methods are rejected as well.
func DecideAgentRequest(f AgentRequestFacts) AgentRequestDecision {
	switch f.Method {
	case "session/request_permission":
		return AgentRequestDecision{
			Kind:   AgentRequestAnswerPermission,
			Reason: "permission requests are answered automatically under the deny policy",
		}
	case "elicitation/create":
		return AgentRequestDecision{
			Kind:   AgentRequestDeclineElicitation,
			Reason: "questions are declined; question round-trip is unsupported",
		}
	default:
		return AgentRequestDecision{
			Kind:   AgentRequestReject,
			Reason: "agent request is not supported by this client",
		}
	}
}

// PermissionOption is one native option offered by session/request_permission.
type PermissionOption struct {
	OptionID string
	Kind     string
}

type PermissionAnswerKind string

const (
	PermissionAnswerRejectOnce PermissionAnswerKind = "reject_once"
	PermissionAnswerCancelled  PermissionAnswerKind = "cancelled"
)

type PermissionAnswerFacts struct {
	Options []PermissionOption
}

// PermissionAnswerDecision carries the option to select when Kind is
// PermissionAnswerRejectOnce.
type PermissionAnswerDecision struct {
	Kind     PermissionAnswerKind
	OptionID string
	Reason   string
}

// DecidePermissionAnswer selects the first reject_once option. Without one, the
// answer is the cancelled outcome; an allow option is never selected.
func DecidePermissionAnswer(f PermissionAnswerFacts) PermissionAnswerDecision {
	for _, option := range f.Options {
		if option.Kind == "reject_once" && option.OptionID != "" {
			return PermissionAnswerDecision{
				Kind:     PermissionAnswerRejectOnce,
				OptionID: option.OptionID,
				Reason:   "permission request rejected once under the deny policy",
			}
		}
	}
	return PermissionAnswerDecision{
		Kind:   PermissionAnswerCancelled,
		Reason: "no reject_once option was offered; answered cancelled",
	}
}
