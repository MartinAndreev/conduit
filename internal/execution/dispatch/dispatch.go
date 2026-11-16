// Package dispatch owns the DispatchStartTurn and DispatchSteer procedures
// of spec 003: a transaction records the intent (with the context manifest
// and payload when memory was composed), a second transaction claims it, the
// harness is called outside any transaction, and a third transaction records
// the outcome. An effect whose outcome is unknown is never repeated. The
// procedures read and write through the consumer-side Store port.
//
// This is a library slice without recovery: a session with an open intent
// (recorded, dispatching, unknown, diverged) accepts no new intent and no
// binding change until a future recovery releases it.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// DefaultSettlementTimeout is the attempt budget of the outcome transaction
// (initial value, Martin 2026-11-16). It bounds the attempts, not the
// non-cancellable COMMIT, ROLLBACK or connection close.
const DefaultSettlementTimeout = 10 * time.Second

// Service exposes the dispatch procedures.
type Service struct {
	Store Store
	// Now is the dispatcher's clock; nil means time.Now.
	Now func() time.Time
	// SettlementTimeout is the outcome transaction's attempt budget; zero
	// means DefaultSettlementTimeout.
	SettlementTimeout time.Duration
}

type ResultKind string

const (
	// ResultDispatched: this call sent the command; Outcome is recorded.
	ResultDispatched ResultKind = "dispatched"
	// ResultRecordedOutcome: an earlier call recorded Outcome; nothing was
	// sent.
	ResultRecordedOutcome ResultKind = "recorded_outcome"
	// ResultDeliveryUnknown: an earlier call claimed the intent without a
	// recorded outcome; the send may have happened and is not repeated.
	ResultDeliveryUnknown ResultKind = "delivery_unknown"
	// ResultInvalidated: the intent was invalidated before sending.
	ResultInvalidated ResultKind = "invalidated"
	// ResultBindingChanged: the session's binding changed before the claim.
	ResultBindingChanged ResultKind = "binding_changed"
	// ResultManifestRefused: the composition cannot be dispatched; Manifest
	// says why. Nothing was written.
	ResultManifestRefused ResultKind = "manifest_refused"
	// ResultIntentRefused: the command was not recorded; Intent says why.
	ResultIntentRefused ResultKind = "intent_refused"
)

// Result is a procedure's outcome. Outcome is set for ResultDispatched and
// ResultRecordedOutcome; StartResult or Receipt only for ResultDispatched.
type Result struct {
	Kind        ResultKind
	Reason      string
	IntentState execution.IntentState
	Outcome     execution.OutcomeRecord
	Manifest    execution.ManifestEnvelopeDecision
	Intent      execution.IntentDecision
	StartResult provider.StartTurnResult
	Receipt     provider.DeliveryReceipt
}

var (
	// ErrSessionBindingDiverged reports that the adapter proved a Steer was
	// applied with advancement but the session no longer had the intent's
	// binding, so the revision could not move. The intent is diverged and
	// blocks the session; the outcome is recorded. A repetition returns it
	// again.
	ErrSessionBindingDiverged = errors.New("dispatch: steer was applied but the session binding diverged")
	// ErrInvalidIntentFacts reports facts a rule found inconsistent, such as
	// a stored intent that breaks the claim invariants.
	ErrInvalidIntentFacts = errors.New("dispatch: invalid intent facts")
	// ErrSettlementFailed reports that recording the outcome was not
	// confirmed. The harness is not called again. With Commit not_committed
	// the intent stays dispatching; with Commit unknown the outcome may
	// already be recorded, and a repetition reads what is stored.
	ErrSettlementFailed = errors.New("dispatch: outcome could not be recorded")
	// ErrObservationNotRecorded reports that the harness answered but the
	// observation could not even be classified for recording (for example
	// the dispatcher's clock gave no time). Nothing was settled; the intent
	// stays dispatching, and ObservationError carries the observation.
	ErrObservationNotRecorded = errors.New("dispatch: harness observation could not be recorded")
	// ErrInvalidClock reports a dispatcher clock that gave no time before
	// anything was sent.
	ErrInvalidClock = errors.New("dispatch: the dispatcher clock gave no time")
)

