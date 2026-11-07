package opencode

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/adaptererror"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/boundedtext"
	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type session struct {
	config        Config
	ref           provider.SessionRef
	nativeSession provider.NativeSessionID
	model         provider.ModelSelection
	workspace     string
	contract      provider.ResultContractNegotiation
	capabilities  provider.CapabilitySnapshot
	broker        *eventstream.Broker
	mu            sync.Mutex
	emitMu        sync.Mutex
	assignment    provider.AssignmentRef
	turn          provider.Optional[provider.TurnRef]
	process       processruntime.Process
	state         provider.SessionState
	closed        bool
	steerPending  bool
	// deliveryUnknown is a turn start whose delivery is unknown.
	deliveryUnknown bool
	// unresolvedSteer is a possibly admitted steer that native
	// reconciliation has not decided. It blocks turns and correction until
	// reconciled or explicitly released by Conduit.
	unresolvedSteer *unresolvedSteer
	// released holds every steer Conduit explicitly released, keyed by native
	// message ID. Each may still be pending in OpenCode and produce output in
	// a later run; OpenCode's run JSONL links output only to assistant
	// messages, never to the user message, so no output is attributed while
	// any of them is unsettled. Each is settled individually by a native query.
	// Steering stays allowed meanwhile: a correction still advances the
	// binding on its own receipt, and the guard keeps output unattributed.
	released map[string]*releasedSteer
	// steerReconcileApplied is a test seam called after an unresolved-steer
	// reconciliation was applied; it is nil in production.
	steerReconcileApplied func(rules.SteerReconcileKind)
	seq                   uint64
}

// releasedSteer is the native message of an explicitly released steer.
type releasedSteer struct {
	nativeMessageID string
	// querying is set while a native query for it runs.
	querying bool
}

// unresolvedSteer keeps the steer's Conduit message and the native message ID
// it was admitted under, so delivery can be queried and a release matched.
type unresolvedSteer struct {
	request         provider.SteerRequest
	nativeMessageID string
	// reconciling is set while the native delivery query runs.
	reconciling bool
}

// deliveryUnresolved must be called with s.mu held.
func (s *session) deliveryUnresolved() bool {
	return s.deliveryUnknown || s.unresolvedSteer != nil
}

func (s *session) Ref() provider.SessionRef                  { return s.ref }
func (s *session) Capabilities() provider.CapabilitySnapshot { return s.capabilities }
func (s *session) Events(_ context.Context, cursor provider.EventCursor) (provider.EventStream, error) {
	if cursor.AdapterCursor != "" {
		return nil, adaptererror.NewWithDetail(provider.OperationSubscribeEvents, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "native replay is unsupported", nil)
	}
	return s.broker.Subscribe()
}

func (s *session) ObserveState(_ context.Context, request provider.ObserveStateRequest) (provider.SessionSnapshot, error) {
	var result provider.SessionSnapshot
	err := lockscope.WithMutexLock(&s.mu, func() error {
		if request.Session != s.ref {
			return s.stale(provider.OperationObserveState)
		}
		knowledge, state := provider.SnapshotPartial, s.state
		if s.deliveryUnresolved() {
			knowledge, state = provider.SnapshotUnknown, provider.SessionStateUnknown
		}
		result = provider.SessionSnapshot{Knowledge: knowledge, SessionState: state, ActiveTurn: s.turn, Native: provider.NativeIDs{Session: provider.Some(s.nativeSession)}, ObservedAt: s.config.Now()}
		return nil
	})
	return result, err
}

