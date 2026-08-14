# GPS Tracking Implementation Plan

Date: 2026-08-13

Source design: [docs/superpowers/specs/2026-08-13-gps-tracking-design.md](../specs/2026-08-13-gps-tracking-design.md)

## Overview

Implement optional, user-configurable GPS trail recording. A new
`service/tracking` package (modeled on `service/recording/manager.go`) samples
the Teltonika RUTX50 GPS position endpoint on an interval, gated optionally by
the Garmin engine-running signal (signal 11), and writes track files as
GeoJSON. The feature is wired into `runtime/app.go`, exposed via new HTTP API
routes, and surfaced in a new "GPS trails" panel in the Settings tab above the
WebSocket recording panel.

## Global Constraints

These bind every task:

- GeoJSON per RFC 7946: a single `Feature` with a `LineString` geometry.
  Coordinates are `[lon, lat]`; when altitude is known the position is
  `[lon, lat, alt]` with `alt` in meters. Positions may mix two- and
  three-element forms within one track.
- `properties.times` is a string array aligned index-for-index with
  `coordinates`, holding ISO 8601 UTC timestamps (`YYYY-MM-DDTHH:MM:SSZ`).
- Track files are written atomically: write `<name>.tmp`, rename over
  `<name>`.
- Default directory: `/var/lib/xtura/tracks`.
- Config defaults: `enabled: false`, `only_when_engine_on: true`,
  `sample_interval: 5s`, `dir: /var/lib/xtura/tracks`.
- Config validation: `tracking.sample_interval` between `1s` and `3600s`;
  `tracking.enabled` requires `location.enabled`.
- Engine signal 11 is the engine-running indication, read from receive
  frames via `heating.ParseWireFrame` (same mechanism `recording.isOnFrame`
  uses: `frame.Data[0]|frame.Data[1]<<8 == want && frame.Data[2]&1 != 0`).
- Engine-only mode: one file per engine on -> off session. Continuous mode:
  one file per UTC day, resumed on restart.
- The engine-on gate only samples once engine state is known (engineKnown).
- API settings shape:
  `{"enabled":bool,"only_when_engine_on":bool,"sample_interval_seconds":number,"directory":string}`
- API state shape:
  `{"enabled":bool,"only_when_engine_on":bool,"sample_interval_seconds":number,"engine_known":bool,"engine_on":bool,"tracking":bool,"current_file"?:"string","point_count":number,"last_sample_at"?:"time","last_error"?:"string","last_error_at"?:"time"}`
- API file info shape:
  `{"name":string,"bytes":number,"start_time"?:"time","end_time"?:"time","point_count":number}`
- `GET /v1/tracks/{name}` serves `application/geo+json` with a
  `Content-Disposition: attachment` header; only `track-*.geojson` names are
  accepted (path-traversal guard).
- SSE event `tracking.state_changed` is published whenever runtime tracking
  state changes.
- File names: engine-only `track-20060102T150405Z.geojson`; continuous
  `track-2006-01-02.geojson`.
- All commits must keep `go test ./...`, `npm run lint` (web), and
  `git diff --check` green.

## Task 1: Config tracking section

Add a top-level `tracking` section to `service/config/config.go`:

```yaml
tracking:
  enabled: false
  only_when_engine_on: true
  sample_interval: 5s
  dir: /var/lib/xtura/tracks
```

- Add `Tracking TrackingConfig` to `Config` (yaml `tracking`) and a matching
  field on `NormalizedConfig` with defaults applied in a new
  `normalizeTracking` helper called from `Normalize`:
  - `enabled` as-is
  - `only_when_engine_on` default `true`
  - `sample_interval` default `5s`
  - `dir` default `/var/lib/xtura/tracks`
- Add validation to `Config.Validate`:
  - `tracking.sample_interval` between `1s` and `3600s`
  - `tracking.enabled` requires `location.enabled`
- Add the tracking section to `config.example.yaml`.
- Extend `service/config/config_test.go`: defaults applied on empty config,
  sample_interval bounds (out-of-range rejected), `tracking.enabled` without
  `location.enabled` rejected, tracking section round-trips through
  `LoadFile`/`Normalize`.

No runtime or HTTP wiring in this task. Keep `go test ./...` green.

## Task 2: Altitude in location domain and RUTX50 adapter

- `service/domains/location/types.go`: add `Altitude *float64` (json
  `altitude`, omitempty) to `Fix`; add `Altitude *float64` (json `altitude`,
  omitempty) to `State`.
- `service/adapters/teltonika/rutx50.go`: extract altitude from the GPS
  position status payload in `fixFromStatus`, keys `altitude`, `alt`,
  `elevation` (numeric or numeric string, matching the existing
  `walkPayload`/`numberValue` approach — reuse `firstCoordinate` with the
  new keys or an equivalent helper). Set `Fix.Altitude` only when the payload
  reports one; leave nil otherwise.
