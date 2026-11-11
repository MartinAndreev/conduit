package compose

import (
	"context"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// decideEligibility evaluates every candidate: file sources against the
// target snapshot, then retrieval eligibility. Every exclusion is recorded and
// stale index results are counted. Eligible candidates then lose deleted
// content and get the conflict label, which needs every candidate's verdict.
// A linked record that does not pass blocks the composition.
func (c *Composer) decideEligibility(ctx context.Context, req Request, entries []*entry, out *Composition) ([]*entry, error) {
	observe := newObserver(c.snapshot, req.Snapshot)
	verdicts := make(map[string]bool, len(entries))
	var eligible []*entry
	for _, e := range entries {
		applicability, err := observe.applicability(ctx, e.candidate.Sources)
		if err != nil {
			return nil, err
		}
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

// observer reads each snapshot path once per composition.
type observer struct {
	reader   SnapshotReader
	snapshot SnapshotRef
	seen     map[string]FileObservation
}

func newObserver(reader SnapshotReader, snapshot SnapshotRef) *observer {
	return &observer{reader: reader, snapshot: snapshot, seen: make(map[string]FileObservation)}
}

// applicability observes every well-formed file source and decides. A
// malformed file source is passed unobserved, so the rule reports it.
func (o *observer) applicability(ctx context.Context, sources []memory.RecordSource) (memory.ApplicabilityKind, error) {
	checks := make([]memory.SourceCheck, 0, len(sources))
	for _, source := range sources {
		check := memory.SourceCheck{Source: source}
		if source.Kind == memory.SourceFile && source.Path != "" && source.BlobHash != "" {
			observed, err := o.observe(ctx, source.Path)
			if err != nil {
				return "", err
			}
			check.Observation, check.ObservedHash = observed.Observation, observed.Hash
		}
		checks = append(checks, check)
	}
	return memory.DecideApplicability(memory.ApplicabilityFacts{Sources: checks}).Kind, nil
}

func (o *observer) observe(ctx context.Context, path string) (FileObservation, error) {
	if observed, ok := o.seen[path]; ok {
		return observed, nil
	}
	observed, err := o.reader.ObserveFile(ctx, o.snapshot, path)
	if err != nil {
		// The path is not repeated: it is memory material that has not been
		// redacted yet.
		return FileObservation{}, fmt.Errorf("observe memory source in target snapshot: %w", err)
	}
	o.seen[path] = observed
	return observed, nil
}
