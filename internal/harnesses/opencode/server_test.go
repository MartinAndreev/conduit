package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func openRequest() provider.OpenSessionRequest {
	return provider.OpenSessionRequest{
		RequestID: "open", Assignment: provider.AssignmentRef{AssignmentID: "x", Revision: 1},
		Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone}, PermissionMode: provider.PermissionDeny,
	}
}

func linuxLaunch() serverLaunch {
	return planServerLaunch("opencode", "/work", testServerPassword, fakeSandboxHost("linux", false, "/home/u", nil).sandboxFacts())
}

func startTestServer(t *testing.T, ctx context.Context, servers *fakeServerLauncher, timeout time.Duration) (*ownedServer, error) {
	t.Helper()
	server, err := startOwnedServer(ctx, servers, linuxLaunch(), testServerPassword, timeout, 200*time.Millisecond)
	if server != nil {
		t.Cleanup(func() { _ = server.stop(context.Background()) })
	}
	return server, err
}

func TestServerReadinessAcceptsExactListeningLine(t *testing.T) {
	for name, script := range map[string]string{
		"listening line":         testListeningLine,
		"after other output":     "INFO booting\n\nwarming up\n" + testListeningLine,
		"output after listening": testListeningLine + "INFO request\n",
	} {
		t.Run(name, func(t *testing.T) {
			server, err := startTestServer(t, context.Background(), &fakeServerLauncher{script: script}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if server.endpoint.URL != testServerURL || server.endpoint.password == nil || server.endpoint.password() != testServerPassword {
				t.Fatalf("endpoint URL = %q", server.endpoint.URL)
			}
		})
	}
}

func TestServerReadinessFailures(t *testing.T) {
	tests := []struct {
		name      string
		script    string
		configure func(*fakeServerProcess)
		ctx       func() context.Context
		want      func(error) bool
	}{
		{"wrong host", "server listening on http://0.0.0.0:4096\n", nil, nil, func(err error) bool { return err != nil && !errors.Is(err, errServerReadyTimeout) }},
		{"bad port", "server listening on http://127.0.0.1:0\n", nil, nil, func(err error) bool { return err != nil && !errors.Is(err, errServerReadyTimeout) }},
		{"generated password", "server password SECRET-VALUE\n" + testListeningLine, nil, nil, func(err error) bool { return err != nil && !errors.Is(err, errServerReadyTimeout) }},
		{"garbage until timeout", "garbage SECRET-VALUE\n", nil, nil, func(err error) bool { return errors.Is(err, errServerReadyTimeout) }},
		{"line bound", strings.Repeat("x", maxServerLineBytes+1), nil, nil, func(err error) bool { return errors.Is(err, errServerOutputBound) }},
		{"early exit, stdout held", "booting\n", func(p *fakeServerProcess) { p.keepStdout = true; p.exit() }, nil, func(err error) bool { return errors.Is(err, errServerExited) }},
		{"early exit", "booting\n", func(p *fakeServerProcess) { p.exit() }, nil, func(err error) bool {
			return errors.Is(err, errServerExited) || errors.Is(err, errServerOutputEnded)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx()
			}
			servers := &fakeServerLauncher{script: tt.script, configure: tt.configure}
			server, err := startTestServer(t, ctx, servers, 100*time.Millisecond)
			if server == nil || !tt.want(err) {
				t.Fatalf("server = %v err = %v", server, err)
			}
			if strings.Contains(err.Error(), "SECRET-VALUE") {
				t.Fatal("readiness error quotes server output")
			}
			if decision := server.stop(context.Background()); !decision.Confirmed() {
				t.Fatalf("stop = %#v", decision)
			}
			if proc, _ := servers.last(); proc.kills.Load() == 0 {
				t.Fatal("failed server was not terminated")
			}
		})
	}
}

type failingCreateControl struct {
	fakeControl
	sent  bool
	model provider.ModelSelection
	err   error
}

