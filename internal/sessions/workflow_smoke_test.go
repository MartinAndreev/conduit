package sessions_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/app"
	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"github.com/MartinAndreev/conduit/internal/storage/turso"
)

var workflowTime = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// This is a test composition proof, not a durable application/recovery service.
// Payloads, decisions and state names here are fixture-local contracts only.
type workflowSmokeDriver struct {
	persistence app.Persistence
	journal     events.Repository
	record      sessions.Record
	current     provider.AssignmentRef
	session     provider.Session
	turn        provider.TurnRef
	contract    provider.ResultContractRef
	workspace   string
	nextEvent   uint64
	verified    bool
	submission  provider.SubmissionID
	digest      string
}
type workflowObservation struct {
	Kind     provider.EventKind
	Envelope provider.EventEnvelope
	Body     json.RawMessage
}
type workflowVerification struct {
	Submission provider.SubmissionID
	Assignment provider.AssignmentRef
	Passed     bool
	Reason     string
	SHA256     string
}
type workflowHumanDecision struct {
	Actor              string
	Submission         provider.SubmissionID
	Assignment         provider.AssignmentRef
	VerificationDigest string
	Accepted           bool
}

func TestWorkflowSmoke(t *testing.T) {
	cases := []struct {
		name, artifact                             string
		stale, unattributed, noCandidate, accepted bool
		finalKind                                  string
	}{
		{name: "verified_then_human_accepted", artifact: "GREEN\n", accepted: true, finalKind: "smoke.human_accepted"},
		{name: "unattributed_candidate_not_accepted", artifact: "GREEN\n", unattributed: true, finalKind: "smoke.candidate_rejected"},
		{name: "stale_candidate_not_accepted", artifact: "GREEN\n", stale: true, finalKind: "smoke.candidate_rejected"},
		{name: "agent_claim_does_not_verify_wrong_artifact", artifact: "BLUE\n", finalKind: "smoke.verification_failed"},
		{name: "commentary_and_idle_without_candidate", artifact: "GREEN\n", noCandidate: true, finalKind: "smoke.candidate_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			workspace := t.TempDir()
			databasePath := filepath.Join(t.TempDir(), "smoke.db")
			store, err := turso.Open(ctx, databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if store != nil {
					_ = store.Close()
				}
			}()
			current := provider.AssignmentRef{ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: 1}
			contract := provider.ResultContractRef{Name: "smoke-color", Version: "1", Requested: provider.SchemaNative, Schema: json.RawMessage(`{"type":"object","properties":{"color":{"type":"string","enum":["GREEN"]}},"required":["color"],"additionalProperties":false}`)}
			fake := &workflowFakeSession{ref: provider.SessionRef{AdapterSessionID: "fake-session"}, turn: provider.TurnRef{AdapterTurnID: "fake-turn"}, artifact: tc.artifact, stale: tc.stale, unattributed: tc.unattributed, noCandidate: tc.noCandidate}
			harness := &workflowFakeProvider{session: fake}
			descriptor, err := harness.Inspect(ctx, provider.InspectRequest{})
			if err != nil || descriptor.ProviderName != "workflow-fixture" {
				t.Fatalf("inspect: %+v %v", descriptor, err)
			}
			driver := &workflowSmokeDriver{persistence: store, journal: store.Events(), current: current, contract: contract, workspace: workspace, record: sessions.Record{ID: "logical-session", ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: 1, State: "opening", CreatedAt: workflowTime, UpdatedAt: workflowTime}}
			if err := driver.save(ctx, "smoke.start_intent", current); err != nil {
				t.Fatal(err)
			}
			opened, err := harness.OpenSession(ctx, provider.OpenSessionRequest{RequestID: "open", Assignment: current, Workspace: provider.WorkspaceBinding{WorkspaceID: "workspace", Path: workspace, Writable: true}, ResultContract: contract, PermissionMode: provider.PermissionDeny})
			if err != nil {
				t.Fatal(err)
			}
			driver.session = opened.Session
			if opened.Contract.Actual != provider.SchemaNative {
				t.Fatal("unexpected schema fallback")
			}
			driver.record.AdapterSessionID, driver.record.State = string(opened.Session.Ref().AdapterSessionID), "open"
			if err := driver.save(ctx, "smoke.session_bound", opened.Session.Ref()); err != nil {
				t.Fatal(err)
			}
			stream, err := driver.session.Events(ctx, provider.EventCursor{})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			started, err := driver.session.StartTurn(ctx, provider.StartTurnRequest{RequestID: "start", Session: driver.session.Ref(), Assignment: current, Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "Write BLUE to result.txt; wait for correction."}}, ResultContract: contract})
			if err != nil {
				t.Fatal(err)
			}
			driver.turn = started.Turn
			blue, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
			if err != nil || string(blue) != "BLUE\n" {
				t.Fatalf("initial artifact: %q %v", blue, err)
			}
			commentary, err := stream.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if commentary.Kind() != provider.EventAssistantText {
				t.Fatal("missing commentary handshake")
			}
			if err := driver.observe(ctx, commentary); err != nil {
				t.Fatal(err)
			}
			if driver.verified {
				t.Fatal("commentary JSON created verification")
			}
			corrected := current
			corrected.Revision++
			decision := sessions.DecideCorrection(current, current, current.Revision, corrected)
			if decision.Kind != sessions.CorrectionAllowed {
				t.Fatal(decision)
			}
			if err := driver.append(ctx, "smoke.correction_intent", corrected, "simulated-human"); err != nil {
				t.Fatal(err)
			}
			receipt, err := driver.session.Steer(ctx, provider.SteerRequest{RequestID: "steer", MessageID: "correction", Session: driver.session.Ref(), ExpectedActiveTurn: driver.turn, CurrentAssignment: current, ExpectedRevision: current.Revision, NewAssignment: corrected, Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "Replace BLUE with GREEN in result.txt."}}})
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Disposition != provider.DeliveryTransportAccepted || !receipt.DispatchedFor.Present || receipt.DispatchedFor.Value != corrected {
				t.Fatalf("receipt: %+v", receipt)
			}
			driver.current, driver.record.Revision = corrected, corrected.Revision
			if err := driver.save(ctx, "smoke.correction_transport_accepted", receipt); err != nil {
				t.Fatal(err)
			}
			// A transport receipt cannot prove that the correction was applied.
			stillBlue, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
			if err != nil || string(stillBlue) != "BLUE\n" {
				t.Fatalf("receipt prematurely changed artifact: %q %v", stillBlue, err)
			}
			control, err := stream.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if control.Kind() != provider.EventControlReceipt {
				t.Fatal("missing control receipt event")
			}
			if err := driver.observe(ctx, control); err != nil {
				t.Fatal(err)
			}
			if err := fake.finish(); err != nil {
				t.Fatal(err)
			}
			final, err := stream.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := driver.observe(ctx, final); err != nil {
				t.Fatal(err)
			}
			candidate, hasCandidate := final.Body().(provider.ResultCandidateEvent)
			if !hasCandidate {
				if err := driver.append(ctx, "smoke.candidate_missing", "no proven final candidate", "conduit-test-driver"); err != nil {
					t.Fatal(err)
				}
			} else if err := driver.verify(ctx, final.Envelope, candidate.Candidate); err != nil {
				t.Fatal(err)
			}
			before, err := driver.journal.Replay(ctx, "run", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range before {
				if event.Kind == "smoke.human_accepted" {
					t.Fatal("verification implicitly accepted")
				}
			}
			accepted, err := driver.simulateHumanAcceptance(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if accepted != tc.accepted {
				t.Fatalf("acceptance = %v, want %v", accepted, tc.accepted)
			}
			cleanup, err := driver.session.Close(ctx, provider.CloseSessionRequest{RequestID: "close", Session: driver.session.Ref(), Policy: provider.CloseRequireQuiescent})
			if err != nil {
				t.Fatal(err)
			}
			driver.record.State = "closed"
			if err := driver.save(ctx, "smoke.cleanup", cleanup); err != nil {
				t.Fatal(err)
			}
			if !fake.closed || !cleanup.HandleClosed || !cleanup.ResourcesReleased {
				t.Fatalf("cleanup: %+v", cleanup)
			}
			expected, err := driver.journal.Replay(ctx, "run", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = nil
			reopened, err := turso.Open(ctx, databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			actual, err := reopened.Events().Replay(ctx, "run", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal("reopen changed durable journal")
			}
			record, err := reopened.Sessions().Get(ctx, "logical-session")
			if err != nil {
				t.Fatal(err)
			}
			if record.Revision != 2 || record.Version != 4 || record.AdapterSessionID != "fake-session" || record.State != "closed" {
				t.Fatalf("reopened record: %+v", record)
			}
			assertWorkflowJournal(t, actual, tc.finalKind, tc.accepted, !tc.noCandidate)
		})
	}
}

