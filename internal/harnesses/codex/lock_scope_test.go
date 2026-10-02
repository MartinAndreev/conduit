package codex

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestObserveStateClockRunsAfterStateSnapshotUnlock(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	wantTime := time.Unix(123, 0)
	s.config.Now = func() time.Time {
		if !s.mu.TryLock() {
			t.Error("clock callback ran under session mutex")
			return wantTime
		}
		// A callback can inspect/change current state after the observation captured
		// its facts; the returned observation must retain that one state snapshot.
		s.state = provider.SessionClosed
		s.active = provider.None[provider.TurnRef]()
		s.mu.Unlock()
		return wantTime
	}
	snapshot, err := s.ObserveState(context.Background(), provider.ObserveStateRequest{Session: s.ref})
	if err != nil || snapshot.SessionState != provider.SessionOpen || !snapshot.ActiveTurn.Present || snapshot.ActiveTurn.Value.AdapterTurnID != "turn-1" || snapshot.ObservedAt != wantTime {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}

func TestRPCLateCallbacksRunOutsidePendingMutex(t *testing.T) {
	for _, finish := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "transport finish"}[finish], func(t *testing.T) {
			client := newRPCClient(newRPCProcess(&recordingWriteCloser{}, io.NopCloser(&emptyReader{})))
			calls := 0
			client.pending[1] = &pendingRequest{abandoned: true, late: func(reply rpcResponse) {
				calls++
				if !client.mu.TryLock() {
					t.Error("late callback ran under RPC mutex")
					return
				}
				remaining := len(client.pending)
				client.mu.Unlock()
				if remaining != 0 {
					t.Error("late callback saw uncleared pending state")
				}
				if finish && reply.err != io.EOF {
					t.Fatalf("late failure=%v", reply.err)
				}
			}}
			if finish {
				client.finish(io.EOF)
				client.finish(io.ErrUnexpectedEOF)
			} else {
				client.deliverResponse(1, rpcEnvelope{})
				client.deliverResponse(1, rpcEnvelope{})
			}
			if calls != 1 {
				t.Fatalf("late callback calls=%d", calls)
			}
		})
	}
}
