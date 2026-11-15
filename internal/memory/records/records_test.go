package records

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// fakeSecret has the shape of a vendor token the redaction policy masks.
const fakeSecret = "sk-test-0123456789abcdefghijklmnop"

func humanCreate() CreateRequest {
	return CreateRequest{
		Producer: memory.ProducerHuman,
		Actor:    memory.ActorHuman,
		Author:   "martin",
		Category: memory.CategoryConvention,
		Content:  "Миграциите са адитивни; api_key = " + fakeSecret,
		Scope:    memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityProject, Origin: memory.OriginHuman},
		Sources: []memory.RecordSource{
			{Kind: memory.SourceFile, Path: "internal/storage/migration.go", BlobHash: "f6783ab4cb25f4cfec064862e7d2f5a8501117a0"},
			{Kind: memory.SourceSpec, Ref: "specs/012 token=" + fakeSecret},
		},
		Now: writeTime,
	}
}

func agentProposal() CreateRequest {
	return CreateRequest{
		Producer: memory.ProducerAgentProposal,
		Actor:    memory.ActorSystem,
		Author:   "codex",
		Category: memory.CategoryGotcha,
		Content:  "Turso keeps a failed COMMIT open",
		Scope:    memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityAttempt, Origin: memory.OriginAttempt, RunID: "run", AttemptID: "a1", BaseCommit: "commit"},
		Now:      writeTime,
	}
}

func TestCreateHumanRecordIsApprovedByItsAuthor(t *testing.T) {
	store := newFakeStore()
	got, err := service(store).CreateRecord(context.Background(), humanCreate())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != memory.CreationAllowed || got.Status != memory.StatusActive || got.RecordID != "record-1" || got.VersionID != "version-2" {
		t.Fatalf("result = %+v", got)
	}
	if !slices.Equal(got.Findings, []redaction.Finding{{Kind: "secret_assignment", Count: 2}}) {
		t.Fatalf("findings = %+v", got.Findings)
	}
	wantWrites(t, store, "insert record record-1", "review version-2 approve by human")
	created := store.created[0]
	if strings.Contains(created.Version.Content, fakeSecret) || strings.Contains(created.Sources[1].Ref, fakeSecret) {
		t.Fatalf("secret persisted: %+v", created)
	}
	if created.Version.ContentHash != memory.ContentHash(created.Version.Content) || got.ContentHash != created.Version.ContentHash {
		t.Fatalf("content hash = %s", created.Version.ContentHash)
	}
	wantRecord := memory.Record{ID: "record-1", ProjectID: "p1", Category: memory.CategoryConvention, Status: memory.StatusActive, CurrentVersionID: "version-2", Version: 1, CreatedAt: writeTime.UTC()}
	if created.Record != wantRecord || created.CreatedAt != writeTime.UTC() {
		t.Fatalf("record = %+v at %v", created.Record, created.CreatedAt)
	}
	if created.Version.Trust != memory.TrustHumanApproved || created.Version.Author != "martin" || created.Version.RedactionPolicyVersion != redaction.PolicyVersion || created.Version.Scope != humanCreate().Scope {
		t.Fatalf("version = %+v", created.Version)
	}
	wantReview := Review{ID: "review-3", VersionID: "version-2", ContentHash: created.Version.ContentHash, Decision: memory.ReviewApprove, Actor: memory.ActorHuman, Reviewer: "martin", ReviewedAt: writeTime.UTC()}
	if store.reviews[0] != wantReview {
		t.Fatalf("review = %+v", store.reviews[0])
	}
}

func TestCreateAgentProposalAndObservation(t *testing.T) {
	store := newFakeStore()
	got, err := service(store).CreateRecord(context.Background(), agentProposal())
	if err != nil || got.Kind != memory.CreationAllowed || got.Status != memory.StatusProposed || got.Findings != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantWrites(t, store, "insert record record-1")
	if store.created[0].Version.Trust != memory.TrustAgentClaim || store.created[0].Sources != nil {
		t.Fatalf("version = %+v", store.created[0])
	}

	observation := agentProposal()
	observation.Producer = memory.ProducerConduitObservation
	store = newFakeStore()
	got, err = service(store).CreateRecord(context.Background(), observation)
	if err != nil || got.Status != memory.StatusProposed || store.created[0].Version.Trust != memory.TrustConduitObserved || len(store.reviews) != 0 {
		t.Fatalf("result %+v, error %v, store %+v", got, err, store)
	}
}

