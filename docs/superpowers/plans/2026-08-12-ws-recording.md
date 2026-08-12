# WebSocket Recording Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an on-demand, triggerable NDJSON recorder for the Garmin WebSocket traffic used by `empirebusd`, controlled from a fourth Settings tab.

**Architecture:** A runtime recording manager owns the lifecycle, output file, timer, and status. The Garmin session invokes a callback for each successful sent or received raw WebSocket frame; the manager writes NDJSON while active and evaluates received signal frames while armed. HTTP and SSE expose the manager state to the static UI.

**Tech Stack:** Go standard library, Gorilla WebSocket, existing service runtime/httpapi/SSE packages, embedded HTML/CSS/JavaScript, Go tests, ESLint.

## Global Constraints

- Keep the existing Garmin session as the only connection to the SERV.
- Store recordings in `/var/lib/xtura/recordings/` with unique UTC `.ndjson` filenames.
- Record raw sent and received WebSocket messages in machine-readable NDJSON compatible with `cmd/wscapture`.
- Defaults are `immediate` and one minute; zero duration means record until Stop or service restart.
- Wait triggers only on a received on-frame after arming: engine signal 11, heating signal 101, Victron inverter signal 197.
- Stop is idempotent and overrides waiting and timer completion.
- Do not persist armed/active state. On shutdown cancel it and append a `service_shutdown` lifecycle line to an active trace where possible.

---

### Task 1: Recording domain manager and NDJSON writer

**Files:**
- Create: `service/recording/manager.go`
- Create: `service/recording/manager_test.go`

**Interfaces:**
- Produces: `type WaitFor string`, constants `WaitImmediate`, `WaitEngineOn`, `WaitHeatingOn`, `WaitVictronOn`.
- Produces: `type StartRequest struct { WaitFor WaitFor; DurationMinutes int }`.
- Produces: `type State struct { Status string; WaitFor WaitFor; DurationMinutes int; StartedAt *time.Time; FileName string; LastFileName string; Error string }`.
- Produces: `func New(dir string, now func() time.Time, logger *log.Logger) *Manager`, `Start(StartRequest) (State, error)`, `Stop(reason string) State`, `Observe(at time.Time, direction heating.Direction, raw string)`, `Shutdown()`, and `State() State`.

- [ ] **Step 1: Write failing manager tests**

```go
func TestManagerStartsImmediatelyAndWritesWebSocketRecords(t *testing.T) {
    dir := t.TempDir()
    manager := recording.New(dir, time.Now, log.New(io.Discard, "", 0))

    state, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1})
    if err != nil || state.Status != "recording" { t.Fatalf("start = %#v, %v", state, err) }
    manager.Observe(time.Date(2026, 8, 12, 14, 30, 0, 0, time.UTC), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[101,0,1]}`)
    manager.Stop("stopped")

    records := readNDJSON(t, filepath.Join(dir, state.FileName))
    if records[1]["direction"] != "receive" || records[1]["signal"].(float64) != 101 { t.Fatal(records) }
}

func TestManagerStartsOnlyAfterNewMatchingOnFrame(t *testing.T) {
    manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
    _, _ = manager.Start(recording.StartRequest{WaitFor: recording.WaitVictronOn, DurationMinutes: 1})
    manager.Observe(time.Now(), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[196,0,1]}`)
    if got := manager.State().Status; got != "armed" { t.Fatalf("status = %q", got) }
    manager.Observe(time.Now(), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[197,0,1]}`)
    if got := manager.State().Status; got != "recording" { t.Fatalf("status = %q", got) }
}
```

- [ ] **Step 2: Run the focused tests to verify failure**

Run: `rtk test go test ./service/recording -run 'TestManager' -count=1`

Expected: FAIL because package `service/recording` does not yet exist.

- [ ] **Step 3: Implement the manager**

```go
const (
    WaitImmediate WaitFor = "immediate"
    WaitEngineOn WaitFor = "engine_on"
    WaitHeatingOn WaitFor = "heating_on"
    WaitVictronOn WaitFor = "victron_on"
)

