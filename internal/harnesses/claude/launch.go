package claude

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Launch policy for the only accepted session mode: a writable workspace with
// provider.PermissionDeny. This is the spec 007 proposal (acceptEdits, sandboxed
// Bash, no web tools); its equivalence to Codex workspace-write/never is not
// yet verified live, so capabilities report permission behaviour as unknown.
// Without --permission-prompt-tool, `claude -p` denies every other request.
const (
	permissionModeArgument  = "acceptEdits"
	disallowedToolsArgument = "WebFetch,WebSearch"
	sandboxSettingsArgument = `{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true}}`
)

// launchArguments returns the argument vector after the executable. schema is
// passed only when native result enforcement was negotiated.
func launchArguments(nativeSessionID, model string, schema json.RawMessage) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--replay-user-messages",
		"--session-id", nativeSessionID,
		"--model", model,
		"--permission-mode", permissionModeArgument,
	}
	if len(schema) != 0 {
		args = append(args, "--json-schema", string(schema))
	}
	return append(args,
		"--settings", sandboxSettingsArgument,
		"--disallowedTools", disallowedToolsArgument,
	)
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

func strippedEnvironmentName(name string) bool {
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
	result := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if !strippedEnvironmentName(name) {
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
