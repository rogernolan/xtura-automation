# Pi Status Design

Date: 2026-08-14

## Problem

The service runs on a Raspberry Pi (target: Pi Zero 2W) but the web UI gives no
visibility into the host itself. When the van's control surface misbehaves the
operator cannot tell whether the Pi is throttling, low on memory, running hot,
or running out of disk. We want a "Pi status" pane in the UI showing a few
useful host metrics, and the Settings tab becomes "Tools" to reflect that it
now groups diagnostic tooling, not just settings.

Requirements:

- Rename the fourth tab from "Settings" to "Tools". The tab bar stays four
  tabs wide.
- A new "Pi status" panel inside the Tools pane, between the GPS trails panel
  and the WebSocket recording panel.
- Show: CPU load + model + core count, memory usage, disk usage (root and
  `/var/lib/xtura`), power quality (undervoltage/throttling), uptime,
  temperature, and any last sampling error.
- Metrics delivered the same way the rest of the UI works: the service samples
  on a background loop and publishes `pi.state_changed` SSE events; the UI
  stays live via the existing event stream.
- Graceful degradation: when a metric cannot be read (non-Pi host, missing
  sysfs file, `vcgencmd` unavailable), that metric is reported unavailable
  rather than failing the whole snapshot.

## Decisions Made in Brainstorming

- Tab structure: keep four top-level tabs (Light, Water, Heat, Tools). The
  Settings tab is renamed to Tools; its content becomes three peer panels:
  GPS trails, Pi status, WebSocket recording (Pi status in the middle).
- Delivery: service-side sampler + SSE, matching the tracking/recording
  pattern. Not frontend polling of a plain GET endpoint.
- Metric set: CPU load + model + cores, memory usage, disk (root +
  `/var/lib/xtura`), power quality, uptime, temperature, and last error if
  present. No hostname/kernel extras.
- Implementation shape: a new standalone `service/host` package modeled on
  `service/tracking/manager.go`, with an injectable metrics reader for tests.

## Architecture

New package `service/host`. A `host.Manager` mirrors the tracking manager
pattern: constructed with a sample interval, an injectable `read` function,
`now`, and a logger; a ticker goroutine samples on interval; state is kept
behind a mutex; `SetOnChange` installs a publish callback; `State()` returns
the latest snapshot.

Components:

- `host.Manager`:
  - Owns a sampling ticker goroutine started via `Manager.Start(ctx)`.
  - `Manager.Sample(ctx)` is public so unit tests drive sampling
    deterministically without the goroutine (same pattern as
    `tracking.Manager.Sample`).
  - Keeps the last successful snapshot plus `last_error`/`last_error_at`
    (recording a failed sample does not wipe the previous snapshot).
  - Publishes via `onChange` only when the new snapshot differs from the last
    published one, so unchanged samples do not spam the event stream.
  - `Shutdown()` stops cleanly.
- Default reader (`readHostMetrics`): best-effort, each metric fails
  independently (see Sources below).
- Wiring in `runtime/app.go`:
  - Constructs the `Manager` with config, `now`, and logger.
  - Starts/stops the sampling loop with the app context.
  - Publishes `pi.state_changed` SSE events.
  - Exposes `App.HostStatus() host.Metrics` for the HTTP layer.

## Sources

| Metric | Source | Unavailable when |
|---|---|---|
| Load 1/5/15 | `/proc/loadavg` (first three fields) | file unreadable |
| Cores | `/proc/cpuinfo` (`processor` line count) | file unreadable |
| Model | `/proc/cpuinfo` (`Model` line; falls back to `Hardware`) | neither line present |
| Memory | `/proc/meminfo` `MemTotal`, `MemAvailable` (KiB) | file unreadable |
| Temperature | `/sys/class/thermal/thermal_zone0/temp` (millidegrees C) | file missing/unreadable |
| Uptime | `/proc/uptime` (first field, whole seconds) | file unreadable |
| Disk | `syscall.Statfs` on `/` and `/var/lib/xtura`, deduplicated by device ID so a shared filesystem is reported once | `statfs` fails for a path |
| Power | `vcgencmd get_throttled` (preferred), falling back to the `rpi_hwmon` sysfs undervoltage alarms | neither `vcgencmd` nor the sysfs path is available |

Power decode (`get_throttled` hex bitmask): current-issue bits 0-3
(undervoltage, arm frequency capped, throttled, soft temperature limit) and
latched-since-boot bits 16-19 (same conditions in order). `status` is `ok`
when no current or latched issue is set, `warning` when any is set, and
`unavailable` when the source could not be read.

The parsers for loadavg, meminfo, cpuinfo, uptime, and the throttle bitmask
are pure functions taking strings, so they are unit-testable on any platform.

## HTTP API

New route in `service/api/httpapi/server.go`:

| Endpoint | Method | Purpose |
|---|---|---|
| `/v1/pi/state` | GET | Current host metrics snapshot. |

Wire shape (`host.Metrics`):

