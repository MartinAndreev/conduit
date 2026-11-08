package claude

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
)

// Launch policy for the only accepted session mode: a writable workspace with
// provider.PermissionDeny. This is the spec 007 proposal (acceptEdits, sandboxed
// Bash, no web tools); its equivalence to Codex workspace-write/never is not
// yet verified live, so capabilities report permission behaviour as unknown.
// Without --permission-prompt-tool, `claude -p` denies every other request.
//
// The settings add the isolation recorded as configuration I in
// specs/012-project-memory/evidence.md. The deny rules are not redundant: with
// the sandbox alone the agent can still read ~/.conduit. The value is a literal
// so its key order, and therefore the launch hash, is deterministic.
const (
	permissionModeArgument  = "acceptEdits"
	disallowedToolsArgument = "WebFetch,WebSearch"
	settingSourcesArgument  = "project,local"
	launchSettingsArgument  = `{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true,"filesystem":{"denyRead":["~/.conduit"]}},"permissions":{"deny":["Read(~/.conduit/**)"]},"autoMemoryEnabled":false}`
)

// launchProtocolArguments are the fixed arguments selecting the stream protocol.
func launchProtocolArguments() []string {
	return []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--replay-user-messages",
	}
}

// launchPolicyArguments are the fixed arguments that decide what claude may
// load and reach. --mcp-config is deliberately absent because Conduit passes no
// MCP servers, and --bare is never used because it breaks subscription login.
func launchPolicyArguments() []string {
	return []string{
		"--setting-sources", settingSourcesArgument,
		"--strict-mcp-config",
		"--permission-mode", permissionModeArgument,
		"--settings", launchSettingsArgument,
		"--disallowedTools", disallowedToolsArgument,
	}
}

// launchArguments returns the argument vector after the executable. schema is
// passed only when native result enforcement was negotiated.
func launchArguments(nativeSessionID, model string, schema json.RawMessage) []string {
	args := launchProtocolArguments()
	args = append(args, "--session-id", nativeSessionID, "--model", model)
	if len(schema) != 0 {
		args = append(args, "--json-schema", string(schema))
	}
	return append(args, launchPolicyArguments()...)
}

// launchConfigHash identifies the launch configuration evidence is bound to.
// It covers every fixed argument: the protocol flags, setting sources, strict
// MCP mode, permission mode, settings JSON and disallowed tools. It excludes
// per-session values (session id, model, JSON schema, workspace directory) and
// the child environment, whose sanitisation is separate from this policy.
func launchConfigHash() string {
	return hashLaunchArguments(append(launchProtocolArguments(), launchPolicyArguments()...))
}

// hashLaunchArguments length-prefixes each argument, so moving text between
// adjacent arguments changes the hash.
func hashLaunchArguments(args []string) string {
	digest := sha256.New()
	for _, arg := range args {
		fmt.Fprintf(digest, "%d:%s", len(arg), arg)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// strippedEnvironmentNames are removed from the child environment exactly;
// every name with a stripped prefix is removed as well. An API key or token
// would take precedence over the user's subscription login in `-p` mode, and
// Claude Code session variables would make the child join a foreign session.
var (
	strippedEnvironmentNames = []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDECODE",
		"CLAUDE_EFFORT", "CLAUDE_PID", "CLAUDE_AGENT_SDK_VERSION",
	}
	strippedEnvironmentPrefixes = []string{"CLAUDE_CODE_"}
)

// strippedEnvironmentName reports whether name is removed. Windows treats
// environment names case-insensitively, so foldCase compares without case
// there; elsewhere names are case-sensitive and compared exactly.
func strippedEnvironmentName(name string, foldCase bool) bool {
	if foldCase {
		name = strings.ToUpper(name)
	}
	if slices.Contains(strippedEnvironmentNames, name) {
		return true
	}
	for _, prefix := range strippedEnvironmentPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// launchEnvironment returns the complete child environment: parent in order
// without the stripped variables. It is launched with Spec.ReplaceEnv, so a
// stripped variable is absent in the child rather than set to an empty value.
func launchEnvironment(parent []string) []string {
	return filterEnvironment(parent, runtime.GOOS == "windows")
}

// filterEnvironment removes stripped names and keeps every retained entry
// unchanged and in order. foldCase selects case-insensitive name matching.
func filterEnvironment(parent []string, foldCase bool) []string {
	result := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if !strippedEnvironmentName(name, foldCase) {
			result = append(result, entry)
		}
	}
	return result
}

// newUUID returns a random RFC 4122 version 4 UUID. It never substitutes a
// predictable fallback when the random source fails.
func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate native UUID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
