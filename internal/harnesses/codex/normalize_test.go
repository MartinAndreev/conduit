package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestArchivedMultifileFixtureNormalizesWithoutWarningStorm(t *testing.T) {
	fixture, err := os.Open("testdata/codex-luna-multifile-sanitized.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()

	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	scanner := bufio.NewScanner(fixture)
	for scanner.Scan() {
		var row struct {
			Kind         string `json:"kind"`
			Method       string `json:"method"`
			ItemType     string `json:"itemType"`
			Phase        string `json:"phase"`
			Text         string `json:"text"`
			Status       string `json:"status"`
			Command      string `json:"command"`
			ExitCode     *int   `json:"exitCode"`
			SummaryParts int    `json:"summaryParts"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Kind != "event" || (row.Method != "item/started" && row.Method != "item/completed") {
			continue
		}
		rows++
		item := map[string]any{"id": "item-" + strconv.Itoa(rows), "type": row.ItemType, "phase": row.Phase, "text": row.Text, "status": row.Status, "command": row.Command}
		if row.ExitCode != nil {
			item["exitCode"] = *row.ExitCode
		}
		if row.ItemType == "fileChange" && row.Method == "item/completed" {
			item["changes"] = []map[string]any{{"path": "toy.py", "kind": "update"}}
		}
		params, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": item})
		s.handleNotification(row.Method, params)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 20 {
		t.Fatalf("fixture rows exercised = %d, want a nontrivial archived trace", rows)
	}
	s.broker.Finish(nil)
	counts := map[provider.EventKind]int{}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		counts[event.Kind()]++
	}
	if counts[provider.EventAssistantText] < 3 || counts[provider.EventCommandStarted] < 2 || counts[provider.EventCommandCompleted] < 2 || counts[provider.EventFileActivity] < 1 {
		t.Fatalf("normalized event counts = %#v", counts)
	}
	if counts[provider.EventProtocolWarning] != 0 {
		t.Fatalf("archived supported events emitted %d protocol warnings", counts[provider.EventProtocolWarning])
	}
}

func TestTextDeltasCarryCorrelationVersionOffsetsAndTruncation(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "message-1", "delta": "hello"})
	s.handleNotification("item/agentMessage/delta", first)
	large := make([]byte, 70*1024)
	for i := range large {
		large[i] = 'x'
	}
	second, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "message-1", "delta": string(large)})
	s.handleNotification("item/agentMessage/delta", second)

	event1 := receiveEvent(t, stream)
	event2 := receiveEvent(t, stream)
	update1 := event1.Body().(provider.AssistantTextEvent).Update
	update2 := event2.Body().(provider.AssistantTextEvent).Update
	if update1.ItemID != "message-1" || update1.SegmentID != "text" || update1.Version != 1 || !update1.BaseVersion.Present || update1.BaseVersion.Value != 0 || update1.ByteOffset != 0 {
		t.Fatalf("first delta = %#v", update1)
	}
	if update2.Version != 2 || update2.BaseVersion.Value != 1 || update2.ByteOffset != 5 || !update2.Truncated || !update2.OriginalBytes.Present || update2.OriginalBytes.Value != uint64(len(large)) {
		t.Fatalf("second delta = %#v", update2)
	}
}

func TestStaleEventsCannotProduceCandidateOrTerminalAndPendingSteerIsUnattributed(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	pending := provider.SteerRequest{RequestID: "steer-1", ExpectedActiveTurn: s.active.Value, CurrentAssignment: testAssignment(1), NewAssignment: testAssignment(2)}
	s.steerPending = &pendingSteer{request: pending}
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	staleFinal := notificationJSON("thread-1", "turn-old", map[string]any{"id": "message-old", "type": "agentMessage", "phase": "final_answer", "text": `{"status":"completed"}`})
	s.handleNotification("item/completed", staleFinal)
	staleTerminal, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-old", "status": "completed"}})
	s.handleNotification("turn/completed", staleTerminal)
	validFinal := notificationJSON("thread-1", "turn-1", map[string]any{"id": "message-1", "type": "agentMessage", "phase": "final_answer", "text": `{"status":"completed"}`})
	s.handleNotification("item/completed", validFinal)

	var candidate *provider.ResultCandidateEvent
	terminal := false
	for i := 0; i < 4; i++ {
		event := receiveEvent(t, stream)
		if !event.Envelope.Assignment.Present && event.Kind() == provider.EventResultCandidate {
			body := event.Body().(provider.ResultCandidateEvent)
			candidate = &body
		}
		if event.Kind() == provider.EventTurnState {
			terminal = true
		}
	}
	if candidate == nil || candidate.Candidate.Assignment.Present {
		t.Fatalf("candidate during unresolved steer = %#v", candidate)
	}
	if terminal {
		t.Fatal("stale turn/completed synthesized a terminal event")
	}
	if !s.active.Present || s.active.Value.AdapterTurnID != "turn-1" {
		t.Fatalf("stale terminal cleared active turn: %#v", s.active)
	}
}

func notificationJSON(threadID, turnID string, item map[string]any) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"threadId": threadID, "turnId": turnID, "item": item})
	return value
}

func receiveEvent(t *testing.T, stream provider.EventStream) provider.ProviderEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	return event
}
