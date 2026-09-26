package runtime

import (
	"bytes"
	"context"
	"log"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"empirebus-tests/service/adapters/btle"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/overview"
)

func TestOverviewDocumentEstimatesChargingTimeLinearly(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	app := &App{
		rawConfig: config.Config{Overview: config.OverviewConfig{
			UsableBatteryCapacityAh: 100,
			BatteryCapacityAh:       100,
			BatteryNominalVoltage:   12.8,
			BatteryFloorSOC:         20,
			BatteryReadySOC:         95,
			ChargeEfficiency:        0.99,
		}},
		now:             func() time.Time { return now },
		batteryEstimate: NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	soc, current := 40.0, 10.0
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current, UpdatedAt: &now})
	if doc.Battery.Mode != "charging" {
		t.Fatalf("expected charging mode, got %q", doc.Battery.Mode)
	}
	if doc.Battery.ETASeconds == nil {
		t.Fatalf("expected ETA seconds, got nil")
	}
	// requiredAh = 100 * (95-40)/100 = 55Ah
	// effective = 10 * 0.99 = 9.9A
	// hours = 55/9.9 ≈ 5.556h ≈ 20000s
	eta := *doc.Battery.ETASeconds
	if eta < 19990 || eta > 20010 {
		t.Fatalf("expected ~20000s ETA, got %v", eta)
	}
}

func TestOverviewDocumentDoesNotEstimateWhenNotCharging(t *testing.T) {
	soc, current := 40.0, -2.0
	app := &App{
		rawConfig: config.Config{Overview: config.OverviewConfig{
			BatteryCapacityAh:     660,
			BatteryNominalVoltage: 12.8,
			BatteryFloorSOC:       20,
			BatteryReadySOC:       95,
			ChargeEfficiency:      0.99,
		}},
		batteryEstimate: NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current})
	// -2A is inside the idle deadband
	if doc.Battery.Mode != "idle" {
		t.Fatalf("expected idle mode, got %q", doc.Battery.Mode)
	}
	if doc.Battery.ETASeconds != nil {
		t.Fatalf("expected no ETA when idle, got %v", doc.Battery.ETASeconds)
	}
}

func TestOverviewDocumentEstimatesDischargeTime(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	app := &App{
		rawConfig: config.Config{Overview: config.OverviewConfig{
			BatteryCapacityAh:     660,
			BatteryNominalVoltage: 12.8,
			BatteryFloorSOC:       20,
			BatteryReadySOC:       95,
			ChargeEfficiency:      0.99,
		}},
		now:             func() time.Time { return now },
		batteryEstimate: NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	soc, current := 80.0, -30.0
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current, UpdatedAt: &now})
	if doc.Battery.Mode != "discharging" {
		t.Fatalf("expected discharging mode, got %q", doc.Battery.Mode)
	}
	if doc.Battery.ETASeconds == nil {
		t.Fatalf("expected ETA seconds, got nil")
	}
	// usableRemaining = 660 * (80-20)/100 = 396Ah
	// hours = 396/30 = 13.2h = 47520s
	eta := *doc.Battery.ETASeconds
	if eta < 47500 || eta > 47540 {
		t.Fatalf("expected ~47520s ETA, got %v", eta)
	}
}

func TestOverviewDocumentBatteryUnavailableWhenMissing(t *testing.T) {
	app := &App{
		rawConfig:       config.Config{Overview: config.OverviewConfig{}},
		batteryEstimate: NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	doc := app.overviewDocument(overview.Telemetry{})
	if doc.Battery.Status != "unavailable" {
		t.Fatalf("expected unavailable status, got %q", doc.Battery.Status)
	}
}

func TestOverviewDocumentExpiresOldTelemetry(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-31 * time.Second)
	temperature := 20.0
	app := &App{rawConfig: config.Config{}, now: func() time.Time { return now }}
	doc := app.overviewDocument(overview.Telemetry{AldeTemperatureC: &temperature, UpdatedAt: &old})
	if doc.Status != "stale" {
		t.Fatalf("expected stale status, got status=%q", doc.Status)
	}
	// Keep last known values when stale - frontend shows "Last seen" indicator
	if doc.AldeTemperatureC == nil || *doc.AldeTemperatureC != temperature {
		t.Fatalf("expected last known temperature to be preserved, got temperature=%v", doc.AldeTemperatureC)
	}
}