// SettlementError carries the classified outcome whose recording was not
// confirmed, how far its last COMMIT got, and the cause. errors.Is matches
// ErrSettlementFailed and the cause.
type SettlementError struct {
	Outcome execution.OutcomeRecord
	Commit  CommitStatus
	Err     error
}

func (e *SettlementError) Error() string {
	return fmt.Sprintf("%v (commit %s): %s/%s: %v", ErrSettlementFailed, e.Commit, e.Outcome.Outcome, e.Outcome.Classification, e.Err)
}

func (e *SettlementError) Unwrap() []error { return []error{ErrSettlementFailed, e.Err} }

// ObservationError carries a harness answer that was received after the
// send but could not be classified, and why. Nothing was recorded for it.
type ObservationError struct {
	StartResult provider.StartTurnResult
	Receipt     provider.DeliveryReceipt
	// HarnessErr is the error the harness call returned, if any.
	HarnessErr error
	Reason     string
}

func (e *ObservationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrObservationNotRecorded, e.Reason)
}

func (e *ObservationError) Unwrap() []error {
	return []error{ErrObservationNotRecorded, ErrInvalidIntentFacts}
}

// settlementFailure builds the public error, with the commit status the
// store reported or unknown when it reported none.
func settlementFailure(record execution.OutcomeRecord, err error) error {
	commit := CommitUnknown
	var failure *SettleFailure
	if errors.As(err, &failure) {
		commit = failure.Commit
	}
	return &SettlementError{Outcome: record, Commit: commit, Err: err}
}

// DispatchStartTurn records, claims and sends one StartTurn and records its
// outcome.
func (s Service) DispatchStartTurn(ctx context.Context, session provider.Session, command execution.Command) (Result, error) {
	if command.Kind != execution.IntentStartTurn {
		return Result{}, fmt.Errorf("%w: not a start turn", execution.ErrInvalidCommand)
	}
	return s.dispatch(ctx, command, func(ctx context.Context, envelope execution.Envelope, input []provider.InputPart) observation {
		result, err := session.StartTurn(ctx, provider.StartTurnRequest{
			RequestID:      provider.RequestID(envelope.IntentID),
			Session:        envelope.Session,
			Assignment:     envelope.Assignment,
			Input:          input,
			ResultContract: command.ResultContract,
		})
		return observation{startResult: result, err: err}
	})
}

// DispatchSteer records, claims and sends one Steer and records its outcome.
// The revision moves only for a correlated receipt with native receipt
// knowledge for the new assignment. ReleaseUnresolvedSteer is never sent.
func (s Service) DispatchSteer(ctx context.Context, session provider.Session, command execution.Command) (Result, error) {
	if command.Kind != execution.IntentSteer {
		return Result{}, fmt.Errorf("%w: not a steer", execution.ErrInvalidCommand)
	}
	return s.dispatch(ctx, command, func(ctx context.Context, envelope execution.Envelope, input []provider.InputPart) observation {
		receipt, err := session.Steer(ctx, provider.SteerRequest{
			RequestID:          provider.RequestID(envelope.IntentID),
			MessageID:          provider.MessageID(envelope.IntentID),
			Session:            envelope.Session,
			ExpectedActiveTurn: envelope.ExpectedTurn,
			CurrentAssignment:  envelope.Assignment,
			ExpectedRevision:   envelope.Assignment.Revision,
			NewAssignment:      newAssignment(envelope),
			Mode:               envelope.Mode,
			Parts:              input,
		})
		return observation{receipt: receipt, err: err}
	})
}

func newAssignment(e execution.Envelope) provider.AssignmentRef {
	next := e.Assignment
	next.Revision = e.TargetRevision
	return next
}

// observation is one raw harness answer.
type observation struct {
	startResult provider.StartTurnResult
	receipt     provider.DeliveryReceipt
	err         error
}

type send func(context.Context, execution.Envelope, []provider.InputPart) observation

