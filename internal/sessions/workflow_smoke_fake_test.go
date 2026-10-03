package sessions_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// These fakes model only the approved smoke handshake, not a native adapter.
type workflowFakeProvider struct{ session *workflowFakeSession }
type workflowFakeSession struct {
	workspace                        string
	assignment                       provider.AssignmentRef
	initial                          provider.AssignmentRef
	contract                         provider.ResultContractRef
	ref                              provider.SessionRef
	turn                             provider.TurnRef
	stream                           *workflowFakeStream
	started, steered, closed         bool
	artifact                         string
	stale, unattributed, noCandidate bool
	sequence                         uint64
}
type workflowFakeStream struct {
	queue  []provider.ProviderEvent
	closed bool
}

var _ provider.Provider = (*workflowFakeProvider)(nil)
var _ provider.Session = (*workflowFakeSession)(nil)
var _ provider.EventStream = (*workflowFakeStream)(nil)

func (f *workflowFakeProvider) Inspect(ctx context.Context, _ provider.InspectRequest) (provider.ProviderDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProviderDescriptor{}, err
	}
	return provider.ProviderDescriptor{ProviderName: "workflow-fixture", Capabilities: f.session.Capabilities()}, nil
}
func (f *workflowFakeProvider) OpenSession(ctx context.Context, r provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.OpenSessionResult{}, err
	}
	s := f.session
	if s.workspace != "" {
		return provider.OpenSessionResult{}, errors.New("fixture already opened")
	}
	if !r.Workspace.Writable || r.PermissionMode != provider.PermissionDeny {
		return provider.OpenSessionResult{}, errors.New("invalid fixture open")
	}
	s.workspace, s.assignment, s.initial, s.contract = r.Workspace.Path, r.Assignment, r.Assignment, r.ResultContract
	return provider.OpenSessionResult{Session: s, Contract: provider.ResultContractNegotiation{Contract: r.ResultContract, Actual: provider.SchemaNative}}, nil
}
func (s *workflowFakeSession) Ref() provider.SessionRef { return s.ref }
func (s *workflowFakeSession) Capabilities() provider.CapabilitySnapshot {
	return provider.CapabilitySnapshot{ProviderName: "workflow-fixture", Items: []provider.CapabilityObservation{
		{Capability: provider.CapabilitySteerActiveTurn, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified, Targeting: provider.TargetNativeTurnCAS},
		{Capability: provider.CapabilityResultNativeSchema, Support: provider.SupportSupported, Evidence: provider.EvidenceFixtureVerified},
	}}
}
func (s *workflowFakeSession) Events(ctx context.Context, _ provider.EventCursor) (provider.EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.stream != nil {
		return nil, errors.New("fixture permits one subscriber")
	}
	s.stream = &workflowFakeStream{}
	return s.stream, nil
}
func (s *workflowFakeSession) ObserveState(ctx context.Context, _ provider.ObserveStateRequest) (provider.SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return provider.SessionSnapshot{}, err
	}
	return provider.SessionSnapshot{Knowledge: provider.SnapshotKnown, SessionState: provider.SessionOpen, ActiveTurn: provider.Some(s.turn)}, nil
}
func (s *workflowFakeSession) StartTurn(ctx context.Context, r provider.StartTurnRequest) (provider.StartTurnResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.StartTurnResult{}, err
	}
	if s.started || s.stream == nil || r.Session != s.ref || r.Assignment != s.assignment || len(r.Input) != 1 || r.Input[0].Text != "Write BLUE to result.txt; wait for correction." {
		return provider.StartTurnResult{}, errors.New("invalid fixture start handshake")
	}
	if err := os.WriteFile(filepath.Join(s.workspace, "result.txt"), []byte("BLUE\n"), 0600); err != nil {
		return provider.StartTurnResult{}, err
	}
	s.started = true
	if err := s.publish(provider.AssistantTextEvent{Phase: provider.AssistantCommentary, Update: provider.TextSegmentUpdate{ItemID: "commentary", SegmentID: "segment", Text: `{"color":"GREEN","claimed_checks":"passed"}`, Mode: provider.UpdateSnapshot, Version: 1}}); err != nil {
		return provider.StartTurnResult{}, err
	}
	return provider.StartTurnResult{Turn: s.turn, Assignment: s.assignment, TargetingStrength: provider.TargetNativeTurnCAS, ResultEnforcement: provider.SchemaNative}, nil
}
func (s *workflowFakeSession) Steer(ctx context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
	if err := ctx.Err(); err != nil {
		return provider.DeliveryReceipt{}, err
	}
	if !s.started || s.steered || r.Session != s.ref || r.ExpectedActiveTurn != s.turn || r.Mode != provider.DeliverActiveTurn || len(r.Parts) != 1 || r.Parts[0].Text != "Replace BLUE with GREEN in result.txt." {
		return provider.DeliveryReceipt{}, errors.New("invalid fixture steer handshake")
	}
	decision := sessions.DecideCorrection(s.assignment, r.CurrentAssignment, r.ExpectedRevision, r.NewAssignment)
	if decision.Kind != sessions.CorrectionAllowed {
		return provider.DeliveryReceipt{}, errors.New(decision.Reason)
	}
	s.assignment, s.steered = r.NewAssignment, true
	receipt := provider.DeliveryReceipt{RequestID: r.RequestID, MessageID: r.MessageID, Disposition: provider.DeliveryTransportAccepted, Receipt: provider.ReceiptNativeKnown, Targeting: provider.TargetNativeTurnCAS, DispatchedFor: provider.Some(s.assignment), NativeReceiptID: provider.Some(provider.NativeReceiptID("fixture-receipt")), ObservedAt: workflowTime}
	if err := s.publish(provider.ControlReceiptEvent{Receipt: receipt}); err != nil {
		return provider.DeliveryReceipt{}, err
	}
	return receipt, nil
}

