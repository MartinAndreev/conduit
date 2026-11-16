package opencode

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// sandboxExecPath is the macOS Seatbelt launcher. It is an absolute path so a
// PATH entry cannot substitute it.
const sandboxExecPath = "/usr/bin/sandbox-exec"

// NOT WIRED YET. The decision of 2026-11-16 runs "the OpenCode server" under
// sandbox-exec on macOS, but this adapter launches no server: `opencode api`
// and `opencode run` connect to the user's shared background service, where
// tools execute. Which process gets wrapped (a Conduit-owned `opencode serve`,
// `--standalone` clients, or something else) is an open decision for Martin;
// until it is made, protected paths stay unknown and nothing calls this.

// sandboxHost supplies the host facts for the sandbox decision. Tests inject
// every field so they never depend on the host OS, PATH or home directory.
type sandboxHost struct {
	goos     string
	lookPath func(string) (string, error)
	homeDir  func() (string, error)
}

func defaultSandboxHost() sandboxHost {
	return sandboxHost{goos: runtime.GOOS, lookPath: exec.LookPath, homeDir: os.UserHomeDir}
}

// sandboxFacts queries the host. The protected directory is derived like
// projectinfra derives Conduit's directory: user home + ".conduit". Seatbelt
// does not expand `~`, so the path is absolute.
func (h sandboxHost) sandboxFacts() rules.SandboxFacts {
	facts := rules.SandboxFacts{Darwin: h.goos == "darwin"}
	if !facts.Darwin {
		return facts
	}
	_, err := h.lookPath(sandboxExecPath)
	facts.SandboxExecFound = err == nil
	home, err := h.homeDir()
	facts.HomeResolved = err == nil && home != ""
	if facts.HomeResolved {
		facts.ProtectedPath = filepath.Join(home, ".conduit")
	}
	return facts
}

// seatbeltProfile allows everything except reading or writing the protected
// directory and everything below it. protectedPath must be admissible under
// rules.DecideSandbox: it is embedded verbatim in the string literal.
func seatbeltProfile(protectedPath string) string {
	return "(version 1)\n(allow default)\n(deny file-read* file-write* (subpath \"" + protectedPath + "\"))\n"
}

// sandboxedSpec returns spec unchanged unless the decision applies the
// sandbox; then it runs the same command under sandbox-exec with an inline
// profile, so no temporary profile file exists.
func sandboxedSpec(spec processruntime.Spec, facts rules.SandboxFacts) (processruntime.Spec, rules.SandboxDecision) {
	decision := rules.DecideSandbox(facts)
	if !decision.Applied() {
		return spec, decision
	}
	wrapped := spec
	wrapped.Path = sandboxExecPath
	wrapped.Args = append([]string{"-p", seatbeltProfile(facts.ProtectedPath), spec.Path}, spec.Args...)
	return wrapped, decision
}
