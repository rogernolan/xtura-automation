package httpapi

import (
	"net/http"

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