// dispatch is the common flow: validate, record, claim, send, classify,
// settle. Only the harness call differs.
func (s Service) dispatch(ctx context.Context, command execution.Command, call send) (Result, error) {
	if err := execution.ValidateCommand(command); err != nil {
		return Result{}, err
	}
	manifest, refused := prepareManifest(command)
	if refused != nil {
		return *refused, nil
	}
	requestHash := execution.RequestHash(command)
	recorded, err := s.record(ctx, command, requestHash, manifest)
	if err != nil || recorded.Kind != "" {
		return recorded, err
	}
	envelope, claimed, err := s.claim(ctx, command.IntentID)
	if err != nil || claimed.Kind != "" {
		return claimed, err
	}
	answer := call(ctx, envelope, sendInput(command, envelope))
	return s.settle(ctx, command, envelope, answer)
}

// prepareManifest normalizes a fresh composition, or returns the refusal.
func prepareManifest(command execution.Command) (*execution.NewManifest, *Result) {
	if command.Memory.Kind != execution.MemoryInputComposed {
		return nil, nil
	}
	target := command.Current
	target.Revision = command.TargetRevision()
	decision := execution.DecideManifestEnvelope(execution.ManifestFacts{
		Assignment:      target,
		Request:         command.Memory.Request,
		Composition:     command.Memory.Composition,
		MaxPayloadBytes: execution.MaxManifestPayloadBytes,
	})
	if decision.Kind != execution.ManifestEnvelope {
		return nil, &Result{Kind: ResultManifestRefused, Reason: decision.Reason, Manifest: decision}
	}
	return &decision.Manifest, nil
}

// record runs the record transaction: an existing ID is judged on identity
// alone; a new one on the session, the target turn, open intents and, for
// composed memory, the commit recheck. It returns a non-empty Result only
// when the command stops here.
func (s Service) record(ctx context.Context, command execution.Command, requestHash string, manifest *execution.NewManifest) (Result, error) {
	var stopped Result
	err := s.Store.WithinIntentRecord(ctx, func(tx RecordTransaction) error {
		facts := execution.IntentFacts{Command: command, RequestHash: requestHash}
		if manifest != nil {
			facts.ManifestHash = manifest.ManifestHash
		}
		existing, found, err := tx.ReadExistingIntent(ctx, command.IntentID)
		if err != nil {
			return err
		}
		facts.ExistingFound, facts.Existing = found, existing
		if !found {
			if err := readNewIntentFacts(ctx, tx, command, manifest, &facts); err != nil {
				return err
			}
		}
		decision := execution.DecideDispatchIntent(facts)
		switch decision.Kind {
		case execution.IntentExisting:
			return nil
		case execution.IntentRecord:
			return s.recordIntent(ctx, tx, command, requestHash, manifest)
		case execution.IntentInvalidFacts:
			return fmt.Errorf("%w: %s", ErrInvalidIntentFacts, decision.Reason)
		default:
			stopped = Result{Kind: ResultIntentRefused, Reason: decision.Reason, Intent: decision}
			return nil
		}
	})
	if err != nil {
		return Result{}, fmt.Errorf("record dispatch intent: %w", err)
	}
	return stopped, nil
}

// readNewIntentFacts reads the session facts and, for composed memory, the
// records the composition relied on.
func readNewIntentFacts(ctx context.Context, tx RecordTransaction, command execution.Command, manifest *execution.NewManifest, facts *execution.IntentFacts) error {
	stored, err := tx.ReadNewIntentFacts(ctx, command.SessionID)
	if err != nil {
		return err
	}
	facts.Stored = stored
	if manifest == nil || len(manifest.Expected) == 0 {
		return nil
	}
	ids := make([]string, len(manifest.Expected))
	for i, expected := range manifest.Expected {
		ids[i] = expected.RecordID
	}
	current, err := tx.ReadRecordStates(ctx, command.Memory.Request.ProjectID, ids)
	if err != nil {
		return err
	}
	facts.Recheck.Expected, facts.Recheck.Current = manifest.Expected, current
	return nil
}

