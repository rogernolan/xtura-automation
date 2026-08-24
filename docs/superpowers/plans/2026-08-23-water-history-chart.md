# Water History Chart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist seven days of fresh/grey water levels, detect sustained fills and empties, expose usage history, and render it on the Water page.

**Architecture:** Add a focused `service/waterhistory` package for samples, persistence, settled event detection, retention, and chart-facing documents. Runtime observes valid Garmin overview telemetry from the existing one-second state loop, feeds the store, and publishes `water.history_changed`; HTTP exposes one read-only history endpoint; the browser renders a dependency-free SVG chart and summaries.

**Tech Stack:** Go, JSON/NDJSON files, existing runtime event broker and HTTP API, vanilla JavaScript, inline SVG, Node test runner.

## Global Constraints

- Use a 5 percentage-point movement threshold, configurable through YAML.
- Use a 10-minute settling period, configurable through YAML.
- Group fresh-fill and grey-empty display markers committed within one hour.
- Retain at least seven days of samples and preserve latest event state across restart.
- Timestamp a reconnect-observed event at the service time it is first observed.
- Treat invalid, missing, stale, and out-of-range telemetry as unavailable; never convert it to zero.
- Keep litres out of the model until water-tank calibration is available.
- Use a blue fresh-water series and dark-grey grey-water series on a fixed 0–100% chart.
- Use TDD: write each focused failing test, run it to confirm failure, implement the minimum behavior, then run the focused test and the relevant package suite.

---

### Task 1: Build the water-history domain and persistent detector

**Files:**
- Create: `service/waterhistory/types.go`
- Create: `service/waterhistory/store.go`
- Create: `service/waterhistory/store_test.go`

**Interfaces:**
- Consumes: `Sample{At time.Time, FreshPercent *float64, GreyPercent *float64}` and detector options `{Threshold, SettlingPeriod, GroupingWindow, Retention time.Duration}`.
- Produces: `Store.Observe(sample Sample, observedAt time.Time) (changed bool, err error)`, `Store.Document(now time.Time) Document`, `Store.Load() error`, and `Store.Compact(now time.Time) error`.

- [ ] **Step 1: Write failing tests for public document types and normal samples.** Define JSON-facing `Document` with `Samples`, `Events`, `Markers`, `Fresh`, and `Grey`; define `Point`, `Event`, `Marker`, and `Summary` with UTC timestamps and JSON tags. Test a valid sample is accepted and returned oldest-first.

- [ ] **Step 2: Run the focused test to verify it fails.**

Run: `rtk go test ./service/waterhistory -run TestObserveValidSample -count=1`

Expected: FAIL because the package and store do not exist.

- [ ] **Step 3: Implement the focused store.** Use separate `samples.ndjson` and `events.ndjson` files plus a small `state.json` file for baselines and active candidates; this makes seven-day sample compaction independent from latest-event state. Use an injected clock and directory so tests use `t.TempDir()`. Reject percentage values outside 0–100 and source timestamps that are not newer than the last accepted source timestamp. Use `observedAt` as the event timestamp when an event is first observed after reconnect.

- [ ] **Step 4: Add detector tests before filling in edge behavior.** Add named tests for: a 96-to-100 fresh rise that commits only after ten minutes; an 80-to-74 grey fall that commits only after ten minutes; a 50-to-56-to-64-to-90 multi-minute fill that produces one event; a 50-to-54 subthreshold movement that produces none; a rising candidate that reverses before settling; a reconnect-observed event whose timestamp equals `observedAt` rather than its old source timestamp; and startup state that establishes a baseline without an event.

- [ ] **Step 5: Run the detector tests to verify the new cases fail.**

Run: `rtk go test ./service/waterhistory -run 'Test(Fill|GreyEmpty|LongFill|Subthreshold|Opposite|Reconnect|Startup)' -count=1`

Expected: FAIL for the unimplemented detector behavior.

- [ ] **Step 6: Implement settled candidates.** Maintain one settled baseline and at most one candidate per tank. A fresh candidate moves upward; a grey candidate moves downward. Once threshold is reached, keep accumulating same-direction readings and reset the candidate’s quiet timer whenever the level changes materially. Commit the candidate at the final observed level after ten minutes without movement; move the settled baseline to that level. Never create an event from the initial sample.

