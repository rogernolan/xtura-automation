# GPS Tracking Design

Date: 2026-08-13

## Problem

The van has a GPS source (Teltonika RUTX50 router GPS position endpoint) that
is currently polled every 5 minutes purely for timezone and movement inference.
We want optional, user-configurable GPS trail recording so a separate consumer
(the InstaBlog agent) can download the day's drive and write about it.

Requirements:

- A settings pane (in the Settings tab, above WebSocket recording) with:
  - a "Generate GPS trails" switch
  - an "Only when engine is on" switch
  - a "Sample every N seconds" numeric editor (default 5)
- Track files in their own directory, in a compact open standard that is easy
  to parse (GeoJSON was chosen over GPX).
- An API to download and delete track files.
- A test that generates a sample track file and a committed sample file for
  offline parsing, plus docs for the agent writing the InstaBlog consumer.

## Decisions Made in Brainstorming

- Format: GeoJSON (RFC 7946), a single Feature with a `LineString` geometry,
  per-point timestamps carried in a `properties.times` array aligned
  index-for-index with the coordinates. Altitude (when the RUTX50 reports it)
  is stored as the third coordinate element `[lon, lat, alt]` per RFC 7946,
  where `alt` is elevation in meters.
- Track lifecycle:
  - Engine-only mode: one file per engine on -> off session.
  - Continuous mode: one file per UTC day, resumed on service restart so a
    calendar day is always one file.
- Settings pane shows the three controls plus a live status line and the track
  file list with Download and Delete buttons.
- Sample track: a Go test generates a sample track file, and a committed
  sample file is added at `docs/examples/sample-track.geojson`.
- The engine-on gate only samples once engine state is known (the first Garmin
  frame after connect carries the current state), so no data is recorded while
  engine state is unknown.

## Architecture

New package `service/tracking`, a `Manager` modeled on
`service/recording/manager.go`. Wired into `runtime/app.go`.

Components:

- `tracking.Manager`:
  - Tracks engine state by observing the Garmin frame stream (same
    `RecordFrame` callback the recording manager registers). Signal `11` is
    the engine-running indication (already relied on by recording).
  - Owns a sampling ticker goroutine started via `Manager.Start(ctx)`.
  - `Manager.Sample(ctx)` is public so unit tests drive sampling
    deterministically without the goroutine.
  - Owns the active track file and writes it atomically.
  - Provides `List`, `Read`, `Delete` for track file management.
- Wiring in `runtime/app.go`:
  - Constructs the `Manager` with the RUTX50 poll function, the Garmin frame
    observer, config, directory, `now`, and logger.
  - Starts/stops the sampling loop with the app context.
  - Publishes `tracking.state_changed` SSE events.
  - Applies live config changes (settings PUT) by updating `a.cfg` and
    reconfiguring the manager, mirroring the heating schedule update path.

## Data Flow

1. Garmin `RecordFrame` -> `Manager.ObserveFrame(at, direction, raw)`. The
   manager parses signal `11` frames to maintain `engineOn` and `engineKnown`.
2. On each tick (default 5s), the manager calls `Sample(ctx)`:
   - Skip if tracking is disabled.
   - Skip if `onlyWhenEngineOn` and (`!engineKnown` or `!engineOn`).
   - Poll the RUTX50 location provider for a fix.
   - On a successful fix, append `{lat, lon, altitude, at}` to the active
     track and atomically rewrite the track file.
   - On poll failure, record `last_error` and continue (do not split the
     track).
3. Lifecycle transitions:
- Engine-only mode: engine-on frame starts a file; engine-off frame
  finalizes it. No forced final sample is taken on the engine-off frame; the
  last sampled point stands.
   - Continuous mode: the first sample of a new UTC day rotates to a new file
     for that day. A session resumes the existing daily file (reads it back,
     continues appending) so restarts mid-day keep one file per day.
   - Tracking disabled or service shutdown: finalize the active file.

## Track File Format

A single valid GeoJSON Feature written to the file:

```json
{
  "type": "Feature",
  "properties": {
    "name": "track-20260813T094000Z.geojson",
    "start_time": "2026-08-13T09:40:00Z",
    "end_time": "2026-08-13T10:15:00Z",
    "point_count": 422,
    "sample_interval_seconds": 5,
    "times": ["2026-08-13T09:40:05Z"]
  },
  "geometry": {
    "type": "LineString",
    "coordinates": [[0.854362, 51.065375, 7]]
  }
}
```

- Coordinates are `[lon, lat]` per RFC 7946. When the RUTX50 reports an
  altitude for a sample, the position is `[lon, lat, alt]` with `alt` in
  meters as the RFC 7946 elevation element; otherwise the position is
  `[lon, lat]`. Coordinates may therefore mix two- and three-element
  positions within one track.
- `properties.times` is aligned index-for-index with `coordinates` and holds
  ISO 8601 UTC timestamps (`YYYY-MM-DDTHH:MM:SSZ`), so consumers can render
  the route and animate progress.
- `properties.name`, `start_time`, `end_time`, `point_count`,
  `sample_interval_seconds` are informational.
- Files are written atomically: write `<name>.tmp`, rename over `<name>`, so a
  crash never leaves a truncated/corrupt track file.

### Altitude Source

- The RUTX50 `GET /api/gps/position/status` response includes an `altitude`
  field (string, meters as reported by the router's GPS module; the live
  router returned `"altitude":"7"`). It is the same GPS module the RUTX50
  uses for its own GPS features.
