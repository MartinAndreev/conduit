package opencode

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Once Close claimed a run client's termination, the run's observation must
// not finalize it: a clean exit would otherwise refuse Close's group kill.
// Without that claim the run finalizes its client itself. readRun runs
// synchronously here, so the decision is observed deterministically.
func TestCloseClaimKeepsRunFromFinalizing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		claimed   bool
		finalizes int32
	}{{"claimed by Close", true, 0}, {"unclaimed", false, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: &fakeLauncher{}}))
			opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{
				Assignment:     provider.AssignmentRef{AssignmentID: "x", Revision: 1},
				Workspace:      provider.WorkspaceBinding{Path: "/work", Writable: true},
				Model:          provider.ModelSelection{ProviderID: "opencode", ModelID: "model"},
				ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone}, PermissionMode: provider.PermissionDeny,
			})
			if err != nil {
				t.Fatal(err)
			}
			s := opened.Session.(*session)
			proc := &fakeProcess{stdout: io.NopCloser(strings.NewReader("")), stderr: io.NopCloser(strings.NewReader(""))}
			client := newRunClient(proc)
			_ = client.closeStdin()
			_ = lockscope.WithMutexLock(&s.mu, func() error {
				s.client = client
				if tc.claimed {
					s.closed = true
					client.claimLocked(rules.ClientActorClose)
				}
				return nil
			})
			s.readRun(client, provider.TurnRef{AdapterTurnID: "turn-1"})
			if got := proc.finalizes.Load(); got != tc.finalizes {
				t.Fatalf("run finalized %d times, want %d", got, tc.finalizes)
			}
		})
	}
}
