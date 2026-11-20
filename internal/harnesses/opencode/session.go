package opencode

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"io"
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
	// server is the session's own `opencode serve`; endpoint is its address
	// and password, used by every client command of the session.
	server       *ownedServer
	endpoint     ServerEndpoint
	contract     provider.ResultContractNegotiation
	capabilities provider.CapabilitySnapshot
	broker       *eventstream.Broker
	mu           sync.Mutex
	emitMu       sync.Mutex
	assignment   provider.AssignmentRef
	turn         provider.Optional[provider.TurnRef]
	process      processruntime.Process
	// closeOwned is the run client Close claimed to terminate; its finalization
	// then belongs to Close, after its kill.
	closeOwned processruntime.Process
	// runKilled is the run client whose failed observation sent its group
	// kill, with runKillErr its outcome; Close uses it when finalization
	// refuses its own kill.
	runKilled  processruntime.Process
	runKillErr error
	// closeKilled is Close's kill of the run client it claimed; a repeated
	// Close reuses its outcome instead of a kill finalization would refuse.
	closeKilled  *closeKillRecord
	state        provider.SessionState
	closed       bool
	steerPending bool
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
	// releases are the releases of owned client processes the session
	// stopped waiting for before they were confirmed: control commands whose
	// cleanup was pending or whose release was unconfirmed, and run clients
	// whose finalization was not confirmed. Close waits for them within its
	// bound and reports resources released only when rules.DecideRelease
	// confirms every one; unconfirmed ones stay tracked.
	releases []processRelease
	// controlOps counts control operations in flight; controlIdle, when
	// set, closes once it reaches zero.
	controlOps  int
	controlIdle chan struct{}
	// transitioned is a test seam called inside transition after the state
	// mutation and before its events are published; nil in production.
	transitioned func()
	seq          uint64
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

// LaunchConfigHash is the launch hash of the session's own server, as bound
// to evidence. It is not part of provider.Session; the live protected paths
// probe reads it to report the exact launch it recorded.
func (s *session) LaunchConfigHash() string {
	if s.server == nil {
		return ""
	}
	return s.server.configHash
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
		if !s.endpoint.usable() {
			return adaptererror.NewWithDetail(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, errNoServer.Error(), nil)
		}
		turnID, err := s.config.NewID("opencode-turn")
		if err != nil {
			return adaptererror.NewWithDetail(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent, "could not allocate adapter turn ID", err)
		}
		turn = provider.TurnRef{AdapterTurnID: provider.AdapterTurnID(turnID)}
		text := joinParts(request.Input)
		// "--" ends flag parsing so prompt text starting with "-" stays the message.
		args := []string{"--session", string(s.nativeSession), "--format", "json", "--model", s.model.ProviderID + "/" + s.model.ModelID, "--", text}
		proc, err = s.config.Launcher.Start(clientSpec(s.config.ExecutablePath, s.endpoint, s.workspace, "run", args...))
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
// The kill is the last signal: finalization is registered right after it,
// independently of Wait. The client's release is tracked before the client
// is forgotten, so Close accounts for it until it is confirmed.
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
	processruntime.RequestFinalize(proc)
	release := startClientRelease(proc)
	s.trackRelease(release)
	done := make(chan processruntime.Result, 1)
	go func() {
		<-release.Done()
		drains.Wait()
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.process == proc {
				s.process = nil
			}
			return nil
		})
		done <- release.result
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

