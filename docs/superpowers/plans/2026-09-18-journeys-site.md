# Journeys Web Site RSS Feed Implementation Plan

> **Revised 2026-09-18** after live review: Tasks 1 and 2 (PNG map renderer and
> cache) and the `/maps/*.png` routes were removed in `6425d6f`. The shipped
> surface is: feed items carry text plus a single `application/geo+json`
> enclosure and link to `/blog/{name}`, an interactive Leaflet page rendering
> the route in the browser over OpenStreetMap tiles. See `docs/gps-tracking.md`
> for the current surface.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a small web site to `empirebusd` that offers an RSS feed of GPS journeys (track files), each item carrying a 1000x1000 map PNG, a link to a lazily-generated 2000x2000 map, and a link to download the track GeoJSON, so the InstaBlog agent can download the image or the GeoJSON.

**Architecture:** Two new pure units in a new package `service/tracksite` — a map renderer (`map.go`) that draws the track route to a PNG with only the Go standard library, and an RSS 2.0 feed builder (`feed.go`) — plus a small TTL cache and three HTTP handlers in the existing `service/api/httpapi` package. All routes mount on the existing `empirebusd` mux, which is already served HTTPS-only over Tailscale Serve, so no config, port, or systemd changes are needed.

**Tech Stack:** Go 1.25, stdlib only (`image`, `image/draw`, `image/png`, `encoding/json`, `encoding/xml`, `html`, `net/http`). No new dependencies.

## Global Constraints

- No new Go dependencies; map images render entirely offline with the standard library.
- Thumbnail map images are exactly 1000x1000 px; the larger map is exactly 2000x2000 px.
- Feed item titles use UTC times in the form `YYYY-MM-DD HH:MM - HH:MM` (e.g. `2026-08-13 09:40 - 10:15`).
- Each item has two `<enclosure>` elements: `image/png` (1000x1000 map) and `application/geo+json` (`/v1/tracks/{name}`).
- Feed URLs/enclosures are absolute, built from the request `Host` header and `X-Forwarded-Proto` (Tailscale Serve sets it), falling back to `http://` in development.
- `GET /maps/{name}.png` serves 1000x1000; `GET /maps/{name}@2000.png` serves the larger image, rendered only on first request then cached (lazy).
- A corrupt/unparsable track is skipped from the feed (never fails the whole feed). An empty track list still yields a valid RSS document with no items.
- Existing routes (`/v1/...`, `/static/...`, `/`, `/ui`, `/v1/events`) must keep working; `GET /rss.xml`, `GET /maps/{name}.png`, and `GET /blog/` only respond to GET (other methods → `405`).
- Interface note vs the spec: `BuildFeed` takes a `[]Item` (with per-item MapPNG/GeoJSON byte lengths) rather than the spec's `([]tracking.FileInfo, map[string]int64)`; the httpapi handler builds `Item` values from `TrackList()` output plus rendered PNG sizes. Behavior is unchanged.
- Run `go test ./...` (or the targeted package tests) after each task. No JS is touched, so the eslint rtk step is not needed.

---

### Task 1: Track map PNG renderer (`service/tracksite/map.go`)

**Files:**
- Create: `service/tracksite/map.go`
- Test: `service/tracksite/map_test.go`

**Interfaces:**
- Consumes: nothing (reads a GeoJSON document `[]byte`).
- Produces: `func RenderTrack(data []byte, size int) ([]byte, error)` — returns PNG bytes of exactly `size` x `size`, or an error for invalid `size`, unparsable JSON, or a track with no route geometry.

- [ ] **Step 1: Write the failing tests**

Create `service/tracksite/map_test.go`:

