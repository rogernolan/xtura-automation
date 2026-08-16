# SwitchBot Temperature Sensors Design

Date: 2026-08-15

## Purpose

Add support for three SwitchBot temperature/humidity sensors (one Meter /
WoSensorTH, two Outdoor Thermo-Hygrometers / WoSensorTHO) that advertise over
BLE, and integrate their readings into the Overview dashboard alongside the
existing Garmin Alde temperature.

Readings are captured passively from BLE advertisements on the Raspberry Pi
via a raw HCI socket. History is stored to disk per sensor. The Overview
temperature group is reworked so the largest card shows the primary sensor and
a small-card row shows the remaining sensors.

## Hardware

- One SwitchBot Meter (WoSensorTH, model W0700010) advertising with device type
  `0x54` (`0x74` in add mode).
- Two SwitchBot Outdoor Thermo-Hygrometers (WoSensorTHO, model W3400010)
  advertising with device type `0x77`.
- Both are captured with the same passive scan; the payload decoder handles the
  two different advertisement layouts. Meter Plus (WoSensorTHP) was explicitly
  not ordered because it does not advertise readings.

## Discovery Approach

Chosen: option A. The daemon continuously runs the BLE scan and keeps a
rolling "seen" table of SwitchBot devices. An interactive CLI
(`cmd/switchbotctl discover`) reads that table from the local HTTP API and
writes the chosen sensors back via `PUT /v1/sensors/settings`. Settings apply
live without a service restart.

## Sensor Identity

- SwitchBot sensors are identified by MAC address: lowercased, colons stripped
  (e.g. `aabbccddeeff`).
- The Garmin Alde sensor uses the fixed id `alde`. It is not listed in the
  switchbot config section but participates in the same history store, trend
  calculation, and Overview sensor list.
- Display names come from configuration. Alde's name is always "Alde".

## Configuration

New `switchbot` section in `config.example.yaml` and `service/config/config.go`:

```yaml
switchbot:
  enabled: false
  hci_device: hci0
  sensors:
    - name: Main
      mac: "AA:BB:CC:DD:EE:FF"
      primary: true
    - name: Outside
      mac: "AA:BB:CC:DD:EE:FF"
    - name: Display
      mac: "AA:BB:CC:DD:EE:FF"
```

- `enabled` defaults to `false`. When false, no HCI socket is opened and the
  service behaves exactly as today (safe rollback). Alde history still works.
- `hci_device` defaults to `hci0`.
- Validation: unique names, unique MACs, at most one `primary`, MAC format
  `xx:xx:xx:xx:xx:xx`.
- Settings are mutable at runtime through `PUT /v1/sensors/settings`; the
  daemon persists them back to the config file using the existing
  atomic-write pattern (`config.SaveFile`).

### Big-card selection (promotion)

The Overview "big" temperature card shows, in priority order:

1. the sensor marked `primary: true` in config, if it has a reading;
2. otherwise the first switchbot sensor in config order that has a reading;
3. otherwise Alde.

A sensor "has a reading" once the daemon has stored at least one sample for
it. Immediately after this feature ships with zero switchbots configured, the
big card shows Alde. Small-row cards show the remaining sensors in config
order, with Alde appended last when it is not already the big card.

## BLE Listener

New package `service/adapters/switchbot`:

- `packet.go` — pure LE advertising-report and AD-structure parser, testable
  without hardware on all platforms.
- `payload.go` — pure SwitchBot payload decoder for WoSensorTH and WoSensorTHO
  advertisement layouts.
- `hci_linux.go` — HCI socket implementation (`//go:build linux`), passive
  scan, event read loop, reconnect with backoff.
- `hci_other.go` — `//go:build !linux` stub returning `ErrUnsupported`.
- `adapter.go` — platform-neutral glue: config map, seen-device table,
  reading callback, health.

### HCI (`hci_linux.go`)

