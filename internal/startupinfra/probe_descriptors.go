package startupinfra

import (
	"regexp"
	"slices"
	"strings"

	"github.com/MartinAndreev/conduit/internal/configuration"
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

func parseVersion(h configuration.HarnessID, output string) string {
	descriptor, ok := probeDescriptorFor(h)
	if !ok {
		return ""
	}
	return descriptor.parse(output)
}

type probeDescriptor struct {
	command string
	args    []string
	parse   func(string) string
}

var probeDescriptors = map[configuration.HarnessID]probeDescriptor{
	configuration.Codex: {"codex", []string{"--version"}, func(output string) string {
		return parseVersionValue(strings.TrimPrefix(strings.TrimSpace(output), "codex-cli "))
	}},
	configuration.OpenCode: {"opencode", []string{"--version"}, func(output string) string {
		return parseVersionValue(strings.TrimPrefix(strings.TrimSpace(output), "opencode v"))
	}},
	// Recorded live output: "2.1.316 (Claude Code)". The version is the part
	// before the product suffix.
	configuration.Claude: {"claude", []string{"--version"}, func(output string) string {
		return parseVersionValue(strings.TrimSuffix(strings.TrimSpace(output), " (Claude Code)"))
	}},
	// No live fx output is recorded yet (spec 009). Accept semver with one
	// optional "fx " or "v" prefix. Any other successful output yields an empty
	// version: Detector then reports Found without a version (warning
	// harness_version_unknown), never ProbeFailed. ProbeFailed stays reserved
	// for a failed or over-limit process, decided by rules.DecideProbe before
	// any parser runs.
	configuration.FX: {"fx", []string{"--version"}, parseFXVersion},
}

func parseFXVersion(output string) string {
	value := strings.TrimSpace(output)
	if trimmed, ok := strings.CutPrefix(value, "fx "); ok {
		return parseVersionValue(trimmed)
	}
	return parseVersionValue(strings.TrimPrefix(value, "v"))
}

func probeDescriptorFor(h configuration.HarnessID) (probeDescriptor, bool) {
	if !slices.Contains(configuration.Supported(), h) {
		return probeDescriptor{}, false
	}
	descriptor, ok := probeDescriptors[h]
	return descriptor, ok
}
func parseVersionValue(value string) string {
	if versionPattern.MatchString(value) {
		return value
	}
	return ""
}