```go
package tracksite

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
)

const sampleLineTrack = `{
  "type": "Feature",
  "properties": {
    "name": "track-2026-08-13-0940-1015.geojson",
    "start_time": "2026-08-13T09:40:05Z",
    "end_time": "2026-08-13T10:15:20Z",
    "point_count": 4,
    "sample_interval_seconds": 5,
    "times": ["2026-08-13T09:40:05Z", "2026-08-13T09:40:10Z", "2026-08-13T09:40:15Z", "2026-08-13T09:40:20Z"]
  },
  "geometry": {
    "type": "LineString",
    "coordinates": [[0.854362, 51.065375], [0.8544, 51.0655], [0.85445, 51.0656], [0.8545, 51.0657]]
  }
}`

const sampleEventTrack = `{
  "type": "FeatureCollection",
  "features": [
    {"type": "Feature", "properties": {"name": "track-x.geojson", "start_time": "2026-08-13T09:40:05Z", "end_time": "2026-08-13T09:40:05Z", "point_count": 1, "sample_interval_seconds": 5, "times": ["2026-08-13T09:40:05Z"]}, "geometry": {"type": "Point", "coordinates": [0.854362, 51.065375]}},
    {"type": "Feature", "properties": {"event": "engine_on", "time": "2026-08-13T09:40:05Z"}, "geometry": {"type": "Point", "coordinates": [0.854362, 51.065375]}}
  ]
}`

func decodePNGRect(t *testing.T, data []byte, wantSize int) (image.Image, int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	rect := img.Bounds()
	if rect.Dx() != wantSize || rect.Dy() != wantSize {
		t.Fatalf("map size %dx%d, want %dx%d", rect.Dx(), rect.Dy(), wantSize, wantSize)
	}
	return img, 0
}

func countNonBackground(img image.Image, rect image.Rectangle) int {
	background := color.RGBA{0xF3, 0xF1, 0xEA, 0xFF}
	count := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 != background.R || g>>8 != background.G || b>>8 != background.B {
				count++
			}
		}
	}
	return count
}

func TestRenderTrackProducesThumbnailSize(t *testing.T) {
	data, err := RenderTrack([]byte(sampleLineTrack), 1000)
	if err != nil {
		t.Fatalf("render 1000: %v", err)
	}
	img, _ := decodePNGRect(t, data, 1000)
	if count := countNonBackground(img, img.Bounds()); count == 0 {
		t.Fatal("map is blank; expected route pixels")
	}
}

func TestRenderTrackProducesLargeSize(t *testing.T) {
	data, err := RenderTrack([]byte(sampleLineTrack), 2000)
	if err != nil {
		t.Fatalf("render 2000: %v", err)
	}
	_, _ = decodePNGRect(t, data, 2000)
}

func TestRenderTrackDeterministic(t *testing.T) {
	first, err := RenderTrack([]byte(sampleLineTrack), 1000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderTrack([]byte(sampleLineTrack), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("map output is not deterministic")
	}
}

func TestRenderTrackSinglePointWithEvent(t *testing.T) {
	data, err := RenderTrack([]byte(sampleEventTrack), 1000)
	if err != nil {
		t.Fatalf("render single point track: %v", err)
	}
	img, _ := decodePNGRect(t, data, 1000)
	if count := countNonBackground(img, img.Bounds()); count == 0 {
		t.Fatal("map is blank; expected markers")
	}
}

func TestRenderTrackDegenerateSinglePoint(t *testing.T) {
	track := `{"type":"Feature","properties":{"name":"p.geojson"},"geometry":{"type":"Point","coordinates":[0.854362,51.065375]}}`
	data, err := RenderTrack([]byte(track), 1000)
	if err != nil {
		t.Fatalf("render degenerate track: %v", err)
	}
	_, _ = decodePNGRect(t, data, 1000)
}

func TestRenderTrackRejectsBadInput(t *testing.T) {
	if _, err := RenderTrack([]byte(`{not json`), 1000); err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	if _, err := RenderTrack([]byte(sampleLineTrack), 0); err == nil {
		t.Fatal("expected error for size 0")
	}
	if _, err := RenderTrack([]byte(`{"type":"FeatureCollection","features":[]}`), 1000); err == nil {
		t.Fatal("expected error for empty feature collection")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./service/tracksite/`
Expected: FAIL — package `tracksite` does not exist yet.

- [ ] **Step 3: Write the implementation**

Create `service/tracksite/map.go`:

```go
// Package tracksite renders journey track files as RSS feed documents and
// map images for a small web site consumed by the InstaBlog agent.
package tracksite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

var (
	bgColor        = color.RGBA{0xF3, 0xF1, 0xEA, 0xFF}
	gridColor      = color.RGBA{0xE4, 0xE0, 0xD4, 0xFF}
	borderColor    = color.RGBA{0xC9, 0xC3, 0xB4, 0xFF}
	routeColor     = color.RGBA{0x1F, 0x5F, 0xD4, 0xFF}
	startColor     = color.RGBA{0x1E, 0x8E, 0x3E, 0xFF}
	endColor       = color.RGBA{0xD9, 0x30, 0x25, 0xFF}
	eventColor     = color.RGBA{0x5F, 0x63, 0x68, 0xFF}
	outlineColor   = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// RenderTrack renders a track GeoJSON document as a size x size PNG map
// image. The track may be a single Feature with a LineString (or Point)
// geometry, or a FeatureCollection whose LineString feature is the route and
// whose Point features carrying a properties.event are engine waypoints.
func RenderTrack(data []byte, size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid map size %d", size)
	}
	route, events, err := parseTrack(data)
	if err != nil {
		return nil, err
	}
	if len(route) == 0 {
		return nil, fmt.Errorf("track contains no route geometry")
	}
	img := renderCanvas(route, events, size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode map: %w", err)
	}
	return buf.Bytes(), nil
}

type point struct{ x, y float64 }

type trackGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type trackProperties struct {
	Event string `json:"event"`
}

type trackFeature struct {
	Type       string          `json:"type"`
	Properties trackProperties `json:"properties"`
	Geometry   trackGeometry   `json:"geometry"`
}

type trackTop struct {
	Type     string            `json:"type"`
	Features []json.RawMessage `json:"features"`
}

func parseTrack(data []byte) (route [][]float64, events [][]float64, err error) {
	var top trackTop
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, fmt.Errorf("parse track: %w", err)
	}
	switch top.Type {
	case "Feature":
		return featurePoints(data)
	case "FeatureCollection":
		var fallback [][]float64
		for _, raw := range top.Features {
			var f trackFeature
			if err := json.Unmarshal(raw, &f); err != nil {
				continue
			}
			switch f.Geometry.Type {
			case "LineString":
				route = append(route, lineCoordinates(f.Geometry.Coordinates)...)
			case "Point":
				pt := pointCoordinates(f.Geometry.Coordinates)
				if pt == nil {
					continue
				}
				if f.Properties.Event != "" {
					events = append(events, pt)
				} else if len(route) == 0 && len(fallback) == 0 {
					fallback = append(fallback, pt)
				}
			}
		}
		if len(route) == 0 {
			route = fallback
		}
		return route, events, nil
	default:
		return nil, nil, fmt.Errorf("unsupported track type %q", top.Type)
	}
}

func featurePoints(data []byte) ([][]float64, [][]float64, error) {
	var f trackFeature
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil, fmt.Errorf("parse feature: %w", err)
	}
	switch f.Geometry.Type {
	case "LineString":
		return lineCoordinates(f.Geometry.Coordinates), nil, nil
	case "Point", "MultiPoint":
		pt := pointCoordinates(f.Geometry.Coordinates)
		if pt == nil {
			return nil, nil, fmt.Errorf("invalid point geometry")
		}
		return [][]float64{pt}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported geometry type %q", f.Geometry.Type)
	}
}

func lineCoordinates(raw json.RawMessage) [][]float64 {
	var pts [][]float64
	_ = json.Unmarshal(raw, &pts)
	out := make([][]float64, 0, len(pts))
	for _, p := range pts {
		if len(p) >= 2 {
			out = append(out, []float64{p[0], p[1]})
		}
	}
	return out
}

func pointCoordinates(raw json.RawMessage) []float64 {
	var p []float64
	_ = json.Unmarshal(raw, &p)
	if len(p) >= 2 {
		return []float64{p[0], p[1]}
	}
	return nil
}

// renderCanvas draws the route and markers onto a size x size RGBA canvas.
func renderCanvas(route, events [][]float64, size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(bgColor), image.Point{}, draw.Src)
	drawGrid(img, size)
	routePts, eventPts := project(route, events, size)
	lineWidth := clamp(size/160, 3, 24)
	if len(routePts) > 0 {
		for i := 0; i+1 < len(routePts); i++ {
			strokeLine(img, routePts[i], routePts[i+1], lineWidth+2, outlineColor)
		}
		for i := 0; i+1 < len(routePts); i++ {
			strokeLine(img, routePts[i], routePts[i+1], lineWidth, routeColor)
		}
	}
	markerRadius := clamp(size/60, 6, 40)
	for _, ep := range eventPts {
		stampDisc(img, ep, markerRadius/2+2, outlineColor)
		stampDisc(img, ep, markerRadius/2, eventColor)
	}
	if len(routePts) > 0 {
		start := routePts[0]
		stampDisc(img, start, markerRadius+2, outlineColor)
		stampDisc(img, start, markerRadius, startColor)
		end := routePts[len(routePts)-1]
		stampDisc(img, end, markerRadius+2, outlineColor)
		stampDisc(img, end, markerRadius, endColor)
	}
	strokeBorder(img, size, borderColor)
	return img
}

func drawGrid(img *image.RGBA, size int) {
	for i := 1; i < 8; i++ {
		x := i * size / 8
		for y := 0; y < size; y++ {
			img.Set(x, y, gridColor)
		}
		yy := i * size / 8
		for x := 0; x < size; x++ {
			img.Set(x, yy, gridColor)
		}
	}
}

// project maps lon/lat coordinates to canvas pixels with a Web Mercator fit
// to the combined bounds, padded so the drawing fills ~80% of the canvas.
// Degenerate bounds (single point) fall back to a one arc-minute span.
func project(route, events [][]float64, size int) ([]point, []point) {
	all := make([][]float64, 0, len(route)+len(events))
	all = append(all, route...)
	all = append(all, events...)
	if len(all) == 0 {
		return nil, nil
	}
	minX, maxX, minY, maxY := mercatorBounds(all)
	const minSpan = 1.0 / 60.0 // one arc-minute fallback in projected units
	spanX := math.Max(maxX-minX, minSpan)
	spanY := math.Max(maxY-minY, minSpan)
	scale := float64(size) * 0.8 / math.Max(spanX, spanY)
	midX, midY := (minX+maxX)/2, (minY+maxY)/2
	offset := float64(size) / 2
	toPoint := func(p []float64) point {
		wx, wy := mercator(p[0], p[1])
		return point{x: offset + (wx - midX) * scale, y: offset - (wy - midY) * scale}
	}
	routeOut := make([]point, 0, len(route))
	for _, p := range route {
		routeOut = append(routeOut, toPoint(p))
	}
	eventsOut := make([]point, 0, len(events))
	for _, p := range events {
		eventsOut = append(eventsOut, toPoint(p))
	}
	return routeOut, eventsOut
}

func mercatorBounds(points [][]float64) (minX, maxX, minY, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range points {
		x, y := mercator(p[0], p[1])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return
}

func mercator(lon, lat float64) (x, y float64) {
	lonRad := lon * math.Pi / 180
	latRad := lat * math.Pi / 180
	return lonRad, math.Log(math.Tan(math.Pi/4 + latRad/2))
}

func strokeLine(img *image.RGBA, a, b point, width int, c color.RGBA) {
	if width <= 0 {
		return
	}
	dx, dy := b.x-a.x, b.y-a.y
	dist := math.Hypot(dx, dy)
	if dist == 0 {
		stampDisc(img, a, width/2, c)
		return
	}
	steps := int(math.Ceil(dist * 2))
	radius := float64(width) / 2
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		stampDisc(img, point{x: a.x + dx*t, y: a.y + dy*t}, int(radius), c)
	}
}

func stampDisc(img *image.RGBA, center point, radius int, c color.RGBA) {
	if radius < 0 {
		return
	}
	cx, cy := int(math.Round(center.x)), int(math.Round(center.y))
	b := img.Bounds()
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			x, y := cx+dx, cy+dy
			if x < b.Min.X || x > b.Max.X-1 || y < b.Min.Y || y > b.Max.Y-1 {
				continue
			}
			img.Set(x, y, c)
		}
	}
}

func strokeBorder(img *image.RGBA, size int, c color.RGBA) {
	if size < 4 {
		return
	}
	for i := 0; i < 2; i++ {
		lo := i
		hi := size - 1 - i
		for px := lo; px <= hi; px++ {
			img.Set(px, lo, c)
			img.Set(px, hi, c)
		}
		for py := lo; py <= hi; py++ {
			img.Set(lo, py, c)
			img.Set(hi, py, c)
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./service/tracksite/`
Expected: PASS (all tests above).

- [ ] **Step 5: Commit**

```bash
git add service/tracksite/map.go service/tracksite/map_test.go
git commit -m "feat: render journey track maps as PNG images"
```

---

### Task 2: Bounded TTL cache for rendered maps (`service/tracksite/cache.go`)

**Files:**
- Create: `service/tracksite/cache.go`
- Test: `service/tracksite/cache_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func NewCache() *Cache`, `func (c *Cache) Get(name string, size int) []byte` (nil when absent or stale), `func (c *Cache) Put(name string, size int, data []byte)`. The `now` field is a `func() time.Time` the tests override.

- [ ] **Step 1: Write the failing tests**

Create `service/tracksite/cache_test.go`:

```go
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
	if _, ok := c.entries["0"]; ok {
		t.Fatal("oldest entry was not evicted")
	}
}
```

Note: `TestCacheEvictsOldestWhenFull` uses the unexported `itoa` (defined in cache.go) and `mapCacheMaxEntries`; same-package tests can use them. Also note `cacheKey("0", 1000)` and `"0"` differ — the test asserts `c.entries["0"]` directly, which is fine because cacheKey for a single-digit name is `"0@1000"`, so "0" was never a key. Use a distinct approach: track the first key and assert it is gone via `c.Get`.

Replace the final assertion block with this equivalent (use `Get`, which is the public surface):

```go
	if got := c.Get("0", 1000); got != nil {
		t.Fatalf("oldest entry still served after eviction: %v", got)
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./service/tracksite/ -run TestCache`
Expected: FAIL — `NewCache` undefined.

- [ ] **Step 3: Write the implementation**

Create `service/tracksite/cache.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./service/tracksite/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/tracksite/cache.go service/tracksite/cache_test.go
git commit -m "feat: add bounded TTL cache for rendered track maps"
```

---

### Task 3: RSS 2.0 feed builder (`service/tracksite/feed.go`)

**Files:**
- Create: `service/tracksite/feed.go`
- Test: `service/tracksite/feed_test.go`

**Interfaces:**
- Consumes: `time.Time` values for `Item`.
- Produces: `type Item struct { Name string; StartTime, EndTime time.Time; MapPNGBytes, GeoJSONBytes int64 }` and `func BuildFeed(baseURL string, items []Item) ([]byte, error)` — an RSS 2.0 document. Titles are `YYYY-MM-DD HH:MM - HH:MM` UTC; channel `lastBuildDate` is the newest item's EndTime.

- [ ] **Step 1: Write the failing tests**

Create `service/tracksite/feed_test.go`:

```go
package tracksite

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type rssItem struct {
	Title       string          `xml:"title"`
	Link        string          `xml:"link"`
	GUID        string          `xml:"guid"`
	PubDate     string          `xml:"pubDate"`
	Description string          `xml:"description"`
	Enclosures  []rssEnclosure  `xml:"enclosure"`
}

type rssChannel struct {
	Title         string   `xml:"title"`
	Link          string   `xml:"link"`
	Description   string   `xml:"description"`
	LastBuildDate string   `xml:"lastBuildDate"`
	Items         []rssItem `xml:"item"`
}

type rssDoc struct {
	XMLName xml.Name  `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

func mustParseRSS(t *testing.T, data []byte) rssDoc {
	t.Helper()
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse rss: %v\n%s", err, data)
	}
	return doc
}