// Trust is never taken from the caller: there is no trust field, and an
// agent cannot pose as a human producer.
func TestCreateRefusalWritesNothing(t *testing.T) {
	for name, change := range map[string]func(*CreateRequest){
		"agent claims human":   func(r *CreateRequest) { r.Actor = memory.ActorAgent },
		"unknown category":     func(r *CreateRequest) { r.Category = "architecture_decision" },
		"redacted to too long": func(r *CreateRequest) { r.Content = strings.Repeat("Ж", memory.MaxStoredContentBytes/2) + "x" },
	} {
		t.Run(name, func(t *testing.T) {
			request := humanCreate()
			change(&request)
			store := newFakeStore()
			got, err := service(store).CreateRecord(context.Background(), request)
			if err != nil || got.Kind == memory.CreationAllowed || got.RecordID != "" || got.Status != "" {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantNoWrites(t, store)
		})
	}
}

func TestCreateWithDefaultIDs(t *testing.T) {
	store := newFakeStore()
	got, err := Service{Store: store}.CreateRecord(context.Background(), humanCreate())
	if err != nil || !strings.HasPrefix(got.RecordID, "record-") || !strings.HasPrefix(got.VersionID, "version-") || !strings.HasPrefix(store.reviews[0].ID, "review-") {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

func TestInvalidRequestsTouchNoStore(t *testing.T) {
	store := newFakeStore(storedRecord(memory.StatusProposed))
	s := service(store)
	ctx := context.Background()
	reason := ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "x"}
	calls := map[string]func() error{
		"create without time": func() error { _, err := s.CreateRecord(ctx, CreateRequest{}); return err },
		"approve without reviewer": func() error {
			_, err := s.Approve(ctx, ApproveRequest{Now: writeTime})
			return err
		},
		"approve without time":     func() error { _, err := s.Approve(ctx, ApproveRequest{Reviewer: "martin"}); return err },
		"reject without time":      func() error { _, err := s.Reject(ctx, reason); return err },
		"archive without time":     func() error { _, err := s.Archive(ctx, reason); return err },
		"mark without time":        func() error { _, err := s.MarkNeedsVerification(ctx, VerificationRequest{}); return err },
		"reactivate without time":  func() error { _, err := s.Reactivate(ctx, ReactivateRequest{Reviewer: "martin"}); return err },
		"supersede without author": func() error { _, err := s.Supersede(ctx, SupersedeRequest{Now: writeTime}); return err },
		"opinion without time":     func() error { _, err := s.AddOpinion(ctx, OpinionRequest{}); return err },
		"merge without author":     func() error { _, err := s.Merge(ctx, MergeRequest{Now: writeTime}); return err },
		"archive within without time": func() error {
			_, err := s.ArchiveWithin(ctx, fakeTransaction{store}, reason)
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, limit := range []int{0, -1} {
		if _, err := s.ArchiveExpiredProposals(ctx, SweepRequest{Now: writeTime, Limit: limit}); !errors.Is(err, ErrInvalidSweep) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	if _, err := s.ArchiveExpiredProposals(ctx, SweepRequest{Limit: 1}); !errors.Is(err, ErrInvalidSweep) {
		t.Fatalf("sweep without time: %v", err)
	}
	if store.transactions != 0 {
		t.Fatalf("transactions = %d", store.transactions)
	}
}

func TestApprove(t *testing.T) {
	request := ApproveRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", VersionID: "v1", ContentHash: currentHash, Now: writeTime}
	store := newFakeStore(storedRecord(memory.StatusProposed))
	got, err := service(store).Approve(context.Background(), request)
	if err != nil || got.Kind != memory.TransitionAllowed || got.RecordVersion != 4 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantWrites(t, store, "status r1 proposed->active", "review v1 approve by human")
	wantChange := StatusChange{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, From: memory.StatusProposed, To: memory.StatusActive, UpdatedAt: writeTime.UTC()}
	if store.changes[0] != wantChange {
		t.Fatalf("change = %+v", store.changes[0])
	}
	wantReview := Review{ID: "review-1", VersionID: "v1", ContentHash: currentHash, Decision: memory.ReviewApprove, Actor: memory.ActorHuman, Reviewer: "martin", ReviewedAt: writeTime.UTC()}
	if store.reviews[0] != wantReview {
		t.Fatalf("review = %+v", store.reviews[0])
	}

	for name, change := range map[string]func(*ApproveRequest, *RecordFacts){
		"stale version":   func(r *ApproveRequest, _ *RecordFacts) { r.ExpectedVersion = 2 },
		"other hash":      func(r *ApproveRequest, _ *RecordFacts) { r.ContentHash = "sha256:other" },
		"agent approval":  func(r *ApproveRequest, _ *RecordFacts) { r.Actor = memory.ActorAgent },
		"deleted content": func(_ *ApproveRequest, f *RecordFacts) { f.CurrentContentDeleted = true },
		"foreign project": func(r *ApproveRequest, _ *RecordFacts) { r.ProjectID = "p2" },
		"missing record":  func(_ *ApproveRequest, f *RecordFacts) { *f = RecordFacts{} },
	} {
		t.Run(name, func(t *testing.T) {
			stored, refused := storedRecord(memory.StatusProposed), request
			change(&refused, &stored)
			store := newFakeStore(stored)
			got, err := service(store).Approve(context.Background(), refused)
			if err != nil || got.Kind == memory.TransitionAllowed || got.Reason == "" || got.RecordVersion != 0 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantNoWrites(t, store)
		})
	}
}

func TestRejectAndArchiveRecordTheRedactedReason(t *testing.T) {
	request := ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "leaked token=" + fakeSecret, Now: writeTime}
	tests := []struct {
		name     string
		from     memory.RecordStatus
		call     func(Service, context.Context, ReasonRequest) (TransitionResult, error)
		writes   []string
		decision memory.ReviewDecision
	}{
		{"reject", memory.StatusProposed, Service.Reject, []string{"status r1 proposed->rejected", "review v1 reject by human"}, memory.ReviewReject},
		{"archive active", memory.StatusActive, Service.Archive, []string{"status r1 active->archived", "review v1 opinion by human"}, memory.ReviewOpinion},
		{"archive stale", memory.StatusNeedsVerification, Service.Archive, []string{"status r1 needs_verification->archived", "review v1 opinion by human"}, memory.ReviewOpinion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(storedRecord(tt.from))
			got, err := tt.call(service(store), context.Background(), request)
			if err != nil || got.Kind != memory.TransitionAllowed || got.RecordVersion != 4 || len(got.Findings) != 1 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantWrites(t, store, tt.writes...)
			review := store.reviews[0]
			if review.Decision != tt.decision || review.ContentHash != currentHash || strings.Contains(review.Reason, fakeSecret) || !strings.HasPrefix(review.Reason, "leaked token=[REDACTED:") {
				t.Fatalf("review = %+v", review)
			}
		})
	}
	for name, change := range map[string]func(*ReasonRequest, *RecordFacts){
		"no reason":       func(r *ReasonRequest, _ *RecordFacts) { r.Reason = "" },
		"deleted content": func(_ *ReasonRequest, f *RecordFacts) { f.CurrentContentDeleted = true },
		"system archive":  func(r *ReasonRequest, _ *RecordFacts) { r.Actor = memory.ActorSystem },
	} {
		t.Run("refused "+name, func(t *testing.T) {
			stored, refused := storedRecord(memory.StatusActive), request
			change(&refused, &stored)
			store := newFakeStore(stored)
			got, err := service(store).Archive(context.Background(), refused)
			if err != nil || got.Kind == memory.TransitionAllowed || got.RecordVersion != 0 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantNoWrites(t, store)
		})
	}
}

func TestArchiveExpiredProposals(t *testing.T) {
	expired := storedRecord(memory.StatusProposed)
	decided := storedRecord(memory.StatusProposed)
	decided.Record.ID, decided.HumanDecisionRecorded = "decided", true
	young := storedRecord(memory.StatusProposed)
	young.Record.ID, young.Record.CreatedAt = "young", writeTime.Add(-time.Hour)
	other := storedRecord(memory.StatusProposed)
	other.Record.ID, other.Record.ProjectID = "other", "p2"
	store := newFakeStore(expired, decided, young, other)
	store.expired = positions("r1", "decided", "young", "other")
	got, err := service(store).ArchiveExpiredProposals(context.Background(), SweepRequest{Now: writeTime, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.Examined != 4 || !slices.Equal(got.Archived, []string{"r1", "other"}) ||
		got.Refused[memory.TransitionDecisionRecorded] != 1 || got.Refused[memory.TransitionRetentionNotElapsed] != 1 || len(got.Refused) != 2 {
		t.Fatalf("result = %+v", got)
	}
	if !store.cutoff.Equal(memory.ProposalRetentionCutoff(writeTime)) || store.limit != 10 || store.after != (ProposalPosition{}) || got.Next != (SweepCursor{}) {
		t.Fatalf("query cutoff %v, limit %d", store.cutoff, store.limit)
	}
	wantWrites(t, store, "status r1 proposed->archived", "status other proposed->archived")
	if store.changes[1].ProjectID != "p2" || store.changes[1].ExpectedVersion != 3 {
		t.Fatalf("change = %+v", store.changes[1])
	}
	if len(store.reviews) != 0 {
		t.Fatalf("sweep wrote reviews: %+v", store.reviews)
	}
}

func TestMarkNeedsVerification(t *testing.T) {
	request := VerificationRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Snapshot: "snap", Now: writeTime}
	tests := []struct {
		name  string
		files map[string]compose.FileObservation
		want  memory.RecordTransitionKind
	}{
		{"changed source", map[string]compose.FileObservation{
			"a.go": {Observation: memory.SourcePresent, Hash: "ae401827daa5a32153625b7b9ad17b13ec94fe0b"}, "b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
		}, memory.TransitionAllowed},
		{"missing source", map[string]compose.FileObservation{"a.go": {Observation: memory.SourcePresent, Hash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"}}, memory.TransitionAllowed},
		{"matching sources", map[string]compose.FileObservation{
			"a.go": {Observation: memory.SourcePresent, Hash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"}, "b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
		}, memory.TransitionSourceNotMismatched},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, snapshot := newFakeStore(storedRecord(memory.StatusActive)), &fakeSnapshot{files: tt.files}
			got, err := Service{Store: store, Snapshot: snapshot}.MarkNeedsVerification(context.Background(), request)
			if err != nil || got.Kind != tt.want {
				t.Fatalf("result %+v, error %v", got, err)
			}
			if !slices.Equal(snapshot.observed, []string{"a.go", "b.go"}) || snapshot.batches != 1 {
				t.Fatalf("observed = %v in %d batches", snapshot.observed, snapshot.batches)
			}
			if tt.want != memory.TransitionAllowed {
				wantNoWrites(t, store)
				return
			}
			wantWrites(t, store, "status r1 active->needs_verification")
			if got.RecordVersion != 4 {
				t.Fatalf("record version = %d", got.RecordVersion)
			}
		})
	}
	store := newFakeStore(storedRecord(memory.StatusActive))
	stale := request
	stale.ExpectedVersion = 2
	got, err := Service{Store: store, Snapshot: &fakeSnapshot{}}.MarkNeedsVerification(context.Background(), stale)
	if err != nil || got.Kind != memory.TransitionVersionConflict {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantNoWrites(t, store)
}

func reactivateRequest(verifications ...Verification) ReactivateRequest {
	return ReactivateRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Snapshot: "snap", Verifications: verifications, Now: writeTime}
}

func TestReactivateCreatesAVersionWithTheVerifiedHashes(t *testing.T) {
	store := newFakeStore(storedRecord(memory.StatusNeedsVerification))
	snapshot := &fakeSnapshot{files: map[string]compose.FileObservation{
		"a.go": {Observation: memory.SourcePresent, Hash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"},
		"b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
	}}
	s := Service{Store: store, Snapshot: snapshot, NewID: sequentialIDs()}
	got, err := s.Reactivate(context.Background(), reactivateRequest(Verification{Path: "a.go", VerifiedHash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"}, Verification{Path: "b.go", VerifiedHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"}))
	if err != nil || got.Kind != memory.TransitionAllowed || got.RecordVersion != 4 || got.VersionID != "version-1" {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantWrites(t, store, "copy v1->version-1", "status r1 needs_verification->active", "review version-1 approve by human")
	wantSources := []memory.RecordSource{
		{Kind: memory.SourceFile, Path: "a.go", BlobHash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"},
		{Kind: memory.SourceFile, Path: "b.go", BlobHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
		{Kind: memory.SourceSpec, Ref: "specs/012"},
	}
	copied := store.copies[0]
	if copied.ProjectID != "p1" || copied.RecordID != "r1" || copied.CreatedAt != writeTime.UTC() || !slices.Equal(copied.Sources, wantSources) {
		t.Fatalf("copy = %+v", copied)
	}
	if store.changes[0].CurrentVersionID != "version-1" {
		t.Fatalf("change = %+v", store.changes[0])
	}
	if review := store.reviews[0]; review.ContentHash != currentHash || review.ID != "review-2" {
		t.Fatalf("review = %+v", review)
	}
}

func TestReactivateRefusals(t *testing.T) {
	present := map[string]compose.FileObservation{
		"a.go": {Observation: memory.SourcePresent, Hash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"},
		"b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
	}
	both := []Verification{{Path: "a.go", VerifiedHash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"}, {Path: "b.go", VerifiedHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"}}
	tests := []struct {
		name          string
		files         map[string]compose.FileObservation
		verifications []Verification
		change        func(*RecordFacts)
		want          memory.RecordTransitionKind
		observed      []string
	}{
		{"verified hash is not current", present, []Verification{{Path: "a.go", VerifiedHash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"}, both[1]}, nil, memory.TransitionVerificationMissing, []string{"a.go", "b.go"}},
		{"missing source", map[string]compose.FileObservation{"b.go": present["b.go"]}, both, nil, memory.TransitionVerificationMissing, []string{"a.go", "b.go"}},
		{"undeclared path is not observed", present, append(slices.Clone(both), Verification{Path: "c.go", VerifiedHash: "x"}), nil, memory.TransitionVerificationMissing, []string{"a.go", "b.go"}},
		{"one source left unverified", present, both[:1], nil, memory.TransitionVerificationMissing, []string{"a.go"}},
		{"deleted content", present, both, func(f *RecordFacts) { f.CurrentContentDeleted = true }, memory.TransitionContentDeleted, nil},
		{"active record", present, both, func(f *RecordFacts) { f.Record.Status = memory.StatusActive }, memory.TransitionNotPermitted, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := storedRecord(memory.StatusNeedsVerification)
			if tt.change != nil {
				tt.change(&stored)
			}
			store, snapshot := newFakeStore(stored), &fakeSnapshot{files: tt.files}
			got, err := Service{Store: store, Snapshot: snapshot, NewID: sequentialIDs()}.Reactivate(context.Background(), reactivateRequest(tt.verifications...))
			if err != nil || got.Kind != tt.want || got.VersionID != "" || got.RecordVersion != 0 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			if !slices.Equal(snapshot.observed, tt.observed) || (tt.observed != nil) != (snapshot.batches == 1) || snapshot.batches > 1 {
				t.Fatalf("observed = %v in %d batches", snapshot.observed, snapshot.batches)
			}
			wantNoWrites(t, store)
		})
	}
}

func supersedeRequest() SupersedeRequest {
	return SupersedeRequest{
		ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Author: "martin",
		Category: memory.CategoryDecision, Content: "Нова политика: password=" + fakeSecret,
		Sources: []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}, Now: writeTime,
	}
}

func TestSupersedeCreatesAnActiveReplacement(t *testing.T) {
	store := newFakeStore(storedRecord(memory.StatusNeedsVerification))
	got, err := service(store).Supersede(context.Background(), supersedeRequest())
	if err != nil || got.Creation.Kind != memory.CreationAllowed || got.Transition.Kind != memory.TransitionAllowed {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if got.Replacement != "record-1" || got.ReplacementVersionID != "version-2" || got.RecordVersion != 4 || len(got.Findings) != 1 {
		t.Fatalf("result = %+v", got)
	}
	wantWrites(t, store, "insert record record-1", "review version-2 approve by human", "status r1 needs_verification->superseded", "relation record-1 supersedes r1")
	created := store.created[0]
	if created.Record.Status != memory.StatusActive || created.Record.ProjectID != "p1" || created.Version.Scope.Origin != memory.OriginHuman ||
		created.Version.Trust != memory.TrustHumanApproved || strings.Contains(created.Version.Content, fakeSecret) || got.ContentHash != created.Version.ContentHash {
		t.Fatalf("replacement = %+v", created)
	}
	if relation := store.relations[0]; relation.ProjectID != "p1" || relation.CreatedAt != writeTime.UTC() {
		t.Fatalf("relation = %+v", relation)
	}
}

func TestSupersedeRefusalWritesNothing(t *testing.T) {
	tests := []struct {
		name           string
		change         func(*SupersedeRequest, *RecordFacts)
		wantCreation   memory.RecordCreationKind
		wantTransition memory.RecordTransitionKind
	}{
		{"invalid replacement", func(r *SupersedeRequest, _ *RecordFacts) { r.Content = " " }, memory.CreationEmptyContent, memory.TransitionReplacementMissing},
		{"stale version", func(r *SupersedeRequest, _ *RecordFacts) { r.ExpectedVersion = 2 }, memory.CreationAllowed, memory.TransitionVersionConflict},
		{"proposal cannot be superseded", func(_ *SupersedeRequest, f *RecordFacts) { f.Record.Status = memory.StatusProposed }, memory.CreationAllowed, memory.TransitionNotPermitted},
		{"agent", func(r *SupersedeRequest, _ *RecordFacts) { r.Actor = memory.ActorAgent }, memory.CreationActorNotAllowed, memory.TransitionActorNotAllowed},
		{"foreign record", func(r *SupersedeRequest, _ *RecordFacts) { r.ProjectID = "p2" }, memory.CreationAllowed, memory.TransitionNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, request := storedRecord(memory.StatusActive), supersedeRequest()
			tt.change(&request, &stored)
			store := newFakeStore(stored)
			got, err := service(store).Supersede(context.Background(), request)
			if err != nil || got.Creation.Kind != tt.wantCreation || got.Transition.Kind != tt.wantTransition || got.Replacement != "" || got.RecordVersion != 0 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantNoWrites(t, store)
		})
	}
}

func opinionRequest() OpinionRequest {
	return OpinionRequest{ProjectID: "p1", RecordID: "r1", VersionID: "v0", ContentHash: "sha256:old", Actor: memory.ActorAgent, Reviewer: "auditor", Reason: "duplicate of r2", Now: writeTime}
}

func TestAddOpinion(t *testing.T) {
	store := newFakeStore(storedRecord(memory.StatusActive))
	store.versions["r1/v0"] = VersionFacts{Found: true, ContentHash: "sha256:old"}
	got, err := service(store).AddOpinion(context.Background(), opinionRequest())
	if err != nil || got.Kind != memory.OpinionAdd || got.ReviewID != "review-1" || got.Findings != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantWrites(t, store, "review v0 opinion by agent")
	want := Review{ID: "review-1", VersionID: "v0", ContentHash: "sha256:old", Decision: memory.ReviewOpinion, Actor: memory.ActorAgent, Reviewer: "auditor", Reason: "duplicate of r2", ReviewedAt: writeTime.UTC()}
	if store.reviews[0] != want {
		t.Fatalf("review = %+v", store.reviews[0])
	}
	if !slices.Equal(store.reads, []string{"record r1", "version r1/v0"}) {
		t.Fatalf("reads = %v", store.reads)
	}

	deleted := newFakeStore(storedRecord(memory.StatusActive))
	deleted.versions["r1/v0"] = VersionFacts{Found: true, ContentHash: "sha256:old", ContentDeleted: true}
	got, err = service(deleted).AddOpinion(context.Background(), opinionRequest())
	if err != nil || got.Kind != memory.OpinionContentDeleted || got.ReviewID != "" {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantNoWrites(t, deleted)
	withoutReason := opinionRequest()
	withoutReason.Reason = ""
	got, err = service(deleted).AddOpinion(context.Background(), withoutReason)
	if err != nil || got.Kind != memory.OpinionAdd || deleted.reviews[0].Reason != "" {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

// An agent's audit opinion carries its suggestion into the review; the rule
// refuses one about an older version and writes nothing.
func TestAddOpinionWithSuggestion(t *testing.T) {
	suggestion := opinionRequest()
	suggestion.VersionID, suggestion.ContentHash, suggestion.Suggestion = "v1", currentHash, memory.SuggestDelete
	suggestion.Reason = "obsolete: token=hunter2"
	store := newFakeStore(storedRecord(memory.StatusActive))
	store.versions["r1/v1"] = VersionFacts{Found: true, ContentHash: currentHash}
	got, err := service(store).AddOpinion(context.Background(), suggestion)
	if err != nil || got.Kind != memory.OpinionAdd || got.ReviewID != "review-1" || len(got.Findings) != 1 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	review := store.reviews[0]
	if review.Suggestion != memory.SuggestDelete || review.Actor != memory.ActorAgent || review.VersionID != "v1" || strings.Contains(review.Reason, "hunter2") {
		t.Fatalf("review = %+v", review)
	}

	older := newFakeStore(storedRecord(memory.StatusActive))
	older.versions["r1/v0"] = VersionFacts{Found: true, ContentHash: "sha256:old"}
	stale := opinionRequest()
	stale.Suggestion = memory.SuggestArchive
	got, err = service(older).AddOpinion(context.Background(), stale)
	if err != nil || got.Kind != memory.OpinionVersionNotCurrent || got.ReviewID != "" {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantNoWrites(t, older)
}

// ArchiveWithin runs Archive's read, decision and writes in the caller's
// transaction and leaves the commit to it.
func TestArchiveWithinUsesTheCallersTransaction(t *testing.T) {
	store := newFakeStore(storedRecord(memory.StatusNeedsVerification))
	request := ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "obsolete", Now: writeTime}
	got, err := service(store).ArchiveWithin(context.Background(), fakeTransaction{store}, request)
	if err != nil || got.Kind != memory.TransitionAllowed || got.RecordVersion != 4 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if store.transactions != 0 {
		t.Fatalf("transactions = %d", store.transactions)
	}
	wantWrites(t, store, "status r1 needs_verification->archived", "review v1 opinion by human")
	if store.reviews[0].Reason != "obsolete" || store.reviews[0].Suggestion != "" {
		t.Fatalf("review = %+v", store.reviews[0])
	}

	stale := newFakeStore(storedRecord(memory.StatusActive))
	request.ExpectedVersion = 2
	got, err = service(stale).ArchiveWithin(context.Background(), fakeTransaction{stale}, request)
	if err != nil || got.Kind != memory.TransitionVersionConflict {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantNoWrites(t, stale)

	failing := newFakeStore(storedRecord(memory.StatusActive))
	failing.failures["ReadRecord"] = errInjected
	if _, err := service(failing).ArchiveWithin(context.Background(), fakeTransaction{failing}, request); !errors.Is(err, errInjected) {
		t.Fatalf("error = %v", err)
	}
}

// failureCase runs one procedure against a store prepared for it to reach
// the injected failure.
type failureCase struct {
	name  string
	setup func(*fakeStore)
	run   func(Service) error
}

func failureCases() []failureCase {
	ctx := context.Background()
	present := map[string]compose.FileObservation{
		"a.go": {Observation: memory.SourcePresent, Hash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"},
		"b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
	}
	reason := ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "x", Now: writeTime}
	withRecord := func(status memory.RecordStatus) func(*fakeStore) {
		return func(s *fakeStore) { s.records["r1"] = storedRecord(status) }
	}
	return []failureCase{
		{"create", func(*fakeStore) {}, func(s Service) error { _, err := s.CreateRecord(ctx, humanCreate()); return err }},
		{"approve", withRecord(memory.StatusProposed), func(s Service) error {
			_, err := s.Approve(ctx, ApproveRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", VersionID: "v1", ContentHash: currentHash, Now: writeTime})
			return err
		}},
		{"reject", withRecord(memory.StatusProposed), func(s Service) error { _, err := s.Reject(ctx, reason); return err }},
		{"archive", withRecord(memory.StatusActive), func(s Service) error { _, err := s.Archive(ctx, reason); return err }},
		{"sweep", func(s *fakeStore) { s.records["r1"] = storedRecord(memory.StatusProposed); s.expired = positions("r1") }, func(s Service) error {
			_, err := s.ArchiveExpiredProposals(ctx, SweepRequest{Now: writeTime, Limit: 5})
			return err
		}},
		{"mark", withRecord(memory.StatusActive), func(s Service) error {
			s.Snapshot = &fakeSnapshot{files: map[string]compose.FileObservation{"a.go": {Observation: memory.SourcePresent, Hash: "8f76e11356f466c4ac351ffe3638aec780cfe9f4"}}}
			_, err := s.MarkNeedsVerification(ctx, VerificationRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Now: writeTime})
			return err
		}},
		{"reactivate", withRecord(memory.StatusNeedsVerification), func(s Service) error {
			s.Snapshot = &fakeSnapshot{files: present}
			_, err := s.Reactivate(ctx, reactivateRequest(Verification{Path: "a.go", VerifiedHash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"}, Verification{Path: "b.go", VerifiedHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"}))
			return err
		}},
		{"supersede", withRecord(memory.StatusActive), func(s Service) error { _, err := s.Supersede(ctx, supersedeRequest()); return err }},
		{"opinion", func(s *fakeStore) {
			s.records["r1"] = storedRecord(memory.StatusActive)
			s.versions["r1/v0"] = VersionFacts{Found: true, ContentHash: "sha256:old"}
		}, func(s Service) error { _, err := s.AddOpinion(ctx, opinionRequest()); return err }},
		{"merge", func(s *fakeStore) {
			for _, proposal := range mergedProposals() {
				s.records[proposal.Record.ID] = proposal
			}
		}, func(s Service) error { _, err := s.Merge(ctx, mergeRequest()); return err }},
	}
}

// Every failure of a read, a write, the transaction or an ID is returned
// with its cause. The fake does not roll back, so a failing write may
// follow earlier ones; the real store rolls them back.
func TestFailuresAreReturnedWithTheirCause(t *testing.T) {
	methods := []string{"ReadRecord", "ReadVersion", "ReadExpiredProposals", "InsertRecord", "ChangeStatus", "CopyVersion", "InsertReview", "InsertRelation"}
	for _, tc := range failureCases() {
		// A clean run counts the IDs and methods the procedure uses.
		clean := newFakeStore()
		tc.setup(clean)
		ids := 0
		s := Service{Store: clean, Snapshot: &fakeSnapshot{}, NewID: func(prefix string) (string, error) { ids++; return prefix, nil }}
		if err := tc.run(s); err != nil {
			t.Fatalf("%s clean run: %v", tc.name, err)
		}
		if len(clean.writes) == 0 {
			t.Fatalf("%s clean run wrote nothing", tc.name)
		}
		for _, method := range methods {
			store := newFakeStore()
			tc.setup(store)
			store.failures[method] = errInjected
			err := tc.run(Service{Store: store, Snapshot: &fakeSnapshot{}, NewID: sequentialIDs()})
			used := slices.ContainsFunc(append(clean.reads, clean.writes...), func(line string) bool { return calledBy(line, method) })
			if used && !errors.Is(err, errInjected) {
				t.Fatalf("%s with %s failing: %v", tc.name, method, err)
			}
			if !used && err != nil {
				t.Fatalf("%s does not use %s but failed: %v", tc.name, method, err)
			}
		}
		for call := 1; call <= ids; call++ {
			store := newFakeStore()
			tc.setup(store)
			if err := tc.run(Service{Store: store, Snapshot: &fakeSnapshot{}, NewID: failingIDs(call, errInjected)}); !errors.Is(err, errInjected) {
				t.Fatalf("%s with id %d failing: %v", tc.name, call, err)
			}
		}
		store := newFakeStore()
		tc.setup(store)
		store.withinErr = errInjected
		if err := tc.run(Service{Store: store, NewID: sequentialIDs()}); !errors.Is(err, errInjected) {
			t.Fatalf("%s with failing transaction: %v", tc.name, err)
		}
	}
}

// calledBy maps a fake's read or write line to the port method behind it.
func calledBy(line, method string) bool {
	prefixes := map[string]string{
		"ReadRecord": "record ", "ReadVersion": "version ", "InsertRecord": "insert record ", "ChangeStatus": "status ",
		"CopyVersion": "copy ", "InsertReview": "review ", "InsertRelation": "relation ", "ReadExpiredProposals": "expired ",
	}
	return strings.HasPrefix(line, prefixes[method])
}

func TestSnapshotFailuresWriteNothing(t *testing.T) {
	snapshotErr := errors.New("snapshot unreadable")
	store := newFakeStore(storedRecord(memory.StatusActive))
	_, err := Service{Store: store, Snapshot: &fakeSnapshot{err: snapshotErr}}.MarkNeedsVerification(context.Background(), VerificationRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Now: writeTime})
	if !errors.Is(err, snapshotErr) {
		t.Fatalf("mark error = %v", err)
	}
	store = newFakeStore(storedRecord(memory.StatusNeedsVerification))
	_, err = Service{Store: store, Snapshot: &fakeSnapshot{err: snapshotErr}, NewID: sequentialIDs()}.Reactivate(context.Background(), reactivateRequest(Verification{Path: "a.go", VerifiedHash: "x"}))
	if !errors.Is(err, snapshotErr) {
		t.Fatalf("reactivate error = %v", err)
	}
	wantNoWrites(t, store)
}

// positions lists proposals in sweep order, one minute apart.
func positions(ids ...string) []ProposalPosition {
	list := make([]ProposalPosition, len(ids))
	for index, id := range ids {
		list[index] = ProposalPosition{CreatedAt: writeTime.Add(-200*24*time.Hour + time.Duration(index)*time.Minute), RecordID: id}
	}
	return list
}

// Refused proposals do not fill later batches: the cursor moves past them at
// the cutoff the sweep started with, and the scan's end returns no cursor.
func TestSweepCursorMovesPastRefusedProposals(t *testing.T) {
	var stored []RecordFacts
	for _, id := range []string{"d1", "d2", "open"} {
		record := storedRecord(memory.StatusProposed)
		record.Record.ID, record.HumanDecisionRecorded = id, id != "open"
		stored = append(stored, record)
	}
	store := newFakeStore(stored...)
	store.expired = positions("d1", "d2", "open")
	s := service(store)
	first, err := s.ArchiveExpiredProposals(context.Background(), SweepRequest{Now: writeTime, Limit: 2})
	if err != nil || first.Examined != 2 || len(first.Archived) != 0 || first.Refused[memory.TransitionDecisionRecorded] != 2 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	wantNext := SweepCursor{Cutoff: memory.ProposalRetentionCutoff(writeTime), After: store.expired[1]}
	if first.Next != wantNext {
		t.Fatalf("next = %+v", first.Next)
	}
	later := writeTime.Add(time.Hour)
	second, err := s.ArchiveExpiredProposals(context.Background(), SweepRequest{Now: later, Limit: 2, Cursor: first.Next})
	if err != nil || !slices.Equal(second.Archived, []string{"open"}) || second.Next != (SweepCursor{}) {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if !store.cutoff.Equal(wantNext.Cutoff) || store.after != wantNext.After {
		t.Fatalf("continued at cutoff %v after %+v", store.cutoff, store.after)
	}
	if store.changes[0].UpdatedAt != later.UTC() {
		t.Fatalf("change = %+v", store.changes[0])
	}
}

// countingSnapshot counts observations and fails each one.
type countingSnapshot struct{ observations int }

func (c *countingSnapshot) ObserveFiles(context.Context, compose.SnapshotRef, []string) ([]compose.FileObservation, error) {
	c.observations++
	return nil, errors.New("snapshot must not be read")
}

// Refusals that do not depend on the sources are decided before the
// snapshot is read: a foreign record is indistinguishable from a missing one
// even when the snapshot fails.
func TestSnapshotIsNotReadForGeneralRefusals(t *testing.T) {
	tests := []struct {
		name   string
		change func(*RecordFacts)
		mark   func(*VerificationRequest)
		react  func(*ReactivateRequest)
		mWant  memory.RecordTransitionKind
		rWant  memory.RecordTransitionKind
	}{
		{name: "foreign project", mark: func(r *VerificationRequest) { r.ProjectID = "p2" }, react: func(r *ReactivateRequest) { r.ProjectID = "p2" },
			mWant: memory.TransitionNotFound, rWant: memory.TransitionNotFound},
		{name: "missing record", change: func(f *RecordFacts) { *f = RecordFacts{} }, mWant: memory.TransitionNotFound, rWant: memory.TransitionNotFound},
		{name: "version conflict", mark: func(r *VerificationRequest) { r.ExpectedVersion = 2 }, react: func(r *ReactivateRequest) { r.ExpectedVersion = 2 },
			mWant: memory.TransitionVersionConflict, rWant: memory.TransitionVersionConflict},
		{name: "wrong actor", react: func(r *ReactivateRequest) { r.Actor = memory.ActorAgent }, mWant: memory.TransitionAllowed, rWant: memory.TransitionActorNotAllowed},
		{name: "terminal status", change: func(f *RecordFacts) { f.Record.Status = memory.StatusArchived }, mWant: memory.TransitionNotPermitted, rWant: memory.TransitionNotPermitted},
		{name: "deleted content", change: func(f *RecordFacts) { f.CurrentContentDeleted = true }, mWant: memory.TransitionAllowed, rWant: memory.TransitionContentDeleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, from := range []memory.RecordStatus{memory.StatusActive, memory.StatusNeedsVerification} {
				stored := storedRecord(from)
				if tt.change != nil {
					tt.change(&stored)
				}
				snapshot := &countingSnapshot{}
				s := Service{Store: newFakeStore(stored), Snapshot: snapshot, NewID: sequentialIDs()}
				if from == memory.StatusActive {
					if tt.mWant == memory.TransitionAllowed {
						continue // the system's check has no actor or content refusal here
					}
					request := VerificationRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Now: writeTime}
					if tt.mark != nil {
						tt.mark(&request)
					}
					got, err := s.MarkNeedsVerification(context.Background(), request)
					if err != nil || got.Kind != tt.mWant || got.Reason == "" {
						t.Fatalf("mark = %+v, %v", got, err)
					}
				} else {
					request := reactivateRequest(Verification{Path: "a.go", VerifiedHash: "x"})
					if tt.react != nil {
						tt.react(&request)
					}
					got, err := s.Reactivate(context.Background(), request)
					if err != nil || got.Kind != tt.rWant || got.Reason == "" {
						t.Fatalf("reactivate = %+v, %v", got, err)
					}
				}
				if snapshot.observations != 0 {
					t.Fatalf("%s: snapshot read %d times", from, snapshot.observations)
				}
				wantNoWrites(t, s.Store.(*fakeStore))
			}
		})
	}
}

// lengthenedByRedaction fits the text field bound as written but not after
// redaction replaces its secret with a longer placeholder.
func lengthenedByRedaction() string {
	return strings.Repeat("r", memory.MaxTextFieldBytes-12) + "\npassword=x"
}

// A value that redaction lengthens past its storage bound is a refusal on
// the stored value, never truncated, and nothing is written.
func TestValuesOverTheirBoundAfterRedactionAreRefused(t *testing.T) {
	ctx := context.Background()
	if len(lengthenedByRedaction()) > memory.MaxTextFieldBytes {
		t.Fatal("fixture exceeds the bound before redaction")
	}
	reason := ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: lengthenedByRedaction(), Now: writeTime}
	for name, run := range map[string]func(Service) (memory.RecordTransitionKind, error){
		"reject": func(s Service) (memory.RecordTransitionKind, error) {
			s.Store.(*fakeStore).records["r1"] = storedRecord(memory.StatusProposed)
			got, err := s.Reject(ctx, reason)
			return got.Kind, err
		},
		"archive": func(s Service) (memory.RecordTransitionKind, error) {
			s.Store.(*fakeStore).records["r1"] = storedRecord(memory.StatusActive)
			got, err := s.Archive(ctx, reason)
			return got.Kind, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			kind, err := run(service(store))
			if err != nil || kind != memory.TransitionReasonTooLong {
				t.Fatalf("kind %s, error %v", kind, err)
			}
			wantNoWrites(t, store)
		})
	}
	t.Run("opinion", func(t *testing.T) {
		store := newFakeStore(storedRecord(memory.StatusActive))
		store.versions["r1/v0"] = VersionFacts{Found: true, ContentHash: "sha256:old"}
		request := opinionRequest()
		request.Reason = lengthenedByRedaction()
		got, err := service(store).AddOpinion(ctx, request)
		if err != nil || got.Kind != memory.OpinionReasonTooLong || got.ReviewID != "" || len(got.Findings) != 1 {
			t.Fatalf("result %+v, error %v", got, err)
		}
		wantNoWrites(t, store)
	})
	t.Run("source path", func(t *testing.T) {
		store := newFakeStore()
		request := agentProposal()
		request.Sources = []memory.RecordSource{{Kind: memory.SourceFile, Path: lengthenedByRedaction(), BlobHash: "0fd0bcfb44f83e7d5ac7a8922578276b9af48746"}}
		got, err := service(store).CreateRecord(ctx, request)
		if err != nil || got.Kind != memory.CreationInvalidSource || got.RecordID != "" {
			t.Fatalf("result %+v, error %v", got, err)
		}
		wantNoWrites(t, store)
	})
	t.Run("merge reason", func(t *testing.T) {
		store := newFakeStore(mergedProposals()...)
		long := Service{Store: store, NewID: func(prefix string) (string, error) { return strings.Repeat("i", memory.MaxTextFieldBytes), nil }}
		got, err := long.Merge(ctx, mergeRequest())
		if err != nil || got.Applied || got.Transitions[0].Decision.Kind != memory.TransitionReasonTooLong {
			t.Fatalf("result %+v, error %v", got, err)
		}
		wantNoWrites(t, store)
	})
}

// A reviewer at MaxIdentityBytes (multibyte) is accepted; one byte more is
// the rule's invalid_facts before any write, on every reviewed path.
func TestReviewerIsBounded(t *testing.T) {
	ctx := context.Background()
	atBound := strings.Repeat("ж", memory.MaxIdentityBytes/2)
	overBound := atBound + "x"
	present := map[string]compose.FileObservation{
		"a.go": {Observation: memory.SourcePresent, Hash: "8538bc9d48b5d707b9c87fe60ae34972bb8cff31"},
		"b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
	}
	type outcome struct {
		allowed bool
		refused bool
	}
	run := map[string]func(Service, *fakeStore, string) outcome{
		"approve": func(s Service, store *fakeStore, reviewer string) outcome {
			store.records["r1"] = storedRecord(memory.StatusProposed)
			got, err := s.Approve(ctx, ApproveRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: reviewer, VersionID: "v1", ContentHash: currentHash, Now: writeTime})
			return outcome{err == nil && got.Kind == memory.TransitionAllowed, err == nil && got.Kind == memory.TransitionInvalidFacts}
		},
		"reject": func(s Service, store *fakeStore, reviewer string) outcome {
			store.records["r1"] = storedRecord(memory.StatusProposed)
			got, err := s.Reject(ctx, ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: reviewer, Reason: "x", Now: writeTime})
			return outcome{err == nil && got.Kind == memory.TransitionAllowed, err == nil && got.Kind == memory.TransitionInvalidFacts}
		},
		"archive": func(s Service, store *fakeStore, reviewer string) outcome {
			store.records["r1"] = storedRecord(memory.StatusActive)
			got, err := s.Archive(ctx, ReasonRequest{ProjectID: "p1", RecordID: "r1", ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: reviewer, Reason: "x", Now: writeTime})
			return outcome{err == nil && got.Kind == memory.TransitionAllowed, err == nil && got.Kind == memory.TransitionInvalidFacts}
		},
		"reactivate": func(s Service, store *fakeStore, reviewer string) outcome {
			store.records["r1"] = storedRecord(memory.StatusNeedsVerification)
			s.Snapshot = &fakeSnapshot{files: present}
			request := reactivateRequest(Verification{Path: "a.go", VerifiedHash: present["a.go"].Hash}, Verification{Path: "b.go", VerifiedHash: present["b.go"].Hash})
			request.Reviewer = reviewer
			got, err := s.Reactivate(ctx, request)
			return outcome{err == nil && got.Kind == memory.TransitionAllowed, err == nil && got.Kind == memory.TransitionInvalidFacts}
		},
		"opinion": func(s Service, store *fakeStore, reviewer string) outcome {
			store.records["r1"] = storedRecord(memory.StatusActive)
			store.versions["r1/v0"] = VersionFacts{Found: true, ContentHash: "sha256:old"}
			request := opinionRequest()
			request.Reviewer = reviewer
			got, err := s.AddOpinion(ctx, request)
			return outcome{err == nil && got.Kind == memory.OpinionAdd, err == nil && got.Kind == memory.OpinionInvalidFacts}
		},
		"merge": func(s Service, store *fakeStore, reviewer string) outcome {
			for _, proposal := range mergedProposals() {
				store.records[proposal.Record.ID] = proposal
			}
			request := mergeRequest()
			request.Author = reviewer
			got, err := s.Merge(ctx, request)
			refused := err == nil && !got.Applied && got.Creation.Kind == memory.CreationInvalidFacts && got.Transitions[0].Decision.Kind == memory.TransitionInvalidFacts
			return outcome{err == nil && got.Applied, refused}
		},
	}
	for name, procedure := range run {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			if got := procedure(service(store), store, atBound); !got.allowed {
				t.Fatalf("reviewer at the bound refused: %+v", got)
			}
			store = newFakeStore()
			if got := procedure(service(store), store, overBound); !got.refused {
				t.Fatalf("reviewer over the bound not refused: %+v", got)
			}
			wantNoWrites(t, store)
		})
	}
}

// A secret posing as a blob hash is refused, never stored.
func TestCreateRefusesABlobHashOutsideItsGrammar(t *testing.T) {
	store := newFakeStore()
	request := agentProposal()
	request.Sources = []memory.RecordSource{{Kind: memory.SourceFile, Path: "a.go", BlobHash: "password=hunter2"}}
	got, err := service(store).CreateRecord(context.Background(), request)
	if err != nil || got.Kind != memory.CreationInvalidSource || strings.Contains(got.Reason, "hunter2") {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantNoWrites(t, store)
}

// A snapshot hash outside the blob hash grammar is never a verification, so
// a faulty snapshot adapter cannot make Reactivate store it.
func TestReactivateRefusesAnInvalidSnapshotHash(t *testing.T) {
	store := newFakeStore(storedRecord(memory.StatusNeedsVerification))
	snapshot := &fakeSnapshot{files: map[string]compose.FileObservation{
		"a.go": {Observation: memory.SourcePresent, Hash: "password=hunter2"},
		"b.go": {Observation: memory.SourcePresent, Hash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
	}}
	got, err := Service{Store: store, Snapshot: snapshot, NewID: sequentialIDs()}.Reactivate(context.Background(),
		reactivateRequest(Verification{Path: "a.go", VerifiedHash: "password=hunter2"}, Verification{Path: "b.go", VerifiedHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"}))
	if err != nil || got.Kind != memory.TransitionVerificationMissing {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantNoWrites(t, store)
}
