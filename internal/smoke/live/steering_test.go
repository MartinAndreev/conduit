//go:build liveharness

package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/codex"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// The agent executes this local, bounded checkpoint. The ready PID and absence
// of done prove that the checkpoint command is still active, rather than relying
// on an arbitrary delay before steering. The trap also covers native aborts.
const steeringCheckpoint = `#!/bin/sh
trap 'touch checkpoint.done' EXIT
printf '%s\n' "$$" > checkpoint.ready
n=0
while [ ! -f checkpoint.release ]; do
 n=$((n+1))
 if [ "$n" -ge 90 ]; then exit 75; fi
 sleep 1
done
`

func writeCheckpoint(t *testing.T, workspace string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, "checkpoint.sh"), []byte(steeringCheckpoint), 0700); err != nil {
		t.Fatal("checkpoint setup failed")
	}
}

func releaseCheckpoint(workspace string) error {
	return os.WriteFile(filepath.Join(workspace, "checkpoint.release"), nil, 0600)
}

func checkpointActive(workspace string) bool {
	data, err := os.ReadFile(filepath.Join(workspace, "checkpoint.ready"))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	if _, err := os.Stat(filepath.Join(workspace, "checkpoint.done")); !os.IsNotExist(err) {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func waitForCheckpoint(ctx context.Context, workspace string) bool {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if checkpointActive(workspace) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func exactAnswer(workspace, expected string) bool {
	data, err := os.ReadFile(filepath.Join(workspace, "answer.txt"))
	return err == nil && string(data) == expected+"\n"
}

// TestManualLiveSteering attempts exactly one StartTurn and one Steer. It checks
// transport and artifacts; it does not implement application acceptance.
func TestManualLiveSteering(t *testing.T) {
	selected, err := CheckGate(os.LookupEnv)
	if err != nil {
		t.Skip("manual gate refused; no effects attempted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	workspace := t.TempDir()
	writeCheckpoint(t, workspace)
	// Registered after TempDir so release happens before directory removal.
	defer func() {
		if releaseCheckpoint(workspace) != nil {
			t.Error("checkpoint release failed")
		}
	}()
	owned := &trackedLauncher{}
	defer func() {
		if !owned.cleanup() {
			t.Error("owned process cleanup incomplete")
		}
	}()
	var adapter provider.Provider
	model := provider.ModelSelection{ProviderID: "openai", ModelID: "gpt-5.6-luna"}
	contract := provider.ResultContractRef{Name: "manual-steering-smoke", Version: "1", Requested: provider.SchemaNative, Schema: json.RawMessage(`{"type":"object","properties":{"completed":{"type":"boolean"}},"required":["completed"],"additionalProperties":false}`)}
	if selected == "codex" {
		adapter = codex.New(codex.Config{Launcher: owned, Ephemeral: true, Approval: codex.ApprovalNever, Sandbox: codex.SandboxWorkspaceWrite, Reasoning: codex.ReasoningLow})
	} else {
		model = provider.ModelSelection{ProviderID: "opencode", ModelID: "mimo-v2.6-flash-free", Variant: "default"}
		contract.Requested = provider.SchemaNone
		adapter = opencode.New(opencode.Config{Launcher: owned})
	}
	initial := provider.AssignmentRef{ProjectID: "manual-steering", RunID: "manual-steering", AttemptID: "one", AssignmentID: "file", Revision: 1}
	revised := initial
	revised.Revision = 2
	opened, err := adapter.OpenSession(ctx, provider.OpenSessionRequest{RequestID: "open", Assignment: initial, Workspace: provider.WorkspaceBinding{WorkspaceID: "isolated", Path: workspace, Writable: true}, Model: model, ResultContract: contract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(safeFailure("open_session", err))
	}
	session := opened.Session
	defer func() {
		_ = releaseCheckpoint(workspace)
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
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
	prompt := "In this isolated workspace write answer.txt containing exactly BLUE followed by one newline. Then run `sh ./checkpoint.sh` in a foreground shell command and wait for it; do not background it or modify any checkpoint files. The checkpoint is bounded and managed by the test. After it returns, apply any steering instructions and finish. Do not access network or inspect other directories. Return the requested JSON object with completed true when finished."
	started, err := session.StartTurn(ctx, provider.StartTurnRequest{RequestID: "turn", Session: session.Ref(), Assignment: initial, ResultContract: contract, Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: prompt}}})
	if err != nil {
		t.Fatal(safeFailure("start_turn", err))
	}
	checkpointCtx, stop := context.WithTimeout(ctx, 60*time.Second)
	ready := waitForCheckpoint(checkpointCtx, workspace)
	stop()
	if !ready || !exactAnswer(workspace, "BLUE") {
		t.Fatal("initial BLUE artifact or active checkpoint not independently verified; no steer attempted")
	}
	t.Log("initial BLUE artifact and active local checkpoint independently verified")
	steerCtx, stopSteer := context.WithTimeout(ctx, 30*time.Second)
	defer stopSteer()
	type outcome struct {
		receipt provider.DeliveryReceipt
		err     error
	}
	delivered := make(chan outcome, 1)
	go func() {
		receipt, err := session.Steer(steerCtx, provider.SteerRequest{RequestID: "steer", MessageID: "green", Session: session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: initial, ExpectedRevision: 1, NewAssignment: revised, Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "Revision 2 supersedes BLUE. Write answer.txt containing exactly GREEN followed by one newline. Do not modify checkpoint files or start another checkpoint. If the blocked command was interrupted, proceed directly. Do not access network or other directories. Finish with the requested completed true JSON."}}})
		delivered <- outcome{receipt, err}
	}()
	// OpenCode may abort the blocked command or wait for it while delivering the
	// prompt. Release after the invocation begins, bounded independently of receipt.
	var result outcome
	select {
	case result = <-delivered:
		if releaseCheckpoint(workspace) != nil {
			t.Fatal("checkpoint release failed")
		}
	case <-time.After(5 * time.Second):
		if releaseCheckpoint(workspace) != nil {
			t.Fatal("checkpoint release failed")
		}
		select {
		case result = <-delivered:
		case <-steerCtx.Done():
			t.Fatal("steer receipt deadline; remote delivery unknown; no retry attempted")
		}
	}
	if result.err != nil {
		t.Fatal(safeFailure("steer", result.err))
	}
	receipt := result.receipt
	expectedTarget := provider.TargetNativeTurnID
	if selected == "opencode" {
		expectedTarget = provider.TargetSessionLease
	}
	if receipt.RequestID != "steer" || receipt.MessageID != "green" || receipt.Disposition != provider.DeliveryTransportAccepted || receipt.Receipt != provider.ReceiptNativeKnown || receipt.Targeting != expectedTarget || !receipt.DispatchedFor.Present || receipt.DispatchedFor.Value != revised {
		t.Fatal("revision 2 receipt binding verification failed")
	}
	t.Logf("receipt transport accepted revision=2 targeting=%s", receipt.Targeting)
	candidate := false
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatal("observation ended without terminal evidence; no retry attempted")
		}
		if event.Kind() == provider.EventGap {
			t.Fatal("event history incomplete")
		}
		if body, ok := event.Body().(provider.ResultCandidateEvent); ok {
			c := body.Candidate
			var payload map[string]json.RawMessage
			valid := json.Unmarshal(c.Payload, &payload) == nil && len(payload) == 1 && string(payload["completed"]) == "true"
			candidate = event.Envelope.Session == session.Ref() && event.Envelope.Turn.Present && event.Envelope.Turn.Value == started.Turn && event.Envelope.Assignment.Present && event.Envelope.Assignment.Value == revised && c.Assignment.Present && c.Assignment.Value == revised && c.Native.Session == started.Native.Session && c.Native.Session.Present && c.Native.Turn == started.Native.Turn && c.Native.Turn.Present && c.Source == provider.CandidateNativeStructured && c.Enforcement == provider.SchemaNative && reflect.DeepEqual(c.Contract, contract) && valid
			if !candidate {
				t.Error("candidate failed revision 2 attribution/schema/payload verification")
			}
		}
		if body, ok := event.Body().(provider.TurnStateEvent); ok {
			if body.Current == provider.TurnLost || body.Current == provider.TurnStateUnknown {
				t.Fatal("turn lost or unknown")
			}
			if body.Current == provider.TurnNativeTerminal && event.Envelope.Turn.Present && event.Envelope.Turn.Value == started.Turn {
				if body.TerminalReason != "completed" {
					t.Error("native turn did not complete successfully")
				}
				break
			}
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && body.Current == provider.SessionIdleObserved && selected == "opencode" && exactAnswer(workspace, "GREEN") {
			break
		}
	}
	if !exactAnswer(workspace, "GREEN") {
		t.Error("independent exact GREEN artifact verification failed")
	}
	if selected == "codex" && !candidate {
		t.Error("Codex did not expose a valid revision 2 candidate")
	}
	if selected == "opencode" && candidate {
		t.Error("OpenCode unexpectedly exposed a candidate; contract review required")
	}
	t.Logf("provider=%s revision=2 final_GREEN=%t candidate_verified=%t application_accepted=false", selected, exactAnswer(workspace, "GREEN"), candidate)
}

// No model or adapter is used: validate the exact checkpoint command locally.
func TestSteeringCheckpointRelease(t *testing.T) {
	workspace := t.TempDir()
	writeCheckpoint(t, workspace)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", "./checkpoint.sh")
	command.Dir = workspace
	if err := command.Start(); err != nil {
		t.Fatal("local checkpoint start failed")
	}
	waited := false
	defer func() {
		_ = releaseCheckpoint(workspace)
		cancel()
		if !waited {
			_ = command.Wait()
		}
	}()
	if !waitForCheckpoint(ctx, workspace) {
		t.Fatal("checkpoint not active")
	}
	if err := releaseCheckpoint(workspace); err != nil {
		t.Fatal("release failed")
	}
	waitErr := command.Wait()
	waited = true
	if err := waitErr; err != nil {
		t.Fatal(fmt.Sprintf("local checkpoint failed: %T", err))
	}
	if checkpointActive(workspace) {
		t.Fatal("checkpoint remained active after release")
	}
	if _, err := os.Stat(filepath.Join(workspace, "checkpoint.done")); err != nil {
		t.Fatal("checkpoint did not record completion")
	}
}