func (s *session) StartTurn(ctx context.Context, request provider.StartTurnRequest) (provider.StartTurnResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.StartTurnResult{}, adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, false, err)
	}
	var turn provider.TurnRef
	var proc processruntime.Process
	err := lockscope.WithMutexLock(&s.mu, func() error {
		release := request.ReleaseUnresolvedSteer
		decision := rules.DecideStartTurn(rules.StartTurnFacts{
			SessionMatches:            request.Session == s.ref,
			AssignmentIdentityMatches: request.Assignment.SameIdentity(s.assignment),
			AssignmentRevisionMatches: request.Assignment.Revision == s.assignment.Revision,
			Closed:                    s.closed, ActiveTurn: s.turn.Present, SteerPending: s.steerPending,
			DeliveryUnknown:               s.deliveryUnknown,
			SteerUnresolved:               s.unresolvedSteer != nil,
			ReleaseRequested:              release.Present,
			ReleaseMatchesUnresolvedSteer: release.Present && s.unresolvedSteer != nil && s.unresolvedSteer.request.MessageID == release.Value,
			SessionLost:                   s.state == provider.SessionLost,
			ResultContractMatches:         reflect.DeepEqual(request.ResultContract, s.contract.Contract),
		})
		switch decision.Kind {
		case rules.StartTurnBindingDiffers:
			return s.stale(provider.OperationStartTurn)
		case rules.StartTurnUnavailable, rules.StartTurnReleaseNothingUnresolved, rules.StartTurnReleaseMismatch:
			return adaptererror.NewWithDetail(provider.OperationStartTurn, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, decision.Reason, nil)
		case rules.StartTurnContractChanged:
			return adaptererror.NewWithDetail(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, decision.Reason, nil)
		}
		turnID, err := s.config.NewID("opencode-turn")
		if err != nil {
			return adaptererror.NewWithDetail(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "could not allocate adapter turn ID", err)
		}
		turn = provider.TurnRef{AdapterTurnID: provider.AdapterTurnID(turnID)}
		text := joinParts(request.Input)
		// "--" ends flag parsing so prompt text starting with "-" stays the message.
		args := []string{"run", "--session", string(s.nativeSession), "--format", "json", "--model", s.model.ProviderID + "/" + s.model.ModelID, "--", text}
		proc, err = s.config.Launcher.Start(processruntime.Spec{Path: s.config.ExecutablePath, Args: args, Dir: s.workspace})
		if err != nil {
			return adaptererror.NewWithDetail(provider.OperationStartTurn, provider.ErrorExecutableMissing, provider.RemoteEffectNotSent, "could not start OpenCode", err)
		}
		if decision.Kind == rules.StartTurnAllowedReleasingSteer {
			// Released only once the turn is launched, so a refused start
			// leaves the steer unresolved. Its delivery stays unknown, the
			// revision it proposed is never adopted, and output observed while
			// it was unresolved stays unattributed.
			id := s.unresolvedSteer.nativeMessageID
			if s.released == nil {
				s.released = make(map[string]*releasedSteer)
			}
			s.released[id] = &releasedSteer{nativeMessageID: id}
			s.unresolvedSteer = nil
		}
		s.turn, s.process, s.state = provider.Some(turn), proc, provider.SessionOpen
		return nil
	})
	if err != nil {
		return provider.StartTurnResult{}, err
	}
	if err := proc.Stdin().Close(); err != nil {
		return provider.StartTurnResult{}, s.handleStartStdinCloseFailure(request, proc, err)
	}
	s.emit(provider.TurnStateEvent{Previous: provider.TurnStarting, Current: provider.TurnActive})
	go s.readRun(proc, turn)
	return provider.StartTurnResult{Turn: turn, Assignment: request.Assignment, TargetingStrength: provider.TargetSessionLease, ResultEnforcement: s.contract.Actual, Native: provider.NativeIDs{Session: provider.Some(s.nativeSession)}}, nil
}

// handleStartStdinCloseFailure preserves the launched prompt's uncertainty and
// cleans up only its owned CLI. Cleanup is bounded; ownership remains recorded
// until the background reaper observes actual exit, even after cleanup failure.
func (s *session) handleStartStdinCloseFailure(request provider.StartTurnRequest, proc processruntime.Process, cause error) error {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.deliveryUnknown = true
		if !s.closed {
			s.state = provider.SessionLost
		}
		return nil
	})
	var drains sync.WaitGroup
	drains.Add(2)
	go func() { defer drains.Done(); _, _ = io.Copy(io.Discard, proc.Stdout()) }()
	go func() { defer drains.Done(); _, _ = io.Copy(io.Discard, proc.Stderr()) }()
	killErr := proc.KillTree()
	done := make(chan processruntime.Result, 1)
	go func() {
		result := proc.Wait()
		drains.Wait()
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.process == proc {
				s.process = nil
			}
			return nil
		})
		done <- result
	}()
	if killErr != nil {
		cause = errors.Join(cause, fmt.Errorf("terminate owned OpenCode CLI: %w", killErr), proc.Stdout().Close(), proc.Stderr().Close())
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case result := <-done:
		cause = errors.Join(cause, result.Err)
	case <-timer.C:
		cause = errors.Join(cause, errors.New("owned OpenCode CLI cleanup timed out; reaping remains pending"), proc.Stdout().Close(), proc.Stderr().Close())
	}
	return adaptererror.Delivery(provider.OperationStartTurn, request.RequestID, true, cause)
}

