package codex

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestLaunchArgumentsSelectConduitPermissionProfile(t *testing.T) {
	tests := []struct {
		access projectRootAccess
		want   []string
	}{
		{projectRootsWrite, []string{
			"app-server",
			"-c", `default_permissions="conduit"`,
			"-c", `permissions.conduit.filesystem={":root"="read",":project_roots"={"."="write"},":slash_tmp"="write",":tmpdir"="write","~/.conduit"="none"}`,
			"-c", "permissions.conduit.network.enabled=false",
		}},
		{projectRootsRead, []string{
			"app-server",
			"-c", `default_permissions="conduit"`,
			"-c", `permissions.conduit.filesystem={":root"="read",":project_roots"={"."="read"},":slash_tmp"="write",":tmpdir"="write","~/.conduit"="none"}`,
			"-c", "permissions.conduit.network.enabled=false",
		}},
	}
	for _, tt := range tests {
		if got := launchArguments(tt.access); !slices.Equal(got, tt.want) {
			t.Fatalf("launchArguments(%s) = %q, want %q", tt.access, got, tt.want)
		}
	}
}

func TestPermissionProfileForMapsSandboxMode(t *testing.T) {
	tests := []struct {
		mode     SandboxMode
		writable bool
		want     projectRootAccess
		kind     rules.PermissionProfileKind
	}{
		{SandboxWorkspaceWrite, true, projectRootsWrite, rules.PermissionProfileWorkspaceWrite},
		{SandboxWorkspaceWrite, false, "", rules.PermissionProfileConflict},
		{SandboxReadOnly, true, projectRootsRead, rules.PermissionProfileWorkspaceRead},
		{SandboxReadOnly, false, projectRootsRead, rules.PermissionProfileWorkspaceRead},
		{"danger-full-access", true, "", rules.PermissionProfileUnsupportedMode},
	}
	for _, tt := range tests {
		access, decision, ok := permissionProfileFor(tt.mode, tt.writable)
		if access != tt.want || decision.Kind != tt.kind || ok != (tt.want != "") {
			t.Fatalf("permissionProfileFor(%s, %t) = %q %#v %t", tt.mode, tt.writable, access, decision, ok)
		}
	}
}

func TestLaunchConfigHashIsStableAndDistinguishesAccess(t *testing.T) {
	write := launchConfigHash(projectRootsWrite)
	if write != launchConfigHash(projectRootsWrite) || len(write) != 64 {
		t.Fatalf("hash = %q", write)
	}
	if write == launchConfigHash(projectRootsRead) {
		t.Fatal("read and write launches share a hash")
	}
	if got := configuredLaunchConfigHash(SandboxWorkspaceWrite); got != write {
		t.Fatalf("configured workspace-write hash = %q, want %q", got, write)
	}
	if got := configuredLaunchConfigHash(SandboxReadOnly); got != launchConfigHash(projectRootsRead) {
		t.Fatalf("configured read-only hash = %q", got)
	}
	if got := configuredLaunchConfigHash("danger-full-access"); got != "" {
		t.Fatalf("unsupported mode hash = %q, want empty", got)
	}
}

func TestLaunchConfigHashCoversEveryArgument(t *testing.T) {
	args := launchArguments(projectRootsWrite)
	base := hashLaunchArguments(args)
	for index := range args {
		changed := slices.Clone(args)
		changed[index] += "x"
		if hashLaunchArguments(changed) == base {
			t.Fatalf("changing %q did not change the hash", args[index])
		}
		if hashLaunchArguments(slices.Delete(slices.Clone(args), index, index+1)) == base {
			t.Fatalf("removing %q did not change the hash", args[index])
		}
	}
	if hashLaunchArguments([]string{"ab", "c"}) == hashLaunchArguments([]string{"a", "bc"}) {
		t.Fatal("argument boundaries are not hashed")
	}
}

// Every recorded entry must carry the current workspace-write launch hash. A
// failure means the launch changed: record new live evidence, then update the
// literal in rules. Never update the literal alone.
func TestEvidenceIsBoundToCurrentLaunch(t *testing.T) {
	evidence := rules.NativeContextEvidence()
	current := launchConfigHash(projectRootsWrite)
	if len(evidence.Sources) != 0 {
		t.Fatalf("source evidence = %+v, want none", evidence.Sources)
	}
	if len(evidence.ProtectedPaths) != 1 {
		t.Fatalf("protected paths evidence = %+v, want one entry", evidence.ProtectedPaths)
	}
	for _, entry := range evidence.ProtectedPaths {
		if !entry.ReadDenied || !entry.WriteDenied || entry.HarnessVersion == "" || entry.Platform == "" {
			t.Fatalf("entry = %+v", entry)
		}
		if entry.LaunchConfigHash != current {
			t.Fatalf("recorded hash = %s, current workspace-write launch hash = %s", entry.LaunchConfigHash, current)
		}
	}
}

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

