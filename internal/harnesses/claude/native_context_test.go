package claude

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/claude/rules"
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

func TestSnapshotNativeContextByVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    provider.ProtectedPathsState
	}{
		{"recorded version", "2.1.316", enforcedOnDarwin()},
		{"other version", "2.1.317", provider.ProtectedPathsUnknown},
		{"no version", "", provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := claudeCapabilities(provider.ModelSelection{}, tt.version, time.Now()).NativeContext
			assertSourcesPossible(t, report)
			if report.ProtectedPaths != tt.want {
				t.Fatalf("protected paths = %s, want %s", report.ProtectedPaths, tt.want)
			}
		})
	}
}

func TestOpenSessionNativeContextUsesInitializeVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    provider.ProtectedPathsState
	}{
		{"recorded version", "2.1.316", enforcedOnDarwin()},
		{"other version", "2.1.317", provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeClaude(t)
			fake.initialize = strings.ReplaceAll(fake.initialize, `"claude_code_version":"2.1.316"`, `"claude_code_version":"`+tt.version+`"`)
			opened, err := New(testConfig(fake)).OpenSession(context.Background(), openRequest(t, noneContract))
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
			capabilities := opened.Session.Capabilities()
			if capabilities.HarnessVersion != tt.version {
				t.Fatalf("version = %q, want %q", capabilities.HarnessVersion, tt.version)
			}
			assertSourcesPossible(t, capabilities.NativeContext)
			if got := capabilities.NativeContext.ProtectedPaths; got != tt.want {
				t.Fatalf("protected paths = %s, want %s", got, tt.want)
			}
		})
	}
}

// enforcedOnDarwin is the expected state for recorded evidence on this host:
// the recording ran on macOS, so another host stays unknown.
func enforcedOnDarwin() provider.ProtectedPathsState {
	if runtime.GOOS == "darwin" {
		return provider.ProtectedPathsEnforced
	}
	return provider.ProtectedPathsUnknown
}

func TestProtectedPathsBindToRecordedPlatform(t *testing.T) {
	tests := []struct {
		platform string
		want     provider.ProtectedPathsState
	}{
		{"darwin", provider.ProtectedPathsEnforced},
		{"linux", provider.ProtectedPathsUnknown},
		{"windows", provider.ProtectedPathsUnknown},
		{"", provider.ProtectedPathsUnknown},
	}
	for _, tt := range tests {
		if got := nativeContextReport("2.1.316", tt.platform).ProtectedPaths; got != tt.want {
			t.Fatalf("platform %q: protected paths = %s, want %s", tt.platform, got, tt.want)
		}
	}
}

func TestInspectNativeContextStaysUnknown(t *testing.T) {
	descriptor, err := New(testConfig(newFakeClaude(t))).Inspect(context.Background(), provider.InspectRequest{Model: testModel})
	if err != nil {
		t.Fatal(err)
	}
	assertSourcesPossible(t, descriptor.Capabilities.NativeContext)
	if got := descriptor.Capabilities.NativeContext.ProtectedPaths; got != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", got)
	}
}

// A failure here means the launch changed: record new live evidence, then
// update the literal in rules. Never update the literal alone.
func TestEvidenceIsBoundToCurrentLaunch(t *testing.T) {
	evidence := rules.NativeContextEvidence()
	if len(evidence.Sources) != 0 || len(evidence.ProtectedPaths) != 1 {
		t.Fatalf("evidence = %+v, want exactly one protected paths entry", evidence)
	}
	entry := evidence.ProtectedPaths[0]
	if !entry.ReadDenied || !entry.WriteDenied || entry.HarnessVersion != "2.1.316" || entry.Platform != "darwin" {
		t.Fatalf("entry = %+v", entry)
	}
	if got := launchConfigHash(); entry.LaunchConfigHash != got {
		t.Fatalf("recorded hash = %s, current launch hash = %s", entry.LaunchConfigHash, got)
	}
}

func TestLaunchConfigHashIsStableAndHexSHA256(t *testing.T) {
	first := launchConfigHash()
	if first != launchConfigHash() || len(first) != 64 {
		t.Fatalf("hash = %q", first)
	}
}

func TestLaunchConfigHashCoversEveryFixedArgument(t *testing.T) {
	fixed := append(launchProtocolArguments(), launchPolicyArguments()...)
	base := hashLaunchArguments(fixed)
	for index := range fixed {
		changed := slices.Clone(fixed)
		changed[index] += "x"
		if hashLaunchArguments(changed) == base {
			t.Fatalf("changing %q did not change the hash", fixed[index])
		}
		if hashLaunchArguments(slices.Delete(slices.Clone(fixed), index, index+1)) == base {
			t.Fatalf("removing %q did not change the hash", fixed[index])
		}
	}
	if hashLaunchArguments([]string{"ab", "c"}) == hashLaunchArguments([]string{"a", "bc"}) {
		t.Fatal("argument boundaries are not hashed")
	}
}

// The hash excludes per-session values because it is built only from the fixed
// arguments, and those must be exactly what a launch uses around them.
func TestLaunchHashInputsMatchLaunchWithoutSessionValues(t *testing.T) {
	want := append(launchProtocolArguments(), launchPolicyArguments()...)
	for _, args := range [][]string{
		launchArguments("sid-1", "model-a", nil),
		launchArguments("sid-2", "model-b", []byte(`{"type":"object"}`)),
	} {
		remaining := slices.Clone(args)
		for _, flag := range []string{"--session-id", "--model", "--json-schema"} {
			if index := slices.Index(remaining, flag); index >= 0 {
				remaining = slices.Delete(remaining, index, index+2)
			}
		}
		if !slices.Equal(remaining, want) {
			t.Fatalf("fixed arguments = %q, want %q", remaining, want)
		}
	}
}
