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
	configuration.OpenCode: {"opencode", []string{"--version"}, func(output string) string { return parseVersionValue(strings.TrimSpace(output)) }},
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
