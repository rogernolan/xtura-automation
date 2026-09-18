package tracksite

import (
	"strconv"
	"sync"
	"time"
)

const (
	// mapCacheTTL bounds how long a rendered map is reused for an active
	// track that is still growing.
	mapCacheTTL = 30 * time.Second
	// mapCacheMaxEntries caps memory used for cached map images.
	mapCacheMaxEntries = 64
)

type cacheEntry struct {
	data       []byte
	renderedAt time.Time
}

// Cache stores rendered map PNGs keyed by track name and pixel size.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	now     func() time.Time
}

// NewCache returns an empty map cache.
func NewCache() *Cache {
	return &Cache{entries: make(map[string]cacheEntry), now: time.Now}
}

// Get returns a fresh cached map for name@size, or nil when absent or stale.
func (c *Cache) Get(name string, size int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[cacheKey(name, size)]
	if !ok {
		return nil
	}
	if c.now().Sub(e.renderedAt) > mapCacheTTL {
		return nil
	}
	return e.data
}

// Put stores a rendered map for name@size, evicting the oldest entry when the
// cache is full.
func (c *Cache) Put(name string, size int, data []byte) {
	key := cacheKey(name, size)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= mapCacheMaxEntries {
		var oldestKey string
		var oldest time.Time
		for k, e := range c.entries {
			if oldestKey == "" || e.renderedAt.Before(oldest) {
				oldestKey, oldest = k, e.renderedAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = cacheEntry{data: data, renderedAt: c.now()}
}

func cacheKey(name string, size int) string {
	return name + "@" + strconv.Itoa(size)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}