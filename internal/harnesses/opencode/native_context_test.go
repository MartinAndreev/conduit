package opencode

import (
	"context"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func assertSourcesPossible(t *testing.T, report provider.NativeContextReport) {
	t.Helper()
	if len(report.Sources) != len(provider.NativeContextSources()) {
		t.Fatalf("sources = %+v", report.Sources)
	}
	for _, source := range provider.NativeContextSources() {
		if got := report.SourceState(source); got != provider.NativeContextPossible {
			t.Fatalf("%s = %s, want possible", source, got)
		}
	}
}

func TestSnapshotReportsNativeContextDefaults(t *testing.T) {
	report := capabilities(provider.ModelSelection{}, time.Now(), noLaunch()).NativeContext
	assertSourcesPossible(t, report)
	if report.ProtectedPaths != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", report.ProtectedPaths)
	}
}

// The binding is exercised with an entry shaped like a future recording, so
// the cases do not depend on the real (empty) table.
func TestProtectedPathsBindToVersionLaunchAndPlatform(t *testing.T) {
	recorded := rules.RecordedLaunchConfigHash
	evidence := provider.NativeContextEvidenceSet{ProtectedPaths: []provider.ProtectedPathsEvidence{{
		ReadDenied: true, WriteDenied: true, HarnessVersion: "2.0.22", LaunchConfigHash: recorded, Platform: "darwin",
	}}}
	applied := rules.DecideSandbox(rules.SandboxFacts{Darwin: true, SandboxExecFound: true, HomeResolved: true, ProtectedPath: "/Users/u/.conduit"})
	refused := rules.DecideSandbox(rules.SandboxFacts{})
	accepted := rules.DecideServerInfo(rules.ServerInfoFacts{Version: "2.0.22", URLs: []string{testServerURL}, PID: fakeServerPID, ExpectedURL: testServerURL, ExpectedPID: fakeServerPID})
	tests := []struct {
		name   string
		launch launchFacts
		want   provider.ProtectedPathsState
	}{
		{"recorded version, sandboxed launch, darwin", launchFacts{launched: true, sandbox: applied, info: accepted, harnessVersion: "2.0.22", configHash: recorded, platform: "darwin"}, provider.ProtectedPathsEnforced},
		{"other version", launchFacts{launched: true, sandbox: applied, info: accepted, harnessVersion: "2.0.23", configHash: recorded, platform: "darwin"}, provider.ProtectedPathsUnknown},
		{"unknown version", launchFacts{launched: true, sandbox: applied, info: accepted, configHash: recorded, platform: "darwin"}, provider.ProtectedPathsUnknown},
		{"symlinked launch", launchFacts{launched: true, sandbox: applied, info: accepted, harnessVersion: "2.0.22", configHash: launchConfigHash([]string{conduitDirPlaceholder, resolvedConduitDirHolder}), platform: "darwin"}, provider.ProtectedPathsUnknown},
		{"unsandboxed launch with the recorded hash", launchFacts{launched: true, sandbox: refused, info: accepted, harnessVersion: "2.0.22", configHash: recorded, platform: "darwin"}, provider.ProtectedPathsUnknown},
		{"linux", launchFacts{launched: true, sandbox: applied, info: accepted, harnessVersion: "2.0.22", configHash: recorded, platform: "linux"}, provider.ProtectedPathsUnknown},
		{"no launch", launchFacts{harnessVersion: "2.0.22", platform: "darwin"}, provider.ProtectedPathsUnknown},
		{"server not identified", launchFacts{launched: true, sandbox: applied, info: rules.DecideServerInfo(rules.ServerInfoFacts{QueryFailed: true}), configHash: recorded, platform: "darwin"}, provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := nativeContextReport(evidence, tt.launch)
			assertSourcesPossible(t, report)
			if report.ProtectedPaths != tt.want || report.ProtectedPathsReason == "" {
				t.Fatalf("protected paths = %s (%q), want %s", report.ProtectedPaths, report.ProtectedPathsReason, tt.want)
			}
		})
	}
	if got := nativeContextReport(evidence, tests[4].launch).ProtectedPathsReason; got != refused.Reason {
		t.Fatalf("unsandboxed reason = %q, want the sandbox refusal", got)
	}
}