func triggerSignal(wait WaitFor) int {
    switch wait { case WaitEngineOn: return 11; case WaitHeatingOn: return 101; case WaitVictronOn: return 197 }
    return -1
}

func isOnFrame(raw string, want int) bool {
    frame, err := heating.ParseWireFrame(raw)
    return err == nil && len(frame.Data) >= 3 && frame.Data[0]|frame.Data[1]<<8 == want && frame.Data[2]&1 != 0
}
```

Protect state and the JSON encoder with a mutex. Create the directory with `0755`, create with `os.OpenFile(..., os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)`, write lifecycle lines with `direction: "event"` and `event`, and close on stop. Use `time.AfterFunc` only after actual recording begins. Validate supported wait values and duration `>= 0`; use timestamp plus a numeric collision suffix to retry `O_EXCL` failures. On `Shutdown`, write `service_shutdown` for an open file, then close it. Convert write failures into state error, log them, and stop safely.

- [ ] **Step 4: Add edge-case tests and run the package**

```go
func TestStopOverridesArmedStateAndIsIdempotent(t *testing.T) {
    manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
    _, _ = manager.Start(recording.StartRequest{WaitFor: recording.WaitEngineOn, DurationMinutes: 1})
    manager.Stop("stopped")
    if got := manager.Stop("stopped").Status; got != "idle" { t.Fatalf("status = %q", got) }
}
func TestZeroDurationHasNoTimer(t *testing.T) {
    manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
    _, _ = manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 0})
    if got := manager.State().Status; got != "recording" { t.Fatalf("status = %q", got) }
}
func TestTimeoutStopsRecording(t *testing.T) {
    manager := recording.NewWithTimeout(t.TempDir(), time.Now, time.Millisecond, log.New(io.Discard, "", 0))
    _, _ = manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1})
    require.Eventually(t, func() bool { return manager.State().Status == "idle" }, time.Second, time.Millisecond)
}
func TestShutdownWritesServiceShutdown(t *testing.T) {
    manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
    state, _ := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1})
    manager.Shutdown()
    if got := lastEvent(t, filepath.Join(manager.Dir(), state.FileName)); got != "service_shutdown" { t.Fatalf("event = %q", got) }
}
func TestStartRejectsInvalidRequestAndSecondActiveRecorder(t *testing.T) {
    manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
    if _, err := manager.Start(recording.StartRequest{WaitFor: "bad", DurationMinutes: 1}); err == nil { t.Fatal("expected wait validation error") }
    _, _ = manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1})
    if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1}); !errors.Is(err, recording.ErrActive) { t.Fatalf("err = %v", err) }
}
```

Run: `rtk test go test ./service/recording -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the manager**

Run: `rtk git add service/recording && rtk git commit -m "feat: add WebSocket recording manager"`

### Task 2: Garmin-session hook and runtime integration

**Files:**
- Modify: `heating/session.go`
- Modify: `heating/session_test.go`
- Modify: `service/adapters/garmin/adapter.go`
- Modify: `service/runtime/app.go`
- Modify: `service/runtime/app_test.go`

**Interfaces:**
- Consumes: `recording.Manager.Observe`, `recording.Manager.Shutdown`, and `recording.Manager.State` from Task 1.
- Produces: `SessionConfig.RecordFrame func(time.Time, Direction, string)`.
- Produces: runtime methods `RecordingState() recording.State`, `StartRecording(context.Context, recording.StartRequest) (recording.State, error)`, and `StopRecording(context.Context) recording.State`.

- [ ] **Step 1: Write failing session and runtime tests**

```go
func TestSessionReportsSuccessfulSentAndReceivedRawFrames(t *testing.T) {
    var got []string
    session := NewSession(SessionConfig{WSURL: server.URL, RecordFrame: func(_ time.Time, d Direction, raw string) { got = append(got, string(d)+":"+raw) }})
    // Connect, send a command, send a server response; assert both directions in got.
}

func TestAppStopsRecordingOnContextShutdown(t *testing.T) {
    // Start an app with a temp recording directory, start immediate recording, cancel root ctx, assert shutdown terminal record.
}
```

