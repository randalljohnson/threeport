package handlers

import (
	"math/rand"
	"sync"
	"time"
)

// nameCacheTTL is how long a resolved (objectType, id) -> Name entry
// stays valid in the in-process cache. Kept short so rename/delete
// churn on hot object types shows up in the events feed within one
// TTL window without an explicit invalidation path.
const nameCacheTTL = 30 * time.Second

// nameCacheSweepInterval is how often the background sweeper evicts
// expired entries. Set to twice the TTL so most entries age out through
// the sweeper rather than at read time.
const nameCacheSweepInterval = 60 * time.Second

// nameCacheMaxEntries caps the total number of cached entries. Above
// this cap, the sweeper drops entries at random until the size is back
// under the cap. Random-drop avoids the bookkeeping cost of full LRU
// while still bounding worst-case memory under a burst of distinct ids.
const nameCacheMaxEntries = 5000

// nameCacheKey identifies a cached name by its fully qualified object
// type and numeric id. Keeping the key value-typed lets it live in a
// plain map without a hash helper.
type nameCacheKey struct {
	ObjType string
	ID      uint
}

// nameCacheEntry is one cached resolved name plus the deadline after
// which it stops counting as a hit.
type nameCacheEntry struct {
	Name      string
	ExpiresAt time.Time
}

// nameCache holds (objectType, id) -> Name entries with per-entry TTL
// and a size cap. Reads take the RLock; writes and sweeps take the
// exclusive lock.
type nameCache struct {
	mu      sync.RWMutex
	entries map[nameCacheKey]nameCacheEntry
}

// moduleNameCache is the process-wide cache consulted by the events
// enrichment path before dispatching to core SQL or module HTTP.
var moduleNameCache = &nameCache{
	entries: make(map[nameCacheKey]nameCacheEntry, nameCacheMaxEntries),
}

// Get returns the cached name for (objType, id) if one is present and
// not expired. A miss returns ("", false); a stale entry counts as a
// miss even before the sweeper evicts it.
func (c *nameCache) Get(objType string, id uint) (string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[nameCacheKey{ObjType: objType, ID: id}]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		return "", false
	}
	return entry.Name, true
}

// Put stores name for (objType, id) with a fresh TTL. When the write
// would push the cache above nameCacheMaxEntries, evict enough entries
// (chosen by map-iteration order) to make room first.
func (c *nameCache) Put(objType string, id uint, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= nameCacheMaxEntries {
		c.evictRandomLocked(len(c.entries) - nameCacheMaxEntries + 1)
	}
	c.entries[nameCacheKey{ObjType: objType, ID: id}] = nameCacheEntry{
		Name:      name,
		ExpiresAt: time.Now().Add(nameCacheTTL),
	}
}

// sweep drops every entry whose deadline has passed, then enforces the
// size cap by evicting extra entries at random when the map is still
// oversized. Runs under the exclusive lock so concurrent Get and Put
// see a consistent view.
func (c *nameCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) > nameCacheMaxEntries {
		c.evictRandomLocked(len(c.entries) - nameCacheMaxEntries)
	}
}

// evictRandomLocked drops n entries chosen by map-iteration order,
// which Go randomizes across ranges. Caller must hold the exclusive
// lock. A non-positive n is a no-op.
func (c *nameCache) evictRandomLocked(n int) {
	if n <= 0 {
		return
	}
	dropped := 0
	for k := range c.entries {
		delete(c.entries, k)
		dropped++
		if dropped >= n {
			return
		}
	}
}

// init starts the background sweeper. The goroutine lives for the
// lifetime of the process; the api-server has no shutdown hook that
// would benefit from tearing it down.
func init() {
	// small jitter on the first tick so multiple processes started at
	// once don't synchronize their sweep passes
	go func() {
		jitter := time.Duration(rand.Int63n(int64(nameCacheSweepInterval)))
		time.Sleep(jitter)
		ticker := time.NewTicker(nameCacheSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			moduleNameCache.sweep()
		}
	}()
}