func testItems() []Item {
	start1 := time.Date(2026, 8, 13, 9, 40, 5, 0, time.UTC)
	end1 := time.Date(2026, 8, 13, 10, 15, 20, 0, time.UTC)
	start2 := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	end2 := time.Date(2026, 8, 12, 9, 10, 5, 0, time.UTC)
	return []Item{
		{Name: "track-2026-08-13-0940-1015.geojson", StartTime: start1, EndTime: end1, MapPNGBytes: 999, GeoJSONBytes: 123},
		{Name: "track-2026-08-12-0800-0910.geojson", StartTime: start2, EndTime: end2, MapPNGBytes: 888, GeoJSONBytes: 111},
	}
}

func TestBuildFeedItemShape(t *testing.T) {
	doc := mustParseRSS(t, mustBuildFeed(t, "https://xtura.example.ts.net", testItems()))
	if len(doc.Channel.Items) != 2 {
		t.Fatalf("item count = %d", len(doc.Channel.Items))
	}
	first := doc.Channel.Items[0]
	if first.Title != "2026-08-13 09:40 - 10:15" {
		t.Fatalf("first title = %q", first.Title)
	}
	if first.PubDate != "Thu, 13 Aug 2026 10:15:20 +0000" {
		t.Fatalf("pubDate = %q", first.PubDate)
	}
	if first.GUID != "https://xtura.example.ts.net/v1/tracks/track-2026-08-13-0940-1015.geojson" {
		t.Fatalf("guid = %q", first.GUID)
	}
	if len(first.Enclosures) != 2 {
		t.Fatalf("enclosures = %d, want 2", len(first.Enclosures))
	}
	img := first.Enclosures[0]
	if img.Type != "image/png" || img.Length != "999" {
		t.Fatalf("image enclosure = %#v", img)
	}
	if img.URL != "https://xtura.example.ts.net/maps/track-2026-08-13-0940-1015.geojson.png" {
		t.Fatalf("image enclosure url = %q", img.URL)
	}
	geo := first.Enclosures[1]
	if geo.Type != "application/geo+json" || geo.Length != "123" {
		t.Fatalf("geojson enclosure = %#v", geo)
	}
	if geo.URL != "https://xtura.example.ts.net/v1/tracks/track-2026-08-13-0940-1015.geojson" {
		t.Fatalf("geojson enclosure url = %q", geo.URL)
	}
}