- Open `socket(AF_BLUETOOTH, SOCK_RAW, BTPROTO_HCI)` via
  `golang.org/x/sys/unix`.
- Bind to the configured controller (default `hci0`), set LE scan parameters
  (passive), enable scanning, then read events in a loop.
- Requires `CAP_NET_ADMIN` and `CAP_NET_RAW`; these are added to the systemd
  unit's `AmbientCapabilities`.
- One controller per daemon; the single HCI socket serves all three sensors.
- On controller drop, reconnect with backoff and rescan.
- Every advertisement updates the in-flight seen-device table (MAC, device
  type, RSSI, last seen) regardless of the configured MAC filter, so discovery
  works before any sensor is configured.

### Payload decoder (`payload.go`)

Both layouts are verified against the OpenWonderLabs BLE API reference and,
for WoSensorTHO, against real captures reported in Home Assistant Community
`ble_monitor` issue #1204:

- Battery is the **last byte of the `0xFD3D` service data payload**, masked
  `& 0x7F`: `3dfd770041` → 65%, `3dfd7700e4` → 100%, `3dfd770064` → 100%.
- Temp/humidity offsets `md[10]`/`md[11]`/`md[12]` were confirmed against a
  capture decoding to −6.5 C / 35% RH.

The decoded value in a real −6.5 C / 35% / 65% battery capture
(`service data 3dfd770041`, `manufacturer data 6909c565688184329d0f05062300`)
is the regression reference in `service/adapters/switchbot/payload_test.go`.
The battery offset is still flagged for one more confirmation against our own
hardware once it arrives, since the code strips the 2-byte company id before
indexing the manufacturer data (offsets [10],[11],[12] → [8],[9],[10]).

- **WoSensorTH (Meter)** — service data for UUID `0xFD3D`, device type byte
  `0x54`/`0x74`:
  - temperature = `(byte4 & 0x7F) + (byte3 & 0x0F)/10`, negated when
    `byte4 & 0x80 == 0`;
  - humidity = `byte5 & 0x7F`; battery = `byte2 & 0x7F`.
- **WoSensorTHO (Outdoor)** — device type byte `0x77` in the `0xFD3D` service
  data; temperature and humidity come from the manufacturer data (`0xFF`)
  element per the OpenWonderLabs reference:
  - temperature = `((md[10] & 0x0F)*0.1 + (md[11] & 0x7F)) *
    (md[11]&0x80 ? 1 : -1)`;
  - humidity = `md[12] & 0x7F`; battery = last byte of the `0xFD3D` service
    data payload `& 0x7F` (confirmed by real captures; still re-checked against
    our own hardware when it arrives).
- Unknown device types (e.g. `0x48` Bot) are ignored silently.

## History Store

New package `service/history`:

- Path: `/var/lib/xtura/sensors/<id>.ndjson`, one JSON object per line:
  `{"t":"2026-08-16T10:32:05Z","temp":20.4,"hum":55}`.
- Append on each reading; keep an in-memory ring buffer of the last 2h per
  sensor.
- On startup, load the tail of each file (last N bytes, split into lines, keep
  <= 2h) so trends and charts survive restarts.
- Alde samples are appended too, from Garmin signal 22 changes, under fixed id
  `alde`.
- Housekeeping: rewrite each file keeping only the last 7 days so disk stays
  bounded. (The SwitchBot Meter itself retains ~2.5 months of data internally;
  we do not use that.)

### Trend

- 30-minute window: mean of the last 5 minutes vs the mean of the 15-30 minute
  bucket.
- Result: `rising` (>= +0.3C), `falling` (<= -0.3C), `steady` (within
  +/-0.3C), or `unavailable` (insufficient samples; never a fabricated zero
  trend).

## Runtime Wiring

- `service/runtime/app.go` constructs the history store and, when
  `switchbot.enabled`, the switchbot adapter. Its reading callback appends to
  history and updates per-sensor state.