func (s Service) recordIntent(ctx context.Context, tx RecordTransaction, command execution.Command, requestHash string, manifest *execution.NewManifest) error {
	now := s.now()
	if now.IsZero() {
		return ErrInvalidClock
	}
	intent := NewIntent{
		ID:             command.IntentID,
		Kind:           command.Kind,
		SessionID:      command.SessionID,
		Session:        command.Session,
		Assignment:     command.Current,
		TargetRevision: command.TargetRevision(),
		ExpectedTurn:   command.ExpectedTurn,
		Mode:           command.Mode,
		RequestHash:    requestHash,
		Memory:         command.Presence(),
		CreatedAt:      now,
	}
	payload := execution.IntentRecordedPayload{
		IntentID:         command.IntentID,
		Kind:             command.Kind,
		SessionID:        string(command.SessionID),
		AdapterSessionID: string(command.Session.AdapterSessionID),
		AssignmentID:     string(command.Current.AssignmentID),
		ExpectedRevision: command.Current.Revision,
		TargetRevision:   intent.TargetRevision,
		Memory:           intent.Memory,
		RequestHash:      requestHash,
	}
	if manifest != nil {
		payload.ManifestHash = manifest.ManifestHash
	}
	event, err := execution.IntentEvent(command.IntentID, string(command.Current.RunID), execution.EventIntentRecorded, payload, now)
	if err != nil {
		return err
	}
	return tx.RecordIntent(ctx, intent, manifest, event)
}

// claim runs the claim transaction. It returns the envelope to send from,
// or a non-empty Result (and for a diverged intent its error) when nothing
// is sent.
func (s Service) claim(ctx context.Context, id execution.IntentID) (execution.Envelope, Result, error) {
	var envelope execution.Envelope
	var stopped Result
	err := s.Store.WithinIntentClaim(ctx, func(tx ClaimTransaction) error {
		read, found, err := tx.ReadEnvelope(ctx, id)
		if err != nil {
			return err
		}
		decision := execution.DecideDispatchClaim(execution.ClaimFacts{Found: found, Envelope: read})
		switch decision.Kind {
		case execution.ClaimSend:
			now := s.now()
			if now.IsZero() {
				return ErrInvalidClock
			}
			event, err := execution.IntentEvent(id, string(read.Assignment.RunID), execution.EventClaimed, execution.ClaimedPayload{IntentID: id}, now)
			if err != nil {
				return err
			}
			if err := tx.ClaimIntent(ctx, id, read.Version, now, event); err != nil {
				return err
			}
			envelope = read
			envelope.State, envelope.Version = execution.StateDispatching, read.Version+1
			return nil
		case execution.ClaimRecordedOutcome:
			stopped = Result{Kind: ResultRecordedOutcome, Reason: decision.Reason, IntentState: read.State, Outcome: read.Outcome}
		case execution.ClaimDeliveryUnknown:
			stopped = Result{Kind: ResultDeliveryUnknown, Reason: decision.Reason, IntentState: read.State}
		case execution.ClaimInvalidated:
			stopped = Result{Kind: ResultInvalidated, Reason: decision.Reason, IntentState: read.State}
		case execution.ClaimBindingChanged:
			stopped = Result{Kind: ResultBindingChanged, Reason: decision.Reason, IntentState: read.State}
		default:
			return fmt.Errorf("%w: %s", ErrInvalidIntentFacts, decision.Reason)
		}
		return nil
	})
	if err != nil {
		return execution.Envelope{}, Result{}, fmt.Errorf("claim dispatch intent: %w", err)
	}
	if stopped.IntentState == execution.StateDiverged {
		return execution.Envelope{}, stopped, ErrSessionBindingDiverged
	}
	return envelope, stopped, nil
}

// sendInput is the command's parts followed by the memory part rebuilt from
// the claimed payload; a composition's own part kind, URI or media type is
// never sent.
func sendInput(command execution.Command, envelope execution.Envelope) []provider.InputPart {
	input := append([]provider.InputPart(nil), command.Parts...)
	if envelope.ManifestFound && envelope.Composition == execution.CompositionComposed {
		input = append(input, provider.InputPart{Kind: provider.InputContext, Text: envelope.PayloadText})
	}
	return input
}