func (f *failingCreateControl) CreateSession(_ context.Context, request CreateSessionRequest) (CreateSessionResult, bool, error) {
	f.create = request
	if f.err != nil {
		return CreateSessionResult{}, f.sent, f.err
	}
	return CreateSessionResult{SessionID: "native-session", Model: f.model, ModelVerified: true}, true, nil
}

func TestOpenSessionStopsServerOnFailure(t *testing.T) {
	tests := []struct {
		name    string
		servers *fakeServerLauncher
		control *failingCreateControl
		code    provider.ErrorCode
		effect  provider.RemoteEffect
		created bool
	}{
		{"launch failure", &fakeServerLauncher{startErr: errors.New("exec: not found")}, &failingCreateControl{}, provider.ErrorExecutableMissing, provider.RemoteEffectNotSent, false},
		{"rejected address", &fakeServerLauncher{script: "server listening on http://10.0.0.1:4096\n"}, &failingCreateControl{}, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, false},
		{"generated password", &fakeServerLauncher{script: "server password SECRET-VALUE\n"}, &failingCreateControl{}, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, false},
		{"readiness timeout", &fakeServerLauncher{script: "booting\n"}, &failingCreateControl{}, provider.ErrorDeadline, provider.RemoteEffectNotSent, false},
		{"create failed after dispatch", &fakeServerLauncher{}, &failingCreateControl{sent: true, err: errors.New("lost reply")}, provider.ErrorDeliveryUnknown, provider.RemoteEffectPossiblySent, true},
		{"create model mismatch", &fakeServerLauncher{}, &failingCreateControl{model: provider.ModelSelection{ProviderID: "other", ModelID: "m"}}, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := New(withFakeServer(Config{Control: tt.control, Launcher: &fakeLauncher{}, serverLauncher: tt.servers, ServerReadyTimeout: 100 * time.Millisecond, StopGrace: 200 * time.Millisecond}))
			_, err := adapter.OpenSession(context.Background(), openRequest())
			var providerErr *provider.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != tt.code || providerErr.RemoteEffect != tt.effect || providerErr.Operation != provider.OperationOpenSession {
				t.Fatalf("error = %#v", err)
			}
			if strings.Contains(fmt.Sprint(err), "SECRET-VALUE") || strings.Contains(fmt.Sprint(err), testServerPassword) {
				t.Fatal("open error carries server output or the password")
			}
			if (tt.control.create.Directory != "") != tt.created {
				t.Fatalf("session.create sent = %t", tt.control.create.Directory != "")
			}
			if tt.created && tt.control.create.Server.URL != testServerURL {
				t.Fatalf("session.create server = %q", tt.control.create.Server.URL)
			}
			proc, _ := tt.servers.last()
			if proc != nil && proc.kills.Load() == 0 {
				t.Fatal("server of a failed open was not stopped")
			}
		})
	}
}

func TestOpenSessionPasswordFailureLaunchesNothing(t *testing.T) {
	servers := &fakeServerLauncher{}
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, serverLauncher: servers, newPassword: func() (string, error) { return "", errors.New("entropy") }}))
	if _, err := adapter.OpenSession(context.Background(), openRequest()); err == nil {
		t.Fatal("open succeeded without a password")
	}
	if len(servers.specs) != 0 {
		t.Fatal("server launched without a password")
	}
}