// Close decides how the session ends, closes the handle, then stops what the
// session owns: the active turn's run client when the decision says so, then
// the session's own server, where every tool executes. Execution is reported
// ended only when the server stop is confirmed; resources are released only
// when both stops are confirmed.
func (s *session) Close(ctx context.Context, request provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	var client processruntime.Process
	var server *ownedServer
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
		}
		if decision.StopsClient() {
			client = s.process
			s.closeOwned = client
		}
		server = s.server
		s.closed, s.state = true, provider.SessionClosed
		s.broker.Finish(nil)
		return nil
	})
	if err != nil {
		return provider.CloseSessionResult{}, err
	}
	clientReleased := true
	if client != nil {
		clientReleased = s.stopOwnedClient(ctx, client)
	}
	result := provider.CloseSessionResult{HandleClosed: true, ExecutionState: provider.ExecutionUnknown}
	if server != nil && server.stop(ctx).Confirmed() {
		result.ExecutionState = provider.ExecutionNone
		result.ResourcesReleased = clientReleased && s.awaitReleases(ctx)
	}
	return result, nil
}

// Control operations of an open session (the steer prompt and the
// reconciliation queries) are registered in controlOps under the state
// mutex, in the same critical section that decides they may start; the
// session refuses new ones once closed (Steer and the reconcile triggers
// check closed). endControlOp unregisters one and keeps the release its
// error carries: a command whose cleanup was still pending when its caller
// stopped waiting, or whose release was not confirmed.
func (s *session) endControlOp(err error) {
	release, retain := trackedRelease(err)
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if retain {
			s.releases = append(s.releases, release)
		}
		s.controlOps--
		if s.controlOps == 0 && s.controlIdle != nil {
			close(s.controlIdle)
			s.controlIdle = nil
		}
		return nil
	})
}

// trackRelease keeps an unconfirmed client release for Close's accounting.
func (s *session) trackRelease(release processRelease) {
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.releases = append(s.releases, release)
		return nil
	})
}

// awaitReleases waits, bounded by ctx and StopGrace, for in-flight control
// operations to end and for every tracked release to finish, including
// releases added while Close waited. It reports true only when no operation
// is in flight and rules.DecideRelease confirms every tracked release;
// confirmed ones are dropped, the others stay tracked so a later Close
// accounts for them again.
func (s *session) awaitReleases(ctx context.Context) bool {
	waitCtx, cancel := context.WithTimeout(ctx, s.config.StopGrace)
	defer cancel()
	var idle chan struct{}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.controlOps > 0 {
			if s.controlIdle == nil {
				s.controlIdle = make(chan struct{})
			}
			idle = s.controlIdle
		}
		return nil
	})
	if idle != nil {
		select {
		case <-idle:
		case <-waitCtx.Done():
		}
	}
	var tracked []processRelease
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		tracked = append(tracked, s.releases...)
		return nil
	})
	confirmed := make(map[processRelease]bool, len(tracked))
	for _, release := range tracked {
		select {
		case <-release.Done():
		case <-waitCtx.Done():
		}
		confirmed[release] = rules.DecideRelease(release.releaseFacts()).Confirmed()
	}
	released := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		kept := s.releases[:0]
		for _, release := range s.releases {
			if !confirmed[release] {
				kept = append(kept, release)
			}
		}
		s.releases = kept
		released = s.controlOps == 0 && len(kept) == 0
		return nil
	})
	return released
}

