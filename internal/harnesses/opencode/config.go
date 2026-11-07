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
	// MessageID is the adapter-chosen native message ID ("msg_" prefix).
	// OpenCode admits the prompt under it, so its delivery can be queried
	// even when the session.prompt reply is lost. Empty lets OpenCode choose.
	MessageID string
	Text      string
	Delivery  string
}

type PromptResult struct {
	Accepted  bool
	ReceiptID provider.NativeReceiptID
}

// SteerDeliveryQuery names one native message of one native session.
type SteerDeliveryQuery struct {
	SessionID provider.NativeSessionID
	Directory string
	MessageID string
}

// SteerDeliveryObservation is what OpenCode reports about one message. A
// message in the history was delivered; one in the inbox is admitted but not
// yet delivered.
type SteerDeliveryObservation struct {
	InHistory bool
	InInbox   bool
}

// ReleasedSteerObservation is what OpenCode reports about a released message:
// whether it is in the ordered message history, whether an idle marker (an
// ended execution) follows it there, and whether it still waits in the inbox.
type ReleasedSteerObservation struct {
	InHistory        bool
	IdleAfterMessage bool
	InInbox          bool
}

// Control is the narrow session-control surface used by the adapter. The sent
// result distinguishes a pre-dispatch failure from an uncertain remote effect.
type Control interface {
	CreateSession(context.Context, CreateSessionRequest) (CreateSessionResult, bool, error)
	Prompt(context.Context, PromptRequest) (PromptResult, bool, error)
	// QuerySteerDelivery reads the native session without changing it. An
	// error means the query could not answer; it never means absence.
	QuerySteerDelivery(context.Context, SteerDeliveryQuery) (SteerDeliveryObservation, error)
	// QueryReleasedSteer reads whether a released message's work completed.
	// It never changes the session; an error means no answer.
	QueryReleasedSteer(context.Context, SteerDeliveryQuery) (ReleasedSteerObservation, error)
}

type Config struct {
	ExecutablePath string
	EventBuffer    int
	MaxTextBytes   int
	// MaxEventLineBytes bounds one native JSONL line. It is separate from
	// MaxTextBytes because one tool event may carry large output that is then
	// truncated per field.
	MaxEventLineBytes int
	// StopGrace bounds how long Close waits for the owned run client to exit
	// after it was terminated.
	StopGrace time.Duration
	Launcher  processruntime.Launcher
	Control   Control
	Now       func() time.Time
	NewID     func(string) (string, error)
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
	if config.MaxEventLineBytes < 1 {
		config.MaxEventLineBytes = 8 * 1024 * 1024
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
	if config.Control == nil {
		config.Control = &cliControl{executable: config.ExecutablePath, launcher: config.Launcher}
	}
	return config
}