// A failed open whose server stop is unconfirmed returns the cleanup owner in
// the error, so its eventual completion stays observable.
func TestOpenSessionUnconfirmedStopIsObservable(t *testing.T) {
	servers := &fakeServerLauncher{script: "booting\n", configure: func(p *fakeServerProcess) { p.killErr = errFakeKill }}
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, serverLauncher: servers, ServerReadyTimeout: 50 * time.Millisecond, StopGrace: 50 * time.Millisecond}))
	_, err := adapter.OpenSession(context.Background(), openRequest())
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorDeadline || !errors.Is(err, errServerReadyTimeout) {
		t.Fatalf("error = %#v", err)
	}
	var pending *CleanupPendingError
	if !errors.As(err, &pending) || pending.Owner == nil || !strings.Contains(pending.Cause.Error(), "native execution is unknown") {
		t.Fatalf("no observable cleanup in %v", err)
	}
	select {
	case <-pending.Owner.Done():
		t.Fatal("cleanup reported complete while the server runs")
	default:
	}
	if latest := pending.Owner.Latest(); !latest.TreeTerminationRequested || latest.TreeTerminationErr == nil || latest.RootReaped {
		t.Fatalf("latest = %#v", latest)
	}
	// The server exits later on its own; the returned owner observes it.
	proc, _ := servers.last()
	proc.exit()
	select {
	case <-pending.Owner.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("eventual cleanup completion was not observable")
	}
	if !pending.Owner.Latest().RootReaped {
		t.Fatal("completed cleanup did not reap the server")
	}
}

func TestCancelledOpenLaunchesNoServer(t *testing.T) {
	servers := &fakeServerLauncher{}
	queried := false
	host := fakeSandboxHost("darwin", true, "/Users/u", nil)
	host.homeDir = func() (string, error) { queried = true; return "/Users/u", nil }
	control := &fakeControl{}
	adapter := New(withFakeServer(Config{Control: control, serverLauncher: servers, host: host}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adapter.OpenSession(ctx, openRequest())
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCallerCancelled || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("error = %#v", err)
	}
	if len(servers.specs) != 0 || queried || control.create.Directory != "" {
		t.Fatalf("cancelled open launched %d servers (host queried %t)", len(servers.specs), queried)
	}
}

// Cancellation during the host queries is seen right before the launch.
func TestCancelDuringHostQueriesLaunchesNoServer(t *testing.T) {
	servers := &fakeServerLauncher{}
	ctx, cancel := context.WithCancel(context.Background())
	host := fakeSandboxHost("darwin", true, "/Users/u", nil)
	host.homeDir = func() (string, error) { cancel(); return "/Users/u", nil }
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, serverLauncher: servers, host: host}))
	_, err := adapter.OpenSession(ctx, openRequest())
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCallerCancelled || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("error = %#v", err)
	}
	if len(servers.specs) != 0 {
		t.Fatal("server launched after cancellation")
	}
}

// server.info must prove the server is ours; any other outcome fails the
// open before anything is sent.
func TestServerInfoWithoutProofFailsOpen(t *testing.T) {
	other := ServerInfoObservation{Version: "2.0.22", PID: fakeServerPID, URLs: []string{"http://127.0.0.1:9999"}}
	wrongPID := ServerInfoObservation{Version: "2.0.22", PID: fakeServerPID + 1, URLs: []string{testServerURL}}
	controls := map[string]*fakeControl{
		"other server":  {info: &other},
		"other process": {info: &wrongPID},
		"query failed":  {infoErr: errors.New("unreachable 9999")},
	}
	for name, control := range controls {
		t.Run(name, func(t *testing.T) {
			servers := &fakeServerLauncher{}
			adapter := New(withFakeServer(Config{Control: control, serverLauncher: servers}))
			_, err := adapter.OpenSession(context.Background(), openRequest())
			var providerErr *provider.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorProtocolViolation || providerErr.RemoteEffect != provider.RemoteEffectNotSent || providerErr.Detail == "" {
				t.Fatalf("error = %#v", err)
			}
			if strings.Contains(providerErr.Detail, "9999") || strings.Contains(fmt.Sprint(err), testServerPassword) {
				t.Fatal("error detail quotes the server.info answer, or the error carries the password")
			}
			if control.create.Directory != "" {
				t.Fatal("session.create was sent to an unidentified server")
			}
			if proc, _ := servers.last(); proc.kills.Load() == 0 {
				t.Fatal("own server was not stopped")
			}
		})
	}
}

