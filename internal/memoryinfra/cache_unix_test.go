//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package memoryinfra

import (
	"testing"
	"time"
)

func otherNativeStat() nativeStat { return nativeStat{ino: 1} }

// The racy rule also checks the change time, which a write updates even
// when the modification time is set back.
func TestDecideSnapshotCacheStoreRacyChangeTime(t *testing.T) {
	start := time.Now()
	fingerprint := fileFingerprint{size: 1, mtime: start.Add(-time.Hour).UnixNano(), native: nativeStat{ctime: start.UnixNano()}}
	got := decideSnapshotCacheStore(snapshotCacheStoreFacts{
		Observation: presentAs(fakeHash("a")),
		BeforeKnown: true, Before: fingerprint,
		SameLeaf:   true,
		AfterKnown: true, After: fingerprint,
		CallStart: start,
	})
	if got.Kind != snapshotCacheSkipRacy {
		t.Fatalf("got %+v", got)
	}
}