// stopOwnedClient terminates only this session's own `opencode run` process
// group and reports whether its root was reaped within the bound. The client
// executes no tool; the server's stop decides native execution.
func (s *session) stopOwnedClient(ctx context.Context, proc processruntime.Process) bool {
	killErr := s.closeKill(proc)
	// Release needs the reap (Finalize) and the pipes' end with Wait's own
	// release (Result.ReleaseErr), each bounded by the same grace. A repeated
	// Close awaits the same release again.
	waited := make(chan processruntime.Result, 1)
	go func() { waited <- proc.Wait() }()
	if !processruntime.KillConfirmed(killErr) {
		return false
	}
	finalizeCtx, cancel := context.WithTimeout(ctx, s.config.StopGrace)
	defer cancel()
	if processruntime.Finalize(finalizeCtx, proc) != nil {
		return false
	}
	select {
	case result := <-waited:
		if result.ReleaseErr != nil {
			return false
		}
	case <-finalizeCtx.Done():
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
	killRun := sync.OnceFunc(func() { s.killRunClient(proc) })
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
	// The run's observation sent its last signal and finalizes, unless Close
	// already claimed this client's termination: Close then finalizes after
	// its own kill. The claim is read under the same lock Close takes it in;
	// a Close that comes later finds a finalized client and gets
	// os.ErrProcessDone from its kill. The session state is changed only by
	// the final transition below, together with its event.
	closeOwns := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		closeOwns = s.closeOwned == proc
		return nil
	})
	// The release is bounded by StopGrace so the run's final events and
	// steer reconciliation still follow; past that the process keeps
	// ownership, and an unconfirmed or failed release is reported.
	var releaseErr error
	if !closeOwns {
		releaseCtx, cancel := context.WithTimeout(context.Background(), s.config.StopGrace)
		releaseErr = processruntime.Finalize(releaseCtx, proc)
		cancel()
	}
	if !closeOwns {
		// Wait's own release error fails the release as a failed
		// finalization does.
		releaseErr = errors.Join(result.ReleaseErr, releaseErr)
	}
	if releaseErr != nil {
		// The turn ends below and forgets the client; its release stays
		// tracked so Close does not report resources released before it is
		// confirmed.
		s.trackRelease(startFinishedRelease(proc, result))
		s.emit(provider.ProtocolWarningEvent{Code: "opencode_run_release", Detail: boundedtext.String("OpenCode run client release was not confirmed; its root process may remain unreaped", s.config.MaxTextBytes)})
	}
	if scanErr != nil {
		s.emit(provider.ProtocolWarningEvent{Code: "opencode_jsonl_read", Detail: boundedtext.String("OpenCode JSONL exceeded the configured bound or could not be read", s.config.MaxTextBytes)})
	} else if result.Err != nil || result.ExitCode != 0 {
		s.emit(provider.ProtocolWarningEvent{Code: "opencode_run_exit", Detail: boundedtext.String(fmt.Sprintf("OpenCode client exited with code %d; native session quiescence is unknown", result.ExitCode), s.config.MaxTextBytes)})
	}
	// A lost session stays lost: a run client that ended cleanly after its
	// server died (or after any other loss) must not make it idle again, so
	// StartTurn and Steer stay refused. Only Close leaves the lost state. The
	// state event is published in the same transition, so it is never stale.
	s.transition(func() []provider.EventBody {
		if s.process != proc || s.closed {
			return nil
		}
		previous := s.state
		switch {
		case failed:
			s.state = provider.SessionLost
			return []provider.EventBody{provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "CLI observation failed; native execution state is unknown"}}
		case s.state == provider.SessionLost:
			return []provider.EventBody{provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: "CLI client completed after the session was lost; native execution state is unknown"}}
		default:
			s.process = nil
			s.turn = provider.None[provider.TurnRef]()
			s.state = provider.SessionIdleObserved
			return []provider.EventBody{provider.SessionStateEvent{Previous: previous, Current: provider.SessionIdleObserved, NativeStatus: "CLI client completed; native server quiescence is not proven"}}
		}
	})
	// The observed turn has ended: a steer whose delivery is unknown can now
	// be reconciled against the native session.
	s.reconcileUnresolvedSteer()
	s.settleReleasedSteers()
}

// watchServer observes the root exit of the session's own server. An exit
// while the session is open loses the session: the server's process group is
// terminated, the active run client (a client of a dead server) is
// terminated so its observation ends instead of waiting, and the loss is
// published. An exit caused by Close publishes nothing.
func (s *session) watchServer(server *ownedServer) {
	<-server.rootExited
	var lost bool
	var client processruntime.Process
	s.transition(func() []provider.EventBody {
		decision := rules.DecideServerExit(rules.ServerExitFacts{Closed: s.closed})
		if decision.Kind != rules.ServerExitSessionLost {
			return nil
		}
		lost = true
		previous := s.state
		s.state = provider.SessionLost
		client = s.process
		return []provider.EventBody{
			provider.ProtocolWarningEvent{Code: "opencode_server_exit", Detail: boundedtext.String(decision.Reason, s.config.MaxTextBytes)},
			provider.SessionStateEvent{Previous: previous, Current: provider.SessionLost, NativeStatus: decision.Reason},
		}
	})
	if !lost {
		return
	}
	server.cleanup.BeginCleanup()
	if client != nil {
		_ = client.KillTree()
	}
}