// OpenSession reports the hash and platform of its own server launch.
func TestOpenSessionReportsOwnServerLaunch(t *testing.T) {
	tests := []struct {
		name     string
		host     sandboxHost
		hash     string
		sandbox  rules.SandboxKind
		platform string
		want     provider.ProtectedPathsState
	}{
		{"darwin sandboxed", fakeSandboxHost("darwin", true, "/Users/u", nil), rules.RecordedLaunchConfigHash, rules.SandboxApply, "darwin", provider.ProtectedPathsEnforced},
		{"darwin without sandbox-exec", fakeSandboxHost("darwin", false, "/Users/u", nil), launchConfigHash(nil), rules.SandboxExecMissing, "darwin", provider.ProtectedPathsUnknown},
		{"linux", fakeSandboxHost("linux", false, "/home/u", nil), launchConfigHash(nil), rules.SandboxUnsupportedPlatform, "linux", provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			servers := &fakeServerLauncher{}
			adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: &fakeLauncher{}, serverLauncher: servers, host: tt.host}))
			opened, err := adapter.OpenSession(context.Background(), openRequest())
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref()})
			s := opened.Session.(*session)
			if s.server.configHash != tt.hash || s.server.sandbox.Kind != tt.sandbox {
				t.Fatalf("launch hash = %s sandbox = %#v", s.server.configHash, s.server.sandbox)
			}
			if got := s.LaunchConfigHash(); got != tt.hash {
				t.Fatalf("reported hash = %s", got)
			}
			_, spec := servers.last()
			if (spec.Path == sandboxExecPath) != (tt.sandbox == rules.SandboxApply) {
				t.Fatalf("server spec = %q", spec.Path)
			}
			info := rules.DecideServerInfo(rules.ServerInfoFacts{Version: "2.0.22", URLs: []string{testServerURL}, PID: fakeServerPID, ExpectedURL: testServerURL, ExpectedPID: fakeServerPID})
			want := nativeContextReport(rules.NativeContextEvidence(), launchFacts{launched: true, sandbox: s.server.sandbox, info: info, harnessVersion: info.Version, configHash: tt.hash, platform: tt.platform})
			got := opened.Session.Capabilities().NativeContext
			assertSourcesPossible(t, got)
			if got.ProtectedPaths != tt.want || want.ProtectedPaths != tt.want || got.ProtectedPathsReason != want.ProtectedPathsReason {
				t.Fatalf("protected paths = %s (%q), want %s (%q)", got.ProtectedPaths, got.ProtectedPathsReason, tt.want, want.ProtectedPathsReason)
			}
			if got := opened.Session.Capabilities().HarnessVersion; got != "2.0.22" {
				t.Fatalf("harness version = %q, want the server.info version", got)
			}
		})
	}
}

func TestInspectProtectedPathsStayUnknown(t *testing.T) {
	servers := &fakeServerLauncher{}
	descriptor, err := New(withFakeServer(Config{serverLauncher: servers})).Inspect(context.Background(), provider.InspectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	assertSourcesPossible(t, descriptor.Capabilities.NativeContext)
	if got := descriptor.Capabilities.NativeContext.ProtectedPaths; got != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", got)
	}
	if len(servers.specs) != 0 {
		t.Fatal("Inspect launched a server")
	}
}

// An unparseable version from the proven own server keeps the session: the
// version stays unknown and protected paths report why.
func TestOpenSessionServerInfoRejectionKeepsVersionUnknown(t *testing.T) {
	badVersion := ServerInfoObservation{Version: "2.0.22-dev", PID: fakeServerPID, URLs: []string{testServerURL}}
	tests := []struct {
		name    string
		control *fakeControl
		want    rules.ServerInfoDecision
	}{
		{"invalid version", &fakeControl{info: &badVersion}, rules.DecideServerInfo(rules.ServerInfoFacts{Version: badVersion.Version, URLs: badVersion.URLs, PID: badVersion.PID, ExpectedURL: testServerURL, ExpectedPID: fakeServerPID})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := New(withFakeServer(Config{Control: tt.control, Launcher: &fakeLauncher{}, host: fakeSandboxHost("darwin", true, "/Users/u", nil)}))
			opened, err := adapter.OpenSession(context.Background(), openRequest())
			if err != nil {
				t.Fatalf("open failed: %v", err)
			}
			defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref()})
			if tt.control.infoRequest.Server.URL != testServerURL || tt.control.infoRequest.Directory != "/work" {
				t.Fatalf("server.info request = %q in %q", tt.control.infoRequest.Server.URL, tt.control.infoRequest.Directory)
			}
			capabilities := opened.Session.Capabilities()
			if capabilities.HarnessVersion != "" || capabilities.NativeContext.ProtectedPaths != provider.ProtectedPathsUnknown || capabilities.NativeContext.ProtectedPathsReason != tt.want.Reason {
				t.Fatalf("version = %q protected paths = %s (%q)", capabilities.HarnessVersion, capabilities.NativeContext.ProtectedPaths, capabilities.NativeContext.ProtectedPathsReason)
			}
		})
	}
}
