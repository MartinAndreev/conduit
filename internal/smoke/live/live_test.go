//go:build liveharness

package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/harnesses/codex"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"github.com/MartinAndreev/conduit/internal/storage/turso"
)

// TestManualLiveHarness makes exactly one turn attempt, with no retry or model
// fallback. It must never be included in an automated invocation.
func TestManualLiveHarness(t *testing.T) {
	selected, err := CheckGate(os.LookupEnv) // Before files, database, adapters, credentials or subprocesses.
	if err != nil {
		t.Skip("manual gate refused; no effects attempted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	workspace := t.TempDir()
	store, err := turso.Open(ctx, filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal("temporary journal open failed")
	}
	defer store.Close()
	owned := &trackedLauncher{}
	defer func() {
		if !owned.cleanup() {
			t.Error("owned process cleanup incomplete")
		}
	}()
	var adapter provider.Provider
	model := provider.ModelSelection{ProviderID: "openai", ModelID: "gpt-5.6-luna"}
	contract := provider.ResultContractRef{Name: "manual-file-smoke", Version: "1", Requested: provider.SchemaNative, Schema: json.RawMessage(`{"type":"object","properties":{"completed":{"type":"boolean"}},"required":["completed"],"additionalProperties":false}`)}
	if selected == "codex" {
		adapter = codex.New(codex.Config{Launcher: owned, Ephemeral: true, Approval: codex.ApprovalNever, Sandbox: codex.SandboxWorkspaceWrite, Reasoning: codex.ReasoningLow})
	} else {
		model = provider.ModelSelection{ProviderID: "opencode", ModelID: "mimo-v2.6-flash-free", Variant: "default"}
		contract.Requested = provider.SchemaNone
		adapter = opencode.New(opencode.Config{Launcher: owned})
	}
	assignment := provider.AssignmentRef{ProjectID: "manual-smoke", RunID: "manual-smoke", AttemptID: "one", AssignmentID: "file", Revision: 1}
	opened, err := adapter.OpenSession(ctx, provider.OpenSessionRequest{RequestID: "open", Assignment: assignment, Workspace: provider.WorkspaceBinding{WorkspaceID: "isolated", Path: workspace, Writable: true}, Model: model, ResultContract: contract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatalf("%s; no retry attempted", safeFailure("open_session", err))
	}
	session := opened.Session
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		policy := provider.CloseInterruptThenClose
		if selected == "opencode" {
			policy = provider.CloseExplicitDetach
		}
		closed, err := session.Close(cleanup, provider.CloseSessionRequest{RequestID: "close", Session: session.Ref(), Policy: policy})
		if err != nil {
			t.Error("adapter cleanup failed; remote execution may remain unknown")
			return
		}
		t.Logf("cleanup handle_closed=%t resources_released=%t execution=%s", closed.HandleClosed, closed.ResourcesReleased, closed.ExecutionState)
	}()
	stream, err := session.Events(ctx, provider.EventCursor{})
	if err != nil {
		t.Fatal("subscription failed")
	}
	defer stream.Close()
	started, err := session.StartTurn(ctx, provider.StartTurnRequest{RequestID: "turn", Session: session.Ref(), Assignment: assignment, ResultContract: contract, Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "In this isolated workspace create exactly one file named answer.txt containing exactly CONDUIT_SMOKE_OK followed by one newline. Do not inspect other directories or access network. Then return the requested JSON object with completed true. Keep this task short."}}})
	if err != nil {
		t.Fatalf("%s; no retry attempted", safeFailure("start_turn", err))
	}
	counts := map[provider.EventKind]int{}
	candidate := false
	for {
		event, receiveErr := stream.Receive(ctx)
		if receiveErr != nil {
			t.Error("observation ended without terminal evidence; no retry attempted")
			break
		}
		counts[event.Kind()]++
		// Persist only safe kind/sequence telemetry: never native text, IDs or payloads.
		payload, _ := json.Marshal(struct {
			Kind            provider.EventKind `json:"kind"`
			AdapterSequence uint64             `json:"adapter_sequence"`
		}{event.Kind(), event.Envelope.AdapterSeq})
		_, err = store.Events().Append(ctx, events.Event{ID: events.EventID(fmt.Sprintf("observation-%d", event.Envelope.AdapterSeq)), RunID: "manual-smoke", Kind: "sanitized_observation", PayloadVersion: 1, Payload: payload, Provenance: "manual-live-smoke", ObservedAt: time.Now()})
		if err != nil {
			t.Fatal("sanitized journal append failed")
		}
		if body, ok := event.Body().(provider.ResultCandidateEvent); ok {
			c := body.Candidate
			var result map[string]json.RawMessage
			validPayload := json.Unmarshal(c.Payload, &result) == nil && len(result) == 1 && string(result["completed"]) == "true"
			candidate = event.Envelope.Session == session.Ref() && event.Envelope.Turn.Present && event.Envelope.Turn.Value == started.Turn && event.Envelope.Assignment.Present && event.Envelope.Assignment.Value == assignment && c.Assignment.Present && c.Assignment.Value == assignment && c.Native.Session.Present && c.Native.Session == started.Native.Session && c.Native.Turn.Present && c.Native.Turn == started.Native.Turn && c.Source == provider.CandidateNativeStructured && c.Enforcement == provider.SchemaNative && reflect.DeepEqual(c.Contract, contract) && validPayload
			if !candidate {
				t.Error("candidate failed attribution/schema/payload checks")
			}
		}
		if event.Kind() == provider.EventGap {
			t.Error("event history incomplete")
			break
		}
		if body, ok := event.Body().(provider.TurnStateEvent); ok {
			if body.Current == provider.TurnLost || body.Current == provider.TurnStateUnknown {
				t.Error("turn lost or unknown")
				break
			}
			if body.Current == provider.TurnNativeTerminal && event.Envelope.Turn.Present && event.Envelope.Turn.Value == started.Turn {
				if body.TerminalReason != "completed" {
					t.Error("native turn did not complete successfully")
				}
				break
			}
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && body.Current == provider.SessionIdleObserved && selected == "opencode" {
			break
		}
	}
	if selected == "codex" && !candidate {
		t.Error("Codex did not expose valid attributed candidate")
	}
	entries, entriesErr := os.ReadDir(workspace)
	if entriesErr != nil || len(entries) != 1 || entries[0].Name() != "answer.txt" {
		t.Error("workspace contains unexpected artifacts")
	}
	actual, err := os.ReadFile(filepath.Join(workspace, "answer.txt"))
	verified := err == nil && string(actual) == "CONDUIT_SMOKE_OK\n"
	if !verified {
		t.Error("independent exact file-content verification failed")
	}
	replayed, err := store.Events().Replay(context.Background(), "manual-smoke", 0, 1000)
	if err != nil {
		t.Error("sanitized journal replay failed")
	}
	t.Logf("provider=%s file_verified=%t native_candidate_observed=%t application_accepted=false steering=not_tested journal_events=%d text_events=%d tool_events=%d file_events=%d", selected, verified, candidate, len(replayed), counts[provider.EventAssistantText], counts[provider.EventToolCompleted], counts[provider.EventFileActivity])
	if selected == "opencode" && candidate {
		t.Error("OpenCode unexpectedly emitted a candidate; contract review required")
	}
}

// trackedLauncher owns only child processes created by this test's adapters.
// It never discovers or signals any shared service.
type trackedLauncher struct {
	mu        sync.Mutex
	processes []*trackedProcess
}

func (l *trackedLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	p, err := (processruntime.ExecLauncher{}).Start(spec)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	tracked := &trackedProcess{Process: p, done: make(chan struct{})}
	l.processes = append(l.processes, tracked)
	l.mu.Unlock()
	return tracked, nil
}
func (l *trackedLauncher) cleanup() bool {
	l.mu.Lock()
	processes := append([]*trackedProcess(nil), l.processes...)
	l.mu.Unlock()
	good := true
	for _, p := range processes {
		select {
		case <-p.done:
			continue
		default:
		}
		err := p.KillTree()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			good = false
		}
		go p.Wait()
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			good = false
		}
	}
	return good
}

