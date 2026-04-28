# Mobile Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the approved mobile-first Xtura web UI served by the Go daemon, using the existing `/v1/...` API and Server-Sent Events without polling.

**Architecture:** Embed static web assets into the Go binary and mount them alongside the existing API routes. The browser app is plain HTML, CSS, and JavaScript with an API wrapper, UI state rendering, and EventSource updates. The initial schedule editor supports the single shared daily schedule shape and refuses unsupported multi-program schedules.

**Tech Stack:** Go `net/http`, Go `embed`, plain HTML/CSS/JavaScript, existing REST API and SSE endpoints.

---

### Task 1: Static Asset Serving

**Files:**
- Create: `service/api/httpapi/static.go`
- Modify: `service/api/httpapi/server.go`
- Modify: `service/api/httpapi/server_test.go`

- [ ] Write failing tests in `service/api/httpapi/server_test.go`:
  - `TestHandlerServesWebIndex` requests `/` and expects `200`, `Content-Type` containing `text/html`, and body containing `id="app"`.
  - `TestHandlerServesStaticJavaScript` requests `/static/app.js` and expects `200`, `Content-Type` containing `javascript`, and body containing `class XturaApi`.
- [ ] Run `go test ./service/api/httpapi -run 'TestHandlerServesWebIndex|TestHandlerServesStaticJavaScript'` and confirm both tests fail with missing static routes.
- [ ] Add `service/api/httpapi/static.go` with embedded `web/static` assets and a `RegisterStaticRoutes(*http.ServeMux)` helper.
- [ ] Update `Server.Handler()` to call `RegisterStaticRoutes(mux)` after registering `/v1/...` routes.
- [ ] Run the two static route tests and confirm they pass.
- [ ] Run `go test ./service/api/httpapi` and confirm existing API handler tests still pass.

### Task 2: Static App Shell And Background Asset

**Files:**
- Create: `web/static/index.html`
- Create: `web/static/styles.css`
- Create: `web/static/app.js`
- Copy: `assets/XturaBackground.png` to `web/static/xtura-background.png`

- [ ] Write the minimal app shell with visible product text `Xtura`, tab buttons for `Lighting` and `Heating`, a lighting panel, a heating target panel, a schedule panel, and a shared status area.
- [ ] Style the app as the approved compact control panel: mobile-first centered control surface, background texture, compact panels, 44px touch targets, two-tab segmented control, and no viewport-scaled fonts.
- [ ] Add JavaScript bootstrapping that renders static initial UI without API calls yet.
- [ ] Run the static route tests again and confirm embedded files compile and serve.

### Task 3: API Client And EventSource

**Files:**
- Modify: `web/static/app.js`

- [ ] Add `XturaApi` with methods for:
  - `getLightsState()`
  - `flashExteriorLights(count)`
  - `getHeatingMode()`
  - `setHeatingModeManual(targetCelsius)`
  - `setHeatingModeBoost(targetCelsius, durationMinutes)`
  - `cancelHeatingModeBoost()`
  - `getHeatingSchedule()`
  - `saveHeatingSchedule(document)`
- [ ] Implement initial load using one fetch each for lights state, heating mode, and heating schedule.
- [ ] Add `EventSource("/v1/events")` and update state from `lights.state_changed`, `heating.mode_changed`, `automation.schedule_updated`, and `heating.state_changed`.
- [ ] Do not add `setInterval`, recursive `setTimeout`, or any polling loop.

### Task 4: Lighting UI Behavior

**Files:**
- Modify: `web/static/app.js`
- Modify: `web/static/styles.css`

- [ ] Render exterior light state, unknown state, flash busy state, and last command error.
- [ ] Wire `Flash exterior lights` to `POST /v1/lights/external/flash` with `{ "count": 1 }`.
- [ ] Disable the button while `flash_in_progress` or request-in-flight.
- [ ] Show `Busy` on HTTP `409` and show returned error text on other failures.

### Task 5: Heating UI Behavior

**Files:**
- Modify: `web/static/app.js`
- Modify: `web/static/styles.css`

- [ ] Render current heating mode, manual target, boost expiry, and latest heating state target when available.
- [ ] Wire quick target buttons `5C`, `18C`, and `21C` plus `-` and `+` controls at `0.5C` increments to `POST /v1/heating/mode/manual`.
- [ ] Wire `Boost 21C for 1 hour` to `POST /v1/heating/mode/boost` with `{ "target_celsius": 21, "duration_minutes": 60 }`.
- [ ] Show `Cancel boost` only in boost mode and wire it to `POST /v1/heating/mode/boost/cancel`.
- [ ] Render the schedule editor for one enabled all-days program with up to four visible slots.
- [ ] Save the full schedule document with preserved `revision`, single enabled all-days program, and validation-friendly period objects.
- [ ] If the fetched schedule is not a single editable shared daily program, render an unsupported-shape message and disable schedule save.

### Task 6: Verification And Polish

**Files:**
- Modify as needed based on verification.

- [ ] Run `rg -n "setInterval|setTimeout\\(|poll" web/static service/api/httpapi` and confirm there is no polling loop.
- [ ] Run `go test ./...`.
- [ ] Run the daemon with `go run ./cmd/empirebusd -config ./config.example.yaml` long enough to verify it starts or identify config/runtime blockers.
- [ ] Open the UI in a browser at the local daemon URL.
- [ ] Verify mobile and desktop widths, the two tabs, background asset rendering, and no incoherent overlaps.
- [ ] Check the browser console for JavaScript errors.
- [ ] Commit the implementation once verification output has been read.
