package startup

import (
	"context"
	"errors"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/configuration"
	"github.com/MartinAndreev/conduit/internal/configuration/rules"
	"github.com/MartinAndreev/conduit/internal/id"
)

// Service exposes startup; Initialize remains the single owner of sequencing.
type Service struct {
	Repository Repository
	Detector   Detector
}
type operation struct {
	snapshot  Snapshot
	progress  *Progress
	published bool
	cleanup   CleanupObserver
}

func (o *operation) emit() { o.snapshot.Revision++; o.progress.publish(o.snapshot) }
func (o *operation) step(index int, status StepStatus, reason string) {
	o.snapshot.Steps[index].Status = status
	o.snapshot.Steps[index].Reason = reason
	o.emit()
}
func (o *operation) finish(status Status, err error) Outcome {
	o.snapshot.Status = status
	o.emit()
	return Outcome{Snapshot: clone(o.snapshot), Published: o.published, Err: err, PendingCleanup: o.cleanup}
}
func (o *operation) cancel(index int, err error) Outcome {
	o.step(index, StepCancelled, "Operation cancelled; completed effects remain.")
	return o.finish(Cancelled, err)
}
func (o *operation) block(index int, code string, err error) Outcome {
	o.snapshot.Diagnostics = append(o.snapshot.Diagnostics, Diagnostic{Code: code, Severity: Error, Step: o.snapshot.Steps[index].ID, Details: "Startup operation could not be completed."})
	o.step(index, Failed, "Startup is blocked; existing files are preserved.")
	return o.finish(Blocked, err)
}
func configCode(kind ReadKind) string { return "configuration_" + string(kind) }

func (s Service) Initialize(ctx context.Context, progress *Progress) Outcome {
	operationID, err := id.New("startup")
	o := operation{progress: progress, snapshot: Snapshot{OperationID: operationID, Status: Running, Steps: []StepState{{CheckConfiguration, Pending, ""}, {DiscoverHarnesses, Pending, ""}, {CreateSettings, Pending, ""}, {CheckReadiness, Pending, ""}}}}
	o.emit()
	if err != nil {
		return o.block(0, "operation_id_failed", err)
	}
	if s.Repository == nil || s.Detector == nil {
		return o.block(0, "startup_dependencies_missing", errors.New("startup dependencies missing"))
	}
	if err = ctx.Err(); err != nil {
		return o.cancel(0, err)
	}
	o.step(0, Working, "")
	path, err := s.Repository.Resolve(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return o.cancel(0, errors.Join(ctx.Err(), err))
		}
		return o.block(0, "home_unavailable", err)
	}
	o.snapshot.ConfigPath = path
	read := s.Repository.Read(ctx, path)
	if ctx.Err() != nil {
		return o.cancel(0, errors.Join(ctx.Err(), read.Err))
	}
	use := rules.DecideUse(rules.UseFacts{Missing: read.Kind == Absent, Valid: read.Kind == Valid})
	if use.Kind == rules.RejectConfiguration {
		return o.block(0, configCode(read.Kind), read.Err)
	}
	o.step(0, Completed, use.Reason)
	observations, discoveryErr := s.discover(ctx, &o, read.Config)
	if discoveryErr != nil {
		return o.finishDiscoveryFailure(ctx, discoveryErr)
	}
	creation := rules.DecideCreation(rules.CreationFacts{Missing: read.Kind == Absent, DiscoveryComplete: true})
	if err = ctx.Err(); err != nil {
		return o.cancel(2, err)
	}
	if creation.Kind == rules.CreateInitial {
		o.step(2, Working, creation.Reason)
		kind, publishErr := s.Repository.Publish(ctx, path, configuration.Initial(observations))
		o.published = kind == Published
		if publishErr != nil {
			if ctx.Err() != nil {
				return o.cancel(2, errors.Join(ctx.Err(), publishErr))
			}
			return o.block(2, "configuration_publish_failed", publishErr)
		}
		if kind != Published && kind != AlreadyExists {
			return o.block(2, "configuration_publish_invalid_result", fmt.Errorf("unexpected publication outcome"))
		}
		if ctx.Err() != nil {
			return o.cancel(2, ctx.Err())
		}
		if kind == AlreadyExists {
			o.step(2, Skipped, "Another operation published configuration; validate its file.")
		} else {
			o.step(2, Completed, "Initial configuration published.")
		}
	} else if creation.Kind == rules.PreserveExisting {
		o.step(2, Skipped, creation.Reason)
	} else {
		return o.block(2, "configuration_creation_rejected", errors.New(creation.Reason))
	}
	return s.confirmReadiness(ctx, &o, path)
}