// settle classifies the harness answer and records it in its own bounded
// transaction. The settlement context ignores the caller's cancellation, so
// an observed effect is recorded even when the caller stopped waiting. A
// settlement failure never calls the harness again; it leaves the intent
// dispatching unless the last COMMIT's status is unknown, in which case the
// outcome may already be recorded (SettlementError.Commit).
func (s Service) settle(ctx context.Context, command execution.Command, envelope execution.Envelope, answer observation) (Result, error) {
	observedAt := s.now()
	decision := execution.DecideDispatchOutcome(execution.OutcomeFacts{
		Command:     command,
		StartResult: answer.startResult,
		Receipt:     answer.receipt,
		Err:         answer.err,
		ObservedAt:  observedAt,
	})
	if decision.Kind == execution.OutcomeInvalidFacts {
		// After the effect: keep the observation instead of dropping it.
		return Result{}, &ObservationError{StartResult: answer.startResult, Receipt: answer.receipt, HarnessErr: answer.err, Reason: decision.Reason}
	}
	settlement, err := newSettlement(envelope, decision.Record, observedAt)
	if err != nil {
		return Result{}, &SettlementError{Outcome: decision.Record, Commit: CommitNotCommitted, Err: err}
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.settlementTimeout())
	defer cancel()
	settled, err := s.Store.SettleIntent(settleCtx, settlement)
	if err != nil {
		var failure *SettleFailure
		if !errors.As(err, &failure) || failure.Commit != CommitCommitted {
			return Result{}, settlementFailure(decision.Record, err)
		}
		// The outcome is durable and only the release failed: report what
		// was recorded, which may be a divergence, not the request.
		failed := &SettlementError{Outcome: settled.Record, Commit: CommitCommitted, Err: err}
		if settled.Record.Divergence != "" {
			return Result{}, errors.Join(failed, ErrSessionBindingDiverged)
		}
		return Result{}, failed
	}
	result := Result{
		Kind:        ResultDispatched,
		Reason:      decision.Reason,
		IntentState: execution.IntentState(settled.Record.Outcome),
		Outcome:     settled.Record,
		StartResult: answer.startResult,
		Receipt:     answer.receipt,
	}
	if settled.Record.Divergence != "" {
		result.IntentState = execution.StateDiverged
		return result, ErrSessionBindingDiverged
	}
	return result, nil
}

// newSettlement builds the settlement and both of its events once, so every
// attempt appends the same content.
func newSettlement(envelope execution.Envelope, record execution.OutcomeRecord, at time.Time) (Settlement, error) {
	runID := string(envelope.Assignment.RunID)
	payload := execution.SettledPayload{
		IntentID:       envelope.IntentID,
		State:          execution.IntentState(record.Outcome),
		Outcome:        record.Outcome,
		Classification: record.Classification,
		Observation:    record.Observation,
		Advanced:       record.Advanced,
	}
	event, err := execution.IntentEvent(envelope.IntentID, runID, execution.EventSettled, payload, at)
	if err != nil {
		return Settlement{}, err
	}
	payload.State, payload.Advanced = execution.StateDiverged, false
	diverged, err := execution.IntentEvent(envelope.IntentID, runID, execution.EventSettled, payload, at)
	if err != nil {
		return Settlement{}, err
	}
	return Settlement{
		IntentID:        envelope.IntentID,
		ExpectedVersion: envelope.Version,
		SessionID:       envelope.SessionID,
		Session:         envelope.Session,
		Assignment:      envelope.Assignment,
		TargetRevision:  envelope.TargetRevision,
		Record:          record,
		SettledAt:       at,
		Event:           event,
		DivergedEvent:   diverged,
	}, nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) settlementTimeout() time.Duration {
	if s.SettlementTimeout > 0 {
		return s.SettlementTimeout
	}
	return DefaultSettlementTimeout
}
