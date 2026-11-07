package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

// replayStep is one line of a sanitized fixture. "client" steps are frames
// the adapter must write (Match is a subset of params/result); "agent" steps
// are frames the fake agent writes. An agent response reuses the ID of the
// latest client request with the same method.
type replayStep struct {
	From   string          `json:"from"`
	Kind   string          `json:"kind"`
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id,omitempty"`
	Match  json.RawMessage `json:"match,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func loadFixture(t *testing.T, name string) []replayStep {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var steps []replayStep
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var step replayStep
		if err := json.Unmarshal(line, &step); err != nil {
			t.Fatalf("fixture line %q: %v", line, err)
		}
		steps = append(steps, step)
	}
	return steps
}

// replayAgent serves the fixture in order and reports the first divergence.
func replayAgent(steps []replayStep, failures chan<- error) func(*agentConn) {
	return func(conn *agentConn) {
		requestIDs := map[string]json.RawMessage{}
		for index, step := range steps {
			if step.From == "client" {
				frame, ok := conn.next()
				if !ok {
					failures <- fmt.Errorf("step %d: adapter closed stdin before %s %s", index, step.Kind, step.Method)
					return
				}
				if err := matchClientFrame(step, frame); err != nil {
					failures <- fmt.Errorf("step %d: %w", index, err)
					return
				}
				if step.Kind == "request" {
					requestIDs[step.Method] = frame.ID
				}
				continue
			}
			switch step.Kind {
			case "response":
				message := map[string]any{"jsonrpc": "2.0", "id": requestIDs[step.Method]}
				if len(step.Error) != 0 {
					message["error"] = step.Error
				} else {
					message["result"] = step.Result
				}
				conn.write(message)
			case "notification":
				conn.write(map[string]any{"jsonrpc": "2.0", "method": step.Method, "params": step.Params})
			case "request":
				conn.write(map[string]any{"jsonrpc": "2.0", "id": step.ID, "method": step.Method, "params": step.Params})
			}
		}
		close(failures)
	}
}

func matchClientFrame(step replayStep, frame clientFrame) error {
	kind := frame.kind()
	if kind != step.Kind {
		return fmt.Errorf("got %s %s, want %s %s", kind, frame.Method, step.Kind, step.Method)
	}
	if kind != "response" && frame.Method != step.Method {
		return fmt.Errorf("got method %s, want %s", frame.Method, step.Method)
	}
	if kind == "response" && !bytes.Equal(frame.ID, step.ID) {
		return fmt.Errorf("response id %s, want %s", frame.ID, step.ID)
	}
	if len(step.Match) == 0 {
		return nil
	}
	actual := frame.Params
	if kind == "response" {
		actual = frame.Result
	}
	var want, got any
	if err := json.Unmarshal(step.Match, &want); err != nil {
		return err
	}
	if err := json.Unmarshal(actual, &got); err != nil {
		return fmt.Errorf("%s payload: %w", step.Method, err)
	}
	if !jsonSubset(want, got) {
		return fmt.Errorf("%s payload %s does not contain %s", step.Method, actual, step.Match)
	}
	return nil
}

func jsonSubset(want, got any) bool {
	switch wantValue := want.(type) {
	case map[string]any:
		gotValue, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range wantValue {
			if !jsonSubset(value, gotValue[key]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(want, got)
	}
}

func awaitReplay(t *testing.T, failures <-chan error) {
	t.Helper()
	select {
	case err, open := <-failures:
		if open && err != nil {
			t.Fatalf("fixture replay: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture replay did not finish")
	}
}
