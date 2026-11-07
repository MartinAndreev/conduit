package acp

import (
	"encoding/json"
	"time"

	internalid "github.com/MartinAndreev/conduit/internal/id"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// ProtocolVersion is the only ACP protocol version this client speaks.
const ProtocolVersion = 1

const protocolName = "acp/1"

// Descriptor is the hook through which a harness package composes the shared
// ACP client. ACP semantics stay in this package; harness-specific launch
// details and extension knowledge stay with the harness owner.
type Descriptor struct {
	// ProviderName and AdapterVersion identify the harness adapter.
	ProviderName   string
	AdapterVersion string
	// Executable is the default command; Config.ExecutablePath overrides it.
	Executable string
	// Args start the agent in ACP stdio mode.
	Args []string
	// Env holds extra KEY=VALUE entries appended to the inherited environment.
	Env []string
	// ParseVersion normalizes a native version string (initialize
	// agentInfo.version) into HarnessVersion. An empty result means unknown.
	ParseVersion func(string) string
	// RecognizedNotifications lists native extension notification methods the
	// harness owner classified as administrative. They are ignored silently;
	// any other unknown notification is warned once per method.
	RecognizedNotifications []string
	// SessionSettings are session config values the harness requires. They
	// are applied after session/new and model selection; a setting the agent
	// cannot honour fails OpenSession.
	SessionSettings []ConfigSetting
	// CapabilityOverrides replaces baseline observations by capability. It
	// receives the agent's initialize facts, or zero facts during Inspect.
	CapabilityOverrides func(AgentFacts) []provider.CapabilityObservation
}

// ConfigSetting is one required ACP session config option value.
type ConfigSetting struct {
	ConfigID string
	Value    string
}

// AgentFacts are the native observations from the initialize response that a
// descriptor may interpret. Meta is agentCapabilities._meta, unparsed.
type AgentFacts struct {
	Observed        bool
	ProtocolVersion int
	AgentName       string
	AgentVersion    string
	LoadSession     bool
	CloseSupported  bool
	Meta            json.RawMessage
}

// Config contains launch and bounding policy common to ACP harnesses.
// Credentials stay in the inherited process environment.
type Config struct {
	ExecutablePath string
	EventBuffer    int
	// MaxLineBytes bounds one native JSON-RPC line. A longer line stops the
	// transport and loses the session. Nonpositive selects 128 MiB.
	MaxLineBytes int
	// MaxTextBytes bounds text copied from one native field into one event.
	MaxTextBytes int
	// StopGrace bounds each close phase: session/close, exit after stdin
	// close, and reaping after the group kill.
	StopGrace time.Duration
	Launcher  processruntime.Launcher
	Now       func() time.Time
	NewID     func(prefix string) (string, error)
}

const (
	defaultMaxLineBytes = 128 * 1024 * 1024
	defaultMaxTextBytes = 64 * 1024
	// maxNativeErrorBytes bounds native error and warning text in one event.
	maxNativeErrorBytes = 4 * 1024
)

func withDefaults(descriptor Descriptor, config Config) Config {
	if config.ExecutablePath == "" {
		config.ExecutablePath = descriptor.Executable
	}
	if config.EventBuffer < 1 {
		config.EventBuffer = 256
	}
	if config.MaxLineBytes < 1 {
		config.MaxLineBytes = defaultMaxLineBytes
	}
	if config.MaxTextBytes < 1 {
		config.MaxTextBytes = defaultMaxTextBytes
	}
	if config.StopGrace <= 0 {
		config.StopGrace = 2 * time.Second
	}
	if config.Launcher == nil {
		config.Launcher = processruntime.ExecLauncher{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = internalid.New
	}
	return config
}

// baselineCapabilities are the ACP observations shared by every harness.
// Evidence names the sanitized protocol fixtures in testdata.
func baselineCapabilities() []provider.CapabilityObservation {
	const fixture = "sanitized ACP JSON-RPC fixtures; configured executable not inspected"
	return []provider.CapabilityObservation{
		{Capability: provider.CapabilityEventText, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: fixture},
		{Capability: provider.CapabilityEventTool, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: fixture},
		{Capability: provider.CapabilityEventCommand, Support: provider.SupportSupported, Evidence: provider.EvidenceDeclared, Detail: "ACP execute tool kind; command output and exit code are not standardized"},
		{Capability: provider.CapabilityEventFile, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: fixture + "; file activity is the agent's own report"},
		{Capability: provider.CapabilityEventReasoning, Support: provider.SupportUnknown, Evidence: provider.EvidenceFixtureVerified, Detail: "agent thought text exists but is not a proven summary"},
		{Capability: provider.CapabilitySteerActiveTurn, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: "base ACP has no steering"},
		{Capability: provider.CapabilitySteerNextBoundary, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: "base ACP has no steering"},
		{Capability: provider.CapabilityTurnInterrupt, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Targeting: provider.TargetSessionLease, Detail: "session/cancel notification; receipt only"},
		{Capability: provider.CapabilitySessionResume, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
		{Capability: provider.CapabilitySessionObserveState, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Detail: "adapter-local partial observation"},
		{Capability: provider.CapabilityPermissionRoundTrip, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: "permission requests are answered reject_once automatically"},
		{Capability: provider.CapabilityQuestionRoundTrip, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: "elicitation requests are declined automatically"},
		{Capability: provider.CapabilityResultNativeSchema, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared, Detail: "ACP has no native result schema"},
		{Capability: provider.CapabilityEventsNativeReplay, Support: provider.SupportUnsupported, Evidence: provider.EvidenceDeclared},
	}
}

func (a *Adapter) capabilities(model provider.ModelSelection, facts AgentFacts, observedAt time.Time) provider.CapabilitySnapshot {
	items := baselineCapabilities()
	if a.descriptor.CapabilityOverrides != nil {
		for _, override := range a.descriptor.CapabilityOverrides(facts) {
			replaced := false
			for index := range items {
				if items[index].Capability == override.Capability {
					items[index] = override
					replaced = true
				}
			}
			if !replaced {
				items = append(items, override)
			}
		}
	}
	harnessVersion := ""
	if facts.Observed && a.descriptor.ParseVersion != nil {
		harnessVersion = a.descriptor.ParseVersion(facts.AgentVersion)
	}
	if harnessVersion == "" {
		harnessVersion = "unknown"
	}
	return provider.CapabilitySnapshot{
		ProviderName: a.descriptor.ProviderName, AdapterVersion: a.descriptor.AdapterVersion,
		HarnessVersion: harnessVersion, ProtocolVersion: protocolName,
		Model: model, ObservedAt: observedAt, Items: items,
	}
}
