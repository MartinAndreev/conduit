package compose

import (
	"context"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// decideEligibility evaluates every candidate: file sources against the
// target snapshot, then retrieval eligibility. Every candidate's file sources
// are observed first, with one snapshot read. Every exclusion is recorded and
// stale index results are counted. Eligible candidates then lose deleted
// content and get the conflict label, which needs every candidate's verdict.
// A linked record that does not pass blocks the composition.
func (c *Composer) decideEligibility(ctx context.Context, req Request, entries []*entry, out *Composition) ([]*entry, error) {
	var paths []string
	for _, e := range entries {
		paths = append(paths, ObservablePaths(e.candidate.Sources)...)
	}
	observed, err := NewSourceObserver(c.snapshot, req.Snapshot).Observe(ctx, paths)
	if err != nil {
		return nil, err
	}
	verdicts := make(map[string]bool, len(entries))
	var eligible []*entry
	for _, e := range entries {
		applicability := decideApplicability(e.candidate.Sources, observed)
		decision := memory.DecideRetrievalEligibility(memory.RetrievalFacts{
			TargetProjectID:      req.ProjectID,
			CurrentAttemptID:     req.AttemptID,
			RecordFound:          e.candidate.RecordFound,
			Record:               e.candidate.Record,
			Version:              e.candidate.Version,
			ExactVersionApproved: e.candidate.ExactVersionApproved,
			FromIndex:            e.fromIndex,
			Applicability:        applicability,
		})
		recordVerdict(verdicts, e.candidate.Record.ID, decision.Kind)
		if decision.Kind == memory.RetrievalStaleIndexResult {
			out.StaleIndexResults++
		}
		if decision.Kind != memory.RetrievalEligible {
			exclude(out, e, StageEligibility, ExclusionKind(decision.Kind), decision.Reason)
			continue
		}
		e.labels = decision.Labels
		eligible = append(eligible, e)
	}
	var kept []*entry
	for _, e := range eligible {
		if keepEligible(e, verdicts, out) {
			kept = append(kept, e)
		}
	}
	blockOnMandatory(out, BlockedExplicitUnavailable, "a record linked by the assignment is not eligible, has deleted content, or has invalid facts")
	return kept, nil
}

// keepEligible applies the checks that follow eligibility. Deleted content is
// excluded rather than sent as a reference: the agent would get only the
// deleted note's sources, and a linked record must not reach it as less than
// its text.
func keepEligible(e *entry, verdicts map[string]bool, out *Composition) bool {
	if e.candidate.ContentDeleted {
		exclude(out, e, StageContent, ExcludedContentDeleted, "version content was deleted")
		return false
	}
	if !e.mandatory && e.candidate.VersionCreatedAt.IsZero() {
		exclude(out, e, StageEligibility, ExcludedNoRecency, "search candidate has no version time to order it by")
		return false
	}
	conflict := memory.DecideConflictLabel(conflictFacts(e.candidate, verdicts))
	switch conflict.Kind {
	case memory.ConflictLabelled:
		e.labels = append(e.labels, memory.LabelConflict)
	case memory.ConflictNone:
	default:
		exclude(out, e, StageConflict, ExclusionKind(conflict.Kind), conflict.Reason)
		return false
	}
	return true
}

// recordVerdict keeps, per record, whether its current version was found
// eligible. Only verdicts about the current version count: a stale or
// non-current hit and invalid facts say nothing about the record, so a record
// seen only that way stays unevaluated. So does a record the size phase
// excluded, which never reaches eligibility.
func recordVerdict(verdicts map[string]bool, recordID string, kind memory.RetrievalKind) {
	switch kind {
	case memory.RetrievalStaleIndexResult, memory.RetrievalVersionNotCurrent, memory.RetrievalInvalidFacts:
		return
	}
	verdicts[recordID] = verdicts[recordID] || kind == memory.RetrievalEligible
}

func conflictFacts(candidate Candidate, verdicts map[string]bool) memory.ConflictFacts {
	facts := memory.ConflictFacts{RecordID: candidate.Record.ID, CounterpartsTruncated: candidate.ContradictsTruncated}
	for _, related := range candidate.Contradicts {
		eligible, evaluated := verdicts[related.RecordID]
		facts.Counterparts = append(facts.Counterparts, memory.ConflictCounterpart{
			RecordID:  related.RecordID,
			Status:    related.Status,
			Evaluated: evaluated,
			Eligible:  eligible,
		})
	}
	return facts
}

// SourceObserver decides the applicability of declared sources against one
// target snapshot, reading each path once. Composition uses one per
// composition and observes all its paths with one SnapshotReader call; the
// record write path uses it to mark a record stale or verify it.
type SourceObserver struct {
	reader   SnapshotReader
	snapshot SnapshotRef
	seen     map[string]FileObservation
}

// NewSourceObserver observes files of snapshot through reader.
func NewSourceObserver(reader SnapshotReader, snapshot SnapshotRef) *SourceObserver {
	return &SourceObserver{reader: reader, snapshot: snapshot, seen: make(map[string]FileObservation)}
}

// ObservablePaths returns the paths of the well-formed file sources, the only
// ones Applicability observes.
func ObservablePaths(sources []memory.RecordSource) []string {
	var paths []string
	for _, source := range sources {
		if observable(source) {
			paths = append(paths, source.Path)
		}
	}
	return paths
}

func observable(source memory.RecordSource) bool {
	return source.Kind == memory.SourceFile && source.Path != "" && source.BlobHash != ""
}

// Applicability observes every well-formed file source and decides with
// memory.DecideApplicability. A malformed file source is passed unobserved, so
// the rule reports it. Paths not observed yet are read with one call.
func (o *SourceObserver) Applicability(ctx context.Context, sources []memory.RecordSource) (memory.ApplicabilityKind, error) {
	observed, err := o.Observe(ctx, ObservablePaths(sources))
	if err != nil {
		return "", err
	}
	return decideApplicability(sources, observed), nil
}

// decideApplicability pairs each well-formed file source with its observation
// and decides with memory.DecideApplicability.
func decideApplicability(sources []memory.RecordSource, observed map[string]FileObservation) memory.ApplicabilityKind {
	checks := make([]memory.SourceCheck, 0, len(sources))
	for _, source := range sources {
		check := memory.SourceCheck{Source: source}
		if observable(source) {
			check.Observation, check.ObservedHash = observed[source.Path].Observation, observed[source.Path].Hash
		}
		checks = append(checks, check)
	}
	return memory.DecideApplicability(memory.ApplicabilityFacts{Sources: checks}).Kind
}

// Observe returns the observation of every path in the snapshot. The distinct
// paths not observed yet are read with one SnapshotReader call, in the order
// they first appear; when every path was seen, the reader is not called. The
// error never repeats a path.
func (o *SourceObserver) Observe(ctx context.Context, paths []string) (map[string]FileObservation, error) {
	var unseen []string
	pending := make(map[string]bool)
	for _, path := range paths {
		if _, ok := o.seen[path]; !ok && !pending[path] {
			pending[path] = true
			unseen = append(unseen, path)
		}
	}
	if len(unseen) > 0 {
		observed, err := o.reader.ObserveFiles(ctx, o.snapshot, unseen)
		if err != nil {
			// The paths are not repeated: they are memory material that has
			// not been redacted yet.
			return nil, fmt.Errorf("observe memory sources in target snapshot: %w", err)
		}
		if len(observed) != len(unseen) {
			return nil, fmt.Errorf("observe memory sources in target snapshot: reader returned %d observations for %d paths", len(observed), len(unseen))
		}
		for index, path := range unseen {
			o.seen[path] = observed[index]
		}
	}
	result := make(map[string]FileObservation, len(paths))
	for _, path := range paths {
		result[path] = o.seen[path]
	}
	return result, nil
}
