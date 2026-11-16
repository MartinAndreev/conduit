package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/MartinAndreev/conduit/internal/execution"
)

// The procedures and their storage, harness and fault paths are tested
// against the real Turso store in internal/storage/turso
// (dispatch_*_test.go). Here: what is decided before any port is used.

func TestProceduresRefuseTheOtherKindBeforeAnyIO(t *testing.T) {
	service := Service{} // a nil Store would panic if used
	if _, err := service.DispatchStartTurn(context.Background(), nil, execution.Command{Kind: execution.IntentSteer}); !errors.Is(err, execution.ErrInvalidCommand) {
		t.Fatalf("start with a steer = %v", err)
	}
	if _, err := service.DispatchSteer(context.Background(), nil, execution.Command{Kind: execution.IntentStartTurn}); !errors.Is(err, execution.ErrInvalidCommand) {
		t.Fatalf("steer with a start = %v", err)
	}
	if _, err := service.DispatchStartTurn(context.Background(), nil, execution.Command{Kind: execution.IntentStartTurn}); !errors.Is(err, execution.ErrInvalidCommand) {
		t.Fatalf("invalid start = %v", err)
	}
}

func TestSettlementErrorMatchesItsCauses(t *testing.T) {
	cause := errors.New("busy")
	err := error(&SettlementError{Outcome: execution.OutcomeRecord{Outcome: execution.OutcomeAccepted, Classification: execution.ClassTurnStarted}, Err: cause})
	var settlement *SettlementError
	if !errors.Is(err, ErrSettlementFailed) || !errors.Is(err, cause) || !errors.As(err, &settlement) || settlement.Outcome.Classification != execution.ClassTurnStarted {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceDefaults(t *testing.T) {
	if (Service{}).settlementTimeout() != DefaultSettlementTimeout || DefaultSettlementTimeout.Seconds() != 10 {
		t.Fatal("default settlement budget is not 10 s")
	}
}