// Every client command of a session targets the session's own server and
// carries its password in the environment.
func TestClientCommandsTargetOwnServer(t *testing.T) {
	assertClient := func(t *testing.T, spec processruntime.Spec, subcommand string) {
		t.Helper()
		if len(spec.Args) < 3 || spec.Args[0] != subcommand || spec.Args[1] != "--server" || spec.Args[2] != testServerURL {
			t.Fatalf("%s args = %q", subcommand, spec.Args)
		}
		if spec.ReplaceEnv || !slices.Equal(spec.Env, []string{"OPENCODE_PASSWORD=" + testServerPassword}) {
			t.Fatalf("%s env is not exactly the client password", subcommand)
		}
	}
	t.Run("session.create", func(t *testing.T) {
		launcher := &fakeLauncher{output: `{"data":{"id":"native-session","model":{"providerID":"opencode","id":"model"}}}`}
		control := &cliControl{executable: "opencode", launcher: launcher}
		if _, _, err := control.CreateSession(context.Background(), CreateSessionRequest{Server: testEndpoint, Directory: "/work", Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}}); err != nil {
			t.Fatal(err)
		}
		assertClient(t, launcher.spec, "api")
	})
	t.Run("session.prompt", func(t *testing.T) {
		launcher := &fakeLauncher{output: `{"data":{"id":"m","sessionID":"native-session"}}`}
		control := &cliControl{executable: "opencode", launcher: launcher}
		if _, _, err := control.Prompt(context.Background(), PromptRequest{Server: testEndpoint, SessionID: "native-session", Directory: "/work", Text: "x", Delivery: "steer"}); err != nil {
			t.Fatal(err)
		}
		assertClient(t, launcher.spec, "api")
	})
	t.Run("steer delivery query", func(t *testing.T) {
		launcher := &scriptedLauncher{responses: []scriptedResponse{{stdout: `{"data":{"id":"` + steerQueryMessage + `"}}`}}}
		control := &cliControl{executable: "opencode", launcher: launcher}
		if _, err := control.QuerySteerDelivery(context.Background(), SteerDeliveryQuery{Server: testEndpoint, SessionID: "ses_1", Directory: "/work", MessageID: steerQueryMessage}); err != nil {
			t.Fatal(err)
		}
		assertClient(t, launcher.specs[0], "api")
	})
	t.Run("released steer query", func(t *testing.T) {
		launcher := &scriptedLauncher{responses: []scriptedResponse{historyPage("", historyItem(steerQueryMessage, "user"))}}
		control := &cliControl{executable: "opencode", launcher: launcher}
		if _, err := control.QueryReleasedSteer(context.Background(), SteerDeliveryQuery{Server: testEndpoint, SessionID: "ses_1", Directory: "/work", MessageID: steerQueryMessage}); err != nil {
			t.Fatal(err)
		}
		assertClient(t, launcher.specs[0], "api")
	})
	t.Run("server.info", func(t *testing.T) {
		launcher := &scriptedLauncher{responses: []scriptedResponse{{stdout: `{"version":"2.0.22","pid":11,"urls":["` + testServerURL + `"],"paths":{"home":"/Users/u"}}`}}}
		control := &cliControl{executable: "opencode", launcher: launcher}
		got, err := control.ServerInfo(context.Background(), ServerInfoRequest{Server: testEndpoint, Directory: "/work"})
		if err != nil || got.Version != "2.0.22" || got.PID != 11 || !slices.Equal(got.URLs, []string{testServerURL}) {
			t.Fatalf("info = %#v err = %v", got, err)
		}
		assertClient(t, launcher.specs[0], "api")
		if !slices.Equal(launcher.specs[0].Args[3:], []string{"server.info"}) {
			t.Fatalf("server.info args = %q", launcher.specs[0].Args)
		}
	})
	t.Run("run", func(t *testing.T) {
		launcher := &fakeLauncher{}
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: launcher}))
		opened, err := adapter.OpenSession(context.Background(), openRequest())
		if err != nil {
			t.Fatal(err)
		}
		request := openRequest()
		if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract}); err != nil {
			t.Fatal(err)
		}
		assertClient(t, launcher.spec, "run")
	})
}

