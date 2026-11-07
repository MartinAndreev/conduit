package acp

import (
	"encoding/json"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp/rules"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// toolCall tracks one native tool call within the active turn. Its category
// is fixed at the first observation; later fields refine display and paths.
type toolCall struct {
	category  rules.ToolCategoryDecision
	title     string
	rawInput  json.RawMessage
	paths     []string
	started   bool
	completed bool
}

type toolCallUpdate struct {
	ToolCallID string          `json:"toolCallId"`
	Title      *string         `json:"title"`
	Kind       *string         `json:"kind"`
	Status     *string         `json:"status"`
	Content    []toolContent   `json:"content"`
	Locations  []toolLocation  `json:"locations"`
	RawInput   json.RawMessage `json:"rawInput"`
}

type toolContent struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type toolLocation struct {
	Path string `json:"path"`
}

// toolStep is a copied, lock-free view of one tool lifecycle step.
type toolStep struct {
	id       provider.NativeToolCallID
	progress rules.ToolProgressDecision
	category rules.ToolCategoryDecision
	title    string
	rawInput json.RawMessage
	paths    []string
	output   string
}

func (s *session) normalizeToolCall(nativeKind string, raw json.RawMessage) {
	var update toolCallUpdate
	if json.Unmarshal(raw, &update) != nil || update.ToolCallID == "" {
		s.warnOnce("tool_call:invalid", provider.ProtocolWarningEvent{Code: "invalid_tool_call", Detail: provider.BoundedText{Text: "ignored tool call without an ID"}, OffendingNativeKind: nativeKind})
		return
	}
	step := s.advanceToolCall(update)
	s.publishToolStep(step)
}

// advanceToolCall merges the update into the tracked call and decides the
// next lifecycle step in one critical section.
func (s *session) advanceToolCall(update toolCallUpdate) toolStep {
	status := ""
	if update.Status != nil {
		status = *update.Status
	}
	output, contentPaths := toolContentText(update.Content)
	var step toolStep
	_ = lockscope.WithMutexLock(&s.toolMu, func() error {
		call := s.tools[update.ToolCallID]
		if call == nil {
			kind := ""
			if update.Kind != nil {
				kind = *update.Kind
			}
			call = &toolCall{category: rules.DecideToolCategory(rules.ToolCategoryFacts{NativeKind: kind})}
			s.tools[update.ToolCallID] = call
		}
		if update.Title != nil {
			call.title = *update.Title
		}
		if len(update.RawInput) != 0 {
			call.rawInput = append(json.RawMessage(nil), update.RawInput...)
		}
		for _, location := range update.Locations {
			call.paths = appendPath(call.paths, location.Path)
		}
		for _, path := range contentPaths {
			call.paths = appendPath(call.paths, path)
		}
		progress := rules.DecideToolProgress(rules.ToolProgressFacts{
			Started: call.started, Completed: call.completed, Status: status, HasContent: output != "",
		})
		switch progress.Kind {
		case rules.ToolProgressStart, rules.ToolProgressUpdate:
			call.started = true
		case rules.ToolProgressStartAndComplete, rules.ToolProgressComplete:
			call.started = true
			call.completed = true
		}
		step = toolStep{
			id: provider.NativeToolCallID(update.ToolCallID), progress: progress, category: call.category,
			title: call.title, rawInput: call.rawInput, paths: append([]string(nil), call.paths...), output: output,
		}
		return nil
	})
	return step
}

func appendPath(paths []string, path string) []string {
	if path == "" {
		return paths
	}
	for _, existing := range paths {
		if existing == path {
			return paths
		}
	}
	return append(paths, path)
}

// toolContentText joins text content and collects diff paths. Terminal
// content refers to client terminals, which this client never provides.
func toolContentText(content []toolContent) (string, []string) {
	var texts, paths []string
	for _, item := range content {
		switch item.Type {
		case "content":
			if item.Content != nil && item.Content.Type == "text" {
				texts = append(texts, item.Content.Text)
			}
		case "diff":
			paths = append(paths, item.Path)
		}
	}
	return joinText(texts), paths
}

func (s *session) publishToolStep(step toolStep) {
	switch step.progress.Kind {
	case rules.ToolProgressStart:
		s.publishToolStart(step)
	case rules.ToolProgressUpdate:
		s.publishToolUpdate(step)
	case rules.ToolProgressStartAndComplete:
		s.publishToolStart(step)
		s.publishToolCompletion(step)
	case rules.ToolProgressComplete:
		s.publishToolCompletion(step)
	}
}

func (s *session) publishToolStart(step toolStep) {
	if step.category.Kind == rules.ToolCategoryCommand {
		input := commandInput(step.rawInput)
		display := input.Command
		if display == "" {
			display = step.title
		}
		s.emit(provider.CommandStartedEvent{
			CommandID: provider.NativeCommandID(step.id), Display: boundedtext.String(display, s.config.MaxTextBytes), CWD: input.CWD,
		})
		return
	}
	s.emit(provider.ToolStartedEvent{
		ToolCallID: step.id, ToolName: step.title, Arguments: boundedtext.Bytes(step.rawInput, s.config.MaxTextBytes),
	})
}

func (s *session) publishToolUpdate(step toolStep) {
	output := boundedtext.String(step.output, s.config.MaxTextBytes)
	if step.category.Kind == rules.ToolCategoryCommand {
		s.emit(provider.CommandOutputEvent{CommandID: provider.NativeCommandID(step.id), Stream: provider.CommandStreamUnknown, Output: output, Mode: provider.UpdateSnapshot})
		return
	}
	s.emit(provider.ToolUpdatedEvent{ToolCallID: step.id, Output: output, Mode: provider.UpdateSnapshot})
}

// publishToolCompletion reports the terminal step. ACP standardizes no exit
// code, so a failed command adds a command_failed warning; a file tool
// reports file activity only when it succeeded.
func (s *session) publishToolCompletion(step toolStep) {
	output := boundedtext.String(step.output, s.config.MaxTextBytes)
	if step.category.Kind == rules.ToolCategoryCommand {
		if step.progress.Outcome == rules.ToolOutcomeFailed {
			s.emit(provider.ProtocolWarningEvent{Code: "command_failed", Detail: provider.BoundedText{Text: "native command tool reported failure without an exit code"}, OffendingNativeKind: "tool_call_update"})
		}
		s.emit(provider.CommandCompletedEvent{CommandID: provider.NativeCommandID(step.id), ExitCode: provider.None[int](), Output: output})
		return
	}
	outcome := provider.ToolSucceeded
	if step.progress.Outcome == rules.ToolOutcomeFailed {
		outcome = provider.ToolFailed
	}
	s.emit(provider.ToolCompletedEvent{ToolCallID: step.id, Outcome: outcome, Output: output})
	if step.category.Kind != rules.ToolCategoryFile || outcome != provider.ToolSucceeded {
		return
	}
	paths := step.paths
	if len(paths) == 0 {
		paths = appendPath(nil, rawInputPath(step.rawInput))
	}
	for _, path := range paths {
		s.emit(provider.FileActivityEvent{Operation: fileOperation(step.category.FileOperation), NativePath: path, ToolCallID: provider.Some(step.id)})
	}
}

type commandRawInput struct {
	Command string `json:"command"`
	CWD     string `json:"cwd"`
}

func commandInput(raw json.RawMessage) commandRawInput {
	var input commandRawInput
	_ = json.Unmarshal(raw, &input)
	return input
}

// rawInputPath is the fallback when an agent reports no locations or diffs.
func rawInputPath(raw json.RawMessage) string {
	var input struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	if input.Path != "" {
		return input.Path
	}
	return input.FilePath
}

func fileOperation(kind rules.FileOperationKind) provider.FileOperation {
	switch kind {
	case rules.FileOperationRead:
		return provider.FileRead
	case rules.FileOperationUpdate:
		return provider.FileUpdate
	case rules.FileOperationDelete:
		return provider.FileDelete
	default:
		return provider.FileUnknown
	}
}