- Extend `service/adapters/teltonika/rutx50_test.go`: altitude present
  (string and numeric forms) and absent cases. The live router returned
  `"altitude":"7"`.
- Runtime state surfacing of `State.Altitude` is Task 4; do not touch
  `runtime/app.go` here.

## Task 3: service/tracking Manager package

New package `service/tracking` modeled on `service/recording/manager.go`.
Files: `service/tracking/manager.go`, `service/tracking/manager_test.go`.

Public API:

- `type Fix struct` alias or direct use of `domainlocation.Fix` for samples.
- `type Settings struct { Enabled bool; OnlyWhenEngineOn bool; SampleInterval time.Duration }`
- `type State struct` matching the global-constraints state shape (JSON tags
  as listed).
- `type FileInfo struct` matching the global-constraints file-info shape.
- `func New(dir string, poll func(context.Context) (domainlocation.Fix, error), now func() time.Time, logger *log.Logger) *Manager`
- `func (m *Manager) SetOnChange(onChange func(State))`
- `func (m *Manager) Configure(settings Settings)` — applies settings live.
- `func (m *Manager) ObserveFrame(at time.Time, direction heating.Direction, raw string)` —
  parses receive frames for signal 11 to maintain `engineKnown`/`engineOn`.
- `func (m *Manager) Start(ctx context.Context)` — sampling ticker goroutine
  using `Settings.SampleInterval`.
- `func (m *Manager) Sample(ctx context.Context)` — public, deterministic
  single sample (drives tests without the goroutine).
- `func (m *Manager) State() State`
- `func (m *Manager) List() ([]FileInfo, error)`
- `func (m *Manager) Read(name string) ([]byte, error)`
- `func (m *Manager) Delete(name string) error`

Behavior:

- `Sample(ctx)`:
  - Skip if tracking disabled (`Settings.Enabled` false).
  - Skip in engine-only mode when `!engineKnown` or `!engineOn`.
  - Poll the provider; on success append a point to the active track and
    rewrite atomically; on poll failure record `last_error`/`last_error_at`
    and continue (do not split the track).
- Lifecycle:
  - Engine-only mode: an engine-on frame starts a file; an engine-off frame
    finalizes it. No forced final sample on engine-off.
  - Continuous mode: first sample of a new UTC day rotates to a new daily
    file; resume an existing daily file (read it back, continue appending).
  - Disabled via `Configure` or shutdown: finalize the active file.
- Track file format (from design spec section "Track File Format"): a single
  GeoJSON Feature with `properties.name`, `start_time`, `end_time`,
  `point_count`, `sample_interval_seconds`, `times`; geometry LineString with
  `[lon, lat]` or `[lon, lat, alt]` positions.
- Atomic write: `name.tmp` then rename.
- `List`/`Read`/`Delete` operate on `track-*.geojson` names under `dir`; names
  that don't match the pattern are rejected (path-traversal guard).
- `Read` derives nothing; `List` parses each file (files are small) to fill
  `start_time`, `end_time`, `point_count`.
- Use the existing engine-frame parser conventions from
  `service/recording/manager.go` (`isOnFrame` for signal 11 on-state; the
  off-state is a signal-11 receive frame with `Data[2]&1 == 0`).
- Only signal on/off transitions notify `onChange`. Emit a state-changed
  notification after each `Sample` and each lifecycle transition.

Tests (`manager_test.go`), using `now func() time.Time` and a fake poll
function:

- Engine-only: engine-on frame starts a file; samples append; engine-off
  frame finalizes.
- Continuous: samples without engine, rotates per UTC day, resumes an
  existing daily file.
- Unknown engine state blocks sampling in engine-only mode.
- Sample writes a valid GeoJSON LineString with aligned `times`.
- Altitude stored as third coordinate element when present; two-element
  position kept when absent.
- Atomic rewrite leaves a valid file after each sample.
- List/Read/Delete behavior and path-traversal rejection.
- A test generates a sample track file into a temp directory.

## Task 4: Runtime app wiring

Wire tracking into `service/runtime/app.go`:

- Add a `tracking` field to `App` (`*tracking.Manager`).
- In `New`:
  - Construct the manager with the tracking directory from normalized config,
    the RUTX50 poll function (the same `location.Poll` the app already owns;
    only when `cfg.Location.Enabled` — guard nil when disabled), `time.Now`,
    and the logger.
  - `Configure` it from normalized config (`Enabled`, `OnlyWhenEngineOn`,
    `SampleInterval`).
  - Register `SetOnChange` to publish `tracking.state_changed` SSE events.
  - Register the manager's `ObserveFrame` as a second `RecordFrame` callback
    alongside the recorder's (the garmin `Config.RecordFrame` is a single
    function — compose them: call recorder.Observe then manager.ObserveFrame,
    or keep one callback that fans out).
  - `Start(ctx)` the sampling loop.
  - On ctx done, finalize the manager.