- `service/adapters/teltonika/rutx50.go` gains altitude extraction (keys
  `altitude`, `alt`, `elevation`), and `domainlocation.Fix` gains an optional
  `Altitude *float64` (nil when the provider did not report one).
- The tracker stores the optional altitude in each sample; the GeoJSON writer
  emits the third coordinate element only when altitude is known.
- `location.State` also gains an optional `altitude` field surfaced by
  `GET /v1/location/state` when the latest fix has one.

### File Names

- Engine-only sessions: `track-20260813T094000Z.geojson` (UTC session start).
- Continuous mode: `track-2026-08-13.geojson` (UTC date).
- Stored in `/var/lib/xtura/tracks/` by default (own directory, created with
  `MkdirAll` on start).

## Config

New top-level `tracking` section in `config.yaml`, persisted with the existing
`config.SaveFile` pattern and applied live without a service restart:

```yaml
tracking:
  enabled: false
  only_when_engine_on: true
  sample_interval: 5s
  dir: /var/lib/xtura/tracks
```

Defaults: `enabled: false`, `only_when_engine_on: true`, `sample_interval: 5s`,
`dir: /var/lib/xtura/tracks`.

Validation in `config.Validate`:

- `tracking.sample_interval` between `1s` and `3600s`.
- `tracking.enabled` requires `location.enabled` (the tracker needs the
  RUTX50 provider).

## HTTP API

New routes in `service/api/httpapi/server.go`:

| Endpoint | Method | Purpose |
|---|---|---|
| `/v1/tracking/settings` | GET | Current tracking settings. |
| `/v1/tracking/settings` | PUT | Save settings to config.yaml and apply live. |
| `/v1/tracking/state` | GET | Runtime status (see shape below). |
| `/v1/tracks` | GET | List track files. |
| `/v1/tracks/{name}` | GET | Download a track file. |
| `/v1/tracks/{name}` | DELETE | Delete a track file. |

JSON shapes:

- `tracking.Settings`: `{"enabled":bool,"only_when_engine_on":bool,"sample_interval_seconds":number,"directory":string}`
- `tracking.State`: `{"enabled":bool,"only_when_engine_on":bool,"sample_interval_seconds":number,"engine_known":bool,"engine_on":bool,"tracking":bool,"current_file"?:"string","point_count":number,"last_sample_at"?:"time","last_error"?:"string","last_error_at"?:"time"}`
- `tracking.FileInfo`: `{"name":string,"bytes":number,"start_time"?:"time","end_time"?:"time","point_count":number}`

Notes:

- `GET /v1/tracks/{name}` serves `application/geo+json` with a
  `Content-Disposition: attachment` header. Only names matching the known
  track patterns (`track-*.geojson` under the tracks directory) are accepted,
  guarding against path traversal.
- `GET /v1/tracks` derives `start_time`, `end_time`, and `point_count` by
  parsing each file (files are small).
- SSE event `tracking.state_changed` is published whenever the runtime state
  changes, so the UI can show live status.

## Web UI

A "GPS trails" panel in the Settings tab, rendered above the WebSocket
recording panel:

- Switch: "Generate GPS trails"
- Switch: "Only when engine is on"
- Numeric editor: "Sample every N seconds" (default 5, range 1-3600)
- Status line with live tracking state and errors
- Track file list with Download and Delete buttons

Behavior:

- The two switches and the numeric editor apply immediately on change (PUT
  `/v1/tracking/settings`), with a status message on save/error. No explicit
  Save button.
- The track list refreshes on panel load, after settings saves, and after
  delete.
- Live status (engine known/on, tracking active, point count) updates from
  the `tracking.state_changed` SSE event.
- The controls are disabled while a settings request is in flight or when the
  settings load has not completed.

## Tests

- `service/tracking/manager_test.go`:
  - Engine-only: starts a file on an engine-on frame, samples while on,
    finalizes on an engine-off frame.
  - Continuous mode: samples without engine, rotates per UTC day, resumes an
    existing daily file.
  - Unknown engine state blocks sampling in engine-only mode.
  - Sample writes a valid GeoJSON LineString with aligned `times`.
  - Altitude is stored as the third coordinate element when present and the
    two-element position is kept when absent.
  - Atomic rewrite leaves a valid file after each sample.
  - List/Read/Delete behavior and path-traversal rejection.
  - Generates a sample track file into a temp directory.
- `service/api/httpapi/server_test.go`:
  - Route/method checks for the new endpoints.
  - Settings GET/PUT round-trip, validation errors, list/download/delete.
- Config tests for tracking defaults and validation.
- `service/adapters/teltonika/rutx50_test.go`: altitude extraction from the
  GPS position status payload (present and absent cases).
- Committed sample file at `docs/examples/sample-track.geojson` (with a few
  `[lon, lat, alt]` positions) for offline InstaBlog parsing, and the unit
  test in `manager_test.go` writes a similar sample track into its temp
  directory to exercise the file writer.

## Docs

- New `docs/gps-tracking.md`: the track file format (including altitude
  semantics), the track/settings API, and the lifecycle, written as a
  consumption guide for the agent writing the InstaBlog integration.
- Update `docs/internal-api.md`: new routes, JSON shapes, and the
  `tracking.state_changed` event.
- Update `docs/garmin-empirbus-signals.md`: note that the GPS tracker also
  relies on signal `11` as the engine-running indication (currently noted only
  for the WebSocket recorder).
- Update `README.md` and `config.example.yaml` with the new `tracking`
  section and feature summary.

## Verification

- `go test ./...`
- `npm run lint` (web)
- `git diff --check`
- Live smoke test on the Pi: enable tracking, observe a sample track file
  appear, download and delete it over the API.
