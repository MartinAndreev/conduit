package compose

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// gather reads the linked records and the search candidates. A linked read
// failure is an error, a search failure degrades search. Missing linked
// records block after both reads, so the search status is still reported.
// Every search hit for a linked record is dropped, whatever its version: the
// linked record is mandatory and search adds nothing to it. The linked read
// and the search may see different snapshots, so a hit can carry a newer
// current version than the linked read; keeping both would put one record in
// the part twice. The linked version is used and the commit recheck catches a
// newer one. Dropped hits are not exclusions and not stale index results.
// Other search hits are deduplicated by record version.
func (c *Composer) gather(ctx context.Context, req Request, out *Composition) ([]*entry, error) {
	linked, err := c.readLinked(ctx, req, out)
	if err != nil {
		return nil, err
	}
	found := c.search(ctx, req, out)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compose memory context: %w", err)
	}
	blockOnMandatory(out, BlockedExplicitNotFound, "a record linked by the assignment does not exist in the project")
	linkedIDs := make(map[string]bool, len(linked))
	for _, e := range linked {
		linkedIDs[e.candidate.Record.ID] = true
	}
	seen := make(map[[2]string]bool, len(found))
	entries := linked
	for _, e := range found {
		key := [2]string{e.candidate.Record.ID, e.candidate.Version.ID}
		if !linkedIDs[key[0]] && !seen[key] {
			seen[key] = true
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// readLinked returns the linked records in request order, once each, and
// records every requested ID the reader did not return as not found. Rows the
// request did not ask for are ignored.
func (c *Composer) readLinked(ctx context.Context, req Request, out *Composition) ([]*entry, error) {
	var ids []string
	for _, id := range req.ExplicitRecordIDs {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	candidates, err := c.memory.ReadLinkedRecords(ctx, req.ProjectID, ids)
	if err != nil {
		return nil, fmt.Errorf("read linked memory records: %w", err)
	}
	byID := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		if _, dup := byID[candidate.Record.ID]; candidate.RecordFound && !dup {
			byID[candidate.Record.ID] = candidate
		}
	}
	entries := make([]*entry, 0, len(ids))
	for _, id := range ids {
		candidate, ok := byID[id]
		if !ok {
			missing := &entry{candidate: Candidate{Record: memory.Record{ID: id}}, mandatory: true}
			exclude(out, missing, StageGather, ExcludedNotFound, "linked record does not exist in the project")
			continue
		}
		entries = append(entries, &entry{candidate: candidate, mandatory: true})
	}
	return entries, nil
}

// search runs the bounded project search unless the query is blank. Its hits
// count as index results: they come from derived search text, so a hit that
// is no longer current is a stale index result.
func (c *Composer) search(ctx context.Context, req Request, out *Composition) []*entry {
	if strings.TrimSpace(req.Query) == "" {
		out.Search = SearchOutcome{Status: SearchDisabled, Reason: "no search query"}
		return nil
	}
	limit := req.ScanLimit
	if limit == 0 {
		limit = DefaultScanLimit
	}
	result, err := c.memory.SearchRecords(ctx, SearchQuery{ProjectID: req.ProjectID, Text: req.Query, ScanLimit: limit})
	if err != nil {
		out.Search = SearchOutcome{Status: SearchDegraded, Reason: "search failed; composition continues without search results", Cause: err}
		return nil
	}
	if reason := invalidSearchResult(result); reason != "" {
		out.Search = SearchOutcome{Status: SearchDegraded, Reason: reason + "; composition continues without search results"}
		return nil
	}
	out.Search = SearchOutcome{Status: SearchOK, Reason: "search completed"}
	if result.LimitReached {
		out.Search = SearchOutcome{Status: SearchDegraded, Reason: "search stopped at its scan limit; later matches were not seen"}
	}
	entries := make([]*entry, 0, len(result.Hits))
	for _, hit := range result.Hits {
		entries = append(entries, &entry{
			candidate: hit.Candidate,
			fromIndex: true,
			relevance: float64(hit.MatchedTerms) / float64(result.QueryTerms),
		})
	}
	return entries
}

// invalidSearchResult rejects a result whose relevance facts are impossible:
// relevance is the share of the query's terms a hit matched.
func invalidSearchResult(result SearchResult) string {
	if result.QueryTerms < 1 {
		return "search reported no query terms"
	}
	for _, hit := range result.Hits {
		if hit.MatchedTerms < 1 || hit.MatchedTerms > result.QueryTerms || hit.Record.ID == "" {
			return "search returned a hit without a record or with an impossible term count"
		}
	}
	return ""
}