- Surface `State.Altitude` in `app.locationState` from the latest fix in
  `pollLocation` (Task 2 field).
- Add App methods used by HTTP (Task 5):
  - `TrackingSettings() tracking.Settings`
  - `UpdateTrackingSettings(context.Context, tracking.Settings) (tracking.Settings, error)` —
    persist to config.yaml via the existing `config.SaveFile` pattern
    (mirror `UpdateHeatingSchedule`: read current raw config, apply the
    tracking section, `Normalize`, `SaveFile`, update `a.cfg`/`a.rawConfig`,
    then `manager.Configure`). Validation errors propagate.
  - `TrackingState() tracking.State`
  - `TrackList() ([]tracking.FileInfo, error)`
  - `TrackRead(name string) ([]byte, error)`
  - `TrackDelete(name string) error`
- Extend `service/runtime/app_test.go` if it exercises New/lifecycle; at
  minimum keep the package building and existing tests green.

## Task 5: HTTP API

In `service/api/httpapi/server.go`:

- Extend the `Application` interface with:
  - `TrackingSettings() tracking.Settings`
  - `UpdateTrackingSettings(context.Context, tracking.Settings) (tracking.Settings, error)`
  - `TrackingState() tracking.State`
  - `TrackList() ([]tracking.FileInfo, error)`
  - `TrackRead(string) ([]byte, error)`
  - `TrackDelete(string) error`
- Register routes:
  - `GET /v1/tracking/settings`
  - `PUT /v1/tracking/settings` — decode `tracking.Settings`, call
    `UpdateTrackingSettings`, return saved settings. Validation errors via
    `writeValidationError`; ensure `isValidationError` matches tracking
    validation messages (add `tracking.` prefixes to the match set).
  - `GET /v1/tracking/state`
  - `GET /v1/tracks`
  - `GET /v1/tracks/{name}` — serve `application/geo+json`,
    `Content-Disposition: attachment`, name validated server-side.
  - `DELETE /v1/tracks/{name}`
- ServeMux path-value parsing: use the stdlib `r.PathValue("name")`
  (Go 1.22+ mux patterns) or an equivalent existing convention in this repo.

Extend `service/api/httpapi/server_test.go`:

- Extend `fakeApp` with the new interface methods and fields.
- Route/method checks for the new endpoints.
- Settings GET/PUT round-trip, validation error mapping.
- Tracks list/download/delete, including a path-traversal name rejection
  (e.g. `../x.geojson`).

## Task 6: Web UI

Add a "GPS trails" panel to the Settings tab in `web/static/index.html`
above the WebSocket recording panel, following the existing panel markup
(`panel`, `panel-heading`, `field`, `state-text`, `detail-text` classes):

- Switch: "Generate GPS trails" (`<input type="checkbox">`)
- Switch: "Only when engine is on"
- Numeric editor: "Sample every N seconds" (default 5, range 1-3600)
- Status line with live tracking state and errors
- Track file list with Download and Delete buttons

In `web/static/app.js`:

- Add API methods: `trackingSettings()`, `updateTrackingSettings(settings)`,
  `trackingState()`, `trackList()`, `trackDownload(name)`, `trackDelete(name)`.
- Add `state.tracking` and `state.tracks` fields.
- Render functions for the panel mirroring `renderRecording`:
  - The two switches and the numeric editor apply immediately on change
    (PUT `/v1/tracking/settings`) with a status message on save/error. No
    Save button.
  - Track list refreshes on panel load, after settings saves, and after
    delete.
  - Live status (engine known/on, tracking active, point count) updates from
    the `tracking.state_changed` SSE event.
  - Controls disabled while a settings request is in flight or the settings
    load has not completed.
- Add SSE listener for `tracking.state_changed`; add the panel to the initial
  load `Promise.all`.
- Download links render as anchor `href="/v1/tracks/{name}"` with
  `download` attribute.

Run `npm run lint` (web) and keep it clean. No style-sheet changes needed if
existing classes cover the new elements.

## Task 7: Docs and sample track file

- New `docs/gps-tracking.md`: consumption guide for the agent writing the
  InstaBlog integration — track file format (including altitude semantics),
  the track/settings API, and the lifecycle.
- Update `docs/internal-api.md`: new routes, JSON shapes, and the
  `tracking.state_changed` event.
- Update `docs/garmin-empirbus-signals.md`: note that the GPS tracker also
  relies on signal `11` as the engine-running indication.
- Update `README.md` and `config.example.yaml` (config example is Task 1; add
  the feature summary here) with the new `tracking` section and feature
  summary.
- Add committed sample file `docs/examples/sample-track.geojson` with a few
  `[lon, lat, alt]` positions for offline InstaBlog parsing.

## Verification

For each task: the task's own tests plus `go test ./...` (and `npm run lint`
for Task 6). Final branch verification: `go test ./...`, `npm run lint`,
`git diff --check`.
