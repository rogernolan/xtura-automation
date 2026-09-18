# Journeys Web Site RSS Feed Design

Date: 2026-09-18

## Problem

The tracked GPS journeys (per-UTC-day GeoJSON track files produced by
`service/tracking`) have an HTTP download API but no browsable presence. We
want a small web site served by the existing daemon that offers an RSS feed of
journeys so an external app (the InstaBlog agent) can turn each journey into a
post. Each feed item must carry a rendered map image at 1000x1000 px, a link
to a larger map (larger map is lazily generated), and a link to download the
GeoJSON.

The site is served "locally" over Tailscale: the existing `empirebusd`
process is already exposed HTTPS-only via Tailscale Serve pointing at the
loopback `api.listen`, so no new port, server, or systemd unit is needed.

Requirements:

- RSS feed with one item per journey track, newest first.
- Item title in the form `Date From - To` (e.g. `2026-08-13 09:40 - 10:15`,
  UTC).
- Item body containing:
  - the journey's map image rendered at 1000x1000 px,
  - a link to a larger map that is lazily generated on first request,
  - a link to download the track GeoJSON.
- The feed is consumed by InstaBlog, which downloads either the map image or
  the GeoJSON. Both are exposed as RSS `<enclosure>` elements so a standard
  reader or InstaBlog can auto-fetch them.
- Map images are rendered server-side in pure Go with no network access
  (no tile providers or API keys), so generation works offline on the road.

## Decisions Made in Brainstorming

- Map rendering: pure-Go offline renderer using only the standard library
  (`image`, `image/draw`, `image/png`). The map is stylized (route polyline,
  markers, subtle grid) rather than photographic; this is deterministic,
  dependency-free, and works with no internet.
- Feed title times use UTC, matching the timestamps the tracking manager
  writes (`track-YYYY-MM-DD-HHMM-HHMM`).
- Each item exposes two `<enclosure>` elements: the 1000x1000 PNG map
  (`image/png`) and the track GeoJSON (`application/geo+json`), plus the same
  artifacts as links in the description HTML. This lets Instablog download
  either the image or the raw journey data.
- The larger map is 2000x2000 px.
- The web site lives inside the existing `empirebusd` HTTP mux.

## Architecture

### Routes (added to `service/api/httpapi` `Handler`)

- `GET /rss.xml` — RSS 2.0 feed. Content-Type `application/rss+xml; charset=utf-8`.
- `GET /maps/{name}.png` — 1000x1000 map PNG. Content-Type `image/png`.
- `GET /maps/{name}@2000.png` — 2000x2000 map PNG, rendered lazily on first
  request, cached afterwards.
- `GET /v1/tracks/{name}` — existing GeoJSON download, reused as the GeoJSON
  link and enclosure URL.
- `GET /blog/` — minimal HTML index page listing recent journeys with links to
  `/rss.xml`, each journey's map, larger map, and GeoJSON download.

Base URLs for feed links/enclosures are built from the request `Host` header
and the `X-Forwarded-Proto` header (set by Tailscale Serve). This keeps links
absolute and resolvable through the Tailscale HTTPS hostname without any new
config. If neither header is available in development, it falls back to
`http://` + `Host`, which is correct for the local sim.

### New package `service/tracksite`

Two focused units plus the handlers:

- `feed.go` — `BuildFeed(baseURL string, tracks []tracking.FileInfo, enclosureSizes map[string]int64) ([]byte, error)`:
  pure function that renders the RSS 2.0 XML document. Each `<item>`
  corresponds to one track. Ordering is whatever the caller passes in; the
  handler passes `TrackList()` output, which is already newest-first. The
  enclosure length for the PNG is supplied by the rendered image cache.
