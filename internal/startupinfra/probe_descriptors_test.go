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
		{"unsupported harness", configuration.HarnessID("unsupported"), "2.0.22", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseVersion(tc.harness, tc.output); got != tc.want {
				t.Fatalf("parseVersion(%q, %q) = %q, want %q", tc.harness, tc.output, got, tc.want)
			}
		})
	}
}
