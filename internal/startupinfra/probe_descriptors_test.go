package startupinfra

import (
	"testing"

	"github.com/MartinAndreev/conduit/internal/configuration"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		harness configuration.HarnessID
		output  string
		want    string
	}{
		{"opencode prefixed newline", configuration.OpenCode, "opencode v2.0.22\n", "2.0.22"},
		{"opencode bare", configuration.OpenCode, "1.2.3\n", "1.2.3"},
		{"opencode prerelease", configuration.OpenCode, "opencode v2.0.22-beta.1\n", "2.0.22-beta.1"},
		{"opencode incomplete", configuration.OpenCode, "opencode v2.0\n", ""},
		{"opencode wrong prefix", configuration.OpenCode, "opencode 2.0.22\n", ""},
		{"opencode warning before", configuration.OpenCode, "warning: update available\nopencode v2.0.22\n", ""},
		{"opencode warning after", configuration.OpenCode, "opencode v2.0.22\nwarning: update available\n", ""},
		{"opencode trailing text", configuration.OpenCode, "opencode v2.0.22 extra\n", ""},
		{"opencode empty", configuration.OpenCode, "\n", ""},
		{"codex prefixed", configuration.Codex, "codex-cli 0.159.3\n", "0.159.3"},
		{"codex bare", configuration.Codex, "0.159.3\n", "0.159.3"},
		{"codex rejects opencode prefix", configuration.Codex, "opencode v2.0.22\n", ""},
		{"codex rejects warning", configuration.Codex, "codex-cli 0.159.3\nwarning\n", ""},
		{"claude live output", configuration.Claude, "2.1.316 (Claude Code)\n", "2.1.316"},
		{"claude bare", configuration.Claude, "2.1.316\n", "2.1.316"},
		{"claude other suffix", configuration.Claude, "2.1.316 (Other)\n", ""},
		{"claude missing space", configuration.Claude, "2.1.316(Claude Code)\n", ""},
		{"claude warning", configuration.Claude, "warning\n2.1.316 (Claude Code)\n", ""},
		{"fx bare", configuration.FX, "1.4.0\n", "1.4.0"},
		{"fx name prefix", configuration.FX, "fx 1.4.0\n", "1.4.0"},
		{"fx v prefix", configuration.FX, "v1.4.0-rc.1\n", "1.4.0-rc.1"},
		{"fx both prefixes", configuration.FX, "fx v1.4.0\n", ""},
		{"fx unrecorded format", configuration.FX, "fx version 1.4.0\n", ""},
		{"fx incomplete", configuration.FX, "fx 1.4\n", ""},
		{"fx empty", configuration.FX, "", ""},
		{"unsupported harness", configuration.HarnessID("unsupported"), "2.0.22", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseVersion(tc.harness, tc.output); got != tc.want {
				t.Fatalf("parseVersion(%q, %q) = %q, want %q", tc.harness, tc.output, got, tc.want)
			}
		})
	}
}