- `map.go` — `RenderTrack(data []byte, size int) ([]byte, error)` plus a small
  cache:
  - parses the track GeoJSON (route `LineString` feature, optional `Point`
    engine event features — the same shape `service/tracking` writes),
  - projects with a Web Mercator fit to the route bounds with padding,
  - draws the route polyline, green start marker, red end marker, small
    markers for engine events, a subtle grid, and a border,
  - encodes as PNG and returns the bytes,
  - `Cache` keyed by `(name, size)` with a short TTL so an active track does
    not serve a stale image for long, and so repeated feed polls do not
    re-render every thumbnail.

`RenderTrack` guards against degenerate input: a single-point track or a
zero-extent bounding box renders centered with a sensible fixed scale instead
of dividing by zero.

### Handler wiring in `service/api/httpapi`

The `Server` already satisfies `TrackList()` and `TrackRead()` on the
`Application` interface, which is all the new handlers need:

- feed handler: `TrackList()`; for each track compute the PNG enclosure
  length via the map cache (renders+warm the 1000x1000 thumbnails it will
  embed); hand both to `tracksite.BuildFeed`.
- map handler: parse `{name}` via `r.PathValue`, read the track through
  `TrackRead`, render at the requested size, serve with `Cache-Control: no-cache`.

## Data Flow

1. Client (browser or Instablog) requests `GET /rss.xml` over Tailscale
   HTTPS.
2. Handler calls `TrackList()` (already newest-first); the GeoJSON enclosure
   `length` comes from `FileInfo.Bytes`, so no read is needed for sizing.
3. For each track the 1000x1000 map is rendered (via the map cache) so the
   image enclosure has an accurate `length`; the cache is thereby warmed for
   the `<img>` fetch that follows. A track that fails to render is skipped
   from the feed.
4. `BuildFeed` produces the XML document with absolute URLs built from the
   request Host/X-Forwarded-Proto.
5. The client fetches `/maps/{name}.png` directly (served from cache), and
   may fetch `/maps/{name}@2000.png`, which is rendered once on first request
   and cached.

## Error Handling

- Track name that does not match `track-*.geojson` (or the `@2000` variant):
  `400` with the existing JSON error shape.
- Track missing on disk: `404` with the existing JSON error shape.
- Render failure: `500`.
- A corrupt/unparsable track file: feed skips the item rather than failing
  the whole feed; map endpoint returns `500` with an error message.
- No tracks at all: the feed is still a valid RSS document with an empty
  item list.

## Testing

- `tracksite` `map_test.go`:
  - renders a straight-line track and asserts the decoded PNG is exactly
    1000x1000, and the `@2000` variant is 2000x2000,
  - asserts the output is not blank (route pixels differ from the
    background),
  - asserts deterministic output for identical input,
  - asserts a single-point track renders without error and stays in-bounds.
- `tracksite` `feed_test.go`:
  - builds a feed from fixture `tracking.FileInfo` values and parses the XML
    with `encoding/xml`,
  - asserts item count and order (newest first as passed in),
  - asserts titles are `Date From - To`,
  - asserts the two enclosure URLs and types,
  - asserts the CDATA description contains the `<img>`, larger-map link, and
    GeoJSON link.
- `service/api/httpapi` route tests (extend `server_test.go` / new `rss_test.go`):
  - `GET /rss.xml` returns `200` + `application/rss+xml`,
  - `GET /maps/{name}.png` returns `200` + `image/png` and a 1000x1000 image,
  - `GET /maps/{name}@2000.png` returns a 2000x2000 image and only renders
    when requested,
  - invalid name → `400`, missing → `404`.

## Integration Point

No config changes: everything routes through the existing daemon and the
existing Tailscale Serve HTTPS entry point. The sim environment
(`./scripts/sim/run-sim.sh`) exercises the new routes at
`http://localhost:8091/rss.xml`.

## Documentation

- Update `README.md` (`Current HTTP endpoints` list and the GPS trails
  section) with the new routes and usage.
- Add a short section to `docs/gps-tracking.md` describing the RSS feed and
  map endpoints for the InstaBlog consumer.