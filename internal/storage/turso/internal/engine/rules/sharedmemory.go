// Package rules holds the engine's pure technical policies. It performs no I/O
// and does not import the engine or tursogo.
package rules

import (
	"fmt"
	"time"
)

type SharedMemoryInitKind string

const (
	// SharedMemoryInitNotRetryable returns the failure unchanged.
	SharedMemoryInitNotRetryable SharedMemoryInitKind = "not_retryable"
	SharedMemoryInitRetry        SharedMemoryInitKind = "retry"
	SharedMemoryInitExhausted    SharedMemoryInitKind = "exhausted"
)

// SharedMemoryInitFacts describe a failed open attempt.
type SharedMemoryInitFacts struct {
	MultiprocessWAL bool
	// InitRace is the engine's classification of the failure as the race on
	// creating the <db>-tshm coordination file.
	InitRace bool
	// Attempts counts the attempts made so far, including the failed one.
	Attempts int
}

type SharedMemoryInitDecision struct {
	Kind SharedMemoryInitKind
	// Delay is the wait before the next attempt; set only for retry.
	Delay  time.Duration
	Reason string
}

// SharedMemoryInitMaxAttempts bounds the opens, the first included. With the
// linear backoff below the waits add up to 100ms.
const SharedMemoryInitMaxAttempts = 5

const sharedMemoryInitBackoffStep = 10 * time.Millisecond

// DecideSharedMemoryInitRetry answers whether a failed open is attempted again
// (decision by Martin, 2026-11-10). Only the coordination file creation race,
// and only with multi-process WAL enabled, is retried; any other failure,
// including real corruption, is returned as is. An invalid attempt count is
// not retried.
func DecideSharedMemoryInitRetry(f SharedMemoryInitFacts) SharedMemoryInitDecision {
	switch {
	case !f.MultiprocessWAL:
		return SharedMemoryInitDecision{Kind: SharedMemoryInitNotRetryable, Reason: "multi-process WAL is disabled"}
	case !f.InitRace:
		return SharedMemoryInitDecision{Kind: SharedMemoryInitNotRetryable, Reason: "failure is not the coordination file creation race"}
	case f.Attempts < 1:
		return SharedMemoryInitDecision{Kind: SharedMemoryInitNotRetryable, Reason: fmt.Sprintf("invalid attempt count %d", f.Attempts)}
	case f.Attempts >= SharedMemoryInitMaxAttempts:
		return SharedMemoryInitDecision{Kind: SharedMemoryInitExhausted, Reason: fmt.Sprintf("%d attempts made", f.Attempts)}
	default:
		return SharedMemoryInitDecision{
			Kind:   SharedMemoryInitRetry,
			Delay:  time.Duration(f.Attempts) * sharedMemoryInitBackoffStep,
			Reason: fmt.Sprintf("coordination file was being created by another process (attempt %d of %d)", f.Attempts, SharedMemoryInitMaxAttempts),
		}
	}
}
