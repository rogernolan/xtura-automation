package tracksite

import (
	"testing"
	"time"
)

func TestCacheGetMissingReturnsNil(t *testing.T) {
	c := NewCache()
	if got := c.Get("track-x.geojson", 1000); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestCachePutThenGet(t *testing.T) {
	c := NewCache()
	c.Put("track-x.geojson", 1000, []byte("png-a"))
	if got := c.Get("track-x.geojson", 1000); string(got) != "png-a" {
		t.Fatalf("got %q, want png-a", got)
	}
	if got := c.Get("track-x.geojson", 2000); got != nil {
		t.Fatalf("different size must not hit: %v", got)
	}
}

func TestCacheExpiresAfterTTL(t *testing.T) {
	fake := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	c := NewCache()
	c.now = func() time.Time { return fake }
	c.Put("track-x.geojson", 1000, []byte("png-a"))
	fake = fake.Add(2 * time.Minute)
	if got := c.Get("track-x.geojson", 1000); got != nil {
		t.Fatalf("stale entry still served: %v", got)
	}
}

func TestCacheEvictsOldestWhenFull(t *testing.T) {
	c := NewCache()
	base := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	c.now = func() time.Time { return base }
	for i := 0; i < mapCacheMaxEntries+5; i++ {
		base = base.Add(time.Second)
		c.Put(itoa(i), 1000, []byte("x"))
	}
	if len(c.entries) > mapCacheMaxEntries {
		t.Fatalf("cache grew to %d entries, cap %d", len(c.entries), mapCacheMaxEntries)
	}
	if got := c.Get("0", 1000); got != nil {
		t.Fatalf("oldest entry still served after eviction: %v", got)
	}
}