- [ ] **Step 7: Add persistence, restart, retention, and grouping tests.** Test `Load` restores samples, baselines, candidates, and latest events; `Compact` retains the seven-day boundary and latest event; duplicate timestamps do not duplicate samples; paired event timestamps within one hour produce one marker while the independent events remain present; events outside one hour produce separate markers.

- [ ] **Step 8: Implement load, compaction, summaries, and markers.** Keep enough event state to calculate `days_since` and `used_percent` from the latest committed event to the current valid level. Pair only a fresh fill with a grey empty, and place the combined marker at the event time selected by the documented deterministic rule (the later commit time). Sort samples and events chronologically in the returned document.

- [ ] **Step 9: Run the complete domain suite.**

Run: `rtk go test ./service/waterhistory -count=1`

Expected: PASS.

- [ ] **Step 10: Commit the domain component.**

Run: `rtk git add service/waterhistory && rtk git commit -m "feat: track water history and events"`

---

### Task 2: Configure and integrate runtime observation

**Files:**
- Modify: `service/config/config.go`
- Modify: `service/config/config_test.go`
- Modify: `service/runtime/app.go`
- Modify: `service/runtime/overview_test.go`
- Modify: `service/runtime/sensors.go` only if the shared history directory helper belongs there

**Interfaces:**
- Consumes: `waterhistory.Store` and `overview.Telemetry` from Task 1.
- Produces: `App.WaterHistory() waterhistory.Document` and `water.history_changed` broker events.

- [ ] **Step 1: Write failing configuration tests.** Add `WaterHistoryConfig` to `config.Config` and `NormalizedConfig` with `threshold_percent`, `settling_period`, and `grouping_window`. Verify omitted values normalize to 5, 10 minutes, and 1 hour; reject non-positive threshold, threshold above 100, negative durations, and grouping shorter than zero.

- [ ] **Step 2: Run the config tests to verify failure.**

Run: `rtk go test ./service/config -run 'Test.*WaterHistory' -count=1`

Expected: FAIL because the config fields and normalization do not exist.

- [ ] **Step 3: Implement config normalization and validation.** Preserve existing config defaults and YAML round-tripping. Add the `water_history` block with the three defaults to both `config.example.yaml` and `config.sim.yaml`, so the threshold and settling period can be tuned in the simulator and deployment configuration.

- [ ] **Step 4: Write failing runtime integration tests.** Construct an app with a temporary water-history directory and fake overview telemetry. Verify repeated one-second loop observations of the same `UpdatedAt` create one sample, stale telemetry is ignored, a committed event publishes `water.history_changed`, and `WaterHistory()` returns persisted data after app construction.

- [ ] **Step 5: Implement runtime wiring.** Create/load `/var/lib/xtura/water-history` during `runtime.New`, inject the normalized options and clock, and add `observeWaterTelemetry()` beside `observeAldeTelemetry()`. Feed only valid, non-stale fresh/grey values; pass `a.now().UTC()` as observation time so reconnect-observed events use service time. Call it from `publishStateLoop` and publish the history event when the store reports a changed response. Run compaction from the existing hourly maintenance path.

- [ ] **Step 6: Run runtime and config tests.**

Run: `rtk go test ./service/config ./service/runtime -count=1`

Expected: PASS.

- [ ] **Step 7: Commit the integration.**

Run: `rtk git add service/config service/runtime config.example.yaml config.sim.yaml && rtk git commit -m "feat: integrate water history telemetry"`

---

### Task 3: Expose the history API and event contract

**Files:**
- Modify: `service/api/httpapi/server.go`
- Modify: `service/api/httpapi/server_test.go`
- Modify: `docs/internal-api.md`

**Interfaces:**
- Consumes: `Application.WaterHistory() waterhistory.Document` and the runtime broker.
- Produces: `GET /v1/water/history` and `water.history_changed` SSE payloads.

- [ ] **Step 1: Add failing HTTP tests.** Extend the fake application with `WaterHistory()`. Test `GET /v1/water/history` returns the document, non-GET requests return 405, and the JSON contains samples, independent events, grouped markers, and both summaries.

- [ ] **Step 2: Run focused HTTP tests to verify failure.**

