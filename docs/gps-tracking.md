# GPS Tracking

The service can sample the GPS location provider (the Teltonika RUTX50 router) into
GeoJSON track files. This is the consumption guide for writing an integration that
downloads a day's drive and renders or writes about it (for example the InstaBlog
agent). It describes the on-disk format and the HTTP API only; a consumer should not
need to read the Go code.

The feature is off by default. It is controlled from the Settings tab ("GPS trails"
panel) or directly through the settings API below.

## Track file format

Each track file is a single valid GeoJSON (RFC 7946) `Feature`:

- `type` is `"Feature"`.
- `geometry.type` is `"LineString"`.
- `geometry.coordinates` is an array of positions, each either `[lon, lat]` or
  `[lon, lat, alt]`.
- `properties.times` is an array of ISO 8601 UTC timestamps aligned index-for-index
  with `geometry.coordinates`.
- `properties.name`, `start_time`, `end_time`, `point_count`, and
  `sample_interval_seconds` are informational.

Example (a small continuous-mode file):

```json
{
  "type": "Feature",
  "properties": {
    "name": "track-2026-08-13.geojson",
    "start_time": "2026-08-13T09:40:05Z",
    "end_time": "2026-08-13T09:40:20Z",
    "point_count": 4,
    "sample_interval_seconds": 5,
    "times": [
      "2026-08-13T09:40:05Z",
      "2026-08-13T09:40:10Z",
      "2026-08-13T09:40:15Z",
      "2026-08-13T09:40:20Z"
    ]
  },
  "geometry": {
    "type": "LineString",
    "coordinates": [
      [0.854362, 51.065375, 7],
      [0.8544, 51.0655],
      [0.85445, 51.0656, 6],
      [0.8545, 51.0657]
    ]
  }
}
```

### Coordinate ordering and altitude

Per RFC 7946, positions are `[longitude, latitude]` (not lat/lon). When the router
reports an altitude for a sample, the position is `[lon, lat, alt]`, where `alt` is
elevation in metres as reported by the router's GPS module; otherwise the position
is `[lon, lat]`. Positions may therefore mix two- and three-element forms within a
single track — treat each position independently. A missing altitude is represented
by the shorter two-element form, never by a `null` third element.

### `properties.times`

`properties.times[i]` is the capture time of `geometry.coordinates[i]`, in UTC
(`YYYY-MM-DDTHH:MM:SSZ`). Consumers should pair the two arrays by index. Do not
assume a fixed time delta between entries: samples are normally taken every
`sample_interval_seconds`, but a failed location poll or a service restart can
leave gaps, and a resumed daily file contains points from earlier in the day.

`start_time` and `end_time` mirror `times[0]` and `times[len-1]`; `point_count`
equals the number of positions; `sample_interval_seconds` is the configured
sampling period. All three are informational.

### Writing and atomicity

Files are written atomically: the manager writes `<name>.tmp` and renames it over
`<name>`, so a crash never leaves a truncated or corrupt track file. A leftover
`.tmp` file does not match the track name pattern and is ignored by the API.

## Lifecycle

### Engine gating

The tracker observes the same Garmin WebSocket frame stream as the WebSocket
recorder and uses signal `11` (`Engine Running Signal Indication`) as the
engine-running indication. It decodes received frames with the repo-wide toggle
semantics: signal id in `data[0..1]` as an unsigned 16-bit little-endian value and
the on bit in `data[2] & 0x01`. Engine state is only trusted once the first
signal-11 frame has been observed, so nothing is sampled while engine state is
unknown in engine-only mode.

### Modes

- **Engine-only** (`only_when_engine_on: true`, the default):
  - Nothing is sampled while engine state is unknown or the engine is off.
  - A received engine-on frame starts a new session; sampling appends to it.
  - A received engine-off frame finalizes the session. The last sampled point
    stands; no forced final sample is taken.
  - An engine-on → engine-off session with no successful samples produces no file.
  - File names: `track-20260813T094000Z.geojson` (UTC session start).
