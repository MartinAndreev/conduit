package proposals

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

func batchRequest(decisions ...Decision) BatchRequest {
	return BatchRequest{ProjectID: "p1", Actor: memory.ActorHuman, Reviewer: "martin", Decisions: decisions, Now: reviewTime}
}

func approveDecision(recordID string) Decision {
	return Decision{Kind: DecisionApprove, RecordID: recordID, ExpectedVersion: 1, VersionID: recordID + "-v", ContentHash: "sha256:" + recordID}
}

func rejectDecision(recordID, reason string) Decision {
	return Decision{Kind: DecisionReject, RecordID: recordID, ExpectedVersion: 1, Reason: reason}
}

func mergeDecision(recordIDs ...string) Decision {
	merged := make([]records.MergedProposal, len(recordIDs))
	for index, recordID := range recordIDs {
		merged[index] = records.MergedProposal{RecordID: recordID, ExpectedVersion: 1}
	}
	return Decision{Kind: DecisionMerge, Merge: MergeDecision{Proposals: merged, Category: memory.CategoryConvention, Content: "Обединено правило"}}
}

func reviewStore() *fakeRecords {
	return newFakeRecords(proposed("r1"), proposed("r2"), proposed("r3"), proposed("r4"), proposed("r5"))
}

// Each decision is its own transaction; refusals are results and the batch
// goes on.
func TestReviewBatchAppliesEachDecisionOnItsOwn(t *testing.T) {
	store := reviewStore()
	stale := approveDecision("r2")
	stale.ExpectedVersion = 7
	got, err := proposalService(store).ReviewBatch(context.Background(), batchRequest(
		approveDecision("r1"),
		stale,
		rejectDecision("r3", "дублира spec 012"),
		rejectDecision("r2", " "),
		mergeDecision("r4", "r5"),
		mergeDecision("r1", "r2"),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind    DecisionKind
		applied bool
	}{{DecisionApprove, true}, {DecisionApprove, false}, {DecisionReject, true}, {DecisionReject, false}, {DecisionMerge, true}, {DecisionMerge, false}}
	if len(got.Results) != len(want) || store.transactions != len(want) {
		t.Fatalf("results %+v in %d transactions", got.Results, store.transactions)
	}
	for index, result := range got.Results {
		if result.Kind != want[index].kind || result.Applied != want[index].applied {
			t.Fatalf("result %d = %+v", index, result)
		}
	}
	if got.Results[1].Transition.Kind != memory.TransitionVersionConflict || got.Results[3].Transition.Kind != memory.TransitionReasonRequired {
		t.Fatalf("refusals = %+v, %+v", got.Results[1], got.Results[3])
	}
	if merged := got.Results[4].Merge; merged.RecordID != "record-3" || merged.Transitions[0].RecordVersion != 2 {
		t.Fatalf("merge = %+v", merged)
	}
	// r1 moved to version 2 with the first decision, so the second merge is
	// refused.
	if refused := got.Results[5].Merge; refused.Transitions[0].Decision.Kind != memory.TransitionVersionConflict || refused.Transitions[1].Decision.Kind != memory.TransitionAllowed {
		t.Fatalf("second merge = %+v", refused)
	}
	wantWrites(t, store,
		"status r1 proposed->active", "review r1-v approve",
		"status r3 proposed->rejected", "review r3-v reject",
		"insert record-3 active human_approved project/", "review version-4 approve",
		"status r4 proposed->rejected", "review r4-v reject",
		"status r5 proposed->rejected", "review r5-v reject",
	)
	for _, review := range store.reviews {
		if review.Actor != memory.ActorHuman || review.Reviewer != "martin" {
			t.Fatalf("review = %+v", review)
		}
	}
	if reason := store.reviews[len(store.reviews)-1].Reason; reason != memory.MergedReason("record-3") {
		t.Fatalf("merge reason = %q", reason)
	}
}

// A technical error stops the batch: the decisions before it stay applied
// and are reported, and later ones are not attempted.
func TestReviewBatchStopsAtAnError(t *testing.T) {
	for name, failing := range map[string]Decision{
		"approve": approveDecision("r2"),
		"reject":  rejectDecision("r2", "x"),
		"merge":   mergeDecision("r2", "r3"),
	} {
		t.Run(name, func(t *testing.T) {
			store := reviewStore()
			store.failTransaction = 2
			got, err := proposalService(store).ReviewBatch(context.Background(), batchRequest(approveDecision("r1"), failing, approveDecision("r4")))
			if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "review decision 1") {
				t.Fatalf("error = %v", err)
			}
			if len(got.Results) != 1 || !got.Results[0].Applied || store.transactions != 2 {
				t.Fatalf("results %+v after %d transactions", got.Results, store.transactions)
			}
		})
	}
}

func TestReviewBatchStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := reviewStore()
	got, err := proposalService(store).ReviewBatch(ctx, batchRequest(approveDecision("r1")))
	if !errors.Is(err, context.Canceled) || got.Results != nil || store.transactions != 0 {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

// An invalid batch applies nothing, even its valid decisions.
func TestReviewBatchRefusesInvalidBatches(t *testing.T) {
	unknown := approveDecision("r2")
	unknown.Kind = "archive"
	for name, request := range map[string]BatchRequest{
		"unknown kind": batchRequest(approveDecision("r1"), unknown),
		"no reviewer":  {ProjectID: "p1", Actor: memory.ActorHuman, Decisions: []Decision{approveDecision("r1")}, Now: reviewTime},
		"no time":      {ProjectID: "p1", Actor: memory.ActorHuman, Reviewer: "martin", Decisions: []Decision{approveDecision("r1")}},
	} {
		t.Run(name, func(t *testing.T) {
			store := reviewStore()
			if _, err := proposalService(store).ReviewBatch(context.Background(), request); !errors.Is(err, ErrInvalidBatch) || store.transactions != 0 {
				t.Fatalf("error = %v after %d transactions", err, store.transactions)
			}
		})
	}
}

func TestReviewBatchWithoutDecisions(t *testing.T) {
	got, err := proposalService(reviewStore()).ReviewBatch(context.Background(), batchRequest())
	if err != nil || got.Results != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

// A reviewer over the identity bound is each decision's refusal, not an
// error, so the batch goes on; nothing is written.
func TestReviewBatchContinuesAfterReviewerRefusals(t *testing.T) {
	store := reviewStore()
	request := batchRequest(approveDecision("r1"), rejectDecision("r2", "x"), mergeDecision("r3", "r4"))
	request.Reviewer = strings.Repeat("ж", memory.MaxIdentityBytes/2) + "x"
	got, err := proposalService(store).ReviewBatch(context.Background(), request)
	if err != nil || len(got.Results) != 3 || store.transactions != 3 || len(store.writes) != 0 {
		t.Fatalf("results %+v, error %v, writes %v", got.Results, err, store.writes)
	}
	if got.Results[0].Transition.Kind != memory.TransitionInvalidFacts || got.Results[1].Transition.Kind != memory.TransitionInvalidFacts ||
		got.Results[2].Merge.Creation.Kind != memory.CreationInvalidFacts {
		t.Fatalf("results = %+v", got.Results)
	}
}
