package opencode

import (
	"context"
	"time"

	internalid "github.com/MartinAndreev/conduit/internal/id"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type CreateSessionRequest struct {
	Directory string
	Model     provider.ModelSelection
	Title     string
}

type CreateSessionResult struct {
	SessionID     provider.NativeSessionID
	Model         provider.ModelSelection
	ModelVerified bool
}

type PromptRequest struct {
	SessionID provider.NativeSessionID
	Directory string
	Text      string
	Delivery  string
}

type PromptResult struct {
	Accepted  bool
	ReceiptID provider.NativeReceiptID
}

// Control is the narrow session-control surface used by the adapter. The sent
// result distinguishes a pre-dispatch failure from an uncertain remote effect.
type Control interface {
	CreateSession(context.Context, CreateSessionRequest) (CreateSessionResult, bool, error)
	Prompt(context.Context, PromptRequest) (PromptResult, bool, error)
}

type Config struct {
	ExecutablePath string
	EventBuffer    int
	MaxTextBytes   int
	Launcher       processruntime.Launcher
	Control        Control
	Now            func() time.Time
	NewID          func(string) (string, error)
}

func withDefaults(config Config) Config {
	if config.ExecutablePath == "" {
		config.ExecutablePath = "opencode"
	}
	if config.EventBuffer < 1 {
		config.EventBuffer = 256
	}
	if config.MaxTextBytes < 1 {
		config.MaxTextBytes = 64 * 1024
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
	if config.Control == nil {
		config.Control = &cliControl{executable: config.ExecutablePath, launcher: config.Launcher}
	}
	return config
}