// Steering and reconciliation hand the session's own server to Control.
func TestSessionControlRequestsCarryOwnServer(t *testing.T) {
	control := &fakeControl{promptErr: errors.New("lost"), promptSent: true, queryObservation: SteerDeliveryObservation{InHistory: true}}
	fixture := newReconcileFixture(t, control, 50*time.Millisecond)
	_, _ = fixture.session.Steer(context.Background(), provider.SteerRequest{Session: fixture.session.Ref(), ExpectedActiveTurn: fixture.started.Turn, CurrentAssignment: fixture.assigned, ExpectedRevision: fixture.assigned.Revision, NewAssignment: nextRevision(fixture.assigned), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	if control.create.Server.URL != testServerURL || control.prompt.Server.URL != testServerURL {
		t.Fatalf("create server = %q prompt server = %q", control.create.Server.URL, control.prompt.Server.URL)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		query, calls := control.queried()
		if calls > 0 {
			if query.Server.URL != testServerURL || query.Server.password == nil {
				t.Fatalf("query server = %q", query.Server.URL)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("steer delivery was never queried")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func nextRevision(a provider.AssignmentRef) provider.AssignmentRef {
	a.Revision++
	return a
}

func TestCLIControlServerInfoFailures(t *testing.T) {
	for name, response := range map[string]scriptedResponse{
		"nonzero exit": {stdout: `{"version":"2.0.22"}`, exitCode: 1},
		"not JSON":     {stdout: "requires a password"},
	} {
		t.Run(name, func(t *testing.T) {
			control := &cliControl{executable: "opencode", launcher: &scriptedLauncher{responses: []scriptedResponse{response}}}
			if _, err := control.ServerInfo(context.Background(), ServerInfoRequest{Server: testEndpoint}); err == nil {
				t.Fatal("unreadable server.info accepted")
			}
		})
	}
	control := &cliControl{executable: "opencode", launcher: &scriptedLauncher{}}
	if _, err := control.ServerInfo(context.Background(), ServerInfoRequest{}); !errors.Is(err, errNoServer) {
		t.Fatalf("server.info without a server: %v", err)
	}
}

func TestSteerAfterServerDeathIsRefused(t *testing.T) {
	control := &fakeControl{}
	run := newKillableProcess()
	defer run.KillTree()
	adapter := New(withFakeServer(Config{Control: control, Launcher: &killableLauncher{proc: run}, StopGrace: 200 * time.Millisecond}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	session := opened.Session
	request := openRequest()
	started, err := session.StartTurn(context.Background(), provider.StartTurnRequest{Session: session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract})
	if err != nil {
		t.Fatal(err)
	}
	server, _ := adapter.config.serverLauncher.(*fakeServerLauncher).last()
	server.exit()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
		if snapshot.SessionState == provider.SessionLost {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server death not observed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err = session.Steer(context.Background(), provider.SteerRequest{Session: session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: request.Assignment, ExpectedRevision: request.Assignment.Revision, NewAssignment: nextRevision(request.Assignment), Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "fix"}}})
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost || providerErr.RemoteEffect != provider.RemoteEffectNotSent || providerErr.Detail == "" {
		t.Fatalf("steer after server death = %#v", err)
	}
	if control.promptCalls != 0 {
		t.Fatal("steer was sent to a dead server")
	}
}

func TestClientCommandWithoutServerIsRefused(t *testing.T) {
	launcher := &fakeLauncher{}
	control := &cliControl{executable: "opencode", launcher: launcher}
	_, sent, err := control.CreateSession(context.Background(), CreateSessionRequest{Directory: "/work"})
	if !errors.Is(err, errNoServer) || sent || launcher.starts != 0 {
		t.Fatalf("err = %v sent = %t starts = %d", err, sent, launcher.starts)
	}
	_, err = control.QuerySteerDelivery(context.Background(), SteerDeliveryQuery{Server: ServerEndpoint{URL: testServerURL}, SessionID: "s", MessageID: "m"})
	if !errors.Is(err, errNoServer) || launcher.starts != 0 {
		t.Fatalf("query without password err = %v", err)
	}
}

func TestPasswordIsNotFormatted(t *testing.T) {
	request := PromptRequest{Server: testEndpoint, Text: "x"}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if strings.Contains(fmt.Sprintf(format, request), testServerPassword) || strings.Contains(fmt.Sprintf(format, testEndpoint), testServerPassword) {
			t.Fatalf("%s prints the password", format)
		}
	}
}

func TestNewServerPassword(t *testing.T) {
	a, err := newServerPassword()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newServerPassword()
	if len(a) != 64 || a == b || strings.Trim(a, "0123456789abcdef") != "" {
		t.Fatalf("password shape is wrong (length %d)", len(a))
	}
}

// openOwned opens a session whose server is a fake from servers and whose run
// clients come from runs.
func openOwned(t *testing.T, runs processruntime.Launcher, servers *fakeServerLauncher) (provider.Session, *fakeServerProcess) {
	t.Helper()
	adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: runs, serverLauncher: servers, StopGrace: 200 * time.Millisecond}))
	opened, err := adapter.OpenSession(context.Background(), openRequest())
	if err != nil {
		t.Fatal(err)
	}
	proc, _ := servers.last()
	return opened.Session, proc
}

func startActive(t *testing.T, session provider.Session) {
	t.Helper()
	request := openRequest()
	if _, err := session.StartTurn(context.Background(), provider.StartTurnRequest{Session: session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract}); err != nil {
		t.Fatal(err)
	}
}

func TestCloseStopsOwnServerUnderEveryPolicy(t *testing.T) {
	tests := []struct {
		name   string
		active bool
		policy provider.ClosePolicy
	}{
		{"idle require_quiescent", false, provider.CloseRequireQuiescent},
		{"idle interrupt_then_close", false, provider.CloseInterruptThenClose},
		{"idle explicit_detach", false, provider.CloseExplicitDetach},
		{"active interrupt_then_close", true, provider.CloseInterruptThenClose},
		{"active explicit_detach", true, provider.CloseExplicitDetach},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newKillableProcess()
			defer run.KillTree()
			session, server := openOwned(t, &killableLauncher{proc: run}, &fakeServerLauncher{})
			if tt.active {
				startActive(t, session)
			}
			result, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: tt.policy})
			if err != nil {
				t.Fatal(err)
			}
			if !result.HandleClosed || result.ExecutionState != provider.ExecutionNone || !result.ResourcesReleased {
				t.Fatalf("close = %#v", result)
			}
			if server.kills.Load() == 0 {
				t.Fatal("own server was not stopped")
			}
			select {
			case <-run.killed:
				if !tt.active {
					t.Fatal("idle close terminated a run client")
				}
			default:
				if tt.active {
					t.Fatal("active run client outlived the session")
				}
			}
			again, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: tt.policy})
			if err != nil || again.ExecutionState != provider.ExecutionNone || server.kills.Load() != 1 {
				t.Fatalf("repeated close = %#v err = %v kills = %d", again, err, server.kills.Load())
			}
		})
	}
}