// The binding is exercised with an entry shaped like the recording, so the
// cases do not depend on the real table.
func TestProtectedPathsBindToVersionLaunchAndPlatform(t *testing.T) {
	write := launchConfigHash(projectRootsWrite)
	evidence := provider.NativeContextEvidenceSet{ProtectedPaths: []provider.ProtectedPathsEvidence{{
		ReadDenied: true, WriteDenied: true, HarnessVersion: "0.155.1", LaunchConfigHash: write, Platform: "darwin",
	}}}
	tests := []struct {
		name     string
		version  string
		hash     string
		platform string
		want     provider.ProtectedPathsState
	}{
		{"recorded version, workspace-write launch, darwin", "0.155.1", write, "darwin", provider.ProtectedPathsEnforced},
		{"other version", "0.155.2", write, "darwin", provider.ProtectedPathsUnknown},
		{"unknown version", "", write, "darwin", provider.ProtectedPathsUnknown},
		{"read-only launch", "0.155.1", launchConfigHash(projectRootsRead), "darwin", provider.ProtectedPathsUnknown},
		{"no launch", "0.155.1", "", "darwin", provider.ProtectedPathsUnknown},
		{"linux", "0.155.1", write, "linux", provider.ProtectedPathsUnknown},
		{"windows", "0.155.1", write, "windows", provider.ProtectedPathsUnknown},
		{"unknown platform", "0.155.1", write, "", provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := nativeContextReportFrom(evidence, tt.version, tt.hash, tt.platform)
			assertSourcesPossible(t, report)
			if report.ProtectedPaths != tt.want {
				t.Fatalf("protected paths = %s, want %s", report.ProtectedPaths, tt.want)
			}
		})
	}
}

func TestCurrentLaunchIsUnknownUntilReRecorded(t *testing.T) {
	report := nativeContextReport("0.155.1", launchConfigHash(projectRootsWrite), "darwin")
	if report.ProtectedPaths != provider.ProtectedPathsUnknown && len(rules.NativeContextEvidence().ProtectedPaths) == 0 {
		t.Fatalf("protected paths = %s without evidence", report.ProtectedPaths)
	}
	snapshot := codexCapabilities(provider.ModelSelection{}, "0.155.1", launchConfigHash(projectRootsWrite), time.Now())
	if snapshot.HarnessVersion != "0.155.1" {
		t.Fatalf("harness version = %q", snapshot.HarnessVersion)
	}
	if want := nativeContextReport("0.155.1", launchConfigHash(projectRootsWrite), runtime.GOOS); snapshot.NativeContext.ProtectedPaths != want.ProtectedPaths {
		t.Fatalf("snapshot protected paths = %s, want %s", snapshot.NativeContext.ProtectedPaths, want.ProtectedPaths)
	}
}

func TestHarnessVersionFromUserAgent(t *testing.T) {
	tests := map[string]string{
		"conduit/0.155.1 (Mac OS 27.0.1; arm64) unknown (conduit; codex-app-server/1)": "0.155.1",
		"conduit/0.156.0-alpha.1 (Linux; x86_64)":                                      "0.156.0-alpha.1",
		"codex_cli_rs/0.155.1 (Linux; x86_64)":                                         "",
		"xconduit/0.155.1 (Mac OS)":                                                    "",
		"Conduit/0.155.1 (Mac OS)":                                                     "",
		"conduit/x/0.155.1 (Mac OS)":                                                   "",
		"conduit/0.155.1":                                                              "0.155.1",
		"":                                                                             "",
		"conduit":                                                                      "",
		"conduit/ (Mac OS)":                                                            "",
		"conduit/latest (Mac OS)":                                                      "",
		"conduit/0.155.1;rm (Mac)":                                                     "",
		"(Mac OS) conduit/0.155.1":                                                     "",
	}
	for userAgent, want := range tests {
		if got := harnessVersionFromUserAgent(userAgent); got != want {
			t.Fatalf("harnessVersionFromUserAgent(%q) = %q, want %q", userAgent, got, want)
		}
	}
}

