package rules

import (
	"testing"
	"time"
)

func TestDecideSharedMemoryInitRetry(t *testing.T) {
	for name, tc := range map[string]struct {
		facts SharedMemoryInitFacts
		want  SharedMemoryInitKind
		delay time.Duration
	}{
		"first race":             {SharedMemoryInitFacts{MultiprocessWAL: true, InitRace: true, Attempts: 1}, SharedMemoryInitRetry, 10 * time.Millisecond},
		"last retry":             {SharedMemoryInitFacts{MultiprocessWAL: true, InitRace: true, Attempts: 4}, SharedMemoryInitRetry, 40 * time.Millisecond},
		"bound reached":          {SharedMemoryInitFacts{MultiprocessWAL: true, InitRace: true, Attempts: 5}, SharedMemoryInitExhausted, 0},
		"beyond bound":           {SharedMemoryInitFacts{MultiprocessWAL: true, InitRace: true, Attempts: 6}, SharedMemoryInitExhausted, 0},
		"flag off":               {SharedMemoryInitFacts{InitRace: true, Attempts: 1}, SharedMemoryInitNotRetryable, 0},
		"other failure":          {SharedMemoryInitFacts{MultiprocessWAL: true, Attempts: 1}, SharedMemoryInitNotRetryable, 0},
		"other failure at bound": {SharedMemoryInitFacts{MultiprocessWAL: true, Attempts: 5}, SharedMemoryInitNotRetryable, 0},
		"invalid attempt count":  {SharedMemoryInitFacts{MultiprocessWAL: true, InitRace: true}, SharedMemoryInitNotRetryable, 0},
		"flag off beats bound":   {SharedMemoryInitFacts{InitRace: true, Attempts: 5}, SharedMemoryInitNotRetryable, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got := DecideSharedMemoryInitRetry(tc.facts)
			if got.Kind != tc.want || got.Delay != tc.delay || got.Reason == "" {
				t.Fatalf("decision = %#v, want %s after %v", got, tc.want, tc.delay)
			}
		})
	}
}
