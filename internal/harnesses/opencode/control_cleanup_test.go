package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// A reconciliation query whose control command could not be stopped leaves
// its cleanup owner with the session: Close does not report resources
// released until it completed, and a later Close observes the completion.
func TestReconcileControlCleanupIsAccountedByClose(t *testing.T) {
	hung := newFakeServerProcess("")
	hung.killErr = errFakeKill
	defer hung.exit()
	owner := processruntime.NewCleanupOwner(hung)
	owner.BeginCleanup()
	control := &fakeControl{promptErr: errors.New("reply lost"), promptSent: true, queryErr: errors.Join(context.DeadlineExceeded, &CleanupPendingError{Owner: owner, Cause: errors.New("not stopped")})}
	reconciled := make(chan rules.SteerReconcileKind, 1)
	fixture := newReconcileFixture(t, control, 20*time.Millisecond, func(s *session) {
		s.config.StopGrace = 100 * time.Millisecond
		s.steerReconcileApplied = func(kind rules.SteerReconcileKind) { reconciled <- kind }
	})
	_, _ = fixture.session.Steer(context.Background(), provider.SteerRequest{Session: fixture.session.Ref(), ExpectedActiveTurn: fixture.started.Turn, CurrentAssignment: fixture.assigned, ExpectedRevision: fixture.assigned.Revision, NewAssignment: nextRevision(fixture.assigned), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	select {
	case kind := <-reconciled:
		if kind != rules.SteerReconcileDoubtRemains {
			t.Fatalf("reconcile = %s", kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reconciliation did not run")
	}
	closeSession := func() provider.CloseSessionResult {
		t.Helper()
		result, err := fixture.session.Close(context.Background(), provider.CloseSessionRequest{Session: fixture.session.Ref(), Policy: provider.CloseInterruptThenClose})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	start := time.Now()
	first := closeSession()
	if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
		t.Fatalf("close with a pending control cleanup = %#v", first)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("close was not bounded")
	}
	hung.exit()
	select {
	case <-owner.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("control cleanup did not complete")
	}
	if again := closeSession(); !again.ResourcesReleased || again.ExecutionState != provider.ExecutionNone {
		t.Fatalf("close after the control cleanup completed = %#v", again)
	}
}

func TestControlIDsCarryingTheSecretAreRefused(t *testing.T) {
	plain := testServerPassword
	// The JSON spells the first letter as a Unicode escape: the encoded
	// response does not contain the secret, only the decoded ID does.
	escaped := `\u0074` + testServerPassword[1:]
	if strings.Contains(escaped, testServerPassword) {
		t.Fatal("escaped fixture contains the plain secret")
	}
	var decoded string
	if err := json.Unmarshal([]byte(`"`+escaped+`"`), &decoded); err != nil || decoded != testServerPassword {
		t.Fatalf("escaped fixture decodes to %q", decoded)
	}
	for name, id := range map[string]string{"plain": "ses_" + plain, "escaped": "ses_" + escaped} {
		t.Run("session.create "+name, func(t *testing.T) {
			launcher := &fakeLauncher{output: `{"data":{"id":"` + id + `","model":{"providerID":"opencode","id":"model"}}}`}
			control := &cliControl{executable: "opencode", launcher: launcher}
			result, sent, err := control.CreateSession(context.Background(), CreateSessionRequest{Server: testEndpoint, Directory: "/work", Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}})
			if !errors.Is(err, errSecretInNativeID) || !sent || result.SessionID != "" || strings.Contains(err.Error(), testServerPassword) {
				t.Fatalf("result = %#v sent = %t err = %v", result, sent, err)
			}
		})
		t.Run("session.prompt "+name, func(t *testing.T) {
			launcher := &fakeLauncher{output: `{"data":{"id":"msg_` + id + `","sessionID":"native-session"}}`}
			control := &cliControl{executable: "opencode", launcher: launcher}
			result, sent, err := control.Prompt(context.Background(), PromptRequest{Server: testEndpoint, SessionID: "native-session", Directory: "/work", Text: "x", Delivery: "steer"})
			if !errors.Is(err, errSecretInNativeID) || !sent || result.ReceiptID != "" {
				t.Fatalf("result = %#v sent = %t err = %v", result, sent, err)
			}
		})
	}
}

func TestOpenSessionRefusesSessionIDCarryingTheSecret(t *testing.T) {
	servers := &fakeServerLauncher{}
	control := &failingCreateControl{sent: true, err: errSecretInNativeID}
	adapter := New(withFakeServer(Config{Control: control, serverLauncher: servers}))
	_, err := adapter.OpenSession(context.Background(), openRequest())
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorProtocolViolation || providerErr.RemoteEffect != provider.RemoteEffectAccepted {
		t.Fatalf("error = %#v", err)
	}
	if strings.Contains(fmt.Sprint(err), testServerPassword) {
		t.Fatal("error carries the password")
	}
	if server, _ := servers.last(); server.kills.Load() == 0 {
		t.Fatal("own server was not stopped")
	}
}

// A steer whose receipt ID carries the secret publishes no receipt with it.
func TestSteerReceiptCarryingTheSecretIsNotPublished(t *testing.T) {
	control := &fakeControl{promptErr: errSecretInNativeID, promptSent: true}
	fixture := newReconcileFixture(t, control, 200*time.Millisecond)
	_, err := fixture.session.Steer(context.Background(), provider.SteerRequest{Session: fixture.session.Ref(), ExpectedActiveTurn: fixture.started.Turn, CurrentAssignment: fixture.assigned, ExpectedRevision: fixture.assigned.Revision, NewAssignment: nextRevision(fixture.assigned), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	if !errors.Is(err, errSecretInNativeID) || strings.Contains(fmt.Sprint(err), testServerPassword) {
		t.Fatalf("steer = %v", err)
	}
}

// blockedPendingQueryControl blocks the delivery query until released, then
// answers with a still pending control cleanup.
type blockedPendingQueryControl struct {
	*fakeControl
	entered chan struct{}
	release chan struct{}
	owner   *processruntime.CleanupOwner
}

func (b *blockedPendingQueryControl) QuerySteerDelivery(context.Context, SteerDeliveryQuery) (SteerDeliveryObservation, error) {
	close(b.entered)
	<-b.release
	return SteerDeliveryObservation{}, errors.Join(context.DeadlineExceeded, &CleanupPendingError{Owner: b.owner, Cause: errors.New("not stopped")})
}

// Close while a reconciliation query is still in flight must not report
// resources released, and the owner that query registers after Close took
// its snapshot is accounted for by a later Close.
func TestCloseCountsInFlightControlQuery(t *testing.T) {
	hung := newFakeServerProcess("")
	hung.killErr = errFakeKill
	defer hung.exit()
	owner := processruntime.NewCleanupOwner(hung)
	owner.BeginCleanup()
	control := &blockedPendingQueryControl{fakeControl: &fakeControl{promptErr: errors.New("reply lost"), promptSent: true}, entered: make(chan struct{}), release: make(chan struct{}), owner: owner}
	fixture := newReconcileFixture(t, control, 20*time.Millisecond, func(s *session) { s.config.StopGrace = 100 * time.Millisecond })
	_, _ = fixture.session.Steer(context.Background(), provider.SteerRequest{Session: fixture.session.Ref(), ExpectedActiveTurn: fixture.started.Turn, CurrentAssignment: fixture.assigned, ExpectedRevision: fixture.assigned.Revision, NewAssignment: nextRevision(fixture.assigned), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	select {
	case <-control.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reconciliation query did not start")
	}
	closeSession := func() provider.CloseSessionResult {
		t.Helper()
		result, err := fixture.session.Close(context.Background(), provider.CloseSessionRequest{Session: fixture.session.Ref(), Policy: provider.CloseInterruptThenClose})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := closeSession(); result.ResourcesReleased || result.ExecutionState != provider.ExecutionNone {
		t.Fatalf("close with a query in flight = %#v", result)
	}
	// The query now returns and registers its pending owner.
	close(control.release)
	s := fixture.session.(*session)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var ops, owners int
		_ = lockscopeRead(s, func() { ops, owners = s.controlOps, len(s.controlCleanups) })
		if ops == 0 && owners == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ops = %d owners = %d", ops, owners)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if result := closeSession(); result.ResourcesReleased {
		t.Fatalf("close with a pending owner registered after the snapshot = %#v", result)
	}
	hung.exit()
	<-owner.Done()
	if result := closeSession(); !result.ResourcesReleased {
		t.Fatalf("close after completion = %#v", result)
	}
}

func lockscopeRead(s *session, read func()) error {
	return lockscope.WithMutexLock(&s.mu, func() error { read(); return nil })
}
