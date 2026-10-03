package startupinfra

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/configuration"
	"github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/startupinfra/rules"
	"time"
)

const probeCaptureBytes = 16 * 1024
const probeTimeout = 3 * time.Second

// Detector uses the established process owner. Timeout is injectable only to
// shorten isolated tests; production's zero value uses the approved three seconds.
type Detector struct {
	Launcher process.Launcher
	LookPath func(string) (string, error)
	Timeout  time.Duration
}

func (d Detector) Discover(ctx context.Context, h configuration.HarnessID, override string) (configuration.Observation, error) {
	if err := ctx.Err(); err != nil {
		return configuration.Observation{}, err
	}
	descriptor, supported := probeDescriptorFor(h)
	if !supported {
		return configuration.Observation{}, errors.New("unsupported harness")
	}
	path, found, err := d.queryExecutable(ctx, descriptor.command, override)
	if err != nil {
		return configuration.Observation{}, err
	}
	if !found {
		return configuration.Observation{Kind: configuration.Missing}, nil
	}
	observation := configuration.Observation{Kind: configuration.Found, Path: path}
	execution, err := d.executeProbe(ctx, path, descriptor.args)
	if err != nil {
		observation.Kind = configuration.ProbeFailed
		return observation, err
	}
	result, timedOut := execution.result, execution.timedOut
	output, exceeded := execution.output, execution.exceeded
	cleanupErr := execution.cleanupErr
	decision := rules.DecideProbe(rules.ProbeFacts{
		Cancelled: ctx.Err() != nil, TimedOut: timedOut,
		CleanupFailed:    cleanupErr != nil,
		ProcessSucceeded: result.Err == nil && result.ExitCode == 0, CaptureExceeded: exceeded,
	})
	switch decision.Kind {
	case rules.MetadataCancelled:
		if cleanupErr != nil {
			return observation, errors.Join(ctx.Err(), cleanupErr)
		}
		return observation, ctx.Err()
	case rules.CleanupUnconfirmed:
		observation.Kind = configuration.ProbeFailed
		return observation, cleanupErr
	case rules.MetadataTimedOut:
		observation.Kind = configuration.ProbeTimeout
		return observation, nil
	case rules.RejectMetadata:
		observation.Kind = configuration.ProbeFailed
		return observation, nil
	}
	observation.Version = descriptor.parse(output)
	return observation, nil
}
