package opencode

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// fakeSandboxHost resolves no symlinks: every path resolves to itself.
func fakeSandboxHost(goos string, sandboxExec bool, home string, homeErr error) sandboxHost {
	return sandboxHost{
		goos: goos,
		lookPath: func(name string) (string, error) {
			if sandboxExec && name == sandboxExecPath {
				return name, nil
			}
			return "", errors.New("not found")
		},
		homeDir:      func() (string, error) { return home, homeErr },
		evalSymlinks: func(path string) (string, error) { return path, nil },
		lstat:        func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist },
	}
}

// withSymlinks resolves the given paths; any other path resolves to itself,
// and a path mapped to an error fails with it.
func withSymlinks(host sandboxHost, links map[string]string, failures map[string]error) sandboxHost {
	host.evalSymlinks = func(path string) (string, error) {
		if err, ok := failures[path]; ok {
			return "", err
		}
		if target, ok := links[path]; ok {
			return target, nil
		}
		return path, nil
	}
	return host
}

func TestSeatbeltProfileText(t *testing.T) {
	want := "(version 1)\n(allow default)\n(deny file-read* file-write* (subpath \"/Users/u/.conduit\"))\n"
	if got := seatbeltProfile("/Users/u/.conduit"); got != want {
		t.Fatalf("profile = %q, want %q", got, want)
	}
	two := "(version 1)\n(allow default)\n" +
		"(deny file-read* file-write* (subpath \"/Users/u/.conduit\"))\n" +
		"(deny file-read* file-write* (subpath \"/Volumes/home/u/.conduit\"))\n"
	if got := seatbeltProfile("/Users/u/.conduit", "/Volumes/home/u/.conduit"); got != two {
		t.Fatalf("profile = %q, want %q", got, two)
	}
}

func TestServerLaunchOnDarwinRunsUnderSandbox(t *testing.T) {
	facts := fakeSandboxHost("darwin", true, "/Users/u", nil).sandboxFacts()
	launch := planServerLaunch("opencode", "/work", "pw", facts)
	if launch.sandbox.Kind != rules.SandboxApply {
		t.Fatalf("decision = %#v", launch.sandbox)
	}
	wantArgs := []string{"-p", seatbeltProfile("/Users/u/.conduit"), "opencode", "serve", "--hostname", "127.0.0.1", "--port", "0"}
	if launch.spec.Path != sandboxExecPath || !slices.Equal(launch.spec.Args, wantArgs) || launch.spec.Dir != "/work" || launch.spec.ReplaceEnv {
		t.Fatalf("spec = %#v", launch.spec)
	}
	if !slices.Equal(launch.spec.Env, []string{"OPENCODE_SERVER_PASSWORD=pw"}) {
		t.Fatal("server env does not carry exactly the server password")
	}
	for _, arg := range launch.spec.Args {
		if strings.Contains(arg, "pw") {
			t.Fatal("password appears in the arguments")
		}
	}
	if launch.configHash != rules.RecordedLaunchConfigHash {
		t.Fatalf("hash = %s, want the recorded sandboxed launch hash", launch.configHash)
	}
}

