package httpapi

import (
	"bytes"
	"errors"
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
		`<enclosure url="https://example.com/maps/track-2026-08-13-0940-1015.geojson.png" length="`,
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
	if strings.Contains(rr.Body.String(), "<item>") {
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