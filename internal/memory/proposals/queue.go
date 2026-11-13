package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

// MaxQueuePageSize is the largest review queue page: each item carries its
// full content, up to memory.MaxStoredContentBytes.
const MaxQueuePageSize = 100

// ErrInvalidQueueRequest reports a queue request without a project or with a
// page size outside 1..MaxQueuePageSize.
var ErrInvalidQueueRequest = errors.New("review queue request needs a project and a page size from 1 to the maximum")

// QueueReader reads the review queue. It only reads; the rules decide.
type QueueReader interface {
	// ReadReviewQueue reads, in one consistent snapshot, at most limit
	// proposed records of the project whose current version's content is not
	// deleted, strictly after the position after in (created_at, id) order,
	// in that order, and whether more follow. A zero after starts at the
	// oldest.
	ReadReviewQueue(ctx context.Context, projectID string, after records.ProposalPosition, limit int) (QueuePage, error)
}

// QueueItem is one proposal with its current version.
type QueueItem struct {
	// Record.CurrentVersionID and Record.Version are what an approval names
	// and expects.
	Record      memory.Record
	ContentHash string
	Content     string
	Trust       memory.Trust
	Scope       memory.Scope
	Sources     []memory.RecordSource
	// SearchText identifies the current version's stored search text.
	SearchText memory.SearchTextIdentity
}

// QueuePage is one read of the queue.
type QueuePage struct {
	Items []QueueItem
	More  bool
}

// QueueRequest asks for one page after a position; a zero After starts at
// the oldest proposal.
type QueueRequest struct {
	ProjectID string
	After     records.ProposalPosition
	Limit     int
}

// QueueEntry is one proposal on a page. NoSource labels a proposal without
// any source: it entered as agent_claim with nothing to check it against.
// Duplicates lists the page's other proposals in the same scope with the
// same normalized text; MergeHints those in another scope (another attempt
// or visibility), which only hint at a human merge. Both are in page order.
type QueueEntry struct {
	Item       QueueItem
	NoSource   bool
	Duplicates []string
	MergeHints []string
}

// Queue is one page. Next continues after it and is zero at the end.
type Queue struct {
	Entries []QueueEntry
	Next    records.ProposalPosition
}

// ReviewQueue reads one page of the project's proposals awaiting review and
// groups its duplicates with memory.DecideDeduplication over
// memory.SameSearchText. Grouping is per page: proposals on different pages
// are not compared. Nothing about duplicates is stored, and trust never
// transfers through a match.
func (s Service) ReviewQueue(ctx context.Context, request QueueRequest) (Queue, error) {
	if request.ProjectID == "" || request.Limit < 1 || request.Limit > MaxQueuePageSize {
		return Queue{}, ErrInvalidQueueRequest
	}
	page, err := s.Queue.ReadReviewQueue(ctx, request.ProjectID, request.After, request.Limit)
	if err != nil {
		return Queue{}, fmt.Errorf("read review queue: %w", err)
	}
	queue := Queue{Entries: make([]QueueEntry, len(page.Items))}
	for index, item := range page.Items {
		queue.Entries[index] = QueueEntry{Item: item, NoSource: len(item.Sources) == 0}
		for other, candidate := range page.Items {
			if other == index {
				continue
			}
			switch decideDuplicate(item, candidate).Kind {
			case memory.DeduplicationDuplicate:
				queue.Entries[index].Duplicates = append(queue.Entries[index].Duplicates, candidate.Record.ID)
			case memory.DeduplicationMergeHint:
				queue.Entries[index].MergeHints = append(queue.Entries[index].MergeHints, candidate.Record.ID)
			}
		}
	}
	if page.More && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1].Record
		queue.Next = records.ProposalPosition{CreatedAt: last.CreatedAt, RecordID: last.ID}
	}
	return queue, nil
}

// decideDuplicate compares two proposals. Invalid stored scope facts are
// neither duplicates nor hints.
func decideDuplicate(left, right QueueItem) memory.DeduplicationDecision {
	return memory.DecideDeduplication(memory.DeduplicationFacts{
		Left:           deduplicationSubject(left),
		Right:          deduplicationSubject(right),
		ContentMatches: memory.SameSearchText(left.SearchText, right.SearchText),
	})
}

func deduplicationSubject(item QueueItem) memory.DeduplicationSubject {
	return memory.DeduplicationSubject{ProjectID: item.Record.ProjectID, Visibility: item.Scope.Visibility, AttemptID: item.Scope.AttemptID}
}
