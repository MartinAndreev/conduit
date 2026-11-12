package records

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
)

// Verification is the hash a human verified for one declared file path.
type Verification struct {
	Path         string
	VerifiedHash string
}

// ReactivateRequest is a human's verification of a needs_verification record
// against the target snapshot.
type ReactivateRequest struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	// Actor is established by the presentation entry point.
	Actor         memory.Actor
	Reviewer      string
	Snapshot      compose.SnapshotRef
	Verifications []Verification
	Now           time.Time
}

// Reactivate returns a needs_verification record to active. It observes each
// declared file source's current hash in the snapshot, inside the
// transaction, and decides with memory.DecideRecordTransition; every refusal
// that does not depend on the sources (memory.PrecheckRecordTransition) is
// decided before the snapshot is read. When allowed it
// writes, in one transaction: a new version with the current content and the
// verified hashes, the approve review naming that version and its content
// hash, and the record's move to active on that version. The content is
// copied, so the new version has the current version's content hash.
func (s Service) Reactivate(ctx context.Context, request ReactivateRequest) (TransitionResult, error) {
	if err := validRequest(request.Now, request.Reviewer); err != nil {
		return TransitionResult{}, err
	}
	var result TransitionResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		stored, err := tx.ReadRecord(ctx, request.RecordID)
		if err != nil {
			return fmt.Errorf("read memory record: %w", err)
		}
		facts := transitionFacts(request.ProjectID, request.ExpectedVersion, memory.StatusActive, request.Actor, stored, request.Now)
		// The snapshot is read only for a record that may make this move, so
		// a missing or foreign record is never observed.
		if precheck := memory.PrecheckRecordTransition(facts); precheck.Kind != memory.TransitionAllowed {
			result = TransitionResult{Kind: precheck.Kind, Reason: precheck.Reason}
			return nil
		}
		verifications, err := s.observeVerifications(ctx, request, stored.Sources)
		if err != nil {
			return err
		}
		versionID, err := s.newID("version")
		if err != nil {
			return err
		}
		newSources := withVerifiedHashes(stored.Sources, request.Verifications)
		facts.Verifications = verifications
		facts.NewVersionID, facts.NewVersionContentHash, facts.NewVersionSources = versionID, stored.CurrentContentHash, newSources
		facts.ApprovedVersionID, facts.ApprovedContentHash = versionID, stored.CurrentContentHash
		decision := memory.DecideRecordTransition(facts)
		result = TransitionResult{Kind: decision.Kind, Reason: decision.Reason}
		if decision.Kind != memory.TransitionAllowed {
			return nil
		}
		if err := s.writeReactivation(ctx, tx, request, stored, versionID, newSources); err != nil {
			return err
		}
		result.RecordVersion, result.VersionID = request.ExpectedVersion+1, versionID
		return nil
	})
	if err != nil {
		return TransitionResult{}, fmt.Errorf("reactivate memory record: %w", err)
	}
	return result, nil
}

// observeVerifications pairs each verification with the current hash of its
// path in the snapshot. Only declared file paths are observed; any other path,
// or one missing or unreadable, has no current hash, so the rule refuses it.
func (s Service) observeVerifications(ctx context.Context, request ReactivateRequest, declared []memory.RecordSource) ([]memory.SourceVerification, error) {
	declaredFiles := make(map[string]bool)
	for _, source := range declared {
		if source.Kind == memory.SourceFile {
			declaredFiles[source.Path] = true
		}
	}
	observer := compose.NewSourceObserver(s.Snapshot, request.Snapshot)
	verifications := make([]memory.SourceVerification, len(request.Verifications))
	for index, verification := range request.Verifications {
		verifications[index] = memory.SourceVerification{Path: verification.Path, VerifiedHash: verification.VerifiedHash}
		if !declaredFiles[verification.Path] {
			continue
		}
		observed, err := observer.Observe(ctx, verification.Path)
		if err != nil {
			return nil, err
		}
		if observed.Observation == memory.SourcePresent {
			verifications[index].CurrentHash = observed.Hash
		}
	}
	return verifications, nil
}

// withVerifiedHashes copies the declared sources, giving each file source the
// hash verified for its path. A path without a verification keeps its
// declared hash, which the rule then refuses.
func withVerifiedHashes(declared []memory.RecordSource, verifications []Verification) []memory.RecordSource {
	verified := make(map[string]string, len(verifications))
	for _, verification := range verifications {
		verified[verification.Path] = verification.VerifiedHash
	}
	sources := make([]memory.RecordSource, len(declared))
	for index, source := range declared {
		if hash, ok := verified[source.Path]; ok && source.Kind == memory.SourceFile {
			source.BlobHash = hash
		}
		sources[index] = source
	}
	return sources
}

// writeReactivation writes an allowed reactivation: the new version, the
// record's move to active on it, and the approve review naming it.
func (s Service) writeReactivation(ctx context.Context, tx Transaction, request ReactivateRequest, stored RecordFacts, versionID string, sources []memory.RecordSource) error {
	now := request.Now.UTC()
	copied := VersionCopy{
		ProjectID:     stored.Record.ProjectID,
		RecordID:      stored.Record.ID,
		FromVersionID: stored.Record.CurrentVersionID,
		VersionID:     versionID,
		Sources:       sources,
		CreatedAt:     now,
	}
	if err := tx.CopyVersion(ctx, copied); err != nil {
		return fmt.Errorf("copy memory version: %w", err)
	}
	change := statusChange(stored, request.ExpectedVersion, memory.StatusActive, now)
	change.CurrentVersionID = versionID
	review := Review{
		VersionID:   versionID,
		ContentHash: stored.CurrentContentHash,
		Decision:    memory.ReviewApprove,
		Actor:       request.Actor,
		Reviewer:    request.Reviewer,
	}
	return s.writeReviewedChange(ctx, tx, change, review)
}