func TestBuildFeedDescriptionContents(t *testing.T) {
	doc := mustParseRSS(t, mustBuildFeed(t, "https://xtura.example.ts.net", testItems()))
	desc := doc.Channel.Items[0].Description
	for _, want := range []string{
		`<img src="https://xtura.example.ts.net/maps/track-2026-08-13-0940-1015.geojson.png" width="1000" height="1000"`,
		`<a href="https://xtura.example.ts.net/maps/track-2026-08-13-0940-1015.geojson@2000.png">View</a>`,
		`<a href="https://xtura.example.ts.net/v1/tracks/track-2026-08-13-0940-1015.geojson">Download</a>`,
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q: %s", want, desc)
		}
	}
}

func TestBuildFeedChannelMetadata(t *testing.T) {
	doc := mustParseRSS(t, mustBuildFeed(t, "https://xtura.example.ts.net", nil))
	if doc.Channel.Title != "Xtura journeys" {
		t.Fatalf("channel title = %q", doc.Channel.Title)
	}
	if doc.Channel.Link != "https://xtura.example.ts.net/rss.xml" {
		t.Fatalf("channel link = %q", doc.Channel.Link)
	}
	if doc.Channel.LastBuildDate == "" {
		t.Fatal("channel lastBuildDate missing")
	}
}