type trackedProcess struct {
	processruntime.Process
	once   sync.Once
	done   chan struct{}
	result processruntime.Result
}

func (p *trackedProcess) Wait() processruntime.Result {
	p.once.Do(func() { p.result = p.Process.Wait(); close(p.done) })
	return p.result
}

// safeFailure preserves machine-readable semantics without native error strings.
func safeFailure(stage string, err error) string {
	causeClass := "unclassified"
	var pathErr *os.PathError
	var execErr *exec.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		causeClass = "deadline"
	case errors.Is(err, context.Canceled):
		causeClass = "cancelled"
	case errors.As(err, &execErr):
		causeClass = "executable"
	case errors.As(err, &pathErr):
		causeClass = "filesystem"
	}
	var failure *provider.ProviderError
	if errors.As(err, &failure) {
		return fmt.Sprintf("stage=%s code=%s remote_effect=%s retry=%s cause_class=%s", stage, failure.Code, failure.RemoteEffect, failure.Retry, causeClass)
	}
	return fmt.Sprintf("stage=%s cause_class=%s", stage, causeClass)
}

func TestSafeFailureDoesNotExposeCause(t *testing.T) {
	err := &provider.ProviderError{Code: provider.ErrorProtocolViolation, Operation: provider.OperationOpenSession, RemoteEffect: provider.RemoteEffectAccepted, Retry: provider.RetryNever, Cause: errors.New("SECRET_ACCOUNT_DATA")}
	expected := "stage=open_session code=protocol_violation remote_effect=accepted retry=never cause_class=unclassified"
	if got := safeFailure("open_session", err); got != expected {
		t.Fatalf("sanitized diagnostics mismatch: %q", got)
	}
}