// newGasApp builds an App with an enabled Mopeka sensor. cfg is the raw
// config; the normalized config is derived from it so tests exercise the same
// defaulting path production uses.
func newGasApp(now time.Time, mopekaCfg config.MopekaConfig, overviewCap float64, state *mopekaState) *App {
	return &App{
		rawConfig: config.Config{
			Overview: config.OverviewConfig{GasTankCapacityLitres: overviewCap},
			Mopeka:   mopekaCfg,
		},
		// Mirror what normalizeMopeka produces, so the normalized config here
		// is the one a real Normalize() would hand the runtime.
		cfg: config.NormalizedConfig{Mopeka: config.MopekaConfig{
			Enabled:            true,
			TankFillHeightMm:   mopekaCfg.TankFillHeightMm,
			TankCapacityLitres: mopekaCfg.TankCapacityLitres,
			TankBaseRadiusMm:   mopekaCfg.TankBaseRadiusMm,
		}},
		now:    func() time.Time { return now },
		mopeka: state,
	}
}

// TestOverviewGasFillIsDistanceOverTankHeight is the regression test for the
// inverted formula. The old code computed (1 - d/H)*100, the empty fraction,
// which coincidentally matched reality only at 0%, 50% and 100%. Using a
// distance that is not the midpoint is what makes this test fail against the
// old implementation.
func TestOverviewGasFillIsDistanceOverTankHeight(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	// 50mm of liquid standing in a 200mm tank is a quarter full, not
	// three-quarters.
	state := &mopekaState{distanceMm: 50, lastSeen: now, hasReading: true, quality: 3}
	app := newGasApp(now, config.MopekaConfig{Enabled: true, MAC: "00:11:22:33:44:55", TankCapacityLitres: 22, TankFillHeightMm: 200}, 0, state)

	gas := app.overviewGas()
	if gas.Status != "ok" {
		t.Fatalf("Status = %q, want ok", gas.Status)
	}
	if gas.LevelPercent == nil || math.Abs(*gas.LevelPercent-25) > 0.001 {
		t.Fatalf("LevelPercent = %#v, want 25", gas.LevelPercent)
	}
	if gas.LevelLitres == nil || math.Abs(*gas.LevelLitres-5.5) > 0.001 {
		t.Fatalf("LevelLitres = %#v, want 5.5", gas.LevelLitres)
	}
}

func TestOverviewGasEmptyTankReadsZeroAndFullTankReadsHundred(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}
	for _, tc := range []struct {
		name     string
		distance float64
		want     float64
	}{
		{"empty echoes off the floor", 0, 0},
		{"full reads the tank height", 200, 100},
		{"midpoint", 100, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &mopekaState{distanceMm: tc.distance, lastSeen: now, hasReading: true, quality: 3}
			gas := newGasApp(now, cfg, 0, state).overviewGas()
			if gas.LevelPercent == nil || math.Abs(*gas.LevelPercent-tc.want) > 0.001 {
				t.Fatalf("LevelPercent = %#v, want %v", gas.LevelPercent, tc.want)
			}
			if gas.Status != "ok" {
				t.Fatalf("Status = %q, want ok", gas.Status)
			}
		})
	}
}

func TestOverviewGasUsesPersistedOverviewCapacity(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	state := &mopekaState{distanceMm: 100, lastSeen: now, hasReading: true, quality: 3}
	app := newGasApp(now, config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}, 31, state)

	gas := app.overviewGas()
	if gas.CapacityLitres == nil || *gas.CapacityLitres != 31 {
		t.Fatalf("expected Overview gas capacity 31L, got %#v", gas.CapacityLitres)
	}
	if gas.LevelLitres == nil || *gas.LevelLitres != 15.5 {
		t.Fatalf("expected level based on Overview capacity, got %#v", gas.LevelLitres)
	}
}

func TestOverviewGasFallsBackToMopekaCapacityWhenOverviewUnset(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	state := &mopekaState{distanceMm: 100, lastSeen: now, hasReading: true, quality: 3}
	app := newGasApp(now, config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}, 0, state)

	gas := app.overviewGas()
	if gas.CapacityLitres == nil || *gas.CapacityLitres != 22 {
		t.Fatalf("expected Mopeka fallback capacity 22L, got %#v", gas.CapacityLitres)
	}
}

func TestOverviewGasNotConfiguredWhenDisabled(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	app := &App{rawConfig: config.Config{}, now: func() time.Time { return now }}
	if got := app.overviewGas().Status; got != "mopeka_not_configured" {
		t.Fatalf("Status = %q, want mopeka_not_configured", got)
	}
}

