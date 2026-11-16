package live

import (
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestCommandRequested(t *testing.T) {
	const command = "echo OK > control.txt; cat '/h/.conduit/c' > read.txt"
	tests := []struct {
		name    string
		started provider.CommandStartedEvent
		want    bool
	}{
		{"argv", provider.CommandStartedEvent{Argv: []string{"/bin/zsh", "-lc", command}}, true},
		{"sh -c", provider.CommandStartedEvent{Argv: []string{"/bin/sh", "-c", command}}, true},
		{"codex display", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "/bin/zsh -lc \"" + command + "\""}}, true},
		{"display sh -c", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "/bin/sh -c \"" + command + "\""}}, true},
		{"display with more", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "/bin/zsh -lc \"" + command + "; echo extra\""}}, false},
		{"display in a comment", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "/bin/zsh -lc \"echo OK # " + command + "\""}}, false},
		{"display relative shell", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "zsh -lc \"" + command + "\""}}, false},
		{"display bare script", provider.CommandStartedEvent{Display: provider.BoundedText{Text: command}}, false},
		{"in a comment", provider.CommandStartedEvent{Argv: []string{"/bin/zsh", "-lc", "echo OK > control.txt # " + command}}, false},
		{"with more commands", provider.CommandStartedEvent{Argv: []string{"/bin/zsh", "-lc", command + "; echo extra"}}, false},
		{"in a branch", provider.CommandStartedEvent{Argv: []string{"/bin/zsh", "-lc", "false && " + command}}, false},
		{"no shell flag", provider.CommandStartedEvent{Argv: []string{"/bin/zsh", "-x", command}}, false},
		{"changed", provider.CommandStartedEvent{Argv: []string{"/bin/zsh", "-lc", "echo OK > control.txt"}}, false},
		{"split across args", provider.CommandStartedEvent{Argv: []string{"echo OK > control.txt;", "cat '/h/.conduit/c' > read.txt"}}, false},
	}
	for _, tt := range tests {
		if got := commandRequested("codex", tt.started, command); got != tt.want {
			t.Fatalf("%s: commandRequested = %t, want %t", tt.name, got, tt.want)
		}
	}
}

func TestCommandRequestedOpenCode(t *testing.T) {
	const command = "echo OK > control.txt; cat '/h/.conduit/c' > read.txt; echo PROBE > '/h/.conduit/p'; true"
	tests := []struct {
		name    string
		started provider.CommandStartedEvent
		want    bool
	}{
		{"exact script", provider.CommandStartedEvent{Display: provider.BoundedText{Text: command}}, true},
		{"truncated", provider.CommandStartedEvent{Display: provider.BoundedText{Text: command, Truncated: true}}, false},
		{"with more", provider.CommandStartedEvent{Display: provider.BoundedText{Text: command + "; echo extra"}}, false},
		{"prefixed", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "cd /tmp && " + command}}, false},
		{"in a comment", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "echo OK # " + command}}, false},
		{"in a branch", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "false && " + command}}, false},
		{"trailing newline", provider.CommandStartedEvent{Display: provider.BoundedText{Text: command + "\n"}}, false},
		{"leading space", provider.CommandStartedEvent{Display: provider.BoundedText{Text: " " + command}}, false},
		{"shell display form", provider.CommandStartedEvent{Display: provider.BoundedText{Text: "/bin/zsh -lc \"" + command + "\""}}, false},
		{"argv present", provider.CommandStartedEvent{Argv: []string{"/bin/sh", "-c", command}, Display: provider.BoundedText{Text: command}}, false},
		{"empty", provider.CommandStartedEvent{}, false},
	}
	for _, tt := range tests {
		if got := commandRequested("opencode", tt.started, command); got != tt.want {
			t.Fatalf("%s: commandRequested = %t, want %t", tt.name, got, tt.want)
		}
	}
	// A bare script is not the Codex display form.
	if commandRequested("codex", provider.CommandStartedEvent{Display: provider.BoundedText{Text: command}}, command) {
		t.Fatal("codex counted a bare script")
	}
}

func TestOSDeniedForAttributesEachPath(t *testing.T) {
	const canary, probe = "/h/.conduit/canary.txt", "/h/.conduit/probe.txt"
	output := "cat: /h/.conduit/canary.txt: Operation not permitted\nzsh:1: operation not permitted: /h/.conduit/probe.txt\n"
	if !osDeniedFor(output, canary) || !osDeniedFor(output, probe) {
		t.Fatal("denials not attributed to both paths")
	}
	readOnly := "cat: /h/.conduit/canary.txt: Operation not permitted\n"
	if !osDeniedFor(readOnly, canary) || osDeniedFor(readOnly, probe) {
		t.Fatal("read denial attributed to the write")
	}
	if osDeniedFor("cat: /h/.conduit/canary.txt: No such file or directory\n", canary) {
		t.Fatal("other error counted as an OS denial")
	}
	if osDeniedFor("Operation not permitted\n/h/.conduit/canary.txt\n", canary) {
		t.Fatal("path and denial on different lines counted")
	}
}

func TestCommandRequestedRefusesAmbiguousDisplay(t *testing.T) {
	for _, command := range []string{"echo \"x\"", "echo $HOME", "echo `id`", "echo a\\b"} {
		started := provider.CommandStartedEvent{Display: provider.BoundedText{Text: "/bin/zsh -lc \"" + command + "\""}}
		if commandRequested("codex", started, command) {
			t.Fatalf("ambiguous display counted for %q", command)
		}
	}
}
