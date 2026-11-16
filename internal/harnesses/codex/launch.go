package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/harnesses/codex/rules"
)

// Launch policy: `codex app-server` selects the Conduit permission profile
// through -c overrides, and thread/start no longer sends `sandbox`, because
// that parameter overrides any profile (specs/012-project-memory/evidence.md,
// 2026-11-09). The profile reads the whole filesystem, writes the project roots
// (or only reads them in read-only mode), writes /tmp and $TMPDIR, and denies
// ~/.conduit entirely. Without the two tmp entries /tmp writes are denied
// (`codex sandbox` probe, codex-cli 0.155.1, 2026-11-16).
//
// Network is disabled explicitly with network.enabled=false. The thread/start
// sandbox "workspace-write" it replaces defaulted to network_access=false. A
// profile without a network table also blocked network in a `codex sandbox`
// probe (codex-cli 0.155.1, 2026-11-16: DNS failed without the key and with
// enabled=false, HTTPS succeeded with enabled=true), but the explicit key keeps
// the policy independent of that default.
//
// ~/.conduit is a literal: Codex expands `~` in permission profile paths (the
// same probe denied reads and writes there), so the launch hash does not depend
// on the user's home directory. The values are literals so the launch hash is
// deterministic.
const (
	permissionProfileName       = "conduit"
	defaultPermissionsArgument  = `default_permissions="` + permissionProfileName + `"`
	profileFilesystemKey        = "permissions." + permissionProfileName + ".filesystem"
	profileFilesystemBeforeRoot = `{":root"="read",":project_roots"={"."="`
	profileFilesystemAfterRoot  = `"},":slash_tmp"="write",":tmpdir"="write","~/.conduit"="none"}`
	profileNetworkArgument      = "permissions." + permissionProfileName + ".network.enabled=false"
)

// projectRootAccess is the profile's access value for the project roots.
type projectRootAccess string

const (
	projectRootsWrite projectRootAccess = "write"
	projectRootsRead  projectRootAccess = "read"
)

// permissionProfileFor maps the configured sandbox mode and the workspace
// binding onto the profile's project-root access. ok is false when the
// decision refuses the launch; the caller maps the decision to its error.
func permissionProfileFor(mode SandboxMode, workspaceWritable bool) (projectRootAccess, rules.PermissionProfileDecision, bool) {
	decision := rules.DecidePermissionProfile(rules.PermissionProfileFacts{
		ModeReadOnly:       mode == SandboxReadOnly,
		ModeWorkspaceWrite: mode == SandboxWorkspaceWrite,
		WorkspaceWritable:  workspaceWritable,
	})
	switch decision.Kind {
	case rules.PermissionProfileWorkspaceWrite:
		return projectRootsWrite, decision, true
	case rules.PermissionProfileWorkspaceRead:
		return projectRootsRead, decision, true
	default:
		return "", decision, false
	}
}

// launchArguments returns the complete argument vector after the executable.
// Every argument is fixed for a given project-root access; the workspace is
// the process directory, not an argument.
func launchArguments(access projectRootAccess) []string {
	return []string{
		"app-server",
		"-c", defaultPermissionsArgument,
		"-c", profileFilesystemKey + "=" + profileFilesystemBeforeRoot + string(access) + profileFilesystemAfterRoot,
		"-c", profileNetworkArgument,
	}
}

// launchConfigHash identifies the launch configuration evidence is bound to:
// the whole argument vector for the given project-root access. It excludes the
// workspace directory, the thread/start parameters and the inherited
// environment.
func launchConfigHash(access projectRootAccess) string {
	return hashLaunchArguments(launchArguments(access))
}

// configuredLaunchConfigHash is the launch hash for the configured mode with a
// writable workspace binding, as reported before a session exists. An
// unsupported mode has no launch and returns the empty hash, which matches no
// evidence.
func configuredLaunchConfigHash(mode SandboxMode) string {
	access, _, ok := permissionProfileFor(mode, true)
	if !ok {
		return ""
	}
	return launchConfigHash(access)
}

// hashLaunchArguments length-prefixes each argument, so moving text between
// adjacent arguments changes the hash. It matches the Claude adapter's scheme.
func hashLaunchArguments(args []string) string {
	digest := sha256.New()
	for _, arg := range args {
		fmt.Fprintf(digest, "%d:%s", len(arg), arg)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
