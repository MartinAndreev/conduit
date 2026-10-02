package opencode

import (
	"encoding/json"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type nativeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Text  string `json:"text"`
		Tool  string `json:"tool"`
		State struct {
			Status   string          `json:"status"`
			Input    json.RawMessage `json:"input"`
			Output   string          `json:"output"`
			Title    string          `json:"title"`
			Metadata struct {
				Metadata struct {
					Exit *int `json:"exit"`
				} `json:"metadata"`
			} `json:"metadata"`
		} `json:"state"`
	} `json:"part"`
}

func (s *session) normalizeLine(line []byte, turn provider.TurnRef) {
	var event nativeEvent
	if err := json.Unmarshal(line, &event); err != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "invalid_jsonl", Detail: boundedtext.String("OpenCode emitted a non-JSON event", s.config.MaxTextBytes)})
		return
	}
	if event.SessionID != "" && provider.NativeSessionID(event.SessionID) != s.nativeSession {
		s.emit(provider.ProtocolWarningEvent{Code: "session_mismatch", Detail: boundedtext.String("OpenCode event referenced a different session", s.config.MaxTextBytes), OffendingNativeKind: event.Type})
		return
	}
	switch event.Type {
	case "text":
		id := event.Part.ID
		if id == "" {
			var err error
			id, err = s.config.NewID("text")
			if err != nil {
				s.failID(err)
				return
			}
		}
		value := boundedtext.String(event.Part.Text, s.config.MaxTextBytes)
		s.emit(provider.AssistantTextEvent{Phase: provider.AssistantPhaseUnknown, Update: provider.TextSegmentUpdate{ItemID: provider.ItemID(id), SegmentID: provider.SegmentID(id), Text: value.Text, Mode: provider.UpdateSnapshot, Version: 1, Truncated: value.Truncated, OriginalBytes: value.OriginalBytes}})
	case "reasoning":
		// OpenCode exposes reasoning text, but experiments did not establish that it
		// is a user-safe reasoning summary. Preserve availability without the text.
		id := event.Part.ID
		if id == "" {
			var err error
			id, err = s.config.NewID("reasoning")
			if err != nil {
				s.failID(err)
				return
			}
		}
		s.emit(provider.ReasoningSummaryEvent{Availability: provider.ReasoningUnknown, Update: provider.TextSegmentUpdate{ItemID: provider.ItemID(id), SegmentID: provider.SegmentID(id), Mode: provider.UpdateSnapshot, Version: 1}})
	case "tool_use":
		s.normalizeTool(event)
	case "step_start", "step_finish":
		return
	default:
		s.emit(provider.ProtocolWarningEvent{Code: "unsupported_native_event", Detail: boundedtext.String("unsupported OpenCode event was not normalized", s.config.MaxTextBytes), OffendingNativeKind: event.Type})
	}
	_ = turn
}

func (s *session) normalizeTool(event nativeEvent) {
	id := event.Part.ID
	if id == "" {
		var err error
		id, err = s.config.NewID("tool")
		if err != nil {
			s.failID(err)
			return
		}
	}
	callID := provider.NativeToolCallID(id)
	args := boundedtext.String(string(event.Part.State.Input), s.config.MaxTextBytes)
	s.emit(provider.ToolStartedEvent{ToolCallID: callID, ToolName: event.Part.Tool, Arguments: args})
	outcome := provider.ToolUnknown
	switch event.Part.State.Status {
	case "completed":
		outcome = provider.ToolSucceeded
	case "error", "failed":
		outcome = provider.ToolFailed
	case "cancelled":
		outcome = provider.ToolCancelled
	}
	output := boundedtext.String(event.Part.State.Output, s.config.MaxTextBytes)
	s.emit(provider.ToolCompletedEvent{ToolCallID: callID, Outcome: outcome, Output: output})
	var input map[string]json.RawMessage
	_ = json.Unmarshal(event.Part.State.Input, &input)
	if event.Part.Tool == "shell" || event.Part.Tool == "bash" {
		var command string
		_ = json.Unmarshal(input["command"], &command)
		commandID := provider.NativeCommandID(id)
		s.emit(provider.CommandStartedEvent{CommandID: commandID, Display: boundedtext.String(command, s.config.MaxTextBytes), CWD: s.workspace})
		if event.Part.State.Output != "" {
			s.emit(provider.CommandOutputEvent{CommandID: commandID, Stream: provider.CommandStreamUnknown, Output: output, Mode: provider.UpdateSnapshot})
		}
		exit := provider.None[int]()
		if event.Part.State.Metadata.Metadata.Exit != nil {
			exit = provider.Some(*event.Part.State.Metadata.Metadata.Exit)
		}
		s.emit(provider.CommandCompletedEvent{CommandID: commandID, ExitCode: exit, Output: output})
	}
	if event.Part.Tool == "write" || event.Part.Tool == "edit" || event.Part.Tool == "read" {
		var path string
		_ = json.Unmarshal(input["path"], &path)
		op := provider.FileUnknown
		switch event.Part.Tool {
		case "write":
			// Native write events do not reveal whether the path already existed.
			op = provider.FileUnknown
		case "edit":
			op = provider.FileUpdate
		case "read":
			op = provider.FileRead
		}
		s.emit(provider.FileActivityEvent{Operation: op, NativePath: path, ToolCallID: provider.Some(callID)})
	}
}

func (s *session) failID(err error) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if !s.closed {
			s.state = provider.SessionLost
		}
		return nil
	})
	s.broker.Finish(adaptererror.NewWithDetail(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, "could not allocate normalized event identity", err))
}
