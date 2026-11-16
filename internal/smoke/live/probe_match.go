package live

import (
	"strings"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// commandRequested reports whether a started command is exactly the requested
// shell script, in the form the selected provider reports it.
//
// OpenCode reports no argv: its bash tool's display is the script itself
// (`state.input.command`, normalized as the CommandStartedEvent display), so
// only an untruncated display equal to command counts.
//
// Codex reports no argv, only its display form
// `<absolute shell> -lc "<script>"` (or -c), which is unambiguous only when the
// script has no character a double-quoted shell string interprets; such a
// script is never counted. The script must equal command: one that only
// contains it (in a comment, a branch that never runs or next to other
// commands) does not count. With an argv, its last two elements must be the
// shell flag and the script.
func commandRequested(selected string, started provider.CommandStartedEvent, command string) bool {
	if selected == "opencode" {
		return len(started.Argv) == 0 && !started.Display.Truncated && started.Display.Text == command
	}
	if argv := started.Argv; len(argv) >= 3 {
		flag := argv[len(argv)-2]
		return (flag == "-c" || flag == "-lc") && argv[len(argv)-1] == command
	}
	if strings.ContainsAny(command, "\"$`\\\n") {
		return false
	}
	for _, flag := range []string{" -lc \"", " -c \""} {
		shell, script, found := strings.Cut(started.Display.Text, flag)
		if found && strings.HasPrefix(shell, "/") && !strings.ContainsAny(shell, " \t\"'") {
			return script == command+"\""
		}
	}
	return false
}

// osDeniedFor reports whether one output line names path together with the
// OS refusal. Shells print it lower case ("zsh:1: operation not permitted:
// <path>"), tools like cat capitalised, so the match ignores case.
func osDeniedFor(output, path string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, path) && strings.Contains(strings.ToLower(line), "operation not permitted") {
			return true
		}
	}
	return false
}