// OpenSession must evaluate the evidence with the version from initialize, the
// session's own launch hash and the host platform.
func TestOpenSessionProtectedPathsUseInitializeVersion(t *testing.T) {
	tests := []struct {
		name      string
		mode      SandboxMode
		userAgent string
		version   string
		access    projectRootAccess
	}{
		{"recorded version, workspace-write", SandboxWorkspaceWrite, "conduit/0.155.1 (Mac OS; arm64) unknown (conduit; codex-app-server/1)", "0.155.1", projectRootsWrite},
		{"other version", SandboxWorkspaceWrite, "conduit/0.155.2 (Mac OS; arm64)", "0.155.2", projectRootsWrite},
		{"recorded version, read-only", SandboxReadOnly, "conduit/0.155.1 (Mac OS; arm64)", "0.155.1", projectRootsRead},
		{"foreign product", SandboxWorkspaceWrite, "codex_cli_rs/0.155.1 (Mac OS; arm64)", "", projectRootsWrite},
		{"no userAgent", SandboxWorkspaceWrite, "", "", projectRootsWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := &scriptedLauncher{userAgent: tt.userAgent}
			adapter := New(Config{Launcher: launcher, Sandbox: tt.mode, CloseGrace: 20 * time.Millisecond})
			opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{
				Assignment: testAssignment(1), Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: true},
				Model: provider.ModelSelection{ModelID: "model"}, ResultContract: robustnessContract, PermissionMode: provider.PermissionDeny,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
			capabilities := opened.Session.Capabilities()
			if capabilities.HarnessVersion != tt.version {
				t.Fatalf("version = %q, want %q", capabilities.HarnessVersion, tt.version)
			}
			assertSourcesPossible(t, capabilities.NativeContext)
			want := nativeContextReport(tt.version, launchConfigHash(tt.access), runtime.GOOS).ProtectedPaths
			if got := capabilities.NativeContext.ProtectedPaths; got != want {
				t.Fatalf("protected paths = %s, want %s", got, want)
			}
		})
	}
}

func TestInspectProtectedPathsStayUnknown(t *testing.T) {
	descriptor, err := New(Config{Launcher: &scriptedLauncher{}}).Inspect(context.Background(), provider.InspectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	assertSourcesPossible(t, descriptor.Capabilities.NativeContext)
	if got := descriptor.Capabilities.NativeContext.ProtectedPaths; got != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", got)
	}
}

func openWithSandbox(t *testing.T, mode SandboxMode, writable bool) (*scriptedLauncher, provider.OpenSessionResult, error) {
	t.Helper()
	launcher := &scriptedLauncher{}
	adapter := New(Config{Launcher: launcher, Sandbox: mode, CloseGrace: 20 * time.Millisecond})
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{
		Assignment: testAssignment(1), Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: writable},
		Model: provider.ModelSelection{ModelID: "model"}, ResultContract: robustnessContract, PermissionMode: provider.PermissionDeny,
	})
	if err == nil {
		t.Cleanup(func() {
			_, _ = opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
		})
	}
	return launcher, opened, err
}

func TestOpenSessionLaunchesProfileAndOmitsThreadSandbox(t *testing.T) {
	tests := []struct {
		name     string
		mode     SandboxMode
		writable bool
		access   projectRootAccess
	}{
		{"default workspace-write", "", true, projectRootsWrite},
		{"read-only writable binding", SandboxReadOnly, true, projectRootsRead},
		{"read-only read-only binding", SandboxReadOnly, false, projectRootsRead},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher, opened, err := openWithSandbox(t, tt.mode, tt.writable)
			if err != nil {
				t.Fatal(err)
			}
			launcher.mu.Lock()
			args := launcher.spec.Args
			launcher.mu.Unlock()
			if !slices.Equal(args, launchArguments(tt.access)) {
				t.Fatalf("launch args = %q", args)
			}
			params, _ := launcher.request("thread/start")["params"].(map[string]any)
			if params == nil {
				t.Fatal("thread/start not sent")
			}
			if _, present := params["sandbox"]; present {
				t.Fatalf("thread/start sends sandbox, which overrides the profile: %#v", params)
			}
			if got := opened.Session.Capabilities().NativeContext.ProtectedPaths; got != provider.ProtectedPathsUnknown {
				t.Fatalf("protected paths = %s, want unknown without a harness version", got)
			}
		})
	}
}

func TestOpenSessionRefusesProfileWithoutLaunch(t *testing.T) {
	tests := []struct {
		name     string
		mode     SandboxMode
		writable bool
		code     provider.ErrorCode
	}{
		{"workspace-write with read-only binding", SandboxWorkspaceWrite, false, provider.ErrorProtocolViolation},
		{"unsupported mode", "danger-full-access", true, provider.ErrorCapabilityUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher, _, err := openWithSandbox(t, tt.mode, tt.writable)
			var providerErr *provider.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != tt.code || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
				t.Fatalf("error = %v", err)
			}
			launcher.mu.Lock()
			starts := launcher.starts
			launcher.mu.Unlock()
			if starts != 0 {
				t.Fatalf("refused profile launched %d processes", starts)
			}
		})
	}
}