// A configured sensor that has never been heard from is a radio problem, not
// a config problem. The two used to collapse into the same misleading status.
func TestOverviewGasNoDataWhenConfiguredButNeverSeen(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}
	for _, state := range []*mopekaState{nil, {}} {
		gas := newGasApp(now, cfg, 0, state).overviewGas()
		if gas.Status != "no_data" {
			t.Fatalf("Status = %q, want no_data", gas.Status)
		}
		if gas.LevelPercent != nil {
			t.Fatalf("expected no level, got %#v", gas.LevelPercent)
		}
	}
}

func TestOverviewGasStaleKeepsLastValuesAndReportsAge(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}

	// Seed a fresh reading so there is a cached lastGas to fall back on.
	fresh := &mopekaState{distanceMm: 50, lastSeen: now, hasReading: true, quality: 3}
	app := newGasApp(now, cfg, 0, fresh)
	if got := app.overviewGas().Status; got != "ok" {
		t.Fatalf("seed reading Status = %q, want ok", got)
	}

	// Ten minutes later the reading is stale, but the values survive and the
	// age is reported so the UI can stop presenting it as live.
	later := newGasApp(now.Add(10*time.Minute), cfg, 0, fresh)
	gas := later.overviewGas()
	if gas.Status != "stale" {
		t.Fatalf("Status = %q, want stale", gas.Status)
	}
	if gas.LevelPercent == nil || *gas.LevelPercent != 25 {
		t.Fatalf("expected last known level 25 to be preserved, got %#v", gas.LevelPercent)
	}
	if gas.AgeSeconds == nil || *gas.AgeSeconds != 600 {
		t.Fatalf("AgeSeconds = %#v, want 600", gas.AgeSeconds)
	}
}

func TestOverviewGasFreshInsideStaleWindow(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}
	for _, age := range []time.Duration{0, time.Minute, 4*time.Minute + 59*time.Second} {
		state := &mopekaState{distanceMm: 50, lastSeen: now.Add(-age), hasReading: true, quality: 3}
		gas := newGasApp(now, cfg, 0, state).overviewGas()
		if gas.Status != "ok" {
			t.Fatalf("age %v: Status = %q, want ok", age, gas.Status)
		}
		if gas.AgeSeconds == nil || *gas.AgeSeconds != int64(age/time.Second) {
			t.Fatalf("age %v: AgeSeconds = %#v, want %d", age, gas.AgeSeconds, int64(age/time.Second))
		}
	}
}

// Quality 0 means the ping returned off the tank floor with no liquid surface
// in the way. Publishing a level for it renders "signal lost" as a real
// reading, which is how an empty or unseated sensor looks like a full tank.
func TestOverviewGasBadQualityPublishesNoLevel(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}
	for _, quality := range []int{0} {
		state := &mopekaState{distanceMm: 50, lastSeen: now, hasReading: true, quality: quality}
		gas := newGasApp(now, cfg, 0, state).overviewGas()
		if gas.Status != "bad_quality" {
			t.Fatalf("quality %d: Status = %q, want bad_quality", quality, gas.Status)
		}
		if gas.LevelPercent != nil {
			t.Fatalf("quality %d: expected no level, got %#v", quality, gas.LevelPercent)
		}
		if gas.Quality == nil || *gas.Quality != quality {
			t.Fatalf("quality %d: Quality not reported as %#v", quality, gas.Quality)
		}
	}
	for _, quality := range []int{1, 2, 3} {
		state := &mopekaState{distanceMm: 50, lastSeen: now, hasReading: true, quality: quality}
		gas := newGasApp(now, cfg, 0, state).overviewGas()
		if gas.Status != "ok" || gas.LevelPercent == nil {
			t.Fatalf("quality %d: expected a usable level, got status=%q level=%#v", quality, gas.Status, gas.LevelPercent)
		}
	}
}