func TestCloseKeepsExecutionUnknownWhenStopUnconfirmed(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeServerProcess)
	}{
		{"kill refused", func(p *fakeServerProcess) { p.killErr = errFakeKill }},
		{"tree kill unsupported", func(p *fakeServerProcess) { p.noTreeKill = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, server := openOwned(t, &fakeLauncher{}, &fakeServerLauncher{configure: tt.configure})
			defer server.exit()
			start := time.Now()
			result, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseRequireQuiescent})
			if err != nil {
				t.Fatal(err)
			}
			if !result.HandleClosed || result.ExecutionState != provider.ExecutionUnknown || result.ResourcesReleased {
				t.Fatalf("close = %#v", result)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("close was not bounded")
			}
		})
	}
}

func TestCloseRespectsCallerContext(t *testing.T) {
	session, server := openOwned(t, &fakeLauncher{}, &fakeServerLauncher{configure: func(p *fakeServerProcess) { p.killErr = errFakeKill }})
	defer server.exit()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := session.Close(ctx, provider.CloseSessionRequest{Session: session.Ref()})
	if err != nil || result.ExecutionState != provider.ExecutionUnknown || result.ResourcesReleased {
		t.Fatalf("close = %#v err = %v", result, err)
	}
}

func TestRefusedCloseLeavesServerRunning(t *testing.T) {
	run := newKillableProcess()
	defer run.KillTree()
	session, server := openOwned(t, &killableLauncher{proc: run}, &fakeServerLauncher{})
	startActive(t, session)
	if _, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseRequireQuiescent}); err == nil {
		t.Fatal("require_quiescent closed an active turn")
	}
	if server.kills.Load() != 0 {
		t.Fatal("refused close stopped the server")
	}
	if _, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: provider.SessionRef{AdapterSessionID: "other"}, Policy: provider.CloseInterruptThenClose}); err == nil || server.kills.Load() != 0 {
		t.Fatal("stale close stopped the server")
	}
	_, _ = session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseInterruptThenClose})
}