func (d *workflowSmokeDriver) event(kind string, payload interface{}, provenance string) (events.Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return events.Event{}, err
	}
	d.nextEvent++
	return events.Event{ID: events.EventID(fmt.Sprintf("durable-%d", d.nextEvent)), RunID: "run", Kind: kind, PayloadVersion: 1, Payload: encoded, Provenance: provenance, CausationID: "smoke-driver", ObservedAt: workflowTime.Add(time.Duration(d.nextEvent) * time.Second)}, nil
}
func (d *workflowSmokeDriver) append(ctx context.Context, kind string, payload interface{}, provenance string) error {
	event, err := d.event(kind, payload, provenance)
	if err != nil {
		return err
	}
	_, err = d.journal.Append(ctx, event)
	return err
}
func (d *workflowSmokeDriver) save(ctx context.Context, kind string, payload interface{}) error {
	event, err := d.event(kind, payload, "conduit-test-driver")
	if err != nil {
		return err
	}
	var saved sessions.Record
	err = d.persistence.WithinUnitOfWork(ctx, func(writer sessions.Writer, appender events.Appender) error {
		var saveErr error
		saved, saveErr = writer.Save(ctx, d.record, d.record.Version)
		if saveErr != nil {
			return saveErr
		}
		_, saveErr = appender.Append(ctx, event)
		return saveErr
	})
	if err == nil {
		d.record = saved
	}
	return err
}
func (d *workflowSmokeDriver) observe(ctx context.Context, event provider.ProviderEvent) error {
	// ProviderEvent.body is private; encode its closed union explicitly.
	body, err := json.Marshal(event.Body())
	if err != nil {
		return err
	}
	return d.append(ctx, "smoke.provider_observation", workflowObservation{Kind: event.Kind(), Envelope: event.Envelope, Body: body}, "fake-harness")
}
func (d *workflowSmokeDriver) verify(ctx context.Context, envelope provider.EventEnvelope, candidate provider.ResultCandidate) error {
	if envelope.Session != d.session.Ref() || !envelope.Turn.Present || envelope.Turn.Value != d.turn || !envelope.Assignment.Present || envelope.Assignment.Value != d.current || !candidate.Assignment.Present || candidate.Assignment.Value != d.current || candidate.Source != provider.CandidateAssistantFinal || candidate.Enforcement != provider.SchemaNative || !reflect.DeepEqual(candidate.Contract, d.contract) {
		return d.append(ctx, "smoke.candidate_rejected", "candidate binding/source/contract differs", "conduit-test-driver")
	}
	// The approved fixture contract allows exactly this payload; no model-supplied
	// file path or command is executed by the independent verifier.
	var fields map[string]string
	if err := json.Unmarshal(candidate.Payload, &fields); err != nil || len(fields) != 1 || fields["color"] != "GREEN" {
		return d.append(ctx, "smoke.candidate_rejected", "fixture payload does not satisfy GREEN contract", "conduit-test-driver")
	}
	actual, err := os.ReadFile(filepath.Join(d.workspace, "result.txt"))
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(actual))
	passed := string(actual) == "GREEN\n"
	kind, reason := "smoke.verification_failed", "actual artifact differs from corrected assignment"
	if passed {
		kind, reason = "smoke.verification_passed", "independent filesystem read matches corrected assignment"
	}
	report := workflowVerification{Submission: candidate.SubmissionID, Assignment: d.current, Passed: passed, Reason: reason, SHA256: digest}
	if err := d.append(ctx, kind, report, "independent-filesystem-verifier"); err != nil {
		return err
	}
	d.verified, d.submission, d.digest = passed, candidate.SubmissionID, digest
	return nil
}
func (d *workflowSmokeDriver) simulateHumanAcceptance(ctx context.Context) (bool, error) {
	if !d.verified {
		return false, nil
	}
	actual, err := os.ReadFile(filepath.Join(d.workspace, "result.txt"))
	if err != nil {
		return false, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(actual)) != d.digest {
		return false, fmt.Errorf("fixture artifact changed since verification")
	}
	decision := workflowHumanDecision{Actor: "simulated-human", Submission: d.submission, Assignment: d.current, VerificationDigest: d.digest, Accepted: true}
	err = d.append(ctx, "smoke.human_accepted", decision, "simulated-human")
	return err == nil, err
}
func assertWorkflowJournal(t *testing.T, journal []events.Event, finalKind string, accepted, hasCandidate bool) {
	t.Helper()
	foundFinal, candidateObserved, verifySequence, acceptSequence := false, false, uint64(0), uint64(0)
	for index, event := range journal {
		if event.Sequence != uint64(index+1) || event.PayloadVersion != 1 || !json.Valid(event.Payload) {
			t.Fatalf("invalid durable event: %+v", event)
		}
		if event.Kind == finalKind {
			foundFinal = true
		}
		if event.Kind == "smoke.verification_passed" {
			verifySequence = event.Sequence
		}
		if event.Kind == "smoke.human_accepted" {
			acceptSequence = event.Sequence
			var decision workflowHumanDecision
			if err := json.Unmarshal(event.Payload, &decision); err != nil {
				t.Fatal(err)
			}
			if decision.Actor != "simulated-human" || !decision.Accepted || decision.Assignment.Revision != 2 || decision.Submission != "submission-1" || decision.VerificationDigest == "" {
				t.Fatalf("human decision: %+v", decision)
			}
		}
		if event.Kind == "smoke.provider_observation" {
			var observation workflowObservation
			if err := json.Unmarshal(event.Payload, &observation); err != nil {
				t.Fatal(err)
			}
			if observation.Kind == provider.EventResultCandidate {
				candidateObserved = true
				var body provider.ResultCandidateEvent
				if err := json.Unmarshal(observation.Body, &body); err != nil {
					t.Fatal(err)
				}
				if body.Candidate.SubmissionID != "submission-1" || string(body.Candidate.Payload) != `{"color":"GREEN"}` {
					t.Fatalf("candidate body lost in journal: %+v", body)
				}
			}
		}
	}
	if !foundFinal || candidateObserved != hasCandidate {
		t.Fatalf("missing outcome/candidate: %v %v", foundFinal, candidateObserved)
	}
	if accepted && (verifySequence == 0 || acceptSequence <= verifySequence) {
		t.Fatal("human acceptance did not follow independent verification")
	}
	if !accepted && acceptSequence != 0 {
		t.Fatal("negative case has accepted decision")
	}
	if journal[len(journal)-1].Kind != "smoke.cleanup" {
		t.Fatal("cleanup outcome not distinct and last")
	}
}
