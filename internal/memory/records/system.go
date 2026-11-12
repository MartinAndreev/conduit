package records

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
)

// ErrInvalidSweep reports a sweep without the current time or a positive
// limit.
var ErrInvalidSweep = errors.New("proposal sweep needs the current time and a positive limit")

// SweepCursor continues a sweep across batches: the cutoff the sweep started
// with, kept fixed until the scan ends, and the last proposal examined. The
// zero cursor starts a new sweep at memory.ProposalRetentionCutoff(now).
type SweepCursor struct {
	Cutoff time.Time
	After  ProposalPosition
}

// SweepRequest is one batch of the retention sweep.
type SweepRequest struct {
	Now    time.Time
	Limit  int
	Cursor SweepCursor
}

// SweepResult counts one batch: the proposals examined, those archived, and
// the refusals by kind. Archived lists the archived record IDs. Next
// continues the sweep after this batch; it is zero when the scan reached the
// end, so the next sweep starts again from the oldest proposal.
type SweepResult struct {
	Examined int
	Archived []string
	Refused  map[memory.RecordTransitionKind]int
	Next     SweepCursor
}

// ArchiveExpiredProposals is one batch of the system's retention sweep. In
// one transaction it reads at most Limit proposals created at or before the
// sweep's cutoff and after the cursor, oldest first, decides each with
// memory.DecideRecordTransition as actor system, and archives those allowed:
// 90 days old without a human decision. A refused proposal is counted, not
// written; the cursor moves past it, so refused proposals never fill later
// batches of the same sweep. An error writes nothing.
func (s Service) ArchiveExpiredProposals(ctx context.Context, request SweepRequest) (SweepResult, error) {
	if request.Now.IsZero() || request.Limit <= 0 {
		return SweepResult{}, ErrInvalidSweep
	}
	cutoff := request.Cursor.Cutoff
	if cutoff.IsZero() {
		cutoff = memory.ProposalRetentionCutoff(request.Now)
	}
	var result SweepResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		result = SweepResult{Refused: make(map[memory.RecordTransitionKind]int)}
		positions, err := tx.ReadExpiredProposals(ctx, cutoff, request.Cursor.After, request.Limit)
		if err != nil {
			return fmt.Errorf("read expired proposals: %w", err)
		}
		for _, position := range positions {
			if err := s.archiveExpiredProposal(ctx, tx, position.RecordID, request.Now, &result); err != nil {
				return err
			}
		}
		if len(positions) == request.Limit {
			result.Next = SweepCursor{Cutoff: cutoff, After: positions[len(positions)-1]}
		}
		return nil
	})
	if err != nil {
		return SweepResult{}, fmt.Errorf("archive expired memory proposals: %w", err)
	}
	return result, nil
}

// archiveExpiredProposal reads one proposal, decides, and archives it or
// counts the refusal. The sweep runs for every project and expects the
// version it just read under the write lock.
func (s Service) archiveExpiredProposal(ctx context.Context, tx Transaction, recordID string, now time.Time, result *SweepResult) error {
	stored, err := tx.ReadRecord(ctx, recordID)
	if err != nil {
		return fmt.Errorf("read memory record: %w", err)
	}
	result.Examined++
	decision := memory.DecideRecordTransition(transitionFacts(stored.Record.ProjectID, stored.Record.Version, memory.StatusArchived, memory.ActorSystem, stored, now))
	if decision.Kind != memory.TransitionAllowed {
		result.Refused[decision.Kind]++
		return nil
	}
	if err := tx.ChangeStatus(ctx, statusChange(stored, stored.Record.Version, memory.StatusArchived, now)); err != nil {
		return fmt.Errorf("archive expired proposal: %w", err)
	}
	result.Archived = append(result.Archived, recordID)
	return nil
}

// VerificationRequest names one record to check against a target snapshot.
type VerificationRequest struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	Snapshot        compose.SnapshotRef
	Now             time.Time
}

// MarkNeedsVerification is the system's staleness check of one active record:
// it observes the current version's declared file sources in the snapshot,
// decides with memory.DecideRecordTransition and, when a source differs or is
// missing or unreadable, moves the record to needs_verification. Every
// refusal that does not depend on the sources (memory.PrecheckRecordTransition)
// is decided before the snapshot is read. The snapshot is read inside the
// transaction, under the write lock, so the sources observed are the ones the
// decision is about. A snapshot error writes nothing.
func (s Service) MarkNeedsVerification(ctx context.Context, request VerificationRequest) (TransitionResult, error) {
	if request.Now.IsZero() {
		return TransitionResult{}, ErrInvalidRequest
	}
	var result TransitionResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		stored, err := tx.ReadRecord(ctx, request.RecordID)
		if err != nil {
			return fmt.Errorf("read memory record: %w", err)
		}
		facts := transitionFacts(request.ProjectID, request.ExpectedVersion, memory.StatusNeedsVerification, memory.ActorSystem, stored, request.Now)
		// The snapshot is read only for a record that may make this move, so
		// a missing or foreign record is never observed.
		if precheck := memory.PrecheckRecordTransition(facts); precheck.Kind != memory.TransitionAllowed {
			result = TransitionResult{Kind: precheck.Kind, Reason: precheck.Reason}
			return nil
		}
		applicability, err := compose.NewSourceObserver(s.Snapshot, request.Snapshot).Applicability(ctx, stored.Sources)
		if err != nil {
			return err
		}
		facts.SourceApplicability = applicability
		decision := memory.DecideRecordTransition(facts)
		result = TransitionResult{Kind: decision.Kind, Reason: decision.Reason}
		if decision.Kind != memory.TransitionAllowed {
			return nil
		}
		if err := tx.ChangeStatus(ctx, statusChange(stored, request.ExpectedVersion, memory.StatusNeedsVerification, request.Now)); err != nil {
			return fmt.Errorf("change memory record status: %w", err)
		}
		result.RecordVersion = request.ExpectedVersion + 1
		return nil
	})
	if err != nil {
		return TransitionResult{}, fmt.Errorf("mark memory record for verification: %w", err)
	}
	return result, nil
}
