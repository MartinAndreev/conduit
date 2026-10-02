package codex

import (
	"time"

	internalid "github.com/MartinAndreev/conduit/internal/id"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

type SandboxMode string

const (
	SandboxReadOnly       SandboxMode = "read-only"
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
)

type ApprovalPolicy string

const (
	ApprovalNever     ApprovalPolicy = "never"
	ApprovalOnRequest ApprovalPolicy = "on-request"
)

type ReasoningEffort string

const (
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
)

// Config contains typed adapter launch policy. Credentials stay in the inherited
// process environment and are never copied into events or requests.
type Config struct {
	ExecutablePath string
	Sandbox        SandboxMode
	Approval       ApprovalPolicy
	Reasoning      ReasoningEffort
	Ephemeral      bool
	EventBuffer    int
	CloseGrace     time.Duration
	Launcher       processruntime.Launcher
	Now            func() time.Time
	NewID          func(prefix string) (string, error)
}

func withDefaults(config Config) Config {
	if config.ExecutablePath == "" {
		config.ExecutablePath = "codex"
	}
	if config.Sandbox == "" {
		config.Sandbox = SandboxWorkspaceWrite
	}
	if config.Approval == "" {
		config.Approval = ApprovalNever
	}
	if config.Reasoning == "" {
		config.Reasoning = ReasoningLow
	}
	if config.EventBuffer < 1 {
		config.EventBuffer = 256
	}
	if config.CloseGrace <= 0 {
		config.CloseGrace = 2 * time.Second
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
