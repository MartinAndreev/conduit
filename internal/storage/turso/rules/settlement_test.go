package rules

import (
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
)

func TestDecideSettlementRetry(t *testing.T) {
	busy := SettlementRetryFacts{Busy: true, Commit: SettlementCommitNotReached, Attempts: 1}
	for _, tc := range []struct {
		name   string
		change func(*SettlementRetryFacts)
		want   SettlementRetryKind
	}{
		{"busy before commit", func(*SettlementRetryFacts) {}, SettlementRetry},
		{"busy commit rolled back", func(f *SettlementRetryFacts) { f.Commit = SettlementCommitFailedRolledBack }, SettlementRetry},
		{"second attempt", func(f *SettlementRetryFacts) { f.Attempts = 2 }, SettlementRetry},
		{"exhausted", func(f *SettlementRetryFacts) { f.Attempts = SettlementMaxAttempts }, SettlementExhausted},
		{"deadline before bound", func(f *SettlementRetryFacts) { f.DeadlineExceeded, f.Attempts = true, SettlementMaxAttempts }, SettlementDeadline},
		{"not busy before deadline", func(f *SettlementRetryFacts) { f.Busy, f.DeadlineExceeded = false, true }, SettlementNotRetryable},
		{"cleanup failed before busy", func(f *SettlementRetryFacts) { f.CleanupFailed = true }, SettlementNotRetryable},
		{"commit unknown", func(f *SettlementRetryFacts) { f.Commit = SettlementCommitFailedUnknown }, SettlementNotRetryable},
		{"committed is not a failure", func(f *SettlementRetryFacts) { f.Commit = SettlementCommitted }, SettlementNotRetryable},
		{"unknown commit", func(f *SettlementRetryFacts) { f.Commit = "" }, SettlementNotRetryable},
		{"no attempts", func(f *SettlementRetryFacts) { f.Attempts = 0 }, SettlementNotRetryable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := busy
			tc.change(&facts)
			if got := DecideSettlementRetry(facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideSettledOutcome(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	advanced := execution.OutcomeRecord{Outcome: execution.OutcomeAccepted, Classification: execution.ClassAdvanced, Observation: execution.ObservedReceipt,
		Disposition: "transport_accepted", ReceiptKnowledge: "native_receipt_known", Targeting: "native_turn_id", DispatchedFor: execution.DispatchedForMatches,
		Advanced: true, ObservedAt: at}
	diverged := advanced
	diverged.Advanced, diverged.Divergence = false, execution.DivergenceBindingChanged
	for _, tc := range []struct {
		name              string
		found             bool
		stored, requested execution.OutcomeRecord
		want              SettledOutcomeKind
	}{
		{"same", true, advanced, advanced, SettledSame},
		{"same instant, other zone", true, advanced, func() execution.OutcomeRecord {
			r := advanced
			r.ObservedAt = at.In(time.FixedZone("EET", 7200))
			return r
		}(), SettledSame},
		{"stored divergence matches the requested advancement", true, diverged, advanced, SettledSame},
		{"stored divergence does not match a non-advancing request", true, diverged, func() execution.OutcomeRecord { r := advanced; r.Advanced = false; return r }(), SettledConflict},
		{"stored advancement does not match a divergence", true, advanced, diverged, SettledConflict},
		{"other time", true, advanced, func() execution.OutcomeRecord { r := advanced; r.ObservedAt = at.Add(time.Nanosecond); return r }(), SettledConflict},
		{"other classification", true, advanced, func() execution.OutcomeRecord {
			r := advanced
			r.Classification = execution.ClassTransportOnly
			return r
		}(), SettledConflict},
		{"other receipt id", true, advanced, func() execution.OutcomeRecord { r := advanced; r.NativeReceiptID = "x"; return r }(), SettledConflict},
		{"missing", false, execution.OutcomeRecord{}, advanced, SettledMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideSettledOutcome(SettledOutcomeFacts{Found: tc.found, Stored: tc.stored, Requested: tc.requested})
			if got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}