// finishDiscoveryFailure stops effects while preserving cancellation and cleanup causes.
func (o *operation) finishDiscoveryFailure(ctx context.Context, err error) Outcome {
	if ctx.Err() != nil || (!errors.Is(err, ErrProbeCleanup) && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))) {
		return o.cancel(1, errors.Join(ctx.Err(), err))
	}
	o.step(1, Failed, "Discovery stopped because probe cleanup could not be confirmed.")
	return o.finish(Blocked, err)
}

// discover continues after ordinary metadata failures, but stops on cancellation
// or uncertain cleanup. Account/model/protocol knowledge remains unknown.
func (s Service) discover(ctx context.Context, o *operation, c configuration.Config) (map[configuration.HarnessID]configuration.Observation, error) {
	o.step(1, Working, "")
	observations := map[configuration.HarnessID]configuration.Observation{}
	for _, h := range configuration.Supported() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		observation, err := s.Detector.Discover(ctx, h, c.Harnesses[h].Executable)
		if errors.Is(err, ErrProbeCleanup) {
			var pending PendingCleanupError
			if errors.As(err, &pending) {
				o.cleanup = pending.CleanupObserver()
			}
			o.snapshot.Diagnostics = append(o.snapshot.Diagnostics, Diagnostic{Code: "harness_probe_cleanup_unconfirmed", Severity: Error, Step: DiscoverHarnesses, Harness: h, Details: "Cleanup of the local probe could not be confirmed."})
			return nil, errors.Join(ctx.Err(), err)
		}
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), err)
		}
		if err != nil { // Unexpected infrastructure failure is safely represented, not raw output.
			observation.Kind = configuration.ProbeFailed
		}
		observations[h] = observation
		o.snapshot.Observations = append(o.snapshot.Observations, HarnessObservation{h, observation, Capabilities{"unknown", "unknown", "unknown"}})
		if observation.Kind != configuration.Found || observation.Version == "" {
			o.snapshot.Diagnostics = append(o.snapshot.Diagnostics, Diagnostic{Code: discoveryCode(observation, c.Harnesses[h].Executable != ""), Severity: Warning, Step: DiscoverHarnesses, Harness: h, Details: "Local discovery did not establish a version; account and model capabilities remain unknown."})
		}
		o.emit()
	}
	o.step(1, Completed, "Local discovery completed.")
	return observations, nil
}
func discoveryCode(o configuration.Observation, explicit bool) string {
	if explicit && o.Kind == configuration.Missing {
		return "harness_override_missing"
	}
	if o.Kind == configuration.Found {
		return "harness_version_unknown"
	}
	return "harness_" + string(o.Kind)
}

// confirmReadiness also reconciles a concurrent publication winner. We never
// assume our proposed defaults describe the file that actually won.
func (s Service) confirmReadiness(ctx context.Context, o *operation, path string) Outcome {
	if ctx.Err() != nil {
		return o.cancel(3, ctx.Err())
	}
	o.step(3, Working, "")
	final := s.Repository.Read(ctx, path)
	if ctx.Err() != nil {
		return o.cancel(3, errors.Join(ctx.Err(), final.Err))
	}
	decision := rules.DecideReadiness(rules.ReadinessFacts{Valid: final.Kind == Valid, Warnings: len(o.snapshot.Diagnostics) > 0})
	if decision.Kind == rules.Blocked {
		return o.block(3, "final_"+configCode(final.Kind), final.Err)
	}
	o.step(3, Completed, decision.Reason)
	if decision.Kind == rules.ReadyWithWarnings {
		return o.finish(ReadyWithWarnings, nil)
	}
	return o.finish(Ready, nil)
}