```json
{
  "sampled_at": "2026-08-14T09:40:00Z",
  "model": "Raspberry Pi Zero 2 W Rev 1.0",
  "cores": 4,
  "load": [0.50, 0.35, 0.20],
  "memory": { "total_bytes": 518000000, "available_bytes": 300000000, "used_percent": 42.0 },
  "disk": [
    { "mount": "/", "total_bytes": 31000000000, "used_percent": 58.0 },
    { "mount": "/var/lib/xtura", "total_bytes": 31000000000, "used_percent": 12.0 }
  ],
  "temperature_c": 52.3,
  "uptime_seconds": 123456,
  "power": {
    "status": "ok",
    "under_voltage": false,
    "throttled": false,
    "frequency_capped": false,
    "soft_temp_limit": false,
    "occurred_since_boot": ["under_voltage", "throttled"],
    "raw_throttle": "0x50000"
  },
  "last_error": "read meminfo: ...",
  "last_error_at": "2026-08-14T09:40:05Z"
}
```

- `load` is always present (length 3); values may be 0 when unreadable.
- `memory.used_percent` derived as `(total - available) / total * 100`.
- `disk` entries have no free-bytes field on the wire; only `mount`,
  `total_bytes`, and `used_percent`. If root and `/var/lib/xtura` share a
  device, one entry (root) is returned.
- `temperature_c` and `last_error`/`last_error_at` are omitted when absent.
- `power.status` is `ok`, `warning`, or `unavailable`. The four booleans are
  current-issue state; `occurred_since_boot` lists latched conditions; both
  are omitted when `status` is `unavailable`. `raw_throttle` is the
  `vcgencmd` hex string when it was the source.
- SSE event `pi.state_changed` carries the same `host.Metrics` payload and is
  published whenever the snapshot changes (including the first sample).

## Config

New optional top-level `host` section:

```yaml
host:
  sample_interval: 5s
```

Default `host.sample_interval: 5s`. Validation: non-negative; zero means the
default. Applied at construction only (no live reconfiguration).

## Web UI

- The fourth tab button changes to `Tools`; HTML ids `settingsTab` and
  `settingsPanel` are renamed to `toolsTab` and `toolsPanel` for consistency,
  with all references updated in `app.js` (setActiveTab, bindActions, state)
  and the index-body assertions in `server_test.go`.
- A new `piStatusPanel` panel inside the Tools pane, between the GPS trails
  panel and the WebSocket recording panel:
  - Panel heading "Pi status" with a state pill: power status (`OK`,
    `Warning`, `Unavailable`) or `Loading`.
  - Stat rows:
    - CPU: model + N cores; load 1/5/15
    - Memory: used % and available bytes
    - Disk: one row per mount (path and used %)
    - Temperature: `-- C` or `Unavailable`
    - Uptime: `3d 4h 5m` format
    - Power: `OK`, or the flagged conditions (e.g. "Under-voltage; throttled
      since boot"), or `Unavailable`
    - Last error: a warning detail line, only when present
- Data flow: `piState` added to `state`; loaded via `api.getPiStatus()` in
  `loadInitialState`'s `Promise.all`; `pi.state_changed` handled in
  `connectEvents`; `renderPiStatus()` added to `render()`. `formatBytes` is
  reused for memory/disk; a `formatUptime` helper is added. Small CSS addition
  for the stat row grid.

## Tests

- `service/host/manager_test.go`:
  - First sample publishes via `onChange`.
  - An unchanged snapshot does not publish again.
  - A failed sample records `last_error`/`last_error_at` and keeps the
    previous snapshot.
  - `Start` samples on the configured cadence (short fake interval) and
    `Shutdown` stops it.
- `service/host/parse_test.go`: pure parser tests (loadavg, meminfo, cpuinfo,
  uptime, throttle bitmask decode) fed literal strings, including malformed
  input returning the "unavailable" zero value.
- `service/api/httpapi/server_test.go`:
  - `fakeApp` gains a `HostStatus()` method backed by a field.
  - `TestHandlerServesPiStatus`: GET shape round-trip and method 405.
  - `TestHandlerServesWebIndex`: tab assertion updated to `Tools`, new
    `piStatusPanel` present.
- `service/runtime`: wiring test that constructing the app wires the host
  manager and that a sampled change is published on the broker (following the
  tracking wiring test pattern).
- Config tests for the `host.sample_interval` default and validation.

## Docs

- Update `docs/internal-api.md`: `/v1/pi/state` route row, `host.Metrics` JSON
  shape, `pi.state_changed` event, and the implementation-map entry for the
  new package.
- Update `README.md`: add `/v1/pi/state` to the endpoint list and a short "Pi
  status" paragraph under the web UI section describing the pane and the
  Tools tab.

## Verification

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- `rtk lint eslint web/static/app.js`
- `git diff --check`
- Live smoke test on the Pi: open the Tools tab, confirm Pi status renders
  real metrics and updates every few seconds; confirm power shows a sensible
  status for that host.