func (s *session) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.NewWithDetail(provider.OperationRespondPermission, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "permission round-trip unsupported", nil)
}
func (s *session) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, adaptererror.NewWithDetail(provider.OperationAnswerQuestion, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "question round-trip unsupported", nil)
}
func (s *session) InterruptTurn(context.Context, provider.InterruptRequest) (provider.InterruptResult, error) {
	return provider.InterruptResult{}, adaptererror.NewWithDetail(provider.OperationInterruptTurn, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, "native interrupt unsupported", nil)
}

func (s *session) Close(ctx context.Context, request provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	var result provider.CloseSessionResult
	var owned processruntime.Process
	err := lockscope.WithMutexLock(&s.mu, func() error {
		decision := rules.DecideClose(rules.CloseFacts{
			SessionMatches:     request.Session == s.ref,
			ActiveTurn:         s.turn.Present,
			ExplicitDetach:     request.Policy == provider.CloseExplicitDetach,
			InterruptThenClose: request.Policy == provider.CloseInterruptThenClose,
		})
		switch decision.Kind {
		case rules.CloseBindingStale:
			return adaptererror.NewWithDetail(provider.OperationCloseSession, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, decision.Reason, nil)
		case rules.CloseActiveWorkRequiresDetach:
			return adaptererror.NewWithDetail(provider.OperationCloseSession, provider.ErrorCapabilityUnsupported, provider.RemoteEffectNotSent, decision.Reason, nil)
		case rules.CloseStopOwnedClient:
			owned = s.process
		}
		s.closed, s.state = true, provider.SessionClosed
		s.broker.Finish(nil)
		result = provider.CloseSessionResult{HandleClosed: true, ExecutionState: provider.ExecutionUnknown, ResourcesReleased: false}
		return nil
	})
	if err != nil || owned == nil {
		return result, err
	}
	result.ResourcesReleased = s.stopOwnedClient(ctx, owned)
	return result, nil
}

// stopOwnedClient terminates only this session's own `opencode run` process
// group and reports whether its root was reaped within the bound. A shared
// OpenCode server is never signalled, so native execution stays unknown.
func (s *session) stopOwnedClient(ctx context.Context, proc processruntime.Process) bool {
	killErr := proc.KillTree()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return false
	}
	reaped := make(chan struct{})
	go func() { _ = proc.Wait(); close(reaped) }()
	timer := time.NewTimer(s.config.StopGrace)
	defer timer.Stop()
	select {
	case <-reaped:
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.process == proc {
			s.process = nil
		}
		s.turn = provider.None[provider.TurnRef]()
		return nil
	})
	return proc.TreeKillSupported()
}

func (s *session) readRun(proc processruntime.Process, turn provider.TurnRef) {
	// killRun terminates this run client's own process group at most once
	// after its observation failed: a descendant may hold stdout open, and a
	// root that exited on its own does not prove its descendants are gone.
	killRun := sync.OnceFunc(func() { _ = proc.KillTree() })
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		s.watchRunRootExit(proc, killRun)
	}()
	go io.Copy(io.Discard, proc.Stderr())
	scanner := bufio.NewScanner(proc.Stdout())
	scanner.Buffer(make([]byte, min(4096, s.config.MaxEventLineBytes)), s.config.MaxEventLineBytes)
	for scanner.Scan() {
		s.normalizeLine(scanner.Bytes(), turn)
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		killRun()
		_, _ = io.Copy(io.Discard, proc.Stdout())
	}
	result := proc.Wait()
	// Wait returned, so the root has exited and the watch has decided; join
	// it so its kill decision precedes the turn's final events.
	<-watchDone
	failed := scanErr != nil || result.Err != nil || result.ExitCode != 0
	if failed {
		killRun()
	}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.process == proc && !s.closed && !failed {
			s.process = nil
			s.turn = provider.None[provider.TurnRef]()
			s.state = provider.SessionIdleObserved
		} else if s.process == proc && !s.closed && failed {
			s.state = provider.SessionLost
		}
		return nil
	})
	if scanErr != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "opencode_jsonl_read", Detail: boundedtext.String("OpenCode JSONL exceeded the configured bound or could not be read", s.config.MaxTextBytes)})
	} else if result.Err != nil || result.ExitCode != 0 {
		s.emit(provider.ProtocolWarningEvent{Code: "opencode_run_exit", Detail: boundedtext.String(fmt.Sprintf("OpenCode client exited with code %d; native session quiescence is unknown", result.ExitCode), s.config.MaxTextBytes)})
	}
	if failed {
		s.emit(provider.SessionStateEvent{Previous: provider.SessionOpen, Current: provider.SessionLost, NativeStatus: "CLI observation failed; native execution state is unknown"})
	} else {
		s.emit(provider.SessionStateEvent{Previous: provider.SessionOpen, Current: provider.SessionIdleObserved, NativeStatus: "CLI client completed; native server quiescence is not proven"})
	}
	// The observed turn has ended: a steer whose delivery is unknown can now
	// be reconciled against the native session.
	s.reconcileUnresolvedSteer()
	s.settleReleasedSteers()
}

