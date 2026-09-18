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
	Title       string         `xml:"title"`
	Link        string         `xml:"link"`
	GUID        string         `xml:"guid"`
	PubDate     string         `xml:"pubDate"`
	Description string         `xml:"description"`
	Enclosures  []rssEnclosure `xml:"enclosure"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []rssItem `xml:"item"`
}

type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
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