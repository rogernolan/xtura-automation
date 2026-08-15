# Overview Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver an Alde-led, read-only Overview dashboard with live Garmin temperature, water, and aggregate battery telemetry plus persisted capacity/comfort settings.

**Architecture:** Extend the Garmin adapter with source-confirmed scalar telemetry. The runtime exposes one Overview API document and publishes it over SSE; the static UI renders it and persists user configuration through a Settings API.

**Tech Stack:** Go, Garmin WebSocket session, YAML configuration, net/http, embedded static HTML/CSS/JavaScript, Node test runner.

## Global Constraints

- Alde is the only temperature source; SwitchBot and humidity are out of scope.
- Scalar Garmin values are signed int32 little-endian milli-units: format 6 current, format 14 percentage, format 22 kelvin.
- Gas remains an explicit Mopeka-not-configured placeholder; do not infer a Garmin gas signal.
- Overview is status/navigation-only and never sends a control request.
- Missing, stale, invalid, and unconfigured values are explicit UI states, never zero.

---

### Task 1: Source-backed Garmin overview telemetry

**Files:**
- Modify: `heating/frame.go`, `heating/session.go`, `service/adapters/garmin/adapter.go`, `service/adapters/garmin/adapter_test.go`
- Create: `service/domains/overview/types.go`
- Modify: `docs/garmin-empirbus-signals.md`

**Interfaces:**
- Produces `overview.Telemetry{AldeTemperatureC, FreshWaterPercent, GreyWaterPercent, BatteryCurrentA, BatteryStateOfChargePercent, UpdatedAt}` with pointer fields for unknown values.
- `garmin.Adapter.OverviewTelemetry() overview.Telemetry` returns only decoded received status frames.

- [ ] Add failing table-driven adapter tests for scalar payloads `106`, `12`, `13`, `212`, and `213`, including invalid type/short-frame rejection.
- [ ] Run `go test ./service/adapters/garmin -run Overview -v`; confirm failure because telemetry is absent.
- [ ] Add a session accessor for the latest received raw frame and timestamp per signal; decode signed int32 little-endian values only from incoming scalar status frames (`messagetype=16`, `messagecmd=5`). Convert: `106 => raw/1000-273.15`, `12/13/213 => raw/1000`, `212 => raw/1000`.
- [ ] Populate `OverviewTelemetry` in `pollState`, preserving a nil field when no valid reading exists.
- [ ] Update the signal reference with signal IDs, conversions, Garmin source-map provenance, and the recording as a smoke-check.
- [ ] Run `go test ./heating ./service/adapters/garmin`; expect PASS.
- [ ] Commit: `feat: decode overview telemetry`.

### Task 2: Settings, derived overview document, API, and SSE

**Files:**
- Modify: `service/config/config.go`, `service/config/config_test.go`, `config.example.yaml`
- Modify: `service/runtime/app.go`, `service/runtime/app_test.go`
- Modify: `service/api/httpapi/server.go`, `service/api/httpapi/server_test.go`

**Interfaces:**
- `config.OverviewConfig` has `UsableBatteryCapacityAh`, `GasTankCapacityLitres`, and ordered comfort thresholds.
- `runtime.App.Overview() overview.Document` and `UpdateOverviewSettings(context.Context, overview.Settings) (overview.Settings, error)`.
- `GET /v1/overview`, `GET|PUT /v1/overview/settings`, and `overview.state_changed` SSE event.

- [ ] Write config tests rejecting non-positive capacities and unordered comfort boundaries; write runtime tests for positive-current linear ETA, non-charging, full, unknown telemetry, and gas placeholder.
- [ ] Run targeted Go tests and confirm failures.
- [ ] Implement validation, YAML persistence through `config.SaveFile`, and `eta_hours = capacity_ah * (1 - soc/100) / current_a` only when current is positive and capacity/SOC are valid.
- [ ] Add API tests for GET, successful PUT, validation 400, and method rejection; publish the overview document on change.
- [ ] Run `go test ./service/config ./service/runtime ./service/api/httpapi`; expect PASS.
- [ ] Commit: `feat: add overview API and settings`.

### Task 3: Overview and More Settings UI

**Files:**
- Modify: `web/static/index.html`, `web/static/styles.css`, `web/static/navigation.js`, `web/static/navigation.test.js`, `web/static/app.js`, `web/static/app.test.js`
- Modify: `service/api/httpapi/server_test.go`

**Interfaces:**
- Route `#/overview` renders cards from `GET /v1/overview` and `overview.state_changed`.
- Route `#/more/settings` saves `PUT /v1/overview/settings`.

- [ ] Add failing navigation/UI tests covering `more/settings`, Overview’s non-mutating cards, unavailable telemetry copy, charging ETA, and gas placeholder.
- [ ] Run `npm test`; confirm the new assertions fail.
- [ ] Add balanced Alde hero, Power pair, water percentage cards, Mopeka placeholder, stale/loading/error copy, and bottom-nav deep links. Add Settings fields for comfort bands, usable battery Ah, and gas capacity.
- [ ] Extend `XturaApi`, initial state fetch, and SSE handling; never attach POST control actions to Overview cards.
- [ ] Update server static assertions for the Overview and Settings structure.
- [ ] Run `npm test && go test ./... && go vet ./... && go build ./... && rtk lint eslint web/static/app.js && git diff --check`; expect PASS.
- [ ] Manually verify mobile layout, direct hash restoration, saving settings, charging/non-charging labels, and unavailable telemetry.
- [ ] Commit: `feat: add overview dashboard`.
