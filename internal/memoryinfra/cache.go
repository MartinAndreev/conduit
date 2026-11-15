package memoryinfra

import (
	"container/list"
	"context"
	"io/fs"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
)

const (
	// snapshotCacheTTL bounds how long a stored hash is trusted, from the
	// moment it was stored. It also bounds the accepted risk that a change
	// git sees but the file's stat does not (git filters, .gitattributes,
	// git configuration) keeps an old hash.
	snapshotCacheTTL = 10 * time.Minute
	// snapshotCacheEntries bounds the cache across all workspaces; the least
	// recently used entry is evicted first.
	snapshotCacheEntries = 8192
	// racyMargin: a file whose modification (or, on Unix, change) time is
	// not strictly older than the call's start minus this margin is not
	// stored, as git does for racily clean index entries. A write in the
	// same timestamp granule as the lstat would otherwise keep the old
	// fingerprint; 2s covers filesystems with coarse timestamps.
	racyMargin = 2 * time.Second
)

// fileFingerprint is what the cache knows about a file without reading it:
// the leaf's lstat facts. A hit needs every field equal. native is the
// platform part (fingerprint_unix.go, fingerprint_windows.go).
type fileFingerprint struct {
	size   int64
	mode   fs.FileMode
	mtime  int64 // nanoseconds since the Unix epoch
	native nativeStat
}

// fingerprintOf returns the fingerprint of an lstat result; ok is false when
// the platform facts are not available (the FileInfo did not come from the
// OS), and the cache is then neither consulted nor filled for that file.
func fingerprintOf(info fs.FileInfo) (fileFingerprint, bool) {
	native, ok := nativeStatOf(info)
	if !ok {
		return fileFingerprint{}, false
	}
	return fileFingerprint{size: info.Size(), mode: info.Mode(), mtime: info.ModTime().UnixNano(), native: native}, true
}

// newestChange is the latest of the timestamps the racy rule checks: the
// modification time and, where the platform has one, the change time.
func (f fileFingerprint) newestChange() int64 {
	return max(f.mtime, f.native.changeTime())
}

// snapshotCacheKey identifies a file: the resolved workspace root and the
// cleaned path relative to it. The fingerprint is kept in the entry, so a
// changed file replaces its entry instead of leaving a stale one behind.
type snapshotCacheKey struct {
	root  string
	clean string
}

type snapshotCacheEntry struct {
	key         snapshotCacheKey
	fingerprint fileFingerprint
	hash        string
	stored      time.Time
}

// snapshotCache is the process-lifetime, in-memory stat cache of
// WorkspaceSnapshots: blob hashes of files whose fingerprint is unchanged
// since a successful hash. Nothing is persisted. Its mutex is never held
// across git or filesystem calls.
type snapshotCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	entries  map[snapshotCacheKey]*list.Element
	// recency holds *snapshotCacheEntry, most recently used first.
	recency *list.List
}

func newSnapshotCache(capacity int, ttl time.Duration) *snapshotCache {
	return &snapshotCache{capacity: capacity, ttl: ttl, entries: map[snapshotCacheKey]*list.Element{}, recency: list.New()}
}

// lookup returns the stored hash for key when the entry's fingerprint equals
// current and it has not expired at now. A stale or expired entry is
// dropped; a hit becomes the most recently used.
func (c *snapshotCache) lookup(key snapshotCacheKey, current fileFingerprint, now time.Time) (hash string, hit bool) {
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		element, found := c.entries[key]
		facts := snapshotCacheLookupFacts{Found: found, Current: current, Now: now, TTL: c.ttl}
		if found {
			entry := element.Value.(*snapshotCacheEntry)
			facts.Stored, facts.StoredAt = entry.fingerprint, entry.stored
		}
		switch decideSnapshotCacheLookup(facts).Kind {
		case snapshotCacheHit:
			c.recency.MoveToFront(element)
			hash, hit = element.Value.(*snapshotCacheEntry).hash, true
		case snapshotCacheStale, snapshotCacheExpired:
			c.recency.Remove(element)
			delete(c.entries, key)
		}
		return nil
	})
	return hash, hit
}

// storeAll publishes one call's entries in one critical section: nothing
// when ctx is cancelled once the lock is held (the error is returned),
// otherwise every entry. Each becomes the most recently used, replacing an
// entry for the same key; the least recently used entries over the capacity
// are evicted.
func (c *snapshotCache) storeAll(ctx context.Context, entries []snapshotCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return lockscope.WithMutexLock(&c.mu, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, entry := range entries {
			if element, found := c.entries[entry.key]; found {
				element.Value = &entry
				c.recency.MoveToFront(element)
				continue
			}
			c.entries[entry.key] = c.recency.PushFront(&entry)
			// Evict per insertion, so a call with more paths than the
			// capacity never grows the map or the list past it.
			if c.recency.Len() > c.capacity {
				oldest := c.recency.Back()
				c.recency.Remove(oldest)
				delete(c.entries, oldest.Value.(*snapshotCacheEntry).key)
			}
		}
		return nil
	})
}

// snapshotCacheLookupKind is the verdict on a cache lookup.
type snapshotCacheLookupKind string

