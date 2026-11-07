// Package fx composes the shared ACP client for the fx harness (`fx acp`).
// It owns only fx launch details and fx extension knowledge; ACP semantics
// stay in the acp package.
package fx

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const (
	providerName   = "fx"
	adapterVersion = "fx-acp/1"
	executable     = "fx"
)

// Provider is the fx harness adapter.
type Provider struct {
	adapter *acp.Adapter
}

// New returns the fx adapter. Config.ExecutablePath overrides "fx".
func New(config acp.Config) *Provider {
	return &Provider{adapter: acp.New(Descriptor(), config)}
}

func (p *Provider) Inspect(ctx context.Context, request provider.InspectRequest) (provider.ProviderDescriptor, error) {
	return p.adapter.Inspect(ctx, request)
}

func (p *Provider) OpenSession(ctx context.Context, request provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	return p.adapter.OpenSession(ctx, request)
}

// Descriptor is the fx hook for the shared ACP client. fx inherits its
// credentials from the environment, so no environment entries are added. An
// fx that cannot switch to mode "auto" fails OpenSession.
func Descriptor() acp.Descriptor {
	return acp.Descriptor{
		ProviderName:   providerName,
		AdapterVersion: adapterVersion,
		Executable:     executable,
		Args:           []string{"acp"},
		ParseVersion:   ParseVersion,
		// Martin, 2026-11-07: fx runs in mode "auto", the closest match to
		// Codex workspace-write with approval never. fx's default "ask"
		// would turn every change into an auto-rejected permission request.
		SessionSettings:     []acp.ConfigSetting{{ConfigID: "mode", Value: "auto"}},
		CapabilityOverrides: capabilityOverrides,
	}
}

// VersionCommand is the probe a discovery owner may run; it is not wired into
// startup discovery by this package.
func VersionCommand() (string, []string) {
	return executable, []string{"--version"}
}

var semanticVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?$`)

// ParseVersion normalizes an fx version. The recorded initialize
// agentInfo.version is a bare semantic version ("0.0.14"); a "fx " or "v"
// prefix, as a --version output might carry, is accepted. Anything else is
// unknown ("").
func ParseVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "fx ")
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if !semanticVersion.MatchString(value) {
		return ""
	}
	return value
}

type agentMeta struct {
	FX struct {
		Steering bool `json:"steering"`
	} `json:"fx"`
}

// capabilityOverrides records fx's _meta.fx.steering advertisement without
// claiming support: no recorded frame shows an fx steering message (the
// recorded steering scenario is session/cancel followed by a new prompt).
func capabilityOverrides(facts acp.AgentFacts) []provider.CapabilityObservation {
	var meta agentMeta
	advertised := facts.Observed && json.Unmarshal(facts.Meta, &meta) == nil && meta.FX.Steering
	detail := "fx steering is not exercised by any recorded frame; base ACP has no steering"
	if advertised {
		detail = "agent advertises _meta.fx.steering, but no recorded frame shows its wire shape; unsupported until fixture evidence exists"
	}
	return []provider.CapabilityObservation{
		{Capability: provider.CapabilitySteerActiveTurn, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: detail},
	}
}

var _ provider.Provider = (*Provider)(nil)
