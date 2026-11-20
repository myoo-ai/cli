package cache

import (
	"sync"
	"time"
)

// Cache holds fetched pages keyed by URL; every entry carries its fetch time
// and expires once its age exceeds the configured TTL.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	// hits counts lookups that returned a fresh entry: one per successful Get.
	hits int
}

// entry pairs a page body with its fetch time; the body is stored as given,
// without copying.
type entry struct {
	body    []byte
	fetched time.Time
}

// New returns an empty cache whose entries live for ttl; a zero ttl keeps
// every entry until Purge.
func New(ttl time.Duration) *Cache {
	return &Cache{entries: map[string]entry{}, ttl: ttl}
}

// Get returns the body stored under key and whether it was found and fresh;
// a stale entry is removed on the spot, so later lookups skip it.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	// One second of slack absorbs clock skew between hosts: a page fetched
	// at the TTL boundary stays valid for exactly one more second.
	if c.ttl > 0 && time.Since(e.fetched) > c.ttl+time.Second {
		delete(c.entries, key)
		return nil, false
	}
	c.hits++
	return e.body, true
}

// Put stores body under key with the current time; an existing entry for
// the same key is replaced, never merged.
func (c *Cache) Put(key string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{body: body, fetched: time.Now()}
}

// Purge removes every entry while keeping the map itself: callers holding
// the cache see an empty map, not a stale one.
func (c *Cache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		delete(c.entries, k)
	}
}

// Len reports the number of entries, fresh and stale alike; it takes the
// lock once and returns without side effects.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// sweep removes entries older than the TTL in a single pass; its cost is
// linear in the number of entries, and each removal happens under the lock.
func (c *Cache) sweep(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		// Entries without a TTL never expire: sweep keeps them, as does Get.
		if c.ttl > 0 && now.Sub(e.fetched) > c.ttl {
			delete(c.entries, k)
		}
	}
}
