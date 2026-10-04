package bridge

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
	"time"
)

// Fixture uses the real application procedure with synthetic ports: no real home,
// configuration files, executables, sessions, prompts or credentials are accessed.
func Fixture(name string) (Initializer, error) {
	switch name {
	case "ready", "warnings", "blocked", "slow":
	default:
		return nil, errors.New("unknown GUI fixture")
	}
	return startup.Service{Repository: fixtureRepository{name: name}, Detector: fixtureDetector{name: name}}, nil
}

type fixtureRepository struct{ name string }

func (f fixtureRepository) Resolve(context.Context) (string, error) {
	return "/synthetic/.conduit/config.yaml", nil
}
func (f fixtureRepository) Read(context.Context, string) startup.ReadResult {
	if f.name == "blocked" {
		return startup.ReadResult{Kind: startup.Invalid, Err: configuration.ErrInvalid}
	}
	return startup.ReadResult{Kind: startup.Valid, Config: configuration.Config{SchemaVersion: 1, Harnesses: map[configuration.HarnessID]configuration.Harness{configuration.Codex: {}, configuration.OpenCode: {}}, Assignments: map[string]string{}}}
}
func (f fixtureRepository) Publish(context.Context, string, configuration.Config) (startup.PublishKind, error) {
	return startup.Published, nil
}

type fixtureDetector struct{ name string }

func (f fixtureDetector) Discover(ctx context.Context, h configuration.HarnessID, _ string) (configuration.Observation, error) {
	delay := 350 * time.Millisecond
	if f.name == "slow" {
		delay = 20 * time.Second
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return configuration.Observation{}, ctx.Err()
	}
	if f.name == "warnings" {
		return configuration.Observation{Kind: configuration.Missing}, nil
	}
	return configuration.Observation{Kind: configuration.Found, Path: "/synthetic/" + string(h), Version: "fixture"}, nil
}
