// Package antigravity composes the shared ACP client for the Antigravity
// harness (`agy acp`). It owns only Antigravity launch details and extension
// knowledge; ACP semantics stay in the acp package.
package antigravity

import (
	"context"
	"regexp"
	"strings"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const (
	providerName   = "antigravity"
	adapterVersion = "antigravity-acp/1"
	executable     = "agy"
)

// Provider is the Antigravity harness adapter.
type Provider struct {
	adapter *acp.Adapter
}

// New returns the Antigravity adapter. Config.ExecutablePath overrides "agy".
func New(config acp.Config) *Provider {
	return &Provider{adapter: acp.New(Descriptor(), config)}
}

func (p *Provider) Inspect(ctx context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	return p.adapter.Inspect(ctx, request)
}

func (p *Provider) OpenSession(ctx context.Context, request provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	return p.adapter.OpenSession(ctx, request)
}

// Descriptor is the Antigravity hook for the shared ACP client. Antigravity
// inherits its credentials and cached CLI login from the environment, so no
// environment entries are added. An agent that cannot switch to mode "auto"
// fails OpenSession.
func Descriptor() acp.Descriptor {
	return acp.Descriptor{
		ProviderName:   providerName,
		AdapterVersion: adapterVersion,
		Executable:     executable,
		Args:           []string{"acp"},
		ParseVersion:   ParseVersion,
		// Recorded live against agy 1.5.13 (2026-11-21): Antigravity exposes no
		// provider selector (ProviderConfigID stays empty) and carries
		// reasoning effort through the "thought_level" config option (values
		// are model-dependent, e.g. low/medium/high/xhigh).
		VariantConfigID: "thought_level",
		// Martin, 2026-11-21: Antigravity sessions require mode "auto"
		// (workspace edits and safe commands approved automatically; default
		// mode would turn edits and commands into auto-rejected permission
		// requests).
		SessionSettings:     []acp.ConfigSetting{{ConfigID: "mode", Value: "auto"}},
		CapabilityOverrides: capabilityOverrides,
	}
}

// VersionCommand is the probe a discovery owner may run.
func VersionCommand() (string, []string) {
	return executable, []string{"--version"}
}

var semanticVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?$`)

// ParseVersion normalizes an Antigravity version. Both `agy --version` and
// initialize `agentInfo.version` were recorded live on 1.5.13 as a bare
// semantic version ("1.5.13"); one optional "agy " or "v" prefix is also
// accepted. Anything else is unknown ("").
func ParseVersion(value string) string {
	value = strings.TrimSpace(value)
	if trimmed, ok := strings.CutPrefix(value, "agy "); ok {
		value = trimmed
	} else {
		value = strings.TrimPrefix(value, "v")
	}
	if !semanticVersion.MatchString(value) {
		return ""
	}
	return value
}

// capabilityOverrides keeps active-turn steering unsupported until a steering
// wire contract is recorded and supported by the shared ACP client.
func capabilityOverrides(_ acp.AgentFacts) []provider.CapabilityObservation {
	return []provider.CapabilityObservation{
		{
			Capability: provider.CapabilitySteerActiveTurn,
			Support:    provider.SupportUnsupported,
			Evidence:   provider.EvidenceDeclared,
			Detail:     "antigravity steering wire shape is not recorded; base ACP has no steering",
		},
	}
}

var _ provider.Provider = (*Provider)(nil)