// watchRunRootExit waits for the run client's root to exit, independently of
// stdout and stderr: a descendant may hold either pipe, keeping the read or
// process.Wait blocked. It ends only at root exit (Wait cannot return before
// it). A failed root exit ends the observation, so the group is always killed.
// A clean root exit kills nothing: the group could contain a process that
// OpenCode intends to outlive the client (such as a shared service), so the
// observation keeps waiting for the pipes to end.
func (s *session) watchRunRootExit(proc processruntime.Process, killRun func()) {
	rootExited, rootResult := processruntime.ObserveRootExit(proc)
	<-rootExited
	if result := rootResult(); result.Err != nil || result.ExitCode != 0 {
		killRun()
	}
}

func (s *session) emit(body provider.EventBody) {
	_ = lockscope.WithMutexLock(&s.emitMu, func() error {
		eventID, idErr := s.config.NewID("opencode-event")
		if idErr != nil {
			_ = lockscope.WithMutexLock(&s.mu, func() error {
				if !s.closed {
					s.state = provider.SessionLost
				}
				return nil
			})
			s.broker.Finish(adaptererror.NewWithDetail(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, "could not allocate event ID", idErr))
			return nil
		}
		var seq uint64
		var turn provider.Optional[provider.TurnRef]
		var assignment provider.Optional[provider.AssignmentRef]
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			s.seq++
			seq = s.seq
			turn = s.turn
			// Output while a released steer may still run cannot be told apart
			// from the turn's own output, so it belongs to no turn or revision.
			if len(s.released) > 0 {
				turn = provider.None[provider.TurnRef]()
			}
			// While a correction is in flight or its delivery is unknown, which
			// revision the agent is working on is unresolved (provider contract).
			if s.steerPending || s.deliveryUnresolved() || len(s.released) > 0 {
				assignment = provider.None[provider.AssignmentRef]()
			} else {
				assignment = provider.Some(s.assignment)
			}
			return nil
		})
		event, err := provider.NewProviderEvent(provider.EventEnvelope{EventID: provider.EventID(eventID), Session: s.ref, Turn: turn, Assignment: assignment, AdapterSeq: seq, Native: provider.NativeIDs{Session: provider.Some(s.nativeSession)}, ObservedAt: s.config.Now()}, body)
		if err == nil {
			if publishErr := s.broker.Publish(event); publishErr != nil {
				_ = lockscope.WithMutexLock(&s.mu, func() error {
					if !s.closed {
						s.state = provider.SessionLost
					}
					return nil
				})
			}
		}
		return nil
	})
}

func (s *session) stale(op provider.Operation) error {
	return adaptererror.NewWithDetail(op, provider.ErrorStaleBinding, provider.RemoteEffectNotSent, rules.StaleBindingReason, nil)
}
func joinParts(parts []provider.InputPart) string {
	var values []string
	for _, part := range parts {
		if part.Text != "" {
			values = append(values, part.Text)
		} else if part.URI != "" {
			values = append(values, part.URI)
		}
	}
	return strings.Join(values, "\n\n")
}

var _ provider.Session = (*session)(nil)
