package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
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

// serveArguments start the session's own server on an ephemeral loopback
// port. The password is passed through the environment, never as an argument.
var serveArguments = []string{"serve", "--hostname", "127.0.0.1", "--port", "0"}

// Placeholders replace host-specific values in the hashed launch vector, so
// the launch hash depends on neither the user's home nor the configured
// executable path.
const (
	executablePlaceholder    = "<opencode>"
	conduitDirPlaceholder    = "<conduit-dir>"
	resolvedConduitDirHolder = "<conduit-dir-resolved>"
)

// sandboxHost supplies the host facts for the sandbox decision. Tests inject
// every field so they never depend on the host OS, PATH or home directory.
type sandboxHost struct {
	goos         string
	lookPath     func(string) (string, error)
	homeDir      func() (string, error)
	evalSymlinks func(string) (string, error)
	lstat        func(string) (fs.FileInfo, error)
}

func defaultSandboxHost() sandboxHost {
	return sandboxHost{goos: runtime.GOOS, lookPath: exec.LookPath, homeDir: os.UserHomeDir, evalSymlinks: filepath.EvalSymlinks, lstat: os.Lstat}
}

// sandboxFacts queries the host. The protected directory is derived like
// projectinfra derives Conduit's directory: user home + ".conduit". Seatbelt
// does not expand `~`, so the path is absolute. Seatbelt matches resolved
// paths, so when the home or ~/.conduit is a symlink the resolved path is a
// second fact. When ~/.conduit does not exist yet, the home is resolved
// instead, so a symlinked home is still covered; a dangling ~/.conduit
// symlink is unresolved.
func (h sandboxHost) sandboxFacts() rules.SandboxFacts {
	facts := rules.SandboxFacts{Darwin: h.goos == "darwin"}
	if !facts.Darwin {
		return facts
	}
	_, err := h.lookPath(sandboxExecPath)
	facts.SandboxExecFound = err == nil
	home, err := h.homeDir()
	facts.HomeResolved = err == nil && home != ""
	if !facts.HomeResolved {
		return facts
	}
	facts.ProtectedPath = filepath.Join(home, ".conduit")
	resolved, err := h.evalSymlinks(facts.ProtectedPath)
	if errors.Is(err, fs.ErrNotExist) {
		// A dangling symlink also resolves to "not exist". Only a path that
		// is itself absent (Lstat) falls back to resolving the home; an
		// existing entry that cannot be resolved stays unresolved, so it is
		// never launched as the recorded, symlink-free profile.
		if _, lstatErr := h.lstat(facts.ProtectedPath); !errors.Is(lstatErr, fs.ErrNotExist) {
			facts.SymlinkResolutionFailed = true
			return facts
		}
		var resolvedHome string
		resolvedHome, err = h.evalSymlinks(home)
		resolved = filepath.Join(resolvedHome, ".conduit")
	}
	switch {
	case err != nil:
		facts.SymlinkResolutionFailed = true
	case resolved != facts.ProtectedPath:
		facts.ResolvedProtectedPath = resolved
	}
	return facts
}

// protectedPaths lists the denied paths: the literal one, then the resolved
// one when it differs.
func protectedPaths(facts rules.SandboxFacts) []string {
	paths := []string{facts.ProtectedPath}
	if facts.ResolvedProtectedPath != "" {
		paths = append(paths, facts.ResolvedProtectedPath)
	}
	return paths
}

// seatbeltProfile allows everything except reading or writing each protected
// directory and everything below it, one denial per path. Every path must be
// admissible under rules.DecideSandbox: it is embedded verbatim in a string
// literal.
func seatbeltProfile(paths ...string) string {
	profile := "(version 1)\n(allow default)\n"
	for _, path := range paths {
		profile += "(deny file-read* file-write* (subpath \"" + path + "\"))\n"
	}
	return profile
}

// launchVector is the executable and argument vector of the server launch.
// Without denied paths it runs the server directly; with them, under
// sandbox-exec with an inline profile, so no temporary profile file exists.
func launchVector(executable string, denied []string) (string, []string) {
	if len(denied) == 0 {
		return executable, append([]string(nil), serveArguments...)
	}
	return sandboxExecPath, append([]string{"-p", seatbeltProfile(denied...), executable}, serveArguments...)
}

// serverLaunch is the planned server launch with what is reported about it.
type serverLaunch struct {
	spec    processruntime.Spec
	sandbox rules.SandboxDecision
	// configHash identifies the launch configuration evidence is bound to.
	configHash string
}

// planServerLaunch decides the sandbox from the host facts and builds the
// server spec. The password is appended to the inherited environment, so it
// wins over an inherited OPENCODE_SERVER_PASSWORD (the last duplicate wins).
func planServerLaunch(executable, dir, password string, facts rules.SandboxFacts) serverLaunch {
	decision := rules.DecideSandbox(facts)
	var denied, placeholders []string
	if decision.Applied() {
		denied = protectedPaths(facts)
		placeholders = []string{conduitDirPlaceholder}
		if len(denied) > 1 {
			placeholders = append(placeholders, resolvedConduitDirHolder)
		}
	}
	path, args := launchVector(executable, denied)
	return serverLaunch{
		spec:       processruntime.Spec{Path: path, Args: args, Dir: dir, Env: []string{serverPasswordEnv + "=" + password}},
		sandbox:    decision,
		configHash: launchConfigHash(placeholders),
	}
}

// launchConfigHash hashes the whole launch vector, including the
// sandbox-exec wrapper and its profile, with the executable and the denied
// paths replaced by placeholders. It excludes the working directory and the
// environment (the per-session password).
func launchConfigHash(deniedPlaceholders []string) string {
	path, args := launchVector(executablePlaceholder, deniedPlaceholders)
	return hashLaunchArguments(append([]string{path}, args...))
}

// sandboxedLaunchConfigHash is the hash of the sandboxed launch without a
// symlinked protected path; the recorded literal is compared with it.
func sandboxedLaunchConfigHash() string {
	return launchConfigHash([]string{conduitDirPlaceholder})
}

// hashLaunchArguments length-prefixes each argument, so moving text between
// adjacent arguments changes the hash. It matches the Codex adapter's scheme.
func hashLaunchArguments(args []string) string {
	digest := sha256.New()
	for _, arg := range args {
		fmt.Fprintf(digest, "%d:%s", len(arg), arg)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
