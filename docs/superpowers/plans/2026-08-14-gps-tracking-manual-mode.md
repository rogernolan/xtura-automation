# GPS Tracking Manual Mode

## Date
2026-08-14

## Context
GPS tracking was just shipped (PR #2, merged) with a top-level "Generate GPS trails" master switch plus an "Only when engine is on" checkbox. When the engine-only flag is off, the manager silently wrote 24-hour daily files (`track-2026-08-13.geojson`) with no user control. The user wants:

- The "Generate GPS trails" master switch removed entirely.
- "Only when engine is on" renamed to "When engine is on".
- When "When engine is on" is unchecked, the UI shows working manual **Start recording** / **Stop recording** buttons that drive per-session track files (`track-<UTC-start>Z.geojson`), exactly like the audio-recording panel pattern.
- No daily/continuous mode at all: both engine and manual modes produce session files.

Approved design (brainstorming session, 2026-08-14): Approach A — remove `Enabled` from config/manager/state/API/UI everywhere; rename `OnlyWhenEngineOn` → `WhenEngineOn` (`when_engine_on`) everywhere; add `StartRecording(at)`/`StopRecording()` to the manager; expose `POST /v1/tracking/start` and `POST /v1/tracking/stop`. Manual sessions are runtime-only: a service restart stops recording (engine mode still auto-resumes on engine-on). A mode flip (configured `when_engine_on` toggles) finalizes the active track.

The user chose to skip the spec; this plan supersedes it.

## Ground Rules
- Follow test-driven development: write the failing test, run it, then implement, then re-run.
- Run the full suite after each task: `go test ./...` in repo root. UI has no tests; verify with `go build ./...` and a manual `curl` pass described in Task 5.
- Commit after each task with a message matching repo style (short imperative summary, see `git log --oneline`). Example: `tracking: replace continuous mode with manual start/stop`.
- yaml.Unmarshal in `LoadFile` is lenient (extra keys are ignored), so stale config keys (`tracking.enabled`, `tracking.only_when_engine_on`) are harmless before the Pi config is cleaned up in Task 6.

## Test Matrix
- `service/config`: TestNormalizeTrackingDefaults, TestTrackingSectionRoundTrip, TestValidateRejectsTrackingWithoutLocation (renamed), TestValidateTrackingSampleIntervalBounds
- `service/tracking`: engine session lifecycle, manual session lifecycle, stop recording finalizes, mode-flip finalizes, state JSON shape, altitude, single-fix skip, delete-active, list/read/delete + path traversal, poll failure, onChange notifications, atomic rewrite, live interval change, engine-state gating
- `service/runtime`: tracking wiring (manual mode driven by StartTracking), settings update, concurrent writers, no-location methods (incl. start/stop errors)
- `service/api/httpapi`: settings GET/PUT, settings DTO conversion, update validation, tracking state route, tracking start/stop routes (200 + 409 in engine mode)

---

## Task 1 — Config: drop `enabled`, rename to `when_engine_on`

### Files
- `service/config/config.go`
- `service/config/config_test.go`
- `config.example.yaml`

### 1a. `service/config/config.go`

**`TrackingConfig` (currently lines 69-74):**
```go
type TrackingConfig struct {
	Enabled          bool          `yaml:"enabled,omitempty"`
	OnlyWhenEngineOn *bool         `yaml:"only_when_engine_on,omitempty"`
	SampleInterval   time.Duration `yaml:"sample_interval,omitempty"`
	Dir              string        `yaml:"dir,omitempty"`
}
```
becomes:
```go
type TrackingConfig struct {
	WhenEngineOn    *bool         `yaml:"when_engine_on,omitempty"`
	SampleInterval  time.Duration `yaml:"sample_interval,omitempty"`
	Dir             string        `yaml:"dir,omitempty"`
}
```

**`NormalizedTracking` (currently lines 135-140):**
```go
type NormalizedTracking struct {
	Enabled          bool
	OnlyWhenEngineOn bool
	SampleInterval   time.Duration
	Dir              string
}
```
becomes:
```go
type NormalizedTracking struct {
	WhenEngineOn   bool
	SampleInterval time.Duration
	Dir            string
}
```

**Validation (currently lines 245-247):**
```go
	if c.Tracking.Enabled && !c.Location.Enabled {
		problems = append(problems, "tracking.enabled requires location.enabled")
	}
```
becomes:
```go
	if (c.Tracking.WhenEngineOn != nil || c.Tracking.SampleInterval != 0 || c.Tracking.Dir != "") && !c.Location.Enabled {
		problems = append(problems, "tracking requires location.enabled")
	}
```

**`normalizeTracking` (currently lines 382-400):**
```go
func normalizeTracking(in TrackingConfig) NormalizedTracking {
	out := NormalizedTracking{
		Enabled:        in.Enabled,
		SampleInterval: in.SampleInterval,
		Dir:            strings.TrimSpace(in.Dir),
	}
	if in.OnlyWhenEngineOn == nil {
		out.OnlyWhenEngineOn = true
	} else {
		out.OnlyWhenEngineOn = *in.OnlyWhenEngineOn
	}
	if out.SampleInterval == 0 {
		out.SampleInterval = 5 * time.Second
	}
	if out.Dir == "" {
		out.Dir = "/var/lib/xtura/tracks"
	}
	return out
}
```
becomes:
```go
func normalizeTracking(in TrackingConfig) NormalizedTracking {
	out := NormalizedTracking{
		SampleInterval: in.SampleInterval,
		Dir:            strings.TrimSpace(in.Dir),
	}
	if in.WhenEngineOn == nil {
		out.WhenEngineOn = true
	} else {
		out.WhenEngineOn = *in.WhenEngineOn
	}
	if out.SampleInterval == 0 {
		out.SampleInterval = 5 * time.Second
	}
	if out.Dir == "" {
		out.Dir = "/var/lib/xtura/tracks"
	}
	return out
}
```

### 1b. `service/config/config_test.go`

Read the current file first. Apply these edits:

1. **`trackingBaseConfig` helper:** change
   ```go
			Tracking: TrackingConfig{
				OnlyWhenEngineOn: ptrBool(true),
			},
   ```
   to
   ```go
			Tracking: TrackingConfig{
				WhenEngineOn: ptrBool(true),
			},
   ```

2. **`TestNormalizeTrackingDefaults`:** delete the assertion block that reads
   ```go
	if normalized.Tracking.Enabled {
		t.Fatal("expected tracking to be disabled by default")
	}
   ```
   and change
   ```go
	if !normalized.Tracking.OnlyWhenEngineOn {
		t.Fatal("expected only_when_engine_on to default to true")
	}
   ```
   to
   ```go
	if !normalized.Tracking.WhenEngineOn {
		t.Fatal("expected when_engine_on to default to true")
	}
   ```

3. **`TestTrackingSectionRoundTrip`:** the yaml fixture becomes
   ```yaml
   tracking:
     when_engine_on: false
     sample_interval: 30s
     dir: /var/lib/xtura/tracks
   ```
   Delete the `if normalized.Tracking.Enabled { ... }` assertion. Change
   ```go
	if normalized.Tracking.OnlyWhenEngineOn {
		t.Fatal("expected only_when_engine_on false to round-trip")
	}
   ```
   to
   ```go
	if normalized.Tracking.WhenEngineOn {
		t.Fatal("expected when_engine_on false to round-trip")
	}
   ```

4. **Rename `TestValidateRejectsTrackingEnabledWithoutLocation` → `TestValidateRejectsTrackingWithoutLocation`** and replace its body with:
   ```go
   func TestValidateRejectsTrackingWithoutLocation(t *testing.T) {
   	cfg := trackingBaseConfig()
   	cfg.Location.Enabled = false
   	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "tracking requires location.enabled") {
   		t.Fatalf("expected tracking requires location.enabled error, got %v", err)
   	}
   }
   ```

### 1c. `config.example.yaml` (lines 44-49)
```yaml
tracking:
  enabled: false
  # Only record fixes while the engine is running (Garmin power state).
  only_when_engine_on: true
  sample_interval: 5s
  dir: /var/lib/xtura/tracks
```
becomes:
```yaml
tracking:
  # Record fixes only while the engine is running (Garmin power state). Set to
  # false to expose manual start/stop in the web UI.
  when_engine_on: true
  sample_interval: 5s
  dir: /var/lib/xtura/tracks
```

### Verify
```
go test ./service/config/
```

### Commit
`config: replace tracking enabled toggle with when_engine_on`

---

## Task 2 — Tracking manager: manual Start/Stop, remove daily mode

### File
- `service/tracking/manager.go`
- `service/tracking/manager_test.go`

### 2a. `service/tracking/manager.go` edits

**Package doc (lines 1-2):** update to drop "per-day":
```go
// Package tracking samples a location provider into per-session GeoJSON track
// files, optionally gated on the Garmin engine signal.
```

**`Settings` (lines 22-27):**
```go
// Settings controls tracking behaviour. Directory is fixed at construction.
type Settings struct {
	Enabled          bool
	OnlyWhenEngineOn bool
	SampleInterval   time.Duration
}
```
becomes:
```go
// Settings controls tracking behaviour. Directory is fixed at construction.
type Settings struct {
	WhenEngineOn   bool
	SampleInterval time.Duration
}
```

**`State` (lines 29-42):** remove the `Enabled` field and rename the engine field:
```go
// State is the runtime tracking state.
type State struct {
	WhenEngineOn          bool       `json:"when_engine_on"`
	SampleIntervalSeconds float64    `json:"sample_interval_seconds"`
	EngineKnown           bool       `json:"engine_known"`
	EngineOn              bool       `json:"engine_on"`
	Tracking              bool       `json:"tracking"`
	CurrentFile           string     `json:"current_file,omitempty"`
	PointCount            int        `json:"point_count"`
	LastSampleAt          *time.Time `json:"last_sample_at,omitempty"`
	LastError             string     `json:"last_error,omitempty"`
	LastErrorAt           *time.Time `json:"last_error_at,omitempty"`
}
```

**Sentinel error:** add after the `FileInfo` struct (near line 51):
```go
// ErrEngineMode reports a manual start/stop call made while tracking is gated
// on the engine. The UI hides manual controls in that mode.
var ErrEngineMode = errors.New("start/stop tracking is only available in manual mode")
```

**`activeTrack` (lines 55-63):** drop the `day` field and fix the comment:
```go
// activeTrack is the in-memory representation of the session file being
// written.
type activeTrack struct {
	name   string
	times  []time.Time
	points [][]float64
}
```

**`Configure` (lines 109-123):** finalize on any engine-mode flip:
```go
// Configure applies settings live. Switching between engine-gated and manual
// mode finalizes the active track so a session cannot leak across modes. A
// wake signal prompts Start to recreate its ticker so a sample-interval change
// takes effect immediately.
func (m *Manager) Configure(settings Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if settings.WhenEngineOn != m.settings.WhenEngineOn {
		m.finalizeLocked()
	}
	m.settings = settings
	m.signalWakeLocked()
	m.notifyLocked(m.snapshotLocked())
}
```

**New `StartRecording`/`StopRecording`:** insert between `Configure` and `signalWakeLocked`:
```go
// StartRecording begins a new session in manual mode. If a session is already
// active it is left untouched. In engine mode it is a no-op; the HTTP layer
// rejects the call via ErrEngineMode.
func (m *Manager) StartRecording(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track == nil {
		m.beginSessionLocked(at)
	}
	m.notifyLocked(m.snapshotLocked())
}

// StopRecording finalizes the active session.
func (m *Manager) StopRecording() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track != nil {
		m.finalizeLocked()
	}
	m.notifyLocked(m.snapshotLocked())
}
```

**`ObserveFrame` (lines 135-159):** drop the `Enabled` gate:
```go
	m.engineKnown = known
	m.engineOn = on
	if m.settings.WhenEngineOn {
		if on {
			m.beginSessionLocked(at.UTC())
		} else {
			m.finalizeLocked()
		}
	}
```

**`Sample` (lines 199-228):** replace the two gate checks (drop `Enabled`, gate manual mode on an active session):
```go
// Sample polls the location provider once and appends a point to the active
// session track. In engine mode it is skipped until the engine is known and
// on; in manual mode it is skipped unless a session is active.
func (m *Manager) Sample(ctx context.Context) {
	m.mu.Lock()
	if m.settings.WhenEngineOn && (!m.engineKnown || !m.engineOn) {
		m.mu.Unlock()
		return
	}
	if !m.settings.WhenEngineOn && m.track == nil {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	fix, err := m.poll(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings.WhenEngineOn && (!m.engineKnown || !m.engineOn) {
		return
	}
	if !m.settings.WhenEngineOn && m.track == nil {
		return
	}
	at := m.now().UTC()
	if err != nil {
		m.recordErrorLocked(at, err)
		m.notifyLocked(m.snapshotLocked())
		return
	}
	m.appendSampleLocked(fix, at)
	m.notifyLocked(m.snapshotLocked())
}
```

**`appendSampleLocked` (lines 321-349):** begin a session in engine mode only; in manual mode the track must already exist:
```go
func (m *Manager) appendSampleLocked(fix domainlocation.Fix, at time.Time) {
	if m.settings.WhenEngineOn && m.track == nil {
		m.beginSessionLocked(at)
	}
	if m.track == nil {
		return
	}
	position := []float64{fix.Longitude, fix.Latitude}
	if fix.Altitude != nil {
		position = append(position, *fix.Altitude)
	}
	m.track.points = append(m.track.points, position)
	m.track.times = append(m.track.times, at)
	if err := m.writeTrackLocked(); err != nil {
		m.track.points = m.track.points[:len(m.track.points)-1]
		m.track.times = m.track.times[:len(m.track.times)-1]
		m.recordErrorLocked(at, err)
		return
	}
	sampleAt := at
	m.lastSampleAt = &sampleAt
	m.lastError = ""
	m.lastErrorAt = nil
}
```

**Delete `beginDailyLocked` (lines 357-372)** in its entirety. Keep `beginSessionLocked`.

**`snapshotLocked` (lines 414-439):** drop the `Enabled`/`OnlyWhenEngineOn` fields:
```go
func (m *Manager) snapshotLocked() State {
	state := State{
		WhenEngineOn:          m.settings.WhenEngineOn,
		SampleIntervalSeconds: m.settings.SampleInterval.Seconds(),
		EngineKnown:           m.engineKnown,
		EngineOn:              m.engineOn,
		LastSampleAt:          m.lastSampleAt,
		LastError:             m.lastError,
		LastErrorAt:           m.lastErrorAt,
	}
	if m.track != nil {
		state.Tracking = true
		state.CurrentFile = m.track.name
		state.PointCount = len(m.track.times)
	}
	if state.LastSampleAt != nil {
		sampleAt := *state.LastSampleAt
		state.LastSampleAt = &sampleAt
	}
	if state.LastErrorAt != nil {
		errorAt := *state.LastErrorAt
		state.LastErrorAt = &errorAt
	}
	return state
}
```

**Delete `sameUTCDay` (lines 527-531).** Leave `engineSignalState` and `validTrackName` unchanged.

### 2b. `service/tracking/manager_test.go` rework

Read the current file first; it is the biggest test surface. Apply these edits:

**Mechanical rename** (Settings/State literals): every occurrence of
`tracking.Settings{Enabled: true, OnlyWhenEngineOn: true, ...}` → `tracking.Settings{WhenEngineOn: true, ...}`; every `OnlyWhenEngineOn:` key inside a `Settings{...}` literal → `WhenEngineOn:`. In `State` assertion helpers, delete `state.Enabled`/`!state.Enabled` assertions and rename `state.OnlyWhenEngineOn` → `state.WhenEngineOn`.

**`TestEngineOnlySessionLifecycle`:** keep the flow (frame on → samples → frame off finalizes). Update the settings literal to `tracking.Settings{WhenEngineOn: true, SampleInterval: 5 * time.Second}` and drop the `state.Enabled` assertions. Add a `manager.StartRecording(clock.Add(1 * time.Minute))` **no-op guard** assertion:
```go
	manager.StartRecording(clock.Add(1 * time.Minute))
	state = manager.State()
	if !state.Tracking {
		t.Fatal("StartRecording must not disturb an engine-gated session")
	}
```
(place after the session is active, before the frame-off; asserts engine-mode manual calls are no-ops).

**Delete `TestContinuousDayRotationAndResume`** entirely — the daily mode it covers no longer exists.

**Replace `TestConfigureDisableFinalizesActiveTrack`** with a manual-mode stop test:
```go
func TestStopRecordingFinalizesActiveTrack(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(t, time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	var records []FixRecord
	manager := newTrackingTestManager(t, dir, clock, recordFixes(&records), 10*time.Millisecond, false)
	manager.Start(context.Background(), tracking.Settings{WhenEngineOn: false, SampleInterval: 5 * time.Second})
	defer manager.Shutdown()

	manager.StartRecording(clock.Add(10 * time.Second))
	sampleAndWait(t, manager, clock, &records)
	sampleAndWait(t, manager, clock, &records)

	state := manager.State()
	if !state.Tracking || state.PointCount != 2 {
		t.Fatalf("expected active 2-point session, got %+v", state)
	}
	manager.StopRecording()
	state = manager.State()
	if state.Tracking || state.CurrentFile != "" || state.PointCount != 0 {
		t.Fatalf("expected finalize after stop, got %+v", state)
	}
	if !manager.Start(context.Background(), tracking.Settings{WhenEngineOn: false, SampleInterval: 5 * time.Second}) {
		// guard: StopRecording must not tear the loop down
	}
}
```
NOTE: read the existing helper signatures (`newTrackingTestManager`, `sampleAndWait`, `recordFixes`, `FixRecord`) in the current test file and match them exactly; if the file uses a different constructor shape, adapt this test to it rather than inventing new helpers.

**New manual-session test** (replaces the coverage lost with the daily test):
```go
func TestManualSessionWritesSessionFileAndIgnoresFrames(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(t, time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	var records []FixRecord
	manager := newTrackingTestManager(t, dir, clock, recordFixes(&records), 10*time.Millisecond, false)
	manager.Start(context.Background(), tracking.Settings{WhenEngineOn: false, SampleInterval: 5 * time.Second})
	defer manager.Shutdown()

	// Frames must not auto-start a session in manual mode.
	manager.ObserveFrame(clock.Add(1*time.Second), heating.DirectionReceive, engineFrame(true))
	manager.Sample(context.Background())
	if state := manager.State(); state.Tracking {
		t.Fatalf("engine frames must not start a manual session, got %+v", state)
	}

	manager.StartRecording(clock.Add(2 * time.Second))
	sampleAndWait(t, manager, clock, &records)
	sampleAndWait(t, manager, clock, &records)
	state := manager.State()
	if !state.Tracking || state.PointCount != 2 {
		t.Fatalf("expected active 2-point session, got %+v", state)
	}
	if state.CurrentFile == "" || !strings.HasPrefix(state.CurrentFile, "track-20260814T") {
		t.Fatalf("expected session file name, got %q", state.CurrentFile)
	}

	manager.StopRecording()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != state.CurrentFile {
		t.Fatalf("expected exactly one session file %s, got %v", state.CurrentFile, entries)
	}
}
```

**Rework `TestConfigureEngineOnlyModeSwitchFinalizesDailyTrack` → `TestConfigureModeSwitchFinalizesActiveTrack`:** drive both flips (manual→engine and engine→manual) and assert the file finalizes each time. Adapt to the helpers in the file; the shape is:
```go
func TestConfigureModeSwitchFinalizesActiveTrack(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(t, time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	var records []FixRecord
	manager := newTrackingTestManager(t, dir, clock, recordFixes(&records), 10*time.Millisecond, false)
	manager.Start(context.Background(), tracking.Settings{WhenEngineOn: false, SampleInterval: 5 * time.Second})
	defer manager.Shutdown()

	manager.StartRecording(clock.Add(1 * time.Second))
	sampleAndWait(t, manager, clock, &records)
	sampleAndWait(t, manager, clock, &records)
	if state := manager.State(); !state.Tracking {
		t.Fatalf("expected active manual session, got %+v", state)
	}

	manager.Configure(tracking.Settings{WhenEngineOn: true, SampleInterval: 5 * time.Second})
	if state := manager.State(); state.Tracking {
		t.Fatalf("expected manual session finalized on engine switch, got %+v", state)
	}

	manager.ObserveFrame(clock.Add(2*time.Second), heating.DirectionReceive, engineFrame(true))
	manager.Sample(context.Background())
	if state := manager.State(); !state.Tracking {
		t.Fatalf("expected engine-gated session to auto-start, got %+v", state)
	}

	manager.Configure(tracking.Settings{WhenEngineOn: false, SampleInterval: 5 * time.Second})
	if state := manager.State(); state.Tracking {
		t.Fatalf("expected engine-gated session finalized on manual switch, got %+v", state)
	}
}
```

**Other tests**: update settings literals/state assertions mechanically. In particular:
- `TestStateAndFileInfoJSONShape`: the JSON-key list assertion drops `"enabled"` and changes `"only_when_engine_on"` → `"when_engine_on"`. (Current list: `[]string{"enabled", "only_when_engine_on", "sample_interval_seconds", "engine_known", "engine_on", "tracking", "current_file", "point_count", "last_sample_at", "last_error", "last_error_at"}`.)
- Tests that previously relied on daily filenames (`TestSampleGeneratesTrackFileInTempDirectory`, `TestAtomicRewriteLeavesValidFileAfterEachSample`, `TestSingleFixTrackIsNotWritten`, `TestDeleteActiveTrackFinalizesAndDoesNotResurrect`, `TestAltitudeStoredAsThirdCoordinateElementWhenPresent`, `TestStartSamplesOnIntervalAndFinalizesOnCancel`, `TestConfigureChangesSampleIntervalLive`): rework to either engine mode (`Settings{WhenEngineOn: true, ...}` + `ObserveFrame(...engineFrame(true))` to auto-start, or manual mode (`Settings{WhenEngineOn: false, ...}` + `StartRecording`). Session file names are `track-20260814T...Z.geojson`, not `track-2026-08-13.geojson` — update any hard-coded names and any resume/rotation assertions (those are gone).
- `TestUnknownEngineStateBlocksSamplingInEngineOnlyMode`: mechanical field rename only.

### Verify
```
go test ./service/tracking/
```

### Commit
`tracking: add manual start/stop, drop continuous daily mode`

---

## Task 3 — Runtime app: settings shape + StartTracking/StopTracking

### File
- `service/runtime/app.go`
- `service/runtime/tracking_test.go`

### 3a. `service/runtime/app.go` edits

**Wiring (lines 162-166):**
```go
		trackingManager.Configure(tracking.Settings{
			Enabled:          cfg.Tracking.Enabled,
			OnlyWhenEngineOn: cfg.Tracking.OnlyWhenEngineOn,
			SampleInterval:   cfg.Tracking.SampleInterval,
		})
```
becomes:
```go
		trackingManager.Configure(tracking.Settings{
			WhenEngineOn:   cfg.Tracking.WhenEngineOn,
			SampleInterval: cfg.Tracking.SampleInterval,
		})
```

**`TrackingSettings` (lines 284-294):**
```go
	return tracking.Settings{
		Enabled:          state.Enabled,
		OnlyWhenEngineOn: state.OnlyWhenEngineOn,
		SampleInterval:   time.Duration(state.SampleIntervalSeconds * float64(time.Second)),
	}
```
becomes:
```go
	return tracking.Settings{
		WhenEngineOn:   state.WhenEngineOn,
		SampleInterval: time.Duration(state.SampleIntervalSeconds * float64(time.Second)),
	}
```

**`UpdateTrackingSettings` (lines 302-347):** the whole method body changes:
```go
func (a *App) UpdateTrackingSettings(ctx context.Context, settings tracking.Settings) (tracking.Settings, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.mu.RLock()
	currentConfig := a.rawConfig
	configPath := a.configPath
	a.mu.RUnlock()
	if strings.TrimSpace(configPath) == "" {
		return tracking.Settings{}, fmt.Errorf("config path is not configured")
	}
	nextConfig := currentConfig
	whenEngineOn := settings.WhenEngineOn
	nextConfig.Tracking = config.TrackingConfig{
		WhenEngineOn:   &whenEngineOn,
		SampleInterval: settings.SampleInterval,
		Dir:            currentConfig.Tracking.Dir,
	}
	nextNormalized, err := nextConfig.Normalize()
	if err != nil {
		return tracking.Settings{}, err
	}
	if err := config.SaveFile(configPath, nextConfig); err != nil {
		return tracking.Settings{}, err
	}
	revision := readConfigRevision(configPath)
	a.mu.Lock()
	a.rawConfig = nextConfig
	a.cfg = nextNormalized
	a.revision = revision
	a.mu.Unlock()
	if a.tracking != nil {
		a.tracking.Configure(tracking.Settings{
			WhenEngineOn:   nextNormalized.Tracking.WhenEngineOn,
			SampleInterval: nextNormalized.Tracking.SampleInterval,
		})
	}
	out := tracking.Settings{
		WhenEngineOn:   nextNormalized.Tracking.WhenEngineOn,
		SampleInterval: nextNormalized.Tracking.SampleInterval,
	}
	a.logger.Printf("tracking settings updated: when_engine_on=%t sample_interval=%s", out.WhenEngineOn, out.SampleInterval)
	return out, nil
}
```

**New `StartTracking`/`StopTracking`:** insert after `TrackingState`:
```go
// StartTracking begins a manual tracking session. It is rejected in engine
// mode, where the engine signal controls sessions.
func (a *App) StartTracking(ctx context.Context) (tracking.State, error) {
	if a.tracking == nil {
		return tracking.State{}, fmt.Errorf("tracking is not configured")
	}
	if a.tracking.State().WhenEngineOn {
		return tracking.State{}, tracking.ErrEngineMode
	}
	a.tracking.StartRecording(time.Now())
	return a.tracking.State(), nil
}

// StopTracking finalizes a manual tracking session. It is rejected in engine
// mode.
func (a *App) StopTracking(ctx context.Context) (tracking.State, error) {
	if a.tracking == nil {
		return tracking.State{}, fmt.Errorf("tracking is not configured")
	}
	if a.tracking.State().WhenEngineOn {
		return tracking.State{}, tracking.ErrEngineMode
	}
	a.tracking.StopRecording()
	return a.tracking.State(), nil
}
```
Place these after `TrackingState()` and before `TrackList`/`TrackRead`/`TrackDelete` (read the file to find the exact insertion point — they live with the other tracking methods).

### 3b. `service/runtime/tracking_test.go` rework

Read the file first. Edits:

1. **`newTrackingTestApp`:** change
   ```go
			Tracking: config.TrackingConfig{
				Enabled:          true,
				OnlyWhenEngineOn: &onlyWhenEngineOn,
				SampleInterval:   time.Second,
				Dir:              trackDir,
			},
   ```
   to
   ```go
			Tracking: config.TrackingConfig{
				WhenEngineOn:   &onlyWhenEngineOn,
				SampleInterval: time.Second,
				Dir:            trackDir,
			},
   ```
   where `onlyWhenEngineOn` is `false` (manual mode), exactly as the current helper does.

2. **`waitForTrackingStateEvent`:** change the predicate from checking `state.Enabled` to checking `state.Tracking` (or point count). Example shape:
   ```go
   func waitForTrackingStateEvent(t *testing.T, events <-chan events.Event, want func(tracking.State) bool) tracking.State {
   ```
   Callers currently wait for `state.Enabled`; change them to wait for `state.Tracking`.

3. **`TestAppTrackingWiring`:** the app starts in manual mode with no session, so it must call StartTracking after startup:
   ```go
	if _, err := app.StartTracking(context.Background()); err != nil {
		t.Fatalf("start tracking: %v", err)
	}
   ```
   Then wait for a 2-point session via the state channel. Update the settings assertion:
   ```go
	if settings.WhenEngineOn || settings.SampleInterval != time.Second {
		t.Fatalf("tracking settings = %#v", settings)
	}
   ```

4. **`TestUpdateTrackingSettings`:** update the settings literal and field assertions:
   ```go
	updated, err := app.UpdateTrackingSettings(context.Background(), tracking.Settings{
		WhenEngineOn:   false,
		SampleInterval: 2 * time.Second,
	})
	...
	if updated.SampleInterval != 2*time.Second || updated.WhenEngineOn {
	...
   ```
   Validation-error case becomes `tracking.Settings{SampleInterval: 2 * time.Hour}` with the error message checked as `"tracking requires location.enabled"` (only if the test asserts the message — read it). Zero-value case: `tracking.Settings{}`.

5. **`TestConcurrentConfigWritersSerialize`:** goroutine body drops `Enabled: true`:
   ```go
	go func() {
		defer wg.Done()
		_, _ = app.UpdateTrackingSettings(context.Background(), tracking.Settings{SampleInterval: interval})
	}()
   ```

6. **`TestTrackingMethodsWithoutLocation`:** keep existing nil-manager assertions and add:
   ```go
	if _, err := app.StartTracking(context.Background()); err == nil {
		t.Fatal("expected StartTracking error without tracking manager")
	}
	if _, err := app.StopTracking(context.Background()); err == nil {
		t.Fatal("expected StopTracking error without tracking manager")
	}
   ```

7. Any test that starts tracking and relies on auto-sampling in manual mode must now call `app.StartTracking` first (the current wiring test auto-started daily files — that behaviour is gone).

### Verify
```
go test ./service/runtime/
go build ./...
```

### Commit
`runtime: expose tracking start/stop, new settings shape`

---

## Task 4 — HTTP API: DTO + start/stop endpoints

### File
- `service/api/httpapi/server.go`
- `service/api/httpapi/server_test.go`

### 4a. `service/api/httpapi/server.go` edits

**`Application` interface (lines 56-62):** add the two methods:
```go
	TrackingSettings() tracking.Settings
	TrackingDirectory() string
	UpdateTrackingSettings(context.Context, tracking.Settings) (tracking.Settings, error)
	TrackingState() tracking.State
	StartTracking(context.Context) (tracking.State, error)
	StopTracking(context.Context) (tracking.State, error)
	TrackList() ([]tracking.FileInfo, error)
	TrackRead(string) ([]byte, error)
	TrackDelete(string) error
	Broker() *events.Broker
```

**Routes (lines 96-99):**
```go
	mux.HandleFunc("/v1/tracking/settings", s.handleTrackingSettings)
	mux.HandleFunc("/v1/tracking/state", s.handleTrackingState)
	mux.HandleFunc("/v1/tracking/start", s.handleTrackingStart)
	mux.HandleFunc("/v1/tracking/stop", s.handleTrackingStop)
	mux.HandleFunc("/v1/tracks", s.handleTracks)
	mux.HandleFunc("/v1/tracks/{name}", s.handleTrack)
```

**`trackingSettingsDTO` + converters (lines 497-519):**
```go
// trackingSettingsDTO is the wire shape for GET/PUT /v1/tracking/settings.
// Directory is fixed at construction and ignored on PUT.
type trackingSettingsDTO struct {
	WhenEngineOn          bool    `json:"when_engine_on"`
	SampleIntervalSeconds float64 `json:"sample_interval_seconds"`
	Directory             string  `json:"directory"`
}

func trackingSettingsFromDTO(body trackingSettingsDTO) tracking.Settings {
	return tracking.Settings{
		WhenEngineOn:   body.WhenEngineOn,
		SampleInterval: time.Duration(body.SampleIntervalSeconds * float64(time.Second)),
	}
}

func trackingSettingsToDTO(settings tracking.Settings, directory string) trackingSettingsDTO {
	return trackingSettingsDTO{
		WhenEngineOn:          settings.WhenEngineOn,
		SampleIntervalSeconds: settings.SampleInterval.Seconds(),
		Directory:             directory,
	}
}
```

**New handlers:** insert after `handleTrackingSettings` (or after `handleRecordingStop`, matching file ordering). Model on the recording handlers:
```go
func (s *Server) handleTrackingStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.StartTracking(ctx)
	if err != nil {
		switch {
		case errors.Is(err, tracking.ErrEngineMode):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleTrackingStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.StopTracking(ctx)
	if err != nil {
		switch {
		case errors.Is(err, tracking.ErrEngineMode):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, state)
}
```

### 4b. `service/api/httpapi/server_test.go` edits

Read the file first. Edits:

1. **`fakeApp`:** add fields
   ```go
   startTrackingErr error
   stopTrackingErr  error
   ```
   and methods:
   ```go
   func (f fakeApp) StartTracking(context.Context) (tracking.State, error) {
   	return f.trackingState, f.startTrackingErr
   }

   func (f fakeApp) StopTracking(context.Context) (tracking.State, error) {
   	return f.trackingState, f.stopTrackingErr
   }
   ```
   (match the existing `fakeApp` receiver/value style — read the file; if the struct holds pointers for update errors, mirror that for consistency.)

2. **`TestTrackingSettingsRoutes`:** update the fake settings and wire shape:
   ```go
   		trackingSettings: tracking.Settings{WhenEngineOn: true, SampleInterval: 2 * time.Second},
   ```
   GET assertions: `got["when_engine_on"]`. PUT body:
   ```go
   strings.NewReader(`{"when_engine_on":false,"sample_interval_seconds":30,"directory":"/ignored"}`)
   ```
   with `updated["when_engine_on"] != false`.

3. **`TestTrackingSettingsDTOConversion`:** rework field names:
   ```go
	settings := trackingSettingsFromDTO(trackingSettingsDTO{WhenEngineOn: true, SampleIntervalSeconds: 2})
	if !settings.WhenEngineOn || settings.SampleInterval != 2*time.Second {
		t.Fatalf("from dto = %#v", settings)
	}
	zero := trackingSettingsFromDTO(trackingSettingsDTO{})
	if zero.SampleInterval != 0 {
		t.Fatalf("zero interval should round-trip as 0, got %v", zero.SampleInterval)
	}
	dto := trackingSettingsToDTO(tracking.Settings{WhenEngineOn: true, SampleInterval: 5 * time.Second}, "/var/lib/xtura/tracks")
	if dto.WhenEngineOn != true || dto.SampleIntervalSeconds != 5.0 || dto.Directory != "/var/lib/xtura/tracks" {
		t.Fatalf("to dto = %#v", dto)
	}
   ```

4. **`TestTrackingSettingsUpdateRejectsValidationError`:** change the asserted error text to `tracking requires location.enabled` (only if the test asserts the message).

5. **`TestTrackingStateRoute`:** drop the `enabled`/`only_when_engine_on` expectations from the fixture state:
   ```go
   	state := tracking.State{WhenEngineOn: false, Tracking: true, PointCount: 3}
   ```

6. **New test** for the start/stop routes:
   ```go
   func TestTrackingStartStopRoutes(t *testing.T) {
   	state := tracking.State{WhenEngineOn: false, Tracking: true, PointCount: 2}
   	server := New(fakeApp{broker: events.NewBroker(1), trackingState: state}).Handler()

   	rr := httptest.NewRecorder()
   	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/start", nil))
   	if rr.Code != http.StatusOK {
   		t.Fatalf("start code = %d, body %s", rr.Code, rr.Body.String())
   	}
   	var started tracking.State
   	if err := json.Unmarshal(rr.Body.Bytes(), &started); err != nil {
   		t.Fatal(err)
   	}
   	if started.PointCount != 2 || !started.Tracking {
   		t.Fatalf("start body = %+v", started)
   	}

   	rr = httptest.NewRecorder()
   	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/stop", nil))
   	if rr.Code != http.StatusOK {
   		t.Fatalf("stop code = %d, body %s", rr.Code, rr.Body.String())
   	}

   	server = New(fakeApp{broker: events.NewBroker(1), startTrackingErr: tracking.ErrEngineMode}).Handler()
   	rr = httptest.NewRecorder()
   	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/start", nil))
   	if rr.Code != http.StatusConflict {
   		t.Fatalf("engine-mode start code = %d, body %s", rr.Code, rr.Body.String())
   	}

   	server = New(fakeApp{broker: events.NewBroker(1), stopTrackingErr: tracking.ErrEngineMode}).Handler()
   	rr = httptest.NewRecorder()
   	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/stop", nil))
   	if rr.Code != http.StatusConflict {
   		t.Fatalf("engine-mode stop code = %d, body %s", rr.Code, rr.Body.String())
   	}
   }
   ```
   (Match the test file's imports; `tracking` and `events` should already be imported.)

### Verify
```
go test ./service/api/httpapi/
go build ./...
```

### Commit
`api: add tracking start/stop endpoints, new settings wire shape`

---

## Task 5 — Web UI: remove master switch, add manual Start/Stop

### Files
- `web/static/index.html`
- `web/static/app.js`

Read both files first. The relevant regions (may have shifted):
- `index.html` tracking panel ~126-145: `trackingEnabled` checkbox ("Generate GPS trails"), `trackingEngineOnly` ("Only when engine is on"), `trackingInterval`, `trackingDetail`, `trackList`.
- `app.js`: api methods ~96-121, state fields ~167-169, `renderTracking` ~350-377, `trackingStateText`/`trackingDetailText` ~379+, `applyTrackingSettings` ~512-531, `withRequest` ~979-998, `loadInitialState` ~1000+, listeners ~1126-1140.

### 5a. `index.html`

Replace the tracking panel block. New shape (match existing markup classes/id style):
```html
    <section id="trackingPanel" class="panel">
      <h2>GPS Tracking</h2>
      <label class="checkbox-row">
        <input type="checkbox" id="trackingEngineOnly" />
        <span>When engine is on</span>
      </label>
      <div id="trackingManualControls" class="row" hidden>
        <button id="trackingStartButton">Start recording</button>
        <button id="trackingStopButton">Stop recording</button>
      </div>
      <label>
        Sample interval
        <select id="trackingInterval">
          <option value="1">1s</option>
          <option value="5" selected>5s</option>
          <option value="10">10s</option>
          <option value="30">30s</option>
          <option value="60">1m</option>
          <option value="300">5m</option>
        </select>
      </label>
      <p id="trackingDetail" class="detail"></p>
      <ul id="trackList" class="list"></ul>
    </section>
```
(Remove the `trackingEnabled` checkbox; read the existing markup to preserve the exact class/id/style conventions the file uses — e.g. whether buttons use `.row`, `hidden` attribute, and option value formatting.)

### 5b. `app.js`

1. **api methods (~96-121):** add
   ```js
   async startTracking() {
     const response = await fetch("/v1/tracking/start", { method: "POST" });
     return handleResponse(response);
   },
   async stopTracking() {
     const response = await fetch("/v1/tracking/stop", { method: "POST" });
     return handleResponse(response);
   },
   ```

2. **state fields (~167-169):** replace `state.tracking.enabled`/`state.tracking.only_when_engine_on` with `state.tracking.when_engine_on` (wherever they are set/read).

3. **`applyTrackingSettings` (~512-531):** stop setting `state.tracking.enabled`; set `state.tracking.when_engine_on = settings.when_engine_on`.

4. **`renderTracking` (~350-377) + `trackingStateText`/`trackingDetailText`:** set the checkbox from `state.tracking.when_engine_on`; toggle `trackingManualControls` visibility on `!state.tracking.when_engine_on`; set `trackingStartButton`/`trackingStopButton` disabled state from `state.tracking.tracking` (e.g. disable Start while a session is active and Stop when idle — mirror the recording button pattern); keep the interval select and track list rendering.

5. **`loadInitialState`:** it must keep fetching `/v1/tracking/settings` + `/v1/tracking/state` + `/v1/tracks` (unchanged endpoints); adjust only the field names it copies.

6. **listeners:** after the existing tracking listeners, add:
   ```js
   byId("trackingStartButton").addEventListener("click", async () => {
     try {
       state.tracking = await withRequest(() => api.startTracking(), "Starting tracking");
       render();
     } catch (_) {
       return;
     }
   });
   byId("trackingStopButton").addEventListener("click", async () => {
     try {
       state.tracking = await withRequest(() => api.stopTracking(), "Stopping tracking");
       render();
     } catch (_) {
       return;
     }
   });
   ```

7. Ensure `withRequest` maps 409 responses to the existing "Busy" message (it already does — verify) so an engine-mode start/stop error surfaces sensibly.

### Verify
- `go build ./...` (static files are served from disk; no Go build impact but catches accidental breakage).
- Manual smoke test against a local server with a test config that has `location.enabled: true` and `tracking.when_engine_on: false`:
  ```
  curl -s localhost:PORT/v1/tracking/settings
  curl -s -X POST localhost:PORT/v1/tracking/start
  curl -s -X POST localhost:PORT/v1/tracking/stop
  ```
  Confirm start returns a session state and stop clears it, and that with `when_engine_on: true` both return 409.

### Commit
`web: manual tracking start/stop, drop generate-trails switch`

---

## Task 6 — Docs and Pi config

### Files
- `docs/gps-tracking.md`
- `docs/internal-api.md`
- `config.example.yaml` (done in Task 1)

### 6a. `docs/gps-tracking.md`
Read it first. Update to describe the two modes:
- Engine mode (`when_engine_on: true`): sessions auto-start when the engine signal (id 11) reports on and finalize when it reports off.
- Manual mode (`when_engine_on: false`): recording happens only between Start and Stop; a service restart stops manual recording.
- Session files are `track-<UTC start>Z.geojson` in both modes; daily/continuous mode no longer exists.
- Config keys: `tracking.when_engine_on`, `tracking.sample_interval`, `tracking.dir` (no `tracking.enabled`).

### 6b. `docs/internal-api.md`
Update `GET/PUT /v1/tracking/settings` wire shape (`enabled`, `only_when_engine_on` → `when_engine_on`; remove `enabled`). Add `POST /v1/tracking/start` and `POST /v1/tracking/stop`:
```
POST /v1/tracking/start
  -> 200 {tracking.State}
  -> 409 {error}  (when when_engine_on is true)

POST /v1/tracking/stop
  -> 200 {tracking.State}
  -> 409 {error}  (when when_engine_on is true)
```

### 6c. Pi config (runtime deployment, done during Task 7 deploy)
On the Pi, edit `/var/lib/xtura/config.yaml`:
- delete the `tracking:` `enabled:` line,
- rename `tracking.only_when_engine_on` → `tracking.when_engine_on`.

### Commit
`docs: manual tracking modes, start/stop endpoints`

---

## Task 7 — Full verification and final commit

1. Run the entire suite from repo root:
   ```
   go build ./...
   go test ./...
   go vet ./...
   ```
2. Grep to confirm no stale identifiers remain anywhere:
   ```
   rg -n "Enabled.*Tracking|only_when_engine_on|OnlyWhenEngineOn|tracking\.enabled|Generate GPS trails" --glob '*.go' --glob '*.js' --glob '*.html' --glob '*.yaml' --glob '*.md'
   ```
   Expect no matches (the `day`/`sameUTCDay`/`beginDailyLocked` removals are internal to `service/tracking`).
3. Commit any leftover changes as `tracking: final cleanup` (or fold into the relevant task commits).

---

## Task 8 — Deploy (after user review of the whole change)

### Steps
1. Update the Pi config `/var/lib/xtura/config.yaml` (see 6c) — or better, apply the config rename on the Pi *before* restarting so the new binary starts with valid keys (lenient decode means stale keys would be ignored anyway, but keeping the file clean is required).
2. Run the deploy script from macOS:
   ```
   ./scripts/deploy/run-deploy-from-mac.sh
   ```
3. Verify on the Pi:
   - `systemctl status empirebusd` shows active/running
   - `curl -s http://<pi-ip>/v1/tracking/settings` returns `when_engine_on` (no `enabled`)
   - open the web UI: the master switch is gone; with `when_engine_on: false` the Start/Stop buttons appear and drive sessions; with `true` they are hidden and engine sessions still work.

### Commit
No code commit required for deployment; if the Pi config edit should be tracked, commit a copy to the repo as `config: migrate tracking keys for manual mode`.

---

## Notes for the executor
- Do **not** run the full `go test ./...` in one go before Task 2 is complete — intermediate states will not compile (Task 1 changes structs that Task 2/3/4 still reference). Run each package's tests at the end of its own task.
- `git status` and `git log --oneline` before each commit; stage only intended files.
- If any helper in `manager_test.go` differs from what this plan assumes (`newTrackingTestManager`, `sampleAndWait`, `recordFixes`, `FixRecord`, `newFakeClock`), adapt the new tests to the real helpers — do not invent parallel helpers.
- The engine signal constant/`engineFrame` helper live in the tracking test file; manual-mode tests still use `ObserveFrame` only to assert frames do *not* start a session.
