package proposals

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

// fakeQueue returns one prepared page and records the request.
type fakeQueue struct {
	page  QueuePage
	err   error
	calls int
	after records.ProposalPosition
	limit int
}

func (q *fakeQueue) ReadReviewQueue(_ context.Context, projectID string, after records.ProposalPosition, limit int) (QueuePage, error) {
	q.calls++
	q.after, q.limit = after, limit
	return q.page, q.err
}

func queueItem(recordID, attemptID, digest string, minute int) QueueItem {
	item := QueueItem{
		Record:     memory.Record{ID: recordID, ProjectID: "p1", Status: memory.StatusProposed, CreatedAt: reviewTime.Add(time.Duration(minute) * time.Minute)},
		Scope:      memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityAttempt, Origin: memory.OriginAttempt, AttemptID: attemptID},
		Sources:    []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}},
		SearchText: memory.SearchTextIdentity{NormalizationVersion: 1, Digest: digest},
	}
	return item
}

func TestReviewQueueGroupsDuplicatesOnThePage(t *testing.T) {
	first, second, other := queueItem("r1", "a1", "same", 1), queueItem("r2", "a1", "same", 2), queueItem("r3", "a2", "same", 3)
	project := queueItem("r4", "", "same", 4)
	project.Scope = memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityProject, Origin: memory.OriginAttempt, AttemptID: "a3", AcceptedResultRef: "result"}
	renormalized := queueItem("r5", "a1", "same", 5)
	renormalized.SearchText.NormalizationVersion = 2
	distinct := queueItem("r6", "a1", "other", 6)
	distinct.Sources = nil
	invalid := queueItem("r7", "", "same", 7)
	queue := &fakeQueue{page: QueuePage{Items: []QueueItem{first, second, other, project, renormalized, distinct, invalid}, More: true}}
	after := records.ProposalPosition{CreatedAt: reviewTime, RecordID: "r0"}
	got, err := Service{Queue: queue}.ReviewQueue(context.Background(), QueueRequest{ProjectID: "p1", After: after, Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	if queue.after != after || queue.limit != 7 {
		t.Fatalf("read after %+v limit %d", queue.after, queue.limit)
	}
	want := map[string][2][]string{
		"r1": {{"r2"}, {"r3", "r4"}},
		"r2": {{"r1"}, {"r3", "r4"}},
		"r3": {nil, {"r1", "r2", "r4"}},
		"r4": {nil, {"r1", "r2", "r3"}},
		"r5": {nil, nil},
		"r6": {nil, nil},
		"r7": {nil, nil},
	}
	for _, entry := range got.Entries {
		groups := want[entry.Item.Record.ID]
		if !slices.Equal(entry.Duplicates, groups[0]) || !slices.Equal(entry.MergeHints, groups[1]) || entry.NoSource != (entry.Item.Record.ID == "r6") {
			t.Fatalf("entry %s = duplicates %v, hints %v, no source %v", entry.Item.Record.ID, entry.Duplicates, entry.MergeHints, entry.NoSource)
		}
	}
	if len(got.Entries) != 7 || got.Next != (records.ProposalPosition{CreatedAt: invalid.Record.CreatedAt, RecordID: "r7"}) {
		t.Fatalf("queue = %+v", got)
	}
}

func TestReviewQueueEndsWithoutACursor(t *testing.T) {
	for name, page := range map[string]QueuePage{
		"last page":  {Items: []QueueItem{queueItem("r1", "a1", "d", 1)}},
		"empty page": {More: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Service{Queue: &fakeQueue{page: page}}.ReviewQueue(context.Background(), QueueRequest{ProjectID: "p1", Limit: 1})
			if err != nil || got.Next != (records.ProposalPosition{}) {
				t.Fatalf("queue %+v, error %v", got, err)
			}
		})
	}
}

func TestReviewQueueRefusesInvalidRequests(t *testing.T) {
	for name, request := range map[string]QueueRequest{
		"no project":    {Limit: 1},
		"zero limit":    {ProjectID: "p1"},
		"limit too big": {ProjectID: "p1", Limit: MaxQueuePageSize + 1},
	} {
		t.Run(name, func(t *testing.T) {
			queue := &fakeQueue{}
			if _, err := (Service{Queue: queue}).ReviewQueue(context.Background(), request); !errors.Is(err, ErrInvalidQueueRequest) || queue.calls != 0 {
				t.Fatalf("error = %v after %d reads", err, queue.calls)
			}
		})
	}
	if _, err := (Service{Queue: &fakeQueue{}}).ReviewQueue(context.Background(), QueueRequest{ProjectID: "p1", Limit: MaxQueuePageSize}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewQueueReturnsReadErrors(t *testing.T) {
	_, err := Service{Queue: &fakeQueue{err: errInjected}}.ReviewQueue(context.Background(), QueueRequest{ProjectID: "p1", Limit: 1})
	if !errors.Is(err, errInjected) {
		t.Fatalf("error = %v", err)
	}
}
