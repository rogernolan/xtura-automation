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