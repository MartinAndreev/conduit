package opencode

import (
	"errors"
	"slices"
	"testing"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

func fakeSandboxHost(goos string, sandboxExec bool, home string, homeErr error) sandboxHost {
	return sandboxHost{
		goos: goos,
		lookPath: func(name string) (string, error) {
			if sandboxExec && name == sandboxExecPath {
				return name, nil
			}
			return "", errors.New("not found")
		},
		homeDir: func() (string, error) { return home, homeErr },
	}
}

func TestSeatbeltProfileText(t *testing.T) {
	want := "(version 1)\n(allow default)\n(deny file-read* file-write* (subpath \"/Users/u/.conduit\"))\n"
	if got := seatbeltProfile("/Users/u/.conduit"); got != want {
		t.Fatalf("profile = %q, want %q", got, want)
	}
}

func TestSandboxedSpecOnDarwin(t *testing.T) {
	spec := processruntime.Spec{Path: "opencode", Args: []string{"serve", "--port", "0"}, Dir: "/work"}
	facts := fakeSandboxHost("darwin", true, "/Users/u", nil).sandboxFacts()
	got, decision := sandboxedSpec(spec, facts)
	if decision.Kind != rules.SandboxApply {
		t.Fatalf("decision = %#v", decision)
	}
	wantArgs := []string{"-p", seatbeltProfile("/Users/u/.conduit"), "opencode", "serve", "--port", "0"}
	if got.Path != sandboxExecPath || !slices.Equal(got.Args, wantArgs) || got.Dir != "/work" {
		t.Fatalf("spec = %#v", got)
	}
	if !slices.Equal(spec.Args, []string{"serve", "--port", "0"}) {
		t.Fatalf("original spec was modified: %#v", spec)
	}
}

func TestSandboxedSpecFallsBackUnsandboxed(t *testing.T) {
	tests := []struct {
		name string
		host sandboxHost
		want rules.SandboxKind
	}{
		{"linux", fakeSandboxHost("linux", true, "/home/u", nil), rules.SandboxUnsupportedPlatform},
		{"windows", fakeSandboxHost("windows", true, `C:\Users\u`, nil), rules.SandboxUnsupportedPlatform},
		{"sandbox-exec missing", fakeSandboxHost("darwin", false, "/Users/u", nil), rules.SandboxExecMissing},
		{"home error", fakeSandboxHost("darwin", true, "", errors.New("no home")), rules.SandboxHomeUnresolved},
		{"empty home", fakeSandboxHost("darwin", true, "", nil), rules.SandboxHomeUnresolved},
		{"quote in home", fakeSandboxHost("darwin", true, `/Users/a"b`, nil), rules.SandboxPathInadmissible},
		{"backslash in home", fakeSandboxHost("darwin", true, `/Users/a\b`, nil), rules.SandboxPathInadmissible},
		{"control in home", fakeSandboxHost("darwin", true, "/Users/a\tb", nil), rules.SandboxPathInadmissible},
		{"relative home", fakeSandboxHost("darwin", true, "Users/u", nil), rules.SandboxPathInadmissible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := processruntime.Spec{Path: "opencode", Args: []string{"serve"}, Dir: "/work"}
			got, decision := sandboxedSpec(spec, tt.host.sandboxFacts())
			if decision.Kind != tt.want || decision.Reason == "" {
				t.Fatalf("decision = %#v, want %s", decision, tt.want)
			}
			if got.Path != "opencode" || !slices.Equal(got.Args, []string{"serve"}) || got.Dir != "/work" {
				t.Fatalf("fallback changed the spec: %#v", got)
			}
		})
	}
}

func TestNonDarwinHostIsNotQueried(t *testing.T) {
	host := sandboxHost{
		goos:     "linux",
		lookPath: func(string) (string, error) { t.Fatal("lookPath called on linux"); return "", nil },
		homeDir:  func() (string, error) { t.Fatal("homeDir called on linux"); return "", nil },
	}
	if facts := host.sandboxFacts(); facts != (rules.SandboxFacts{}) {
		t.Fatalf("facts = %#v", facts)
	}
}