// finish is the explicit test barrier: receipt persistence precedes final work.
func (s *workflowFakeSession) finish() error {
	if !s.steered {
		return errors.New("finish before correction")
	}
	if err := os.WriteFile(filepath.Join(s.workspace, "result.txt"), []byte(s.artifact), 0600); err != nil {
		return err
	}
	if s.noCandidate {
		return s.publish(provider.SessionStateEvent{Previous: provider.SessionOpen, Current: provider.SessionIdleObserved, NativeStatus: "idle"})
	}
	assignment := s.assignment
	if s.stale {
		assignment = s.initial
	}
	candidate := provider.ResultCandidate{SubmissionID: "submission-1", Assignment: provider.Some(assignment), Source: provider.CandidateAssistantFinal, Contract: s.contract, Enforcement: provider.SchemaNative, Payload: json.RawMessage(`{"color":"GREEN"}`)}
	if s.unattributed {
		candidate.Assignment = provider.None[provider.AssignmentRef]()
	}
	return s.publishFor(assignment, provider.ResultCandidateEvent{Candidate: candidate})
}
func (s *workflowFakeSession) publish(body provider.EventBody) error {
	return s.publishFor(s.assignment, body)
}
func (s *workflowFakeSession) publishFor(assignment provider.AssignmentRef, body provider.EventBody) error {
	s.sequence++
	event, err := provider.NewProviderEvent(provider.EventEnvelope{EventID: provider.EventID(fmt.Sprintf("observation-%d", s.sequence)), Session: s.ref, Turn: provider.Some(s.turn), Assignment: provider.Some(assignment), AdapterSeq: s.sequence, ObservedAt: workflowTime.Add(time.Duration(s.sequence) * time.Second)}, body)
	if err != nil {
		return err
	}
	s.stream.queue = append(s.stream.queue, event)
	return nil
}
func (s *workflowFakeSession) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, errors.New("fixture permission unsupported")
}
func (s *workflowFakeSession) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, errors.New("fixture questions unsupported")
}
func (s *workflowFakeSession) InterruptTurn(context.Context, provider.InterruptRequest) (provider.InterruptResult, error) {
	return provider.InterruptResult{}, errors.New("fixture interrupt unsupported")
}
func (s *workflowFakeSession) Close(ctx context.Context, r provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.CloseSessionResult{}, err
	}
	if r.Session != s.ref || !s.steered {
		return provider.CloseSessionResult{}, errors.New("invalid fixture close")
	}
	s.closed = true
	return provider.CloseSessionResult{HandleClosed: true, ExecutionState: provider.ExecutionTerminal, ResourcesReleased: true}, nil
}
func (s *workflowFakeStream) Receive(ctx context.Context) (provider.ProviderEvent, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProviderEvent{}, err
	}
	if s.closed || len(s.queue) == 0 {
		return provider.ProviderEvent{}, io.EOF
	}
	event := s.queue[0]
	s.queue = s.queue[1:]
	return event, nil
}
func (s *workflowFakeStream) Close() error { s.closed = true; return nil }