const (
	// snapshotCacheHit: the stored hash is the file's hash; git is skipped.
	snapshotCacheHit snapshotCacheLookupKind = "hit"
	// snapshotCacheMiss: nothing is stored for the file.
	snapshotCacheMiss snapshotCacheLookupKind = "miss"
	// snapshotCacheStale: the file's fingerprint changed; the entry is
	// dropped.
	snapshotCacheStale snapshotCacheLookupKind = "stale"
	// snapshotCacheExpired: the entry outlived the TTL; it is dropped.
	snapshotCacheExpired snapshotCacheLookupKind = "expired"
)

type snapshotCacheLookupFacts struct {
	Found    bool
	Stored   fileFingerprint
	StoredAt time.Time
	Current  fileFingerprint
	Now      time.Time
	TTL      time.Duration
}

type snapshotCacheLookupDecision struct {
	Kind   snapshotCacheLookupKind
	Reason string
}

// decideSnapshotCacheLookup decides whether a stored entry answers for the
// file. In order: no entry is a miss; an entry at or past its TTL is expired
// (a clock that went backwards does not extend it: an entry stored in the
// future is expired too); a different fingerprint is stale; otherwise a hit.
func decideSnapshotCacheLookup(f snapshotCacheLookupFacts) snapshotCacheLookupDecision {
	switch age := f.Now.Sub(f.StoredAt); {
	case !f.Found:
		return snapshotCacheLookupDecision{Kind: snapshotCacheMiss, Reason: "no entry for the file"}
	case age < 0 || age >= f.TTL:
		return snapshotCacheLookupDecision{Kind: snapshotCacheExpired, Reason: "entry is outside its time to live"}
	case f.Stored != f.Current:
		return snapshotCacheLookupDecision{Kind: snapshotCacheStale, Reason: "file's fingerprint changed"}
	}
	return snapshotCacheLookupDecision{Kind: snapshotCacheHit, Reason: "file's fingerprint is unchanged"}
}

// snapshotCacheStoreKind is the verdict on storing one hashed observation.
type snapshotCacheStoreKind string

const (
	snapshotCacheStore           snapshotCacheStoreKind = "store"
	snapshotCacheSkipNotPresent  snapshotCacheStoreKind = "not_present"
	snapshotCacheSkipUnknownStat snapshotCacheStoreKind = "unknown_stat"
	snapshotCacheSkipRacy        snapshotCacheStoreKind = "racy"
	snapshotCacheSkipMoved       snapshotCacheStoreKind = "moved"
	snapshotCacheSkipChanged     snapshotCacheStoreKind = "changed"
)

// snapshotCacheStoreFacts: the observation git produced for an admitted
// path; the fingerprint from its lstat before hashing (Before, when
// BeforeKnown); the lookup after git returned, repeated the way the rules
// did it: SameLeaf when the file rule still admits the path and its
// directory resolves to the same leaf, and the leaf's fingerprint (After,
// when AfterKnown); and the call's start, taken before any lstat of the
// call.
type snapshotCacheStoreFacts struct {
	Observation compose.FileObservation
	BeforeKnown bool
	Before      fileFingerprint
	SameLeaf    bool
	AfterKnown  bool
	After       fileFingerprint
	CallStart   time.Time
}

type snapshotCacheStoreDecision struct {
	Kind   snapshotCacheStoreKind
	Reason string
}

// decideSnapshotCacheStore decides whether an observation may be stored. In
// order: only a present observation with a valid blob hash; only with both
// fingerprints known; only when the file's newest timestamp is strictly older
// than CallStart minus racyMargin; only when the lookup after hashing still
// admits the same leaf (a directory symlink on the path may have been
// switched while git read through it); only when that leaf's fingerprint is
// identical and still a regular file, not a symlink. Anything else is returned to the
// caller for this call but not stored.
func decideSnapshotCacheStore(f snapshotCacheStoreFacts) snapshotCacheStoreDecision {
	skip := func(kind snapshotCacheStoreKind, reason string) snapshotCacheStoreDecision {
		return snapshotCacheStoreDecision{Kind: kind, Reason: reason}
	}
	switch {
	case f.Observation.Observation != memory.SourcePresent || !memory.ValidBlobHash(f.Observation.Hash):
		return skip(snapshotCacheSkipNotPresent, "only a present file with a valid hash is stored")
	case !f.BeforeKnown || !f.AfterKnown:
		return skip(snapshotCacheSkipUnknownStat, "file's fingerprint is not known before and after hashing")
	case f.Before.newestChange() >= f.CallStart.Add(-racyMargin).UnixNano():
		return skip(snapshotCacheSkipRacy, "file changed too close to the call's start")
	case !f.SameLeaf:
		return skip(snapshotCacheSkipMoved, "path no longer resolves to the same admitted file")
	case f.After != f.Before || !f.After.mode.IsRegular():
		return skip(snapshotCacheSkipChanged, "file changed while it was hashed")
	}
	return snapshotCacheStoreDecision{Kind: snapshotCacheStore, Reason: "file was stable while it was hashed"}
}
