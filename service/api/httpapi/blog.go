package httpapi

import (
	"fmt"
	"html"
	"net/http"

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