func TestServerDeathLosesSession(t *testing.T) {
	run := newKillableProcess()
	defer run.KillTree()
	session, server := openOwned(t, &killableLauncher{proc: run}, &fakeServerLauncher{})
	stream, err := session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	startActive(t, session)
	server.exit()
	warned, lost := false, false
	for !warned || !lost {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("server death not observed: warned=%t lost=%t err=%v", warned, lost, err)
		}
		switch body := event.Body().(type) {
		case provider.ProtocolWarningEvent:
			warned = warned || body.Code == "opencode_server_exit"
		case provider.SessionStateEvent:
			lost = lost || body.Current == provider.SessionLost
		}
	}
	select {
	case <-run.killed:
	case <-time.After(2 * time.Second):
		t.Fatal("run client of the dead server was not terminated")
	}
	snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
	if snapshot.SessionState != provider.SessionLost {
		t.Fatalf("state = %s, want lost", snapshot.SessionState)
	}
}

func TestIdleServerDeathRefusesNextTurn(t *testing.T) {
	runs := &fakeLauncher{}
	session, server := openOwned(t, runs, &fakeServerLauncher{})
	stream, _ := session.Events(context.Background(), provider.EventCursor{})
	server.exit()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatal("server death not observed")
		}
		if body, ok := event.Body().(provider.SessionStateEvent); ok && body.Current == provider.SessionLost {
			break
		}
	}
	request := openRequest()
	_, err := session.StartTurn(context.Background(), provider.StartTurnRequest{Session: session.Ref(), Assignment: request.Assignment, ResultContract: request.ResultContract})
	if err == nil || runs.starts != 0 {
		t.Fatalf("turn started on a dead server: err = %v starts = %d", err, runs.starts)
	}
	result, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref()})
	if err != nil || result.ExecutionState != provider.ExecutionNone {
		t.Fatalf("close after death = %#v err = %v", result, err)
	}
}

func TestCloseDoesNotReportServerExitAsLoss(t *testing.T) {
	session, _ := openOwned(t, &fakeLauncher{}, &fakeServerLauncher{})
	stream, _ := session.Events(context.Background(), provider.EventCursor{})
	if _, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref()}); err != nil {
		t.Fatal(err)
	}
	for {
		event, err := stream.Receive(context.Background())
		if err != nil {
			break
		}
		if body, ok := event.Body().(provider.ProtocolWarningEvent); ok && body.Code == "opencode_server_exit" {
			t.Fatal("Close reported its own server stop as a loss")
		}
	}
	time.Sleep(50 * time.Millisecond)
	snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
	if snapshot.SessionState != provider.SessionClosed {
		t.Fatalf("state = %s", snapshot.SessionState)
	}
}

const envHelperVar = "OPENCODE_ADAPTER_ENV_HELPER"

