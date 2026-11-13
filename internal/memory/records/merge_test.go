package records

import (
	"context"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// mergedProposals are proposals r1 and r2 of project p1 at version 3, with
// current versions v1 and v2.
func mergedProposals() []RecordFacts {
	first, second := storedRecord(memory.StatusProposed), storedRecord(memory.StatusProposed)
	second.Record.ID, second.Record.CurrentVersionID, second.CurrentContentHash = "r2", "v2", "sha256:second"
	return []RecordFacts{first, second}
}

func mergeRequest() MergeRequest {
	return MergeRequest{
		ProjectID: "p1",
		Proposals: []MergedProposal{{RecordID: "r1", ExpectedVersion: 3}, {RecordID: "r2", ExpectedVersion: 3}},
		Actor:     memory.ActorHuman,
		Author:    "martin",
		Category:  memory.CategoryConvention,
		Content:   "Обединено правило; token=" + fakeSecret,
		Sources:   []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}},
		Now:       writeTime,
	}
}

func TestMergeCreatesAHumanRecordAndRejectsTheProposals(t *testing.T) {
	store := newFakeStore(mergedProposals()...)
	got, err := service(store).Merge(context.Background(), mergeRequest())
	if err != nil || !got.Applied || got.Merge.Kind != memory.ProposalMergeAllowed || got.Creation.Kind != memory.CreationAllowed {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if got.RecordID != "record-1" || got.VersionID != "version-2" || len(got.Findings) != 1 {
		t.Fatalf("result = %+v", got)
	}
	for index, transition := range got.Transitions {
		if transition.RecordID != []string{"r1", "r2"}[index] || transition.Decision.Kind != memory.TransitionAllowed || transition.RecordVersion != 4 {
			t.Fatalf("transition %d = %+v", index, transition)
		}
	}
	wantWrites(t, store,
		"insert record record-1", "review version-2 approve by human",
		"status r1 proposed->rejected", "review v1 reject by human",
		"status r2 proposed->rejected", "review v2 reject by human",
	)
	created := store.created[0]
	if created.Record.Status != memory.StatusActive || created.Record.ProjectID != "p1" || created.Version.Trust != memory.TrustHumanApproved ||
		created.Version.Scope != (memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityProject, Origin: memory.OriginHuman}) ||
		strings.Contains(created.Version.Content, fakeSecret) || got.ContentHash != created.Version.ContentHash {
		t.Fatalf("merged record = %+v", created)
	}
	for index, review := range store.reviews[1:] {
		want := Review{
			ID: []string{"review-4", "review-5"}[index], VersionID: []string{"v1", "v2"}[index], ContentHash: []string{currentHash, "sha256:second"}[index],
			Decision: memory.ReviewReject, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "merged into record record-1", ReviewedAt: writeTime.UTC(),
		}
		if review != want {
			t.Fatalf("review = %+v, want %+v", review, want)
		}
	}
	if len(store.relations) != 0 || store.transactions != 1 {
		t.Fatalf("relations %v in %d transactions", store.relations, store.transactions)
	}
}

// A refused set is decided before the store is touched.
func TestMergeRefusesTheSetBeforeReading(t *testing.T) {
	for name, proposals := range map[string][]MergedProposal{
		"one":      {{RecordID: "r1", ExpectedVersion: 3}},
		"repeated": {{RecordID: "r1", ExpectedVersion: 3}, {RecordID: "r1", ExpectedVersion: 3}},
		"empty id": {{RecordID: "r1", ExpectedVersion: 3}, {ExpectedVersion: 3}},
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore(mergedProposals()...)
			request := mergeRequest()
			request.Proposals = proposals
			got, err := service(store).Merge(context.Background(), request)
			if err != nil || got.Merge.Kind == memory.ProposalMergeAllowed || got.Applied || got.Transitions != nil || got.Creation.Kind != "" {
				t.Fatalf("result %+v, error %v", got, err)
			}
			if store.transactions != 0 {
				t.Fatalf("transactions = %d", store.transactions)
			}
		})
	}
}

// Any refused decision writes nothing, and every proposal's decision is
// still reported.
func TestMergeRefusalWritesNothing(t *testing.T) {
	tests := []struct {
		name         string
		change       func(*MergeRequest, []RecordFacts)
		wantCreation memory.RecordCreationKind
		wantSecond   memory.RecordTransitionKind
	}{
		{"stale second proposal", func(r *MergeRequest, _ []RecordFacts) { r.Proposals[1].ExpectedVersion = 2 }, memory.CreationAllowed, memory.TransitionVersionConflict},
		{"second already active", func(_ *MergeRequest, f []RecordFacts) { f[1].Record.Status = memory.StatusActive }, memory.CreationAllowed, memory.TransitionNotPermitted},
		{"second of another project", func(_ *MergeRequest, f []RecordFacts) { f[1].Record.ProjectID = "p2" }, memory.CreationAllowed, memory.TransitionNotFound},
		{"second missing", func(r *MergeRequest, _ []RecordFacts) { r.Proposals[1].RecordID = "r9" }, memory.CreationAllowed, memory.TransitionNotFound},
		{"second content deleted", func(_ *MergeRequest, f []RecordFacts) { f[1].CurrentContentDeleted = true }, memory.CreationAllowed, memory.TransitionContentDeleted},
		{"empty merged text", func(r *MergeRequest, _ []RecordFacts) { r.Content = " " }, memory.CreationEmptyContent, memory.TransitionAllowed},
		{"agent", func(r *MergeRequest, _ []RecordFacts) { r.Actor = memory.ActorAgent }, memory.CreationActorNotAllowed, memory.TransitionActorNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, request := mergedProposals(), mergeRequest()
			tt.change(&request, stored)
			store := newFakeStore(stored...)
			got, err := service(store).Merge(context.Background(), request)
			if err != nil || got.Applied || got.RecordID != "" || got.Creation.Kind != tt.wantCreation || len(got.Transitions) != 2 ||
				got.Transitions[1].Decision.Kind != tt.wantSecond || got.Transitions[0].RecordVersion != 0 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantNoWrites(t, store)
		})
	}
}