- **Continuous** (`only_when_engine_on: false`):
  - Samples regardless of engine state.
  - One file per UTC day: `track-2026-08-13.geojson`. The first sample after a UTC
    midnight rotates to the new day's file.
  - A restart mid-day resumes the existing daily file by reading it back and
    continuing to append, so a calendar day stays one file.

A failed location poll (router unreachable, timeout) does not split or abandon the
track: the runtime state reports the error and sampling continues on the next tick.
The track file is only written once a poll succeeds.

### Files on disk

Tracks live in the configured directory, `/var/lib/xtura/tracks` by default, which
is created on demand. Only names matching `track-*.geojson` are treated as tracks by
the API.

## HTTP API

### Settings

`GET /v1/tracking/settings` returns the current settings:

```json
{"enabled":false,"only_when_engine_on":true,"sample_interval_seconds":5,"directory":"/var/lib/xtura/tracks"}
```

`PUT /v1/tracking/settings` accepts the same shape (the `directory` field is
read-only and ignored if supplied), saves the settings to the service config, and
applies them live:

```json
{"enabled":true,"only_when_engine_on":true,"sample_interval_seconds":5}
```

- Success returns the applied settings with the runtime `directory`.
- `400` with `{"error":"validation_failed","details":[{"message":"..."}]}` when
  validation fails: a non-zero `sample_interval_seconds` must be between `1` and
  `3600` (a value of `0` is accepted and defaults to `5`), and `enabled: true`
  requires `location.enabled` (the tracker needs the RUTX50 provider).
- `400` on malformed JSON.

### State

`GET /v1/tracking/state` returns the runtime tracking state:

```json
{
  "enabled": true,
  "only_when_engine_on": true,
  "sample_interval_seconds": 5,
  "engine_known": true,
  "engine_on": true,
  "tracking": true,
  "current_file": "track-20260813T094000Z.geojson",
  "point_count": 42,
  "last_sample_at": "2026-08-13T09:40:05Z"
}
```

- `engine_known` / `engine_on`: the Garmin engine indication (signal 11).
- `tracking`: true while a track is active.
- `current_file` and `point_count` describe the active track.
- `last_sample_at`, `last_error`, and `last_error_at` are omitted until set.

### Track files

- `GET /v1/tracks` lists the track files, sorted by name:

  ```json
  [{"name":"track-2026-08-13.geojson","bytes":3124,"start_time":"2026-08-13T07:02:00Z","end_time":"2026-08-13T18:41:15Z","point_count":420}]
  ```

  `start_time` and `end_time` are parsed from each file and omitted when the file
  has no points or does not parse; `point_count` is always present (`0` when
  nothing parsed).

- `GET /v1/tracks/{name}` downloads a track file. The response is the raw GeoJSON
  file with `Content-Type: application/geo+json` and a
  `Content-Disposition: attachment` header, so a standard GeoJSON parser can read
  the body directly. Names that do not match `track-*.geojson` return `400`; missing
  files return `404`.

- `DELETE /v1/tracks/{name}` deletes a track file and returns `204` on success
  (`400` for invalid names, `404` when missing).

Only names matching the track pattern are accepted, which guards against path
traversal.

### Live updates

`GET /v1/events` (server-sent events) emits `tracking.state_changed` whenever the
tracking state changes — after every sample, on engine on/off transitions, and after
settings changes. The event payload is the same shape as `GET /v1/tracking/state`:

```
event: tracking.state_changed
data: {"type":"tracking.state_changed","timestamp":"2026-08-13T09:40:05Z","payload":{"enabled":true,...}}
```

## Parsing checklist for a consumer

1. Parse the file with a standard GeoJSON / RFC 7946 parser — it is a `Feature`
   whose `geometry` is a `LineString`.
2. Read `geometry.coordinates` for the route; each position is `[lon, lat]` or
   `[lon, lat, alt]` (altitude in metres).
3. Read `properties.times` and pair it index-for-index with the coordinates for
   per-point timing.
4. Use `properties.name` for display and `properties.point_count` for a quick sanity
   check.
5. To fetch a specific day: `GET /v1/tracks` to find `track-<UTC date>.geojson`,
   then `GET /v1/tracks/<name>` to download it.