func TestBuildFeedOrderingPreserved(t *testing.T) {
	items := testItems()
	doc := mustParseRSS(t, mustBuildFeed(t, "https://x.example.net", items))
	if doc.Channel.Items[0].Title != "2026-08-13 09:40 - 10:15" {
		t.Fatalf("item 0 = %q", doc.Channel.Items[0].Title)
	}
	if doc.Channel.Items[1].Title != "2026-08-12 08:00 - 09:10" {
		t.Fatalf("item 1 = %q", doc.Channel.Items[1].Title)
	}
}

func mustBuildFeed(t *testing.T, base string, items []Item) []byte {
	t.Helper()
	data, err := BuildFeed(base, items)
	if err != nil {
		t.Fatalf("build feed: %v", err)
	}
	return data
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./service/tracksite/ -run TestBuildFeed`
Expected: FAIL — `BuildFeed` undefined.

- [ ] **Step 3: Write the implementation**

Create `service/tracksite/feed.go`:

```go
package tracksite

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"time"
)

// Item is one journey feed entry.
type Item struct {
	Name         string
	StartTime    time.Time
	EndTime      time.Time
	MapPNGBytes  int64
	GeoJSONBytes int64
}

const feedTitle = "Xtura journeys"

// BuildFeed renders the RSS 2.0 document for the given journey items. baseURL
// is the origin (scheme and host) from which absolute URLs are built; the
// caller passes items newest-first.
func BuildFeed(baseURL string, items []Item) ([]byte, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	lastBuild := time.Now().UTC()
	if len(items) > 0 {
		lastBuild = items[0].EndTime.UTC()
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0">` + "\n<channel>\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", xmlEscape(feedTitle))
	fmt.Fprintf(&b, "<link>%s</link>\n", xmlEscape(baseURL+"/rss.xml"))
	fmt.Fprintf(&b, "<description>%s</description>\n", xmlEscape("Recent driving sessions recorded by Xtura"))
	fmt.Fprintf(&b, "<lastBuildDate>%s</lastBuildDate>\n", rssTime(lastBuild))
	b.WriteString("<ttl>15</ttl>\n")
	for _, item := range items {
		writeItem(&b, baseURL, item)
	}
	b.WriteString("</channel>\n</rss>\n")
	return b.Bytes(), nil
}

func writeItem(b *bytes.Buffer, baseURL string, item Item) {
	thumb := baseURL + "/maps/" + item.Name + ".png"
	big := baseURL + "/maps/" + item.Name + "@2000.png"
	geo := baseURL + "/v1/tracks/" + item.Name
	title := trackTitle(item.StartTime, item.EndTime)
	b.WriteString("<item>\n")
	fmt.Fprintf(b, "<title>%s</title>\n", xmlEscape(title))
	fmt.Fprintf(b, "<link>%s</link>\n", xmlEscape(big))
	fmt.Fprintf(b, "<guid isPermaLink=\"false\">%s</guid>\n", xmlEscape(geo))
	fmt.Fprintf(b, "<pubDate>%s</pubDate>\n", rssTime(item.EndTime))
	fmt.Fprintf(b, "<enclosure url=\"%s\" length=\"%d\" type=\"image/png\"/>\n", xmlEscape(thumb), item.MapPNGBytes)
	fmt.Fprintf(b, "<enclosure url=\"%s\" length=\"%d\" type=\"application/geo+json\"/>\n", xmlEscape(geo), item.GeoJSONBytes)
	description := fmt.Sprintf(
		"<p><img src=\"%s\" width=\"1000\" height=\"1000\" alt=\"%s\"/></p>\n"+
			"<p>Larger map: <a href=\"%s\">View</a> \u00b7 GeoJSON: <a href=\"%s\">Download</a></p>",
		thumb, xmlEscape(title), big, geo,
	)
	fmt.Fprintf(b, "<description><![CDATA[%s]]></description>\n", description)
	b.WriteString("</item>\n")
}

func trackTitle(start, end time.Time) string {
	return fmt.Sprintf("%s - %s",
		start.UTC().Format("2006-01-02 15:04"),
		end.UTC().Format("15:04"))
}

// rssTime renders a UTC timestamp in RSS 2.0 RFC-822 form, e.g.
// "Thu, 13 Aug 2026 10:15:20 +0000".
func rssTime(t time.Time) string {
	return t.UTC().Format(time.RFC1123Z)
}

func xmlEscape(s string) string {
	return html.EscapeString(s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./service/tracksite/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/tracksite/feed.go service/tracksite/feed_test.go
git commit -m "feat: build journeys RSS 2.0 feed"
```

---

### Task 4: HTTP routes for the journeys site (`service/api/httpapi`)

**Files:**
- Create: `service/api/httpapi/rss.go`
- Create: `service/api/httpapi/maps.go`
- Create: `service/api/httpapi/blog.go`
- Modify: `service/api/httpapi/server.go` (struct field, `New`, imports, route registration)
- Test: `service/api/httpapi/site_test.go`

**Interfaces:**
- Consumes: `tracksite.RenderTrack`, `tracksite.NewCache`, `tracksite.BuildFeed`, `tracksite.Item`, plus the existing `Application.TrackList()` / `Application.TrackRead(name)` (already on `Server.app`).
- Produces: three handlers `handleRSSFeed`, `handleTrackMap`, `handleBlogIndex`; route patterns `/rss.xml`, `/maps/{name}.png`, `/blog/`. `siteBaseURL(r)` returns `X-Forwarded-Proto://Host` (defaulting the scheme to `http`).

- [ ] **Step 1: Write the failing tests**

Create `service/api/httpapi/site_test.go`:

```go
package httpapi

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/tracking"
)

const siteTestTrack = `{
  "type": "Feature",
  "properties": {
    "name": "track-2026-08-13-0940-1015.geojson",
    "start_time": "2026-08-13T09:40:05Z",
    "end_time": "2026-08-13T10:15:20Z",
    "point_count": 2,
    "sample_interval_seconds": 5,
    "times": ["2026-08-13T09:40:05Z", "2026-08-13T10:15:20Z"]
  },
  "geometry": {
    "type": "LineString",
    "coordinates": [[0.854362, 51.065375], [0.8545, 51.0657]]
  }
}`

func siteTestFileInfo() []tracking.FileInfo {
	start := time.Date(2026, 8, 13, 9, 40, 5, 0, time.UTC)
	end := time.Date(2026, 8, 13, 10, 15, 20, 0, time.UTC)
	return []tracking.FileInfo{{
		Name:       "track-2026-08-13-0940-1015.geojson",
		Bytes:      123,
		StartTime:  &start,
		EndTime:    &end,
		PointCount: 2,
	}}
}

func decodePNGSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	r := img.Bounds()
	return r.Dx(), r.Dy()
}

func TestRSSFeedRoute(t *testing.T) {
	app := fakeApp{
		broker:        events.NewBroker(1),
		trackFiles:    siteTestFileInfo(),
		trackReadData: []byte(siteTestTrack),
	}
	server := New(app).Handler()
	req := httptest.NewRequest(http.MethodGet, "/rss.xml", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "rss+xml") {
		t.Fatalf("content type = %q", ct)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"https://example.com/rss.xml",
		"<title>2026-08-13 09:40 - 10:15</title>",
		`<enclosure url="https://example.com/maps/track-2026-08-13-0940-1015.geojson.png" length="3" type="image/png"`,
		`<enclosure url="https://example.com/v1/tracks/track-2026-08-13-0940-1015.geojson" length="123" type="application/geo+json"`,
		`https://example.com/maps/track-2026-08-13-0940-1015.geojson@2000.png`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("feed body missing %q: %s", want, body)
		}
	}
}

