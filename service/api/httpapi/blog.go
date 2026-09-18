package httpapi

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"

	"empirebus-tests/service/tracking"
)

const leafletCSS = "https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"
const leafletJS = "https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"

func (s *Server) handleBlog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if r.URL.Path == "/blog" || r.URL.Path == "/blog/" {
		s.handleBlogIndex(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/blog/")
	if !validBlogTrackName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid track name %q", name))
		return
	}
	s.handleBlogTrack(w, r, name)
}

// validBlogTrackName mirrors the tracking manager's name check: names must
// look like generated track files so the path segment cannot escape into the
// filesystem or a different URL space.
func validBlogTrackName(name string) bool {
	return strings.HasPrefix(name, "track-") && strings.HasSuffix(name, ".geojson")
}

func (s *Server) handleBlogIndex(w http.ResponseWriter, r *http.Request) {
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
			"<p>%s &middot; <a href=\"%s\">map</a> &middot; <a href=\"%s\">GeoJSON</a></p>",
			title,
			html.EscapeString(base+"/blog/"+track.Name),
			html.EscapeString(base+"/v1/tracks/"+track.Name),
		)
	}
	fmt.Fprintf(w, "</body></html>")
}

func (s *Server) handleBlogTrack(w http.ResponseWriter, r *http.Request, name string) {
	base := siteBaseURL(r)
	tracks, err := s.app.TrackList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var info *tracking.FileInfo
	for i := range tracks {
		if tracks[i].Name == name {
			info = &tracks[i]
			break
		}
	}
	if info == nil {
		http.NotFound(w, r)
		return
	}
	title := html.EscapeString(trackTitle(*info))
	nameJSON, _ := json.Marshal(name)
	nameJSON = []byte(strings.ReplaceAll(string(nameJSON), "</script", "<\\/script"))
	geoHref := html.EscapeString(base + "/v1/tracks/" + name)
	indexHref := html.EscapeString(base + "/blog/")
	rssHref := html.EscapeString(base + "/rss.xml")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, `<!doctype html>`+"\n<html lang=\"en\">\n<head>\n")
	fmt.Fprintf(w, `<meta charset="utf-8">`+"\n")
	fmt.Fprintf(w, `<meta name="viewport" content="width=device-width, initial-scale=1">`+"\n")
	fmt.Fprintf(w, "<title>Xtura journeys - %s</title>\n", title)
	fmt.Fprintf(w, `<link rel="stylesheet" href="%s" integrity="sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=" crossorigin="">`+"\n", leafletCSS)
	fmt.Fprintf(w, `<script src="%s" integrity="sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=" crossorigin=""></script>`+"\n", leafletJS)
	fmt.Fprint(w, `<style>html,body{margin:0;padding:0;height:100%;overflow:hidden;font-family:system-ui,sans-serif}header{position:relative;z-index:1000;background:#f3f1ea;padding:8px 14px;border-bottom:1px solid #c9c3b4}header h1{font-size:16px;margin:0;padding-bottom:2px}header nav a{color:#1f5fd4}nav{font-size:13px}#status{position:relative;z-index:1000;padding:6px 14px;color:#555}#map{position:fixed;top:0;left:0;right:0;bottom:0}</style>`+"\n")
	fmt.Fprintf(w, "</head>\n<body>\n")
	fmt.Fprintf(w, "<header><h1>Xtura journeys &middot; %s</h1><nav><a href=\"%s\">All journeys</a> &middot; <a href=\"%s\">RSS</a> &middot; <a href=\"%s\">GeoJSON</a></nav></header>\n", title, indexHref, rssHref, geoHref)
	fmt.Fprintf(w, "<div id=\"status\">Loading track&hellip;</div>\n<div id=\"map\"></div>\n")
	fmt.Fprintf(w, "<script>\nconst TRACK = %s;\n", nameJSON)
	fmt.Fprintf(w, `async function load(){const resp=await fetch("/v1/tracks/"+encodeURIComponent(TRACK));if(!resp.ok)throw new Error("HTTP "+resp.status);return await resp.json();}`+"\n")
	fmt.Fprintf(w, `function lineOf(g){const feats=Array.isArray(g&&g.features)?g.features:(g&&g.geometry?[g]:[]);let line=null;const points=[];for(const f of feats){const ge=f&&f.geometry;if(!ge||!ge.coordinates)continue;if(ge.type==="LineString")line=ge.coordinates;else if(ge.type==="Point"&&f.properties&&f.properties.event)points.push(ge.coordinates);}return{line:line||[],points};}`+"\n")
	fmt.Fprintf(w, `(async()=>{const g=await load();const{line,points}=lineOf(g);if(!line.length)throw new Error("no LineString geometry");const latlngs=line.map(c=>[c[1],c[0]]);const map=L.map("map");L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png",{attribution:'&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',maxZoom:19}).addTo(map);L.polyline(latlngs,{color:"#1f5fd4",weight:4,opacity:0.9}).addTo(map);const start=latlngs[0],end=latlngs[latlngs.length-1];L.circleMarker(start,{radius:7,color:"#ffffff",weight:2,fillColor:"#1e8e3e",fillOpacity:1}).addTo(map).bindTooltip("Start");L.circleMarker(end,{radius:7,color:"#ffffff",weight:2,fillColor:"#d93025",fillOpacity:1}).addTo(map).bindTooltip("End");for(const p of points){const ll=[p[1],p[0]];if((ll[0]===start[0]&&ll[1]===start[1])||(ll[0]===end[0]&&ll[1]===end[1]))continue;L.circleMarker(ll,{radius:4,color:"#5f6368",weight:1,fillColor:"#5f6368",fillOpacity:0.8}).addTo(map);}map.fitBounds(latlngs,{padding:[32,32]});document.getElementById("status").textContent="";})().catch(err=>{document.getElementById("status").textContent="Error loading track: "+err.message;});`+"\n")
	fmt.Fprintf(w, "</script>\n</body>\n</html>\n")
}

func trackTitle(track tracking.FileInfo) string {
	if track.StartTime == nil || track.EndTime == nil {
		return track.Name
	}
	return fmt.Sprintf("%s - %s",
		track.StartTime.UTC().Format("2006-01-02 15:04"),
		track.EndTime.UTC().Format("15:04"))
}