func TestServerLaunchProfileDeniesSymlinkResolvedPath(t *testing.T) {
	tests := []struct {
		name     string
		links    map[string]string
		failures map[string]error
		want     []string
	}{
		{"symlinked home", map[string]string{"/Users/u/.conduit": "/Volumes/home/u/.conduit"}, nil, []string{"/Users/u/.conduit", "/Volumes/home/u/.conduit"}},
		{"symlinked conduit dir", map[string]string{"/Users/u/.conduit": "/data/conduit"}, nil, []string{"/Users/u/.conduit", "/data/conduit"}},
		{"conduit dir absent, home symlinked", map[string]string{"/Users/u": "/Volumes/home/u"}, map[string]error{"/Users/u/.conduit": fs.ErrNotExist}, []string{"/Users/u/.conduit", "/Volumes/home/u/.conduit"}},
		{"conduit dir absent, home resolved", nil, map[string]error{"/Users/u/.conduit": fs.ErrNotExist}, []string{"/Users/u/.conduit"}},
		{"no symlink", nil, nil, []string{"/Users/u/.conduit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := withSymlinks(fakeSandboxHost("darwin", true, "/Users/u", nil), tt.links, tt.failures)
			launch := planServerLaunch("opencode", "/work", "pw", host.sandboxFacts())
			if launch.sandbox.Kind != rules.SandboxApply || launch.spec.Args[1] != seatbeltProfile(tt.want...) {
				t.Fatalf("decision = %#v profile = %q", launch.sandbox, launch.spec.Args[1])
			}
			resolved := len(tt.want) > 1
			if (launch.configHash == rules.RecordedLaunchConfigHash) == resolved {
				t.Fatalf("hash = %s; a second denial must change it, none must keep the recorded one", launch.configHash)
			}
			if resolved && launch.configHash != launchConfigHash([]string{conduitDirPlaceholder, resolvedConduitDirHolder}) {
				t.Fatal("resolved denial is not hashed as its placeholder")
			}
		})
	}
}

func TestServerLaunchSymlinkFactsAreAdmitted(t *testing.T) {
	tests := []struct {
		name     string
		links    map[string]string
		failures map[string]error
		want     rules.SandboxKind
	}{
		{"resolved path inadmissible", map[string]string{"/Users/u/.conduit": `/Volumes/a"b/.conduit`}, nil, rules.SandboxPathInadmissible},
		{"resolution refused", nil, map[string]error{"/Users/u/.conduit": fs.ErrPermission}, rules.SandboxPathUnresolved},
		{"conduit absent and home unresolvable", nil, map[string]error{"/Users/u/.conduit": fs.ErrNotExist, "/Users/u": fs.ErrPermission}, rules.SandboxPathUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := withSymlinks(fakeSandboxHost("darwin", true, "/Users/u", nil), tt.links, tt.failures)
			launch := planServerLaunch("opencode", "/work", "pw", host.sandboxFacts())
			if launch.sandbox.Kind != tt.want || launch.spec.Path != "opencode" {
				t.Fatalf("decision = %#v spec = %#v", launch.sandbox, launch.spec.Path)
			}
		})
	}
}

func TestServerLaunchFallsBackUnsandboxed(t *testing.T) {
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
	unsandboxed := launchConfigHash(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launch := planServerLaunch("opencode", "/work", "pw", tt.host.sandboxFacts())
			if launch.sandbox.Kind != tt.want || launch.sandbox.Reason == "" {
				t.Fatalf("decision = %#v, want %s", launch.sandbox, tt.want)
			}
			if launch.spec.Path != "opencode" || !slices.Equal(launch.spec.Args, serveArguments) || launch.spec.Dir != "/work" {
				t.Fatalf("fallback spec = %#v", launch.spec)
			}
			if !slices.Equal(launch.spec.Env, []string{"OPENCODE_SERVER_PASSWORD=pw"}) {
				t.Fatal("unsandboxed server env does not carry exactly the server password")
			}
			if launch.configHash != unsandboxed || unsandboxed == rules.RecordedLaunchConfigHash {
				t.Fatalf("hash = %s", launch.configHash)
			}
		})
	}
}

func TestNonDarwinHostIsNotQueried(t *testing.T) {
	host := sandboxHost{
		goos:         "linux",
		lookPath:     func(string) (string, error) { t.Fatal("lookPath called on linux"); return "", nil },
		homeDir:      func() (string, error) { t.Fatal("homeDir called on linux"); return "", nil },
		evalSymlinks: func(string) (string, error) { t.Fatal("evalSymlinks called on linux"); return "", nil },
		lstat:        func(string) (fs.FileInfo, error) { t.Fatal("lstat called on linux"); return nil, nil },
	}
	if facts := host.sandboxFacts(); facts != (rules.SandboxFacts{}) {
		t.Fatalf("facts = %#v", facts)
	}
}