func TestRSSFeedEmptyAndSkippedTracks(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)}).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/rss.xml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("empty feed status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<item>") {
		t.Fatal("empty feed must contain no items")
	}

	app := fakeApp{
		broker:        events.NewBroker(1),
		trackFiles:    siteTestFileInfo(),
		trackReadErr:  errors.New("boom"),
	}
	server = New(app).Handler()
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/rss.xml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("feed with unreadable track status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "<item>") {
		t.Fatal("unreadable track must be skipped")
	}
}

func TestRSSFeedMethodNotAllowed(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)}).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/rss.xml", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestMapThumbnailRoute(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), trackReadData: []byte(siteTestTrack)}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/maps/track-2026-08-13-0940-1015.geojson.png", nil)
	server.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type = %q", ct)
	}
	w, h := decodePNGSize(t, rr.Body.Bytes())
	if w != 1000 || h != 1000 {
		t.Fatalf("map size %dx%d, want 1000x1000", w, h)
	}
}

func TestMapLargeRouteIsLazy(t *testing.T) {
	readNames := []string{}
	app := fakeApp{
		broker:         events.NewBroker(1),
		trackReadData:  []byte(siteTestTrack),
		trackReadNames: &readNames,
	}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/maps/track-2026-08-13-0940-1015.geojson@2000.png", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	w, h := decodePNGSize(t, rr.Body.Bytes())
	if w != 2000 || h != 2000 {
		t.Fatalf("map size %dx%d, want 2000x2000", w, h)
	}
	if len(readNames) != 1 || readNames[0] != "track-2026-08-13-0940-1015.geojson" {
		t.Fatalf("large map read names = %#v, want single track read", readNames)
	}
}

func TestMapRouteRejectsInvalidName(t *testing.T) {
	app := fakeApp{
		broker:       events.NewBroker(1),
		trackReadErr: errors.New(`invalid track name "../x.png"`),
	}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/maps/..%2Fx.png", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMapRouteMissingTrackNotFound(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), trackReadErr: os.ErrNotExist}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/maps/track-2026-08-13.geojson.png", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMapRouteMethodNotAllowed(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)}).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/maps/track-x.geojson.png", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestBlogIndexRoute(t *testing.T) {
	app := fakeApp{
		broker:        events.NewBroker(1),
		trackFiles:    siteTestFileInfo(),
		trackReadData: []byte(siteTestTrack),
	}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/blog/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	server.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`rel="alternate" type="application/rss+xml" href="https://example.com/rss.xml"`,
		`href="https://example.com/maps/track-2026-08-13-0940-1015.geojson.png"`,
		`https://example.com/maps/track-2026-08-13-0940-1015.geojson@2000.png`,
		`https://example.com/v1/tracks/track-2026-08-13-0940-1015.geojson`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("blog body missing %q: %s", want, body)
		}
	}
}
```

Note: `TestRSSFeedRoute` asserts the PNG enclosure `length="3"` because the rendered 1000x1000 PNG of the fixture track is a small byte buffer whose exact size depends on the renderer. That exact value is brittle. Replace the assertion with a `length="` presence check scoped to the two enclosure lines:

Replace the `want` list line:
```go
		`<enclosure url="https://example.com/maps/track-2026-08-13-0940-1015.geojson.png" length="3" type="image/png"`,
```
with:
```go
		`<enclosure url="https://example.com/maps/track-2026-08-13-0940-1015.geojson.png" length="`,
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./service/api/httpapi/ -run 'TestRSS|TestMap|TestBlog'`
Expected: FAIL — handlers undefined; `NewServer` still compiles but the routes do not exist, so status codes will not match.

- [ ] **Step 3: Wire the handlers into `Server`**

Modify `service/api/httpapi/server.go`:

1. Add the import (in the import block, alphabetical):
```go
	"empirebus-tests/service/tracksite"
