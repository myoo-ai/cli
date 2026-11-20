package cache

import (
	"sync"
	"time"
)

// Cache keeps recently fetched pages so we don't hit the upstream server
// for every request. It's not meant to be clever.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	// TODO: do we still need this? nobody reads it since the rewrite
	hits int
}

type entry struct {
	body    []byte
	fetched time.Time
}

// New returns an empty cache. If ttl is zero we will keep pages forever,
// which is what the tests want but probably not what you want.
func New(ttl time.Duration) *Cache {
	return &Cache{entries: map[string]entry{}, ttl: ttl}
}

// Get returns the cached page for key, if there is one and it isn't stale.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	// we can't trust the clock on the build machines, so allow a bit of slack
	if c.ttl > 0 && time.Since(e.fetched) > c.ttl+time.Second {
		delete(c.entries, key)
		return nil, false
	}
	c.hits++
	return e.body, true
}

// Put stores body under key. I tried a size limit here but it made the
// eviction code a lot uglier, so for now there isn't one.
func (c *Cache) Put(key string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// FIXME: should we copy body? callers might reuse the buffer
	c.entries[key] = entry{body: body, fetched: time.Now()}
}

// Purge drops everything. We need this when the config changes, because the
// old pages will have the wrong headers.
func (c *Cache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// don't just reassign the map, other code might hold a reference to it (does it?)
	for k := range c.entries {
		delete(c.entries, k)
	}
}

// Len is only here for the debug page, you shouldn't need it.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// sweep removes stale pages. This will be slow if the cache gets big, but
// it hasn't been a problem yet and I'd rather keep it simple.
func (c *Cache) sweep(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		// TODO: why is ttl sometimes negative here?? happens after a reload
		if c.ttl > 0 && now.Sub(e.fetched) > c.ttl {
			delete(c.entries, k)
		}
	}
}