- `publishStateLoop` already publishes `overview.state_changed` whenever the
  overview document changes; sensor readings flow through that.
- Alde history is appended from Garmin telemetry whenever the signal-22 value
  or timestamp changes.
- Settings changes via API are applied live: the adapter's MAC->name map is a
  mutex-guarded swap, no restart.

## HTTP API

Documented in `docs/internal-api.md`:

- `GET /v1/sensors/settings` / `PUT /v1/sensors/settings` — read / live-apply
  the switchbot config section; daemon persists to the config file.
- `GET /v1/sensors/discover` — rolling list of seen SwitchBot devices:
  `[{mac, dev_type, last_seen, rssi}]`.
- `GET /v1/sensors/history/{id}` — recent samples for charts.

### Overview document shape

The overview document gains a `temperature` object:

```json
{
  "temperature": {
    "sensors": [
      {"id": "alde", "name": "Alde", "source": "garmin",
       "temp": 19.5, "humidity": null, "battery": null,
       "trend": "steady", "last_seen": "2026-08-16T10:32:05Z"}
    ],
    "primary_id": "alde",
    "primary": {"id": "alde", "temp": 19.5, "trend": "steady",
                "humidity": null, "history": []}
  }
}
```

- `temperature.sensors[]` = all available sensors in display order (config
  order for switchbot, Alde last).
- `temperature.primary_id` + `temperature.primary` = the promoted big-card
  sensor per the selection rule above.
- `primary.history` = up to 2h of samples for the chart.
- The existing top-level `telemetry` / `alde_temperature_c` fields are
  preserved unchanged so nothing else breaks.

## Overview UI

Rework the Temperature group in `web/static/index.html`, `app.js`,
`styles.css`:

- **Big card** driven by `temperature.primary`: large temp value, trend
  element (rising/falling/steady/unavailable), humidity line for switchbot
  sensors, canvas 2h chart from `primary.history`, colour-coded against the
  comfort bands using the big-card temperature.
- **Small-row cards**: each remaining sensor — name, temp, trend; no chart.
  Rendered only when there is more than one sensor. A missing sensor shows a
  "-" temperature and "unavailable" trend with its name still visible.
- Zero-switchbot state: big card = Alde, no small row (looks like today).
- No detail page; the chart lives on the overview card itself.

## CLI

`cmd/switchbotctl`:

- `discover` — interactive: reads `GET /v1/sensors/discover`, shows the live
  table of seen SwitchBot devices, lets the user select sensors, assign names,
  mark primary, then writes via `PUT /v1/sensors/settings`.
- `status` — shows the daemon's current sensor settings, last readings, and
  scan health.
- Talks only to the local HTTP API; no direct BLE.

## Ops

- `ops/systemd/empirebusd.service`: add `CAP_NET_ADMIN,CAP_NET_RAW` to
  `AmbientCapabilities`.
- `config.example.yaml`: add the `switchbot:` section (`enabled:false`).
- `go.mod`: add `golang.org/x/sys` (only new dependency).
- Build remains `CGO_ENABLED=0` linux/arm64 for the Pi zero 2w; non-linux
  builds compile via the `hci_other.go` stub.

## Docs

- `docs/internal-api.md`: new endpoints + overview `temperature` shape.
- `docs/garmin-empirbus-signals.md`: note that signal 22 (Alde temperature)
  is now also consumed by the sensor history store.

## Testing

- Unit (all platforms): payload decoders with synthetic packets; LE packet
  parser; history ring/load-tail/retention; trend buckets; settings
  validation; overview document build (promotion + ordering); HTTP handlers.
- Web: extend `web/static/app.test.js` for sensors/primary/missing rendering
  and promotion cases.
- Live (on the Pi once the sensors arrive): real advertisements, confirm the
  WoSensorTHO battery offset against a capture before trusting it, confirm the
  2h chart and trends.

Verification commands: `go build ./...`, `go vet ./...`, `go test ./...`,
`npm test`, `npm run lint`.