// killRunClient sends a failed run's single group kill and records its
// outcome for Close. The kill is the run's last signal: unless Close already
// claimed the client's termination (Close then finalizes after its own
// kill), finalization is registered at once, independently of Wait, which a
// descendant outside the group can block.
func (s *session) killRunClient(proc processruntime.Process) {
	err := proc.KillTree()
	claimed := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		s.runKilled = proc
		s.runKillErr = err
		claimed = s.closeOwned == proc
		return nil
	})
	if !claimed {
		processruntime.RequestFinalize(proc)
	}
}

// closeKillRecord is Close's single group kill of one run client; err, its
// termination evidence, is read after done closed.
type closeKillRecord struct {
	proc processruntime.Process
	done chan struct{}
	err  error
}

// closeKill sends Close's group kill of proc once and registers
// finalization right after it, also when the kill failed, independently of
// Wait, which a descendant outside the group can block. A repeated Close of
// the same client sends nothing (finalization would refuse it) and reuses
// that kill's outcome.
func (s *session) closeKill(proc processruntime.Process) error {
	var record *closeKillRecord
	first := false
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.closeKilled == nil || s.closeKilled.proc != proc {
			s.closeKilled = &closeKillRecord{proc: proc, done: make(chan struct{})}
			first = true
		}
		record = s.closeKilled
		return nil
	})
	if first {
		record.err = s.closeKillOutcome(proc, proc.KillTree())
		processruntime.RequestFinalize(proc)
		close(record.done)
	}
	<-record.done
	return record.err
}

// closeKillOutcome is the termination evidence of Close's kill. When
// finalization refused it (the run's failed observation ended signalling
// first), that kill's own outcome stands; a refusal without one confirms
// nothing.
func (s *session) closeKillOutcome(proc processruntime.Process, killErr error) error {
	if !errors.Is(killErr, processruntime.ErrSignalsFinalized) {
		return killErr
	}
	_ = lockscope.WithMutexLock(&s.mu, func() error {
		if s.runKilled == proc {
			killErr = s.runKillErr
		}
		return nil
	})
	return killErr
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
		s.publishLocked(body)
		return nil
	})
}

// transition orders a session state transition together with the events it
// publishes: it holds the publication order (emitMu) across the mutation,
// made under the state mutex by change, and the publication of the bodies
// change returns, so no other transition's event can be published between
// them. Publishing is a nonblocking broker send, never harness I/O, and the
// state mutex is released before it.
func (s *session) transition(change func() []provider.EventBody) {
	_ = lockscope.WithMutexLock(&s.emitMu, func() error {
		var bodies []provider.EventBody
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			bodies = change()
			return nil
		})
		if s.transitioned != nil {
			s.transitioned()
		}
		for _, body := range bodies {
			s.publishLocked(body)
		}
		return nil
	})
}

// publishLocked publishes one event; emitMu must be held.
func (s *session) publishLocked(body provider.EventBody) {
	eventID, idErr := s.config.NewID("opencode-event")
	if idErr != nil {
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if !s.closed {
				s.state = provider.SessionLost
			}
			return nil
		})
		s.broker.Finish(adaptererror.NewWithDetail(provider.OperationReceiveEvent, provider.ErrorProtocolViolation, provider.RemoteEffectUnknown, "could not allocate event ID", idErr))
		return
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