// The recorded literal must equal the current sandboxed launch hash. A
// failure means the launch changed: record new live evidence, then update the
// literal in rules. Never update the literal alone.
func TestRecordedHashIsCurrentSandboxedLaunch(t *testing.T) {
	if got := sandboxedLaunchConfigHash(); got != rules.RecordedLaunchConfigHash {
		t.Fatalf("current sandboxed launch hash = %s, recorded = %s", got, rules.RecordedLaunchConfigHash)
	}
	evidence := rules.NativeContextEvidence()
	if len(evidence.Sources) != 0 || len(evidence.ProtectedPaths) != 1 {
		t.Fatalf("evidence = %+v, want exactly the one live recording", evidence)
	}
	for _, entry := range evidence.ProtectedPaths {
		if entry.LaunchConfigHash != rules.RecordedLaunchConfigHash {
			t.Fatalf("entry bound to %s", entry.LaunchConfigHash)
		}
	}
}

// The hash depends on neither the user's home nor the executable path.
func TestLaunchHashIsHostIndependent(t *testing.T) {
	a := planServerLaunch("opencode", "/w1", "a", fakeSandboxHost("darwin", true, "/Users/a", nil).sandboxFacts())
	b := planServerLaunch("/opt/bin/opencode", "/w2", "b", fakeSandboxHost("darwin", true, "/Users/b", nil).sandboxFacts())
	if a.configHash != b.configHash {
		t.Fatal("hash depends on the home, executable, directory or password")
	}
}

func TestLaunchConfigHashCoversEveryArgument(t *testing.T) {
	path, args := launchVector(executablePlaceholder, []string{conduitDirPlaceholder})
	vector := append([]string{path}, args...)
	base := hashLaunchArguments(vector)
	if base != sandboxedLaunchConfigHash() {
		t.Fatal("hash is not over the full launch vector")
	}
	for index := range vector {
		changed := slices.Clone(vector)
		changed[index] += "x"
		if hashLaunchArguments(changed) == base {
			t.Fatalf("changing %q did not change the hash", vector[index])
		}
		if hashLaunchArguments(slices.Delete(slices.Clone(vector), index, index+1)) == base {
			t.Fatalf("removing %q did not change the hash", vector[index])
		}
	}
}

// A ~/.conduit symlink whose target is missing resolves to "not exist" like an
// absent directory; it must stay unresolved and never get the recorded hash.
func TestDanglingConduitSymlinkIsUnresolved(t *testing.T) {
	for name, lstatErr := range map[string]error{"dangling symlink": nil, "lstat refused": fs.ErrPermission} {
		t.Run(name, func(t *testing.T) {
			host := withSymlinks(fakeSandboxHost("darwin", true, "/Users/u", nil), nil, map[string]error{"/Users/u/.conduit": fs.ErrNotExist})
			host.lstat = func(path string) (fs.FileInfo, error) {
				if path != "/Users/u/.conduit" {
					t.Fatalf("lstat(%q)", path)
				}
				return nil, lstatErr
			}
			facts := host.sandboxFacts()
			launch := planServerLaunch("opencode", "/work", "pw", facts)
			if !facts.SymlinkResolutionFailed || launch.sandbox.Kind != rules.SandboxPathUnresolved {
				t.Fatalf("facts = %#v decision = %#v", facts, launch.sandbox)
			}
			if launch.configHash == rules.RecordedLaunchConfigHash {
				t.Fatal("dangling symlink launch carries the recorded hash")
			}
			report := nativeContextReport(rules.NativeContextEvidence(), launchFacts{launched: true, sandbox: launch.sandbox, info: rules.ServerInfoDecision{Kind: rules.ServerInfoAccepted, Version: "2.0.22"}, harnessVersion: "2.0.22", configHash: launch.configHash, platform: "darwin"})
			if report.ProtectedPaths != provider.ProtectedPathsUnknown || report.ProtectedPathsReason != launch.sandbox.Reason {
				t.Fatalf("protected paths = %s (%q)", report.ProtectedPaths, report.ProtectedPathsReason)
			}
		})
	}
}