```

2. Add a `maps` field to the `Server` struct:
```go
type Server struct {
	app    Application
	broker *events.Broker
	maps   *tracksite.Cache
}
```

3. Initialize it in `New`:
```go
func New(app Application) *Server {
	return &Server{app: app, broker: app.Broker(), maps: tracksite.NewCache()}
}
```

4. Register the routes in `Handler()` (after the `mux.HandleFunc("/v1/events", s.handleEvents)` line, before `registerStaticRoutes(mux)`):
```go
	mux.HandleFunc("/rss.xml", s.handleRSSFeed)
	mux.HandleFunc("/maps/{name}.png", s.handleTrackMap)
	mux.HandleFunc("/blog/", s.handleBlogIndex)
```

- [ ] **Step 4: Write the handlers**

Create `service/api/httpapi/rss.go`:

```go
package httpapi

import (
	"net/http"
	"strings"
	"time"

	"empirebus-tests/service/tracksite"
)

// siteBaseURL returns the absolute origin for feed links and enclosures.
// Tailscale Serve terminates HTTPS and sets X-Forwarded-Proto; development
// requests without it fall back to plain http, matching the sim listener.
func siteBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	return proto + "://" + r.Host
}

func (s *Server) handleRSSFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	tracks, err := s.app.TrackList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items := make([]tracksite.Item, 0, len(tracks))
	for _, track := range tracks {
		if track.StartTime == nil || track.EndTime == nil {
			continue
		}
		raw, err := s.app.TrackRead(track.Name)
		if err != nil {
			continue
		}
		mapPNG, err := s.renderTrack(track.Name, 1000, raw)
		if err != nil {
			continue
		}
		items = append(items, tracksite.Item{
			Name:         track.Name,
			StartTime:    track.StartTime.UTC(),
			EndTime:      track.EndTime.UTC(),
			MapPNGBytes:  int64(len(mapPNG)),
			GeoJSONBytes: track.Bytes,
		})
	}
	feed, err := tracksite.BuildFeed(siteBaseURL(r), items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(feed)
}

var _ = time.Now // placeholder removed below
```

Remove the final `var _ = time.Now` line (leftover noise) — it is not part of the file. The `time` import is not needed in `rss.go`; drop it.

Create `service/api/httpapi/rss.go` (final):

```go
package httpapi

import (
	"net/http"
	"strings"

	"empirebus-tests/service/tracksite"
)

// siteBaseURL returns the absolute origin for feed links and enclosures.
// Tailscale Serve terminates HTTPS and sets X-Forwarded-Proto; development
// requests without it fall back to plain http, matching the sim listener.
func siteBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	return proto + "://" + r.Host
}

func (s *Server) handleRSSFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	tracks, err := s.app.TrackList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items := make([]tracksite.Item, 0, len(tracks))
	for _, track := range tracks {
		if track.StartTime == nil || track.EndTime == nil {
			continue
		}
		raw, err := s.app.TrackRead(track.Name)
		if err != nil {
			continue
		}
		mapPNG, err := s.renderTrack(track.Name, 1000, raw)
		if err != nil {
			continue
		}
		items = append(items, tracksite.Item{
			Name:         track.Name,
			StartTime:    track.StartTime.UTC(),
			EndTime:      track.EndTime.UTC(),
			MapPNGBytes:  int64(len(mapPNG)),
			GeoJSONBytes: track.Bytes,
		})
	}
	feed, err := tracksite.BuildFeed(siteBaseURL(r), items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(feed)
}
```

Create `service/api/httpapi/maps.go`:

```go
package httpapi

import (
	"net/http"
	"strings"

	"empirebus-tests/service/tracksite"
)

// renderTrack returns a cached map PNG for name@size, rendering from raw
// (when non-nil) or by reading the track when absent.
func (s *Server) renderTrack(name string, size int, raw []byte) ([]byte, error) {
	if data := s.maps.Get(name, size); data != nil {
		return data, nil
	}
	if raw == nil {
		var err error
		raw, err = s.app.TrackRead(name)
		if err != nil {
			return nil, err
		}
	}
	rendered, err := tracksite.RenderTrack(raw, size)
	if err != nil {
		return nil, err
	}
	s.maps.Put(name, size, rendered)
	return rendered, nil
}

func (s *Server) handleTrackMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	name := r.PathValue("name")
	size := 1000
	if strings.HasSuffix(name, "@2000") {
		size = 2000
		name = strings.TrimSuffix(name, "@2000")
	}
	if name == "" || name == "@2000" {
		writeError(w, http.StatusBadRequest, errors.New("invalid track name"))
		return
	}
	data, err := s.renderTrack(name, size, nil)
	if err != nil {
		writeTrackError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
```

The `name == "@2000"` guard needs `errors` (stdlib); add `"errors"` to the import. Final imports for `maps.go`:

```go
import (
	"errors"
	"net/http"
	"strings"

	"empirebus-tests/service/tracksite"
)
```

Create `service/api/httpapi/blog.go`:

```go
package httpapi

import (
	"fmt"
	"html"
	"net/http"
	"time"

	"empirebus-tests/service/tracking"
)

func (s *Server) handleBlogIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if r.URL.Path != "/blog" && r.URL.Path != "/blog/" {
		http.NotFound(w, r)
		return
	}
	base := siteBaseURL(r)
	tracks, err := s.app.TrackList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\">")
	fmt.Fprintf(w, "<title>Xtura journeys</title>")
	fmt.Fprintf(w, "<link rel=\"alternate\" type=\"application/rss+xml\" href=\"%s\">", html.EscapeString(base+"/rss.xml"))
	fmt.Fprintf(w, "</head><body>")
	fmt.Fprintf(w, "<h1>Xtura journeys</h1>")
	fmt.Fprintf(w, "<p><a href=\"%s\">RSS feed</a></p>", html.EscapeString(base+"/rss.xml"))
	for _, track := range tracks {
		title := html.EscapeString(trackTitle(track))
		fmt.Fprintf(w,
			"<p>%s &middot; <a href=\"%s\">map</a> &middot; <a href=\"%s\">larger map</a> &middot; <a href=\"%s\">GeoJSON</a></p>",
			title,
			html.EscapeString(base+"/maps/"+track.Name+".png"),
			html.EscapeString(base+"/maps/"+track.Name+"@2000.png"),
			html.EscapeString(base+"/v1/tracks/"+track.Name),
		)
	}
	fmt.Fprintf(w, "</body></html>")
}