Run: `rtk go test ./service/api/httpapi -run 'TestHandleWaterHistory' -count=1`

Expected: FAIL because the interface and route do not exist.

- [ ] **Step 3: Implement the route and interface method.** Add the water-history domain import, interface method, route registration, and a GET-only handler following the existing `/v1/water/state` pattern.

- [ ] **Step 4: Update the internal API document.** Document the endpoint, response fields, event name, seven-day window, and percentage-point semantics. Explicitly document reconnect-observed event timestamps.

- [ ] **Step 5: Run HTTP tests and the full Go suite.**

Run: `rtk go test ./service/api/httpapi ./service/runtime ./service/waterhistory -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the API surface.**

Run: `rtk git add service/api/httpapi docs/internal-api.md && rtk git commit -m "feat: expose water history API"`

---

### Task 4: Render the seven-day Water chart and summaries

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/styles.css`
- Modify: `web/static/app.js`
- Modify: `web/static/app.test.js`
- Modify: `service/api/httpapi/server_test.go` if embedded static-page assertions need updating

**Interfaces:**
- Consumes: `GET /v1/water/history` and `water.history_changed` payloads from Task 3.
- Produces: accessible `waterHistoryChart`, fresh/grey SVG paths, grouped event markers, and summary text nodes.

- [ ] **Step 1: Write failing browser tests.** Add history fixtures and assert Water-page markup owns the chart and summary IDs. Test `renderWaterHistory` produces a 0–100 SVG scale, blue fresh path, dark-grey grey path, seven-day domain, one marker for paired events, and exact no-event/usage text.

- [ ] **Step 2: Run the focused browser tests to verify failure.**

Run: `rtk npm test -- --test-name-pattern='water history|water chart'`

Expected: FAIL because the API method, state field, markup, and renderer do not exist.

- [ ] **Step 3: Add the API method, state, and markup.** Add `getWaterHistory()`, `waterHistory` state, a chart container with explicit labels, and two summary paragraphs below the chart. Keep the existing valve controls intact.

- [ ] **Step 4: Implement SVG rendering.** Map `t` from `now - 7d` to `now` and percentage to the fixed chart bounds; generate separate paths with gaps for unavailable points; render Y labels at 100/75/50/25/0 and readable X labels across seven days; use the requested colors; render grouped markers with accessible labels and event details.

- [ ] **Step 5: Load and refresh history.** Include the history request in `loadInitialState`, call `renderWaterHistory` from `renderWater`, and update the history state on `water.history_changed`. Preserve the existing one-request initialization and avoid polling.

- [ ] **Step 6: Run all frontend tests and formatting checks.**

Run: `rtk npm test`

Expected: PASS for every intended Node test file, including navigation and rendered-DOM assertions.

- [ ] **Step 7: Commit the UI.**

Run: `rtk git add web/static/index.html web/static/styles.css web/static/app.js web/static/app.test.js service/api/httpapi/server_test.go && rtk git commit -m "feat: chart water history on water page"`

---

### Task 5: Verify simulation, retention, and handoff

**Files:**
- Modify: `docs/internal-api.md` or relevant project docs if verification reveals a missing operational note

- [ ] **Step 1: Run the complete verification suite.**

Run: `rtk go test ./... && rtk npm test`

Expected: PASS with no test-file omissions.

- [ ] **Step 2: Run the mandated simulator.**

Run: `rtk proxy ./scripts/sim/run-sim.sh`

Expected: the service builds from the current checkout and serves the Water page with the chart endpoint available; stop the simulator after the smoke check using its normal process handling.

- [ ] **Step 3: Verify the API and chart manually.** Confirm `/v1/water/history` returns a seven-day document, values remain within 0–100, the chart has both requested series colors, paired markers are not duplicated, and the summaries update only after committed events.

- [ ] **Step 4: Inspect the final diff and status.**

Run: `rtk git diff HEAD~4 --check && rtk git status --short`

Expected: no whitespace errors; only intended feature commits are present. Preserve the pre-existing untracked `garmin-ws20260815T142323Z.ndjson` capture.

- [ ] **Step 5: Confirm the final handoff.** Record the feature commit IDs, verification commands and results, the configured history directory, and the preserved pre-existing untracked capture in the final handoff.
