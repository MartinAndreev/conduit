package rules

import "testing"

func TestDecidePromptOutcome(t *testing.T) {
	cases := []struct {
		name  string
		facts PromptOutcomeFacts
		want  PromptOutcomeKind
	}{
		{"end_turn completes", PromptOutcomeFacts{ResultDecoded: true, StopReason: "end_turn"}, PromptCompleted},
		{"cancelled", PromptOutcomeFacts{ResultDecoded: true, StopReason: "cancelled"}, PromptCancelled},
		{"max_tokens warns", PromptOutcomeFacts{ResultDecoded: true, StopReason: "max_tokens"}, PromptCompletedWithWarning},
		{"refusal warns", PromptOutcomeFacts{ResultDecoded: true, StopReason: "refusal"}, PromptCompletedWithWarning},
		{"max_turn_requests warns", PromptOutcomeFacts{ResultDecoded: true, StopReason: "max_turn_requests"}, PromptCompletedWithWarning},
		{"unknown stop reason warns", PromptOutcomeFacts{ResultDecoded: true, StopReason: "later"}, PromptCompletedWithWarning},
		{"native error fails", PromptOutcomeFacts{NativeError: true}, PromptFailed},
		{"undecodable result", PromptOutcomeFacts{}, PromptMalformed},
		{"transport precedes native error", PromptOutcomeFacts{TransportStopped: true, NativeError: true}, PromptTransportLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecidePromptOutcome(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecidePermissionAnswer(t *testing.T) {
	offered := []PermissionOption{{OptionID: "allow", Kind: "allow_once"}, {OptionID: "always", Kind: "allow_always"}, {OptionID: "no", Kind: "reject_once"}, {OptionID: "never", Kind: "reject_always"}}
	if got := DecidePermissionAnswer(PermissionAnswerFacts{Options: offered}); got.Kind != PermissionAnswerRejectOnce || got.OptionID != "no" {
		t.Fatalf("decision = %+v", got)
	}
	for _, options := range [][]PermissionOption{nil, offered[:2], {{OptionID: "never", Kind: "reject_always"}}, {{Kind: "reject_once"}}} {
		if got := DecidePermissionAnswer(PermissionAnswerFacts{Options: options}); got.Kind != PermissionAnswerCancelled || got.OptionID != "" || got.Reason == "" {
			t.Fatalf("options %+v: decision = %+v", options, got)
		}
	}
}

func TestDecideAgentRequest(t *testing.T) {
	cases := map[string]AgentRequestKind{
		"session/request_permission": AgentRequestAnswerPermission,
		"elicitation/create":         AgentRequestDeclineElicitation,
		"fs/read_text_file":          AgentRequestReject,
		"terminal/create":            AgentRequestReject,
		"_vendor/ask":                AgentRequestReject,
	}
	for method, want := range cases {
		if got := DecideAgentRequest(AgentRequestFacts{Method: method}); got.Kind != want || got.Reason == "" {
			t.Fatalf("%s: decision = %+v", method, got)
		}
	}
}

func TestDecideSessionUpdate(t *testing.T) {
	cases := map[string]SessionUpdateKind{
		"agent_message_chunk":       SessionUpdateText,
		"agent_thought_chunk":       SessionUpdateReasoning,
		"tool_call":                 SessionUpdateTool,
		"tool_call_update":          SessionUpdateTool,
		"usage_update":              SessionUpdateUsage,
		"user_message_chunk":        SessionUpdateEcho,
		"available_commands_update": SessionUpdateAdministrative,
		"session_info_update":       SessionUpdateAdministrative,
		"config_option_update":      SessionUpdateAdministrative,
		"plan":                      SessionUpdateUnknown,
	}
	for kind, want := range cases {
		if got := DecideSessionUpdate(SessionUpdateFacts{SessionUpdate: kind}); got.Kind != want {
			t.Fatalf("%s: decision = %+v", kind, got)
		}
	}
}

func TestDecideToolCategory(t *testing.T) {
	cases := map[string]ToolCategoryDecision{
		"execute": {Kind: ToolCategoryCommand},
		"read":    {Kind: ToolCategoryFile, FileOperation: FileOperationRead},
		"edit":    {Kind: ToolCategoryFile, FileOperation: FileOperationUpdate},
		"delete":  {Kind: ToolCategoryFile, FileOperation: FileOperationDelete},
		"move":    {Kind: ToolCategoryFile, FileOperation: FileOperationUnknown},
		"other":   {Kind: ToolCategoryGeneric},
		"":        {Kind: ToolCategoryGeneric},
	}
	for kind, want := range cases {
		got := DecideToolCategory(ToolCategoryFacts{NativeKind: kind})
		if got.Kind != want.Kind || got.FileOperation != want.FileOperation || got.Reason == "" {
			t.Fatalf("%q: decision = %+v", kind, got)
		}
	}
}

func TestDecideToolProgress(t *testing.T) {
	cases := []struct {
		name    string
		facts   ToolProgressFacts
		kind    ToolProgressKind
		outcome ToolOutcomeKind
	}{
		{"first pending", ToolProgressFacts{Status: "pending"}, ToolProgressStart, ToolOutcomeNone},
		{"first without status", ToolProgressFacts{}, ToolProgressStart, ToolOutcomeNone},
		{"first completed", ToolProgressFacts{Status: "completed"}, ToolProgressStartAndComplete, ToolOutcomeSucceeded},
		{"in progress without content", ToolProgressFacts{Started: true, Status: "in_progress"}, ToolProgressNone, ToolOutcomeNone},
		{"content update", ToolProgressFacts{Started: true, HasContent: true}, ToolProgressUpdate, ToolOutcomeNone},
		{"failed", ToolProgressFacts{Started: true, Status: "failed", HasContent: true}, ToolProgressComplete, ToolOutcomeFailed},
		{"after completion", ToolProgressFacts{Started: true, Completed: true, Status: "completed"}, ToolProgressAfterCompletion, ToolOutcomeNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideToolProgress(tc.facts)
			if got.Kind != tc.kind || got.Outcome != tc.outcome || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideSelection(t *testing.T) {
	cases := []struct {
		name  string
		facts SelectionFacts
		want  SelectionKind
	}{
		{"nothing requested", SelectionFacts{SetModelListed: true}, SelectionNotRequested},
		{"set_model preferred", SelectionFacts{Requested: "m", SetModelListed: true, ConfigOptionMatches: 1}, SelectionSetModel},
		{"single config option", SelectionFacts{Requested: "m", ConfigOptionMatches: 1}, SelectionConfigOption},
		{"ambiguous config options", SelectionFacts{Requested: "m", ConfigOptionMatches: 2}, SelectionAmbiguous},
		{"not advertised", SelectionFacts{Requested: "m"}, SelectionUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideSelection(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideStartTurn(t *testing.T) {
	ok := StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true}
	cases := []struct {
		name  string
		facts StartTurnFacts
		want  StartTurnKind
	}{
		{"allowed", ok, StartTurnAllowed},
		{"binding precedes occupancy", StartTurnFacts{TurnUnresolved: true}, StartTurnBindingDiffers},
		{"closing", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, Closing: true}, StartTurnUnavailable},
		{"lost", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, SessionLost: true}, StartTurnUnavailable},
		{"occupied", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, TurnUnresolved: true}, StartTurnOccupied},
		{"steer release is stale", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, SteerReleaseRequested: true}, StartTurnStaleRelease},
		{"binding precedes release", StartTurnFacts{SteerReleaseRequested: true}, StartTurnBindingDiffers},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideStartTurn(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideInterrupt(t *testing.T) {
	cases := []struct {
		name  string
		facts InterruptFacts
		want  InterruptKind
	}{
		{"allowed", InterruptFacts{SessionMatches: true, AssignmentMatches: true, ActiveTurn: true, ActiveTurnMatches: true}, InterruptAllowed},
		{"no turn", InterruptFacts{SessionMatches: true, AssignmentMatches: true}, InterruptNoActiveTurn},
		{"other turn", InterruptFacts{SessionMatches: true, AssignmentMatches: true, ActiveTurn: true}, InterruptBindingDiffers},
		{"other session", InterruptFacts{AssignmentMatches: true, ActiveTurn: true, ActiveTurnMatches: true}, InterruptBindingDiffers},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideInterrupt(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideResultEnforcement(t *testing.T) {
	if got := DecideResultEnforcement(ResultEnforcementFacts{SchemaPresent: true, AllowsNone: true}); got.Kind != ResultEnforcementInvalidSchema {
		t.Fatalf("invalid schema = %+v", got)
	}
	if got := DecideResultEnforcement(ResultEnforcementFacts{AllowsNone: true}); got.Kind != ResultEnforcementNone {
		t.Fatalf("none = %+v", got)
	}
	if got := DecideResultEnforcement(ResultEnforcementFacts{SchemaPresent: true, SchemaValidJSON: true}); got.Kind != ResultEnforcementUnsupported {
		t.Fatalf("native = %+v", got)
	}
}

func TestDecideClose(t *testing.T) {
	cases := []struct {
		name  string
		facts CloseFacts
		want  CloseKind
	}{
		{"stale", CloseFacts{ActiveWork: true}, CloseBindingStale},
		{"already closing", CloseFacts{SessionMatches: true, Closing: true}, CloseAlreadyClosing},
		{"idle detach", CloseFacts{SessionMatches: true, ExplicitDetach: true}, CloseAllowed},
		{"idle quiescent", CloseFacts{SessionMatches: true, RequireQuiescent: true}, CloseAllowed},
		{"active interrupt", CloseFacts{SessionMatches: true, ActiveWork: true, InterruptThenClose: true}, CloseCancelThenClose},
		{"active detach", CloseFacts{SessionMatches: true, ActiveWork: true, ExplicitDetach: true}, CloseDetachUnsupported},
		{"active quiescent", CloseFacts{SessionMatches: true, ActiveWork: true, RequireQuiescent: true}, CloseWorkUnresolved},
		{"active without policy", CloseFacts{SessionMatches: true, ActiveWork: true}, CloseUnresolvedNoPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideClose(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideOwnedProcessTerminationAndQuiescence(t *testing.T) {
	if got := DecideOwnedProcessTermination(OwnedProcessTerminationFacts{RootReaped: true, TreeKillSupported: true}); got.Kind != OwnedProcessKillRequired {
		t.Fatalf("reaped group still needs a kill: %+v", got)
	}
	if got := DecideOwnedProcessTermination(OwnedProcessTerminationFacts{RootReaped: true}); got.Kind != OwnedProcessRootAlreadyReaped {
		t.Fatalf("root-only platform = %+v", got)
	}
	if got := DecideOwnedProcessTermination(OwnedProcessTerminationFacts{}); got.Kind != OwnedProcessKillRequired {
		t.Fatalf("unreaped root = %+v", got)
	}
	if got := DecideQuiescence(QuiescenceFacts{RootReaped: true, TreeKillSupported: true, KillAttempted: true, KillConfirmed: true}); got.Kind != QuiescenceConfirmed {
		t.Fatalf("confirmed = %+v", got)
	}
	for _, facts := range []QuiescenceFacts{
		{TreeKillSupported: true, KillAttempted: true, KillConfirmed: true},
		{RootReaped: true, KillAttempted: true, KillConfirmed: true},
		{RootReaped: true, TreeKillSupported: true, KillConfirmed: true},
		{RootReaped: true, TreeKillSupported: true, KillAttempted: true},
	} {
		if got := DecideQuiescence(facts); got.Kind != QuiescenceUnproven {
			t.Fatalf("%+v: decision = %+v", facts, got)
		}
	}
}

func TestDecideSessionSetting(t *testing.T) {
	cases := []struct {
		name  string
		facts SessionSettingFacts
		want  SessionSettingKind
	}{
		{"apply", SessionSettingFacts{Requested: true, Mapped: true, OptionAdvertised: true, ValueListed: true}, SessionSettingApply},
		{"already current", SessionSettingFacts{Requested: true, Mapped: true, OptionAdvertised: true, ValueListed: true, AlreadyCurrent: true}, SessionSettingAlreadyCurrent},
		{"not requested", SessionSettingFacts{Mapped: true, OptionAdvertised: true}, SessionSettingNotRequested},
		{"not requested without mapping", SessionSettingFacts{}, SessionSettingNotRequested},
		{"no descriptor mapping", SessionSettingFacts{Requested: true, OptionAdvertised: true, ValueListed: true}, SessionSettingUnsupported},
		{"option missing", SessionSettingFacts{Requested: true, Mapped: true}, SessionSettingUnsupported},
		{"value not listed", SessionSettingFacts{Requested: true, Mapped: true, OptionAdvertised: true}, SessionSettingUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideSessionSetting(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideSettingConfirmation(t *testing.T) {
	cases := []struct {
		name  string
		facts SettingConfirmationFacts
		want  SettingConfirmationKind
	}{
		{"confirmed", SettingConfirmationFacts{OptionsReported: true, OptionPresent: true, CurrentMatches: true}, SettingConfirmed},
		{"other current value", SettingConfirmationFacts{OptionsReported: true, OptionPresent: true}, SettingNotApplied},
		{"option dropped", SettingConfirmationFacts{OptionsReported: true}, SettingNotApplied},
		{"no options reported", SettingConfirmationFacts{OptionPresent: true, CurrentMatches: true}, SettingUnverifiable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideSettingConfirmation(tc.facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}