func trackTitle(track tracking.FileInfo) string {
	if track.StartTime == nil || track.EndTime == nil {
		return track.Name
	}
	return fmt.Sprintf("%s - %s",
		track.StartTime.UTC().Format("2006-01-02 15:04"),
		track.EndTime.UTC().Format("15:04"))
}
```

`blog.go` needs the `tracking` import and `time` is unused — drop `time`, keep:

```go
import (
	"fmt"
	"html"
	"net/http"

	"empirebus-tests/service/tracking"
)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./service/api/httpapi/`
Expected: PASS (existing tests plus the new site tests).

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS everywhere.

If `go vet` is available in the repo workflow, run `go vet ./service/tracksite/ ./service/api/httpapi/` and fix any reported issues.

- [ ] **Step 7: Commit**

```bash
git add service/api/httpapi/server.go service/api/httpapi/rss.go service/api/httpapi/maps.go service/api/httpapi/blog.go service/api/httpapi/site_test.go
git commit -m "feat: serve journeys RSS feed and map images over HTTP"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/gps-tracking.md`

**Interfaces:**
- Consumes: nothing more than the completed routes.

- [ ] **Step 1: Update README.md**

In `README.md`, under `Current HTTP endpoints:`, add after the `- GET /v1/events` line:

```markdown
- `GET /rss.xml`
- `GET /maps/{name}.png`
- `GET /maps/{name}@2000.png`
- `GET /blog/`
```

In `README.md`, in the `### GPS trails` section, append a paragraph:

```markdown
The daemon also serves a journeys web site from the same process. `GET /rss.xml`
is an RSS 2.0 feed with one item per track (title `Date From - To` in UTC); each
item body embeds a 1000x1000 map image, links to a larger 2000x2000 map (rendered
lazily on first request), and links to the GeoJSON download, with both the map and
the GeoJSON exposed as RSS enclosures so a consumer such as the InstaBlog agent
can download either. `GET /blog/` is a minimal HTML index. See
[gps-tracking.md](docs/gps-tracking.md) for the feed and map endpoint details.
```

- [ ] **Step 2: Update docs/gps-tracking.md**

Add a new section after the `### Track files` section (before `### Live updates`):

```markdown
### Journeys web site (RSS feed and map images)

The daemon serves a small journeys web site from the same process and HTTPS
entry point:

- `GET /rss.xml` — an RSS 2.0 feed with one item per track, newest first. Item
  title is `YYYY-MM-DD HH:MM - HH:MM` using the track's UTC start and end.
  The item description embeds the 1000x1000 map image and links to a larger
  2000x2000 map and to the GeoJSON download. Each item also carries two
  `<enclosure>` elements — the 1000x1000 PNG (`image/png`) and the track
  GeoJSON (`application/geo+json`, the same bytes as `GET /v1/tracks/{name}`)
  — so a reader such as the InstaBlog agent can download the image or the
  journey data.
- `GET /maps/{name}.png` — the 1000x1000 map image for a track.
- `GET /maps/{name}@2000.png` — a larger 2000x2000 map, generated lazily on
  first request and cached afterwards.
- `GET /blog/` — a minimal HTML index of recent journeys.

Maps are rendered server-side in pure Go from the track coordinates (Web
Mercator projection, route polyline with start/end markers); no tile provider
or internet connection is required, so rendering works offline. Feed links and
enclosures are absolute and derived from the request host and the
`X-Forwarded-Proto` header set by Tailscale Serve.

Tracks that fail to render are omitted from the feed rather than failing it;
an empty track directory still produces a valid feed with no items.
```

- [ ] **Step 3: Verify tests still pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/gps-tracking.md
git commit -m "docs: document journeys website RSS feed and map endpoints"
```

---

## Self-Review Notes

- **Spec coverage:** RSS feed + item title/body shape (Task 3, 4), 1000x1000 thumbnail and lazy 2000x2000 map (Task 1, 2, 4), GeoJSON download link via existing `/v1/tracks/{name}` (Task 4), enclosures for both artifacts (Task 3), Tailscale-hosted via existing mux (Task 4), `/blog/` index (Task 4), docs (Task 5). All requirements from the 2026-09-18 journeys-site-design spec are covered.
- **Type consistency:** `tracksite.RenderTrack`, `tracksite.NewCache`, `tracksite.Cache.Get/Put`, `tracksite.Item`, `tracksite.BuildFeed` are defined once (Tasks 1–3) and consumed consistently in Task 4. `siteBaseURL`, `renderTrack`, `handleRSSFeed`, `handleTrackMap`, `handleBlogIndex` are referenced in `server.go` and defined in `rss.go`/`maps.go`/`blog.go`.
- **Deliberate deviation from spec:** `/blog/` is implemented as a Go-rendered page in `service/api/httpapi/blog.go` rather than an embedded `web/static/blog.html`; it needs the live track list, so a Go template is simpler and avoids an embed/rebuild cycle.