// TestEnvHelperProcess is not a test: run by TestPasswordEnvWinsOverInherited
// as a child process, it prints the password variables it received.
func TestEnvHelperProcess(t *testing.T) {
	if os.Getenv(envHelperVar) != "1" {
		t.Skip("helper process only")
	}
	fmt.Printf("%s|%s\n", os.Getenv(clientPasswordEnv), os.Getenv(serverPasswordEnv))
	os.Exit(0)
}

// The appended password must win over an inherited one: os/exec keeps the
// last value of a duplicate key, and internal/process appends Spec.Env to the
// inherited environment.
func TestPasswordEnvWinsOverInherited(t *testing.T) {
	t.Setenv(clientPasswordEnv, "inherited-client")
	t.Setenv(serverPasswordEnv, "inherited-server")
	helper := func(spec processruntime.Spec) string {
		t.Helper()
		spec.Path, spec.Args, spec.Dir = os.Args[0], []string{"-test.run=^TestEnvHelperProcess$"}, ""
		spec.Env = append([]string{envHelperVar + "=1"}, spec.Env...)
		proc, err := processruntime.ExecLauncher{}.Start(spec)
		if err != nil {
			t.Fatal(err)
		}
		_ = proc.Stdin().Close()
		go io.Copy(io.Discard, proc.Stderr())
		out, _ := io.ReadAll(proc.Stdout())
		result := proc.Wait()
		// The helper is never signalled; finalization releases its root.
		if err := processruntime.FinalizeWithin(proc, 5*time.Second); err != nil {
			t.Fatalf("helper release: %v", err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("helper exit = %d", result.ExitCode)
		}
		return strings.TrimSpace(string(out))
	}
	client := helper(clientSpec("opencode", testEndpoint, "", "api"))
	if client != testServerPassword+"|inherited-server" {
		t.Fatal("client did not receive the session password over the inherited one")
	}
	server := helper(linuxLaunch().spec)
	if server != "inherited-client|"+testServerPassword {
		t.Fatal("server did not receive the session password over the inherited one")
	}
}

// hangingControlLauncher starts control commands that never exit and whose
// termination is refused until released.
type hangingControlLauncher struct{ proc *fakeServerProcess }

func (l *hangingControlLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	return l.proc, nil
}

// A server.info call that times out with a refused kill fails the open, and
// the control command's cleanup stays observable through the error.
func TestServerInfoTimeoutKeepsControlCleanupObservable(t *testing.T) {
	hung := newFakeServerProcess("")
	hung.killErr = errFakeKill
	defer hung.exit()
	servers := &fakeServerLauncher{}
	adapter := New(withFakeServer(Config{Launcher: &hangingControlLauncher{proc: hung}, serverLauncher: servers, StopGrace: 50 * time.Millisecond}))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := adapter.OpenSession(ctx, openRequest())
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorProtocolViolation || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("error = %#v", err)
	}
	var pending *CleanupPendingError
	if !errors.As(err, &pending) || pending.Owner == nil {
		t.Fatalf("control cleanup not observable in %v", err)
	}
	if latest := pending.Owner.Latest(); !latest.TreeTerminationRequested || latest.TreeTerminationErr == nil {
		t.Fatalf("latest = %#v", latest)
	}
	if server, _ := servers.last(); server.kills.Load() == 0 {
		t.Fatal("own server was not stopped")
	}
	hung.exit()
	select {
	case <-pending.Owner.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("control cleanup completion not observable")
	}
}

func TestCLIControlCancelledCommandIsCleanedUp(t *testing.T) {
	proc := newFakeServerProcess("")
	control := &cliControl{executable: "opencode", launcher: &hangingControlLauncher{proc: proc}, stopGrace: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A cancelled ctx is refused before the start.
	if _, err := control.ServerInfo(ctx, ServerInfoRequest{Server: testEndpoint}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := control.ServerInfo(ctx, ServerInfoRequest{Server: testEndpoint})
	var pending *CleanupPendingError
	if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &pending) || proc.kills.Load() != 1 {
		t.Fatalf("err = %v kills = %d", err, proc.kills.Load())
	}
}
