package rules

// SessionUpdateKind classifies one ACP session/update notification.
type SessionUpdateKind string

const (
	SessionUpdateText           SessionUpdateKind = "text"
	SessionUpdateReasoning      SessionUpdateKind = "reasoning"
	SessionUpdateTool           SessionUpdateKind = "tool"
	SessionUpdateUsage          SessionUpdateKind = "usage"
	SessionUpdateEcho           SessionUpdateKind = "echo"
	SessionUpdateAdministrative SessionUpdateKind = "administrative"
	SessionUpdateUnknown        SessionUpdateKind = "unknown"
)

type SessionUpdateFacts struct {
	SessionUpdate string
}

type SessionUpdateDecision struct {
	Kind   SessionUpdateKind
	Reason string
}

// DecideSessionUpdate maps the approved session/update table. Anything not in
// it is unknown and warned once per kind by the caller.
func DecideSessionUpdate(f SessionUpdateFacts) SessionUpdateDecision {
	switch f.SessionUpdate {
	case "agent_message_chunk":
		return SessionUpdateDecision{Kind: SessionUpdateText, Reason: "agent message text delta"}
	case "agent_thought_chunk":
		return SessionUpdateDecision{Kind: SessionUpdateReasoning, Reason: "agent thought text delta"}
	case "tool_call", "tool_call_update":
		return SessionUpdateDecision{Kind: SessionUpdateTool, Reason: "tool call lifecycle"}
	case "usage_update":
		return SessionUpdateDecision{Kind: SessionUpdateUsage, Reason: "session usage observation"}
	case "user_message_chunk":
		return SessionUpdateDecision{Kind: SessionUpdateEcho, Reason: "echo of client input; no event"}
	case "available_commands_update", "session_info_update", "config_option_update":
		return SessionUpdateDecision{Kind: SessionUpdateAdministrative, Reason: "administrative observation is ignored"}
	default:
		return SessionUpdateDecision{Kind: SessionUpdateUnknown, Reason: "session update kind is not mapped"}
	}
}

// ToolCategoryKind selects which provider events describe a tool call.
type ToolCategoryKind string

const (
	ToolCategoryCommand ToolCategoryKind = "command"
	ToolCategoryFile    ToolCategoryKind = "file"
	ToolCategoryGeneric ToolCategoryKind = "generic"
)

// FileOperationKind is the file activity a successful file tool reports.
type FileOperationKind string

const (
	FileOperationRead    FileOperationKind = "read"
	FileOperationUpdate  FileOperationKind = "update"
	FileOperationDelete  FileOperationKind = "delete"
	FileOperationUnknown FileOperationKind = "unknown"
)

type ToolCategoryFacts struct {
	// NativeKind is the ACP ToolKind; empty when not (yet) reported.
	NativeKind string
}

type ToolCategoryDecision struct {
	Kind          ToolCategoryKind
	FileOperation FileOperationKind
	Reason        string
}

// DecideToolCategory maps the ACP tool kind. The caller fixes the category at
// the first observation of a tool call so its events never switch family.
func DecideToolCategory(f ToolCategoryFacts) ToolCategoryDecision {
	switch f.NativeKind {
	case "execute":
		return ToolCategoryDecision{Kind: ToolCategoryCommand, Reason: "execute tool runs a command"}
	case "read":
		return ToolCategoryDecision{Kind: ToolCategoryFile, FileOperation: FileOperationRead, Reason: "read tool observes files"}
	case "edit":
		return ToolCategoryDecision{Kind: ToolCategoryFile, FileOperation: FileOperationUpdate, Reason: "edit tool modifies files"}
	case "delete":
		return ToolCategoryDecision{Kind: ToolCategoryFile, FileOperation: FileOperationDelete, Reason: "delete tool removes files"}
	case "move":
		return ToolCategoryDecision{Kind: ToolCategoryFile, FileOperation: FileOperationUnknown, Reason: "move tool changes file locations"}
	default:
		return ToolCategoryDecision{Kind: ToolCategoryGeneric, Reason: "tool kind has no command or file mapping"}
	}
}

// ToolProgressKind is the next lifecycle step to publish for a tool call.
type ToolProgressKind string

const (
	ToolProgressStart            ToolProgressKind = "start"
	ToolProgressStartAndComplete ToolProgressKind = "start_and_complete"
	ToolProgressUpdate           ToolProgressKind = "update"
	ToolProgressComplete         ToolProgressKind = "complete"
	ToolProgressNone             ToolProgressKind = "none"
	ToolProgressAfterCompletion  ToolProgressKind = "after_completion"
)

// ToolOutcomeKind is the terminal outcome of a completed tool call.
type ToolOutcomeKind string

const (
	ToolOutcomeNone      ToolOutcomeKind = ""
	ToolOutcomeSucceeded ToolOutcomeKind = "succeeded"
	ToolOutcomeFailed    ToolOutcomeKind = "failed"
)

type ToolProgressFacts struct {
	Started    bool
	Completed  bool
	Status     string
	HasContent bool
}

type ToolProgressDecision struct {
	Kind    ToolProgressKind
	Outcome ToolOutcomeKind
	Reason  string
}

// DecideToolProgress maps ACP status (pending, in_progress, completed, failed)
// onto start/update/complete steps. An update for an unseen call starts it.
func DecideToolProgress(f ToolProgressFacts) ToolProgressDecision {
	if f.Completed {
		return ToolProgressDecision{Kind: ToolProgressAfterCompletion, Reason: "tool call already completed"}
	}
	outcome := ToolOutcomeNone
	switch f.Status {
	case "completed":
		outcome = ToolOutcomeSucceeded
	case "failed":
		outcome = ToolOutcomeFailed
	}
	if !f.Started {
		if outcome != ToolOutcomeNone {
			return ToolProgressDecision{Kind: ToolProgressStartAndComplete, Outcome: outcome, Reason: "first observation is already terminal"}
		}
		return ToolProgressDecision{Kind: ToolProgressStart, Reason: "first observation of a tool call"}
	}
	if outcome != ToolOutcomeNone {
		return ToolProgressDecision{Kind: ToolProgressComplete, Outcome: outcome, Reason: "tool call reached a terminal status"}
	}
	if f.HasContent {
		return ToolProgressDecision{Kind: ToolProgressUpdate, Reason: "tool call reported new content"}
	}
	return ToolProgressDecision{Kind: ToolProgressNone, Reason: "status change without new content"}
}
