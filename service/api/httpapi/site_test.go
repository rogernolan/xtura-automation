package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestRSSFeedRoute(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), trackFiles: siteTestFileInfo()}
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
		`<category>journey</category>`,
		`<category>2026-08-13</category>`,
		`<link>https://example.com/blog/track-2026-08-13-0940-1015.geojson</link>`,
		`<guid isPermaLink="false">https://example.com/blog/track-2026-08-13-0940-1015.geojson</guid>`,
		`<enclosure url="https://example.com/v1/tracks/track-2026-08-13-0940-1015.geojson" length="123" type="application/geo+json"`,
		`<a href="https://example.com/blog/track-2026-08-13-0940-1015.geojson">Open interactive map</a>`,
		`<a href="https://example.com/v1/tracks/track-2026-08-13-0940-1015.geojson">Download GeoJSON</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("feed body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "image/png") || strings.Contains(body, "/maps/") {
		t.Fatalf("feed must not reference map images: %s", body)
	}
}

func TestRSSFeedEmptySkipsUntimedTracks(t *testing.T) {
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
		broker:     events.NewBroker(1),
		trackFiles: []tracking.FileInfo{{Name: "track-2026-08-13-0940-1015.geojson", Bytes: 123}},
	}
	server = New(app).Handler()
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/rss.xml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("feed with untimed track status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "<item>") {
		t.Fatal("track without start/end times must be skipped")
	}

	app = fakeApp{broker: events.NewBroker(1), trackListErr: errors.New("boom")}
	server = New(app).Handler()
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/rss.xml", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("feed list error status = %d", rr.Code)
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

func TestBlogIndexRoute(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), trackFiles: siteTestFileInfo()}
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
		`href="https://example.com/blog/track-2026-08-13-0940-1015.geojson"`,
		`https://example.com/v1/tracks/track-2026-08-13-0940-1015.geojson`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("blog body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, ".png") {
		t.Fatalf("blog index must not reference map images: %s", body)
	}
}

func TestBlogTrackPageRoute(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), trackFiles: siteTestFileInfo()}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/blog/track-2026-08-13-0940-1015.geojson", nil)
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
		"unpkg.com/leaflet@1.9.4/dist/leaflet",
		`const TRACK = "track-2026-08-13-0940-1015.geojson";`,
		`<title>Xtura journeys - 2026-08-13 09:40 - 10:15</title>`,
		`href="https://example.com/v1/tracks/track-2026-08-13-0940-1015.geojson"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("track page missing %q: %s", want, body)
		}
	}
}

func TestBlogTrackPageNotFound(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)}).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/blog/track-2026-08-13-0940-1015.geojson", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestBlogTrackPageRejectsInvalidName(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)}).Handler()
	for _, path := range []string{"/blog/track-x.txt", "/blog/x.png", "/blog/..%2Fetc%2Fpasswd"} {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestBlogMethodNotAllowed(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)}).Handler()
	for _, path := range []string{"/blog/", "/blog/track-x.geojson"} {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, path, nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d", path, rr.Code)
		}
	}
}