- [ ] **Step 2: Run focused tests to verify failure**

Run: `rtk test go test ./heating ./service/runtime -run 'Test(SessionReports|AppStopsRecording)' -count=1`

Expected: FAIL because `RecordFrame` and runtime recording methods do not exist.

- [ ] **Step 3: Implement the session callback and runtime ownership**

```go
type SessionConfig struct {
    // existing fields
    RecordFrame func(time.Time, Direction, string)
}

func (s *Session) record(at time.Time, direction Direction, raw string) {
    if s.cfg.RecordFrame != nil { s.cfg.RecordFrame(at, direction, raw) }
}
```

Call `record` after successful `WriteMessage` and immediately after each receive, before parsing. Thread the callback from `garmin.Config` through `garmin.New` into `heating.NewSession`. Create the recording manager in `runtime.New` with `/var/lib/xtura/recordings`, expose the three runtime methods, and call `Shutdown` from the application's context cleanup path. Publish a `recording.state_changed` broker event after every start, trigger, stop, timeout, and failure transition; inject a manager change callback rather than duplicating state transition logic.

- [ ] **Step 4: Run affected tests**

Run: `rtk test go test ./heating ./service/adapters/garmin ./service/runtime -count=1`

Expected: PASS.

- [ ] **Step 5: Commit integration**

Run: `rtk git add heating service/adapters/garmin service/runtime && rtk git commit -m "feat: record daemon WebSocket traffic"`

### Task 3: Recording HTTP API and SSE contract

**Files:**
- Modify: `service/api/httpapi/server.go`
- Modify: `service/api/httpapi/server_test.go`

**Interfaces:**
- Consumes: runtime recording methods from Task 2.
- Produces: `GET /v1/recording/state`, `POST /v1/recording/start`, and `POST /v1/recording/stop`.

- [ ] **Step 1: Write failing HTTP tests**

```go
func TestRecordingRoutes(t *testing.T) {
    app := &fakeApp{}
    server := New(app).Handler()
    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"victron_on","duration_minutes":0}`))
    server.ServeHTTP(rr, req)
    if rr.Code != http.StatusOK { t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String()) }
}

func TestRecordingStartRejectsBadDurationAndConflict(t *testing.T) {
    app := &fakeApp{startRecordingErr: recording.ErrActive}
    server := New(app).Handler()
    bad := httptest.NewRecorder()
    server.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"immediate","duration_minutes":-1}`)))
    if bad.Code != http.StatusBadRequest { t.Fatalf("bad status = %d", bad.Code) }
    conflict := httptest.NewRecorder()
    server.ServeHTTP(conflict, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"immediate","duration_minutes":1}`)))
    if conflict.Code != http.StatusConflict { t.Fatalf("conflict status = %d", conflict.Code) }
}
func TestRecordingStopIsIdempotent(t *testing.T) {
    app := &fakeApp{}
    server := New(app).Handler()
    for range 2 {
        rr := httptest.NewRecorder()
        server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/recording/stop", nil))
        if rr.Code != http.StatusOK { t.Fatalf("status = %d", rr.Code) }
    }
    if app.stopRecordingCalls != 2 { t.Fatalf("stops = %d", app.stopRecordingCalls) }
}
```

- [ ] **Step 2: Run API tests to verify failure**

Run: `rtk test go test ./service/api/httpapi -run TestRecording -count=1`

Expected: FAIL because the recording routes and fake-app methods do not exist.

- [ ] **Step 3: Implement endpoints**

```go
type Application interface {
    // existing methods
    RecordingState() recording.State
    StartRecording(context.Context, recording.StartRequest) (recording.State, error)
    StopRecording(context.Context) recording.State
}
```

Register the three routes. Decode only `wait_for` and `duration_minutes`; reject malformed JSON and manager validation errors with HTTP 400, an already armed/active recorder with HTTP 409, and unexpected failures with HTTP 500. Return JSON `recording.State` for all success responses. Preserve all existing API behavior.

- [ ] **Step 4: Run API package tests**

Run: `rtk test go test ./service/api/httpapi -count=1`

Expected: PASS.

- [ ] **Step 5: Commit API**

Run: `rtk git add service/api/httpapi && rtk git commit -m "feat: expose recording API"`

### Task 4: Settings UI, documentation, and full verification

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/app.js`
- Modify: `web/static/styles.css`
- Modify: `README.md`
- Modify: `docs/garmin-empirbus-signals.md`

