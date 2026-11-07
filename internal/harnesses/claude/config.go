package claude

import (
	"os"
	"time"

	internalid "github.com/MartinAndreev/conduit/internal/id"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// Config contains typed adapter launch policy. The adapter launches the user's
// own `claude` executable; it never reads, stores, or forwards credentials.
type Config struct {
	ExecutablePath string
	EventBuffer    int
	// MaxLineBytes bounds one native stream-json line read from claude stdout.
	// A longer line stops the transport and loses the session visibly. One
	// assistant or tool result message arrives as a single line, so the default
	// is generous (128 MiB, as for Codex); nonpositive values select it.
	MaxLineBytes int
	// StopGrace bounds each wait for the owned process to exit while closing:
	// after stdin is closed and again after the process group is killed.
	StopGrace time.Duration
	// StderrTailBytes bounds the stderr tail kept only for open-failure
	// diagnostics. Nonpositive values select 8 KiB.
	StderrTailBytes int
	Launcher        processruntime.Launcher
	// Environ supplies the parent environment the launch environment is
	// derived from; it defaults to os.Environ.
	Environ func() []string
	Now     func() time.Time
	NewID   func(prefix string) (string, error)
	// NewUUID generates native UUIDs (--session-id and message uuid).
	NewUUID func() (string, error)
}

const (
	defaultMaxLineBytes    = 128 * 1024 * 1024
	defaultStderrTailBytes = 8 * 1024
)

func withDefaults(config Config) Config {
	if config.ExecutablePath == "" {
		config.ExecutablePath = "claude"
	}
	if config.EventBuffer < 1 {
		config.EventBuffer = 256
	}
	if config.MaxLineBytes < 1 {
		config.MaxLineBytes = defaultMaxLineBytes
	}
	if config.StopGrace <= 0 {
		config.StopGrace = 2 * time.Second
	}
	if config.StderrTailBytes < 1 {
		config.StderrTailBytes = defaultStderrTailBytes
	}
	if config.Launcher == nil {
		config.Launcher = processruntime.ExecLauncher{}
	}
	if config.Environ == nil {
		config.Environ = os.Environ
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = internalid.New
	}
	if config.NewUUID == nil {
		config.NewUUID = newUUID
	}
	return config
}