// More liquid standing than the tank can hold means tank_fill_height_mm is
// miscalibrated. Clamping to 100% silently hid that for the lifetime of the
// bug, so it gets its own status.
func TestOverviewGasOutOfRangeWhenAboveTankHeight(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.MopekaConfig{Enabled: true, MAC: "AA", TankCapacityLitres: 22, TankFillHeightMm: 200}
	state := &mopekaState{distanceMm: 260, lastSeen: now, hasReading: true, quality: 3}

	gas := newGasApp(now, cfg, 0, state).overviewGas()
	if gas.Status != "out_of_range" {
		t.Fatalf("Status = %q, want out_of_range", gas.Status)
	}
	if gas.LevelPercent == nil || *gas.LevelPercent != 100 {
		t.Fatalf("expected the displayed level to clamp to 100, got %#v", gas.LevelPercent)
	}
}

func TestHandleMopekaReadingRecordsFrameAndLogs(t *testing.T) {
	clock := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	var logs bytes.Buffer
	app := &App{
		now:    func() time.Time { return clock },
		logger: log.New(&logs, "", 0),
	}

	app.handleMopekaReading(btle.MopekaReading{DistanceMm: 146.3023, BatteryPct: 89.4, TempC: 21, Quality: 3, MAC: "00:11:22:33:44:55"})

	if !strings.Contains(logs.String(), "146mm") {
		t.Fatalf("expected the decoded frame to be logged, got %q", logs.String())
	}
	if app.mopeka == nil || app.mopeka.distanceMm != 146.3023 || !app.mopeka.hasReading {
		t.Fatalf("reading not recorded: %#v", app.mopeka)
	}

	// Mopeka advertises about once a second; logging every frame would bury
	// the journal, so readings inside the window stay quiet.
	logs.Reset()
	clock = clock.Add(time.Second)
	app.handleMopekaReading(btle.MopekaReading{DistanceMm: 147, Quality: 3})
	if logs.Len() != 0 {
		t.Fatalf("expected rate limiting, got %q", logs.String())
	}

	// But a reading after the interval logs again.
	logs.Reset()
	clock = clock.Add(mopekaLogInterval)
	app.handleMopekaReading(btle.MopekaReading{DistanceMm: 148, Quality: 3})
	if !strings.Contains(logs.String(), "148mm") {
		t.Fatalf("expected a log after the interval, got %q", logs.String())
	}
}

func TestUpdateOverviewSettingsPersistsAllSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	initial := config.Config{
		Garmin: config.GarminConfig{WSURL: "ws://localhost:8090/ws", HeartbeatInterval: 4 * time.Second},
		Automation: config.AutomationConfig{
			Timezone: "UTC",
			HeatingPrograms: []config.HeatingProgramConfig{{
				ID:      "test",
				Days:    []string{"mon"},
				Periods: []config.HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: config.APIConfig{Listen: ":8091"},
	}
	if err := config.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	app := &App{rawConfig: initial, configPath: path, broker: events.NewBroker(1), now: func() time.Time { return now }}
	want := overview.Settings{Comfort: []float64{11, 19, 25, 31}, UsableBatteryCapacityAh: 120, GasTankCapacityLitres: 31}
	if _, err := app.UpdateOverviewSettings(context.Background(), want); err != nil {
		t.Fatal(err)
	}

	saved, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Overview.Comfort; len(got) != 4 || got[0] != 11 || got[3] != 31 {
		t.Fatalf("comfort settings were not persisted: %#v", got)
	}
	if saved.Overview.UsableBatteryCapacityAh != 120 || saved.Overview.GasTankCapacityLitres != 31 {
		t.Fatalf("capacity settings were not persisted: %#v", saved.Overview)
	}
}

func TestOverviewUpdateSettingsBatteryCapacityAh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	initial := config.Config{
		Garmin: config.GarminConfig{WSURL: "ws://localhost:8090/ws", HeartbeatInterval: 4 * time.Second},
		Automation: config.AutomationConfig{
			Timezone: "UTC",
			HeatingPrograms: []config.HeatingProgramConfig{{
				ID:      "test",
				Days:    []string{"mon"},
				Periods: []config.HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: config.APIConfig{Listen: ":8091"},
	}
	if err := config.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	app := &App{rawConfig: initial, configPath: path, broker: events.NewBroker(1), now: func() time.Time { return now }}

	saved, err := app.UpdateOverviewSettings(context.Background(), overview.Settings{Comfort: []float64{11, 19, 25, 31}, UsableBatteryCapacityAh: 120, GasTankCapacityLitres: 31, BatteryCapacityAh: 660})
	if err != nil {
		t.Fatal(err)
	}
	if saved.BatteryCapacityAh != 660 {
		t.Fatalf("expected returned BatteryCapacityAh 660, got %v", saved.BatteryCapacityAh)
	}

	saved, err = app.UpdateOverviewSettings(context.Background(), overview.Settings{Comfort: []float64{11, 19, 25, 31}, UsableBatteryCapacityAh: 120, GasTankCapacityLitres: 31, BatteryCapacityAh: 800})
	if err != nil {
		t.Fatal(err)
	}
	if saved.BatteryCapacityAh != 800 {
		t.Fatalf("expected updated BatteryCapacityAh 800, got %v", saved.BatteryCapacityAh)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Overview.BatteryCapacityAh != 800 {
		t.Fatalf("expected persisted BatteryCapacityAh 800, got %v", loaded.Overview.BatteryCapacityAh)
	}
}

// TestTankFillPercentModelsTheDome is the regression test for treating a
// composite cylinder as a straight tube. The interior is a 150mm dome topped
// by a cylinder of the same radius, so a level below 150mm is still up inside
// the dome where the bore is much narrower. A linear distance/height model
// reports 43% at 146.3mm in a 340mm tank; the true volume fraction, and what
// the Mopeka app displays, is 33%.
func TestTankFillPercentModelsTheDome(t *testing.T) {
	const base, height = 150.0, 340.0

	if got := tankFillPercent(146.3023, base, height); math.Abs(got-33.2) > 0.1 {
		t.Fatalf("volumetric fill at 146.3mm = %.2f%%, want ~33.2%% (the app reads 33%%)", got)
	}
	if linear := 146.3023 / height * 100; math.Abs(linear-43.0) > 0.1 {
		t.Fatalf("sanity: linear model should give ~43%%, got %.2f", linear)
	}

	// The dome is explicitly non-linear: half the dome's height must hold far
	// less than half its volume, because the cross-section grows with h^2.
	halfDome := tankVolumeBelowMm(base/2, base) / tankVolumeBelowMm(base, base)
	if halfDome >= 0.5 {
		t.Fatalf("half the dome height holds %.3f of its volume, want well under 0.5", halfDome)
	}

	// A straight-walled tank (no dome) must stay linear, which is what the
	// existing distance/height tests rely on.
	if got := tankFillPercent(50, 0, 200); math.Abs(got-25) > 0.001 {
		t.Fatalf("straight tank at 50/200mm = %.3f%%, want 25%%", got)
	}

	for _, tc := range []struct{ level, want float64 }{
		{0, 0}, {height, 100},
	} {
		if got := tankFillPercent(tc.level, base, height); math.Abs(got-tc.want) > 0.001 {
			t.Fatalf("level %v = %.3f%%, want %v%%", tc.level, got, tc.want)
		}
	}

	// Monotonic across the dome/barrel join, which is where a naive
	// implementation tends to put a kink.
	prev := -1.0
	for level := 0.0; level <= height; level += 5 {
		got := tankFillPercent(level, base, height)
		if got < prev {
			t.Fatalf("fill%% dropped from %.3f to %.3f at level %v", prev, got, level)
		}
		prev = got
	}
}

// TestOverviewGasUsesConfiguredBaseRadius checks the configured dome radius
// actually reaches the percentage, through the whole app path rather than just
// the geometry helper.
func TestOverviewGasUsesConfiguredBaseRadius(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	radius := 150.0
	cfg := config.MopekaConfig{
		Enabled: true, MAC: "00:11:22:33:44:55",
		TankCapacityLitres: 44, TankFillHeightMm: 340, TankBaseRadiusMm: &radius,
	}
	state := &mopekaState{distanceMm: 146.3023, lastSeen: now, hasReading: true, quality: 3}
	app := newGasApp(now, cfg, 0, state)
	gas := app.overviewGas()

	if gas.Status != "ok" || gas.LevelPercent == nil {
		t.Fatalf("expected a usable reading, got status=%q level=%#v", gas.Status, gas.LevelPercent)
	}
	if math.Abs(*gas.LevelPercent-33.2) > 0.1 {
		t.Fatalf("LevelPercent = %.2f, want ~33.2 to match the Mopeka app", *gas.LevelPercent)
	}
	// Litres must follow the same volumetric fraction, not the linear one.
	wantLitres := 33.2 / 100.0 * 44
	if gas.LevelLitres == nil || math.Abs(*gas.LevelLitres-wantLitres) > 0.1 {
		t.Fatalf("LevelLitres = %#v, want ~%.2f", gas.LevelLitres, wantLitres)
	}
}