**Interfaces:**
- Consumes: recording HTTP routes and `recording.state_changed` SSE events from Tasks 2–3.
- Produces: a Settings tab and a documented recording output contract.

- [ ] **Step 1: Add static UI assertions before changing UI**

```bash
rtk proxy rg -n 'settingsTab|recordingPanel|recordingWaitFor|recordingDuration|recordingButton' web/static/index.html web/static/app.js
```

Expected: no matches before implementation.

- [ ] **Step 2: Add the fourth tab and panel markup**

```html
<button id="settingsTab" class="tab" type="button" aria-controls="settingsPanel">Settings</button>
<section id="settingsPanel" class="tab-panel" hidden>
  <div class="panel">
    <div class="panel-heading"><h2>WebSocket recording</h2><span id="recordingState" class="state-text">Idle</span></div>
    <label for="recordingWaitFor">Wait for</label>
    <select id="recordingWaitFor"><option value="immediate" selected>Start immediately</option><option value="engine_on">Engine on</option><option value="heating_on">Heating on</option><option value="victron_on">Victron inverter on</option></select>
    <label for="recordingDuration">Record for minutes</label>
    <input id="recordingDuration" type="number" min="0" step="1" value="1" inputmode="numeric">
    <button id="recordingButton" class="primary-action" type="button">Start recording</button>
    <p>0 records until you press Stop or the service restarts.</p>
    <p id="recordingDetail" class="detail-text">No recording is active.</p>
  </div>
</section>
```

- [ ] **Step 3: Implement API client, render, actions, and SSE update**

```js
async startRecording(waitFor, durationMinutes) {
  return this.request("/v1/recording/start", { method: "POST", body: { wait_for: waitFor, duration_minutes: durationMinutes } });
}
async stopRecording() { return this.request("/v1/recording/stop", { method: "POST" }); }
async recordingState() { return this.request("/v1/recording/state"); }
```

Extend `setActiveTab`, initial parallel loading, and SSE listeners. `renderRecording` must use “Start recording” only in idle state and “Stop recording” for armed or recording state; disable the select/input while non-idle. Display remaining duration from `started_at`, “until stopped” for zero, the wait condition for armed state, error text, and the returned filename. Follow the existing panel and form styles; add only focused CSS for stacked recording fields.

- [ ] **Step 4: Document recording and signal usage**

Add README endpoint and UI instructions, output location/naming, NDJSON schema, all trigger semantics, zero duration, stop priority, and restart cancellation. Update `docs/garmin-empirbus-signals.md` only with a repo-usage note that recording now relies on signal 11 as engine indication, 101 as heating-on indication, and 197 as inverter-on indication; preserve its confidence labels and cite this feature's source files.

- [ ] **Step 5: Verify UI and whole repository**

Run: `rtk proxy rg -n 'settingsTab|recordingPanel|recordingWaitFor|recordingDuration|recordingButton' web/static/index.html web/static/app.js`

Expected: all five identifiers appear in the HTML and JavaScript.

Run: `rtk lint eslint web/static/app.js && rtk test go test ./... && rtk git diff --check`

Expected: all commands exit 0.

- [ ] **Step 6: Commit UI and documentation**

Run: `rtk git add web/static README.md docs/garmin-empirbus-signals.md && rtk git commit -m "feat: add recording settings UI"`
