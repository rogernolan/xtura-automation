package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

func TestOverviewGasUsesPersistedOverviewCapacity(t *testing.T) {
	now := time.Now().UTC()
	level := &mopekaState{distanceMm: 100, lastSeen: now, hasReading: true}
	app := &App{
		rawConfig: config.Config{
			Overview: config.OverviewConfig{GasTankCapacityLitres: 31},
			Mopeka:   config.MopekaConfig{TankCapacityLitres: 22, TankFillHeightMm: 200},
		},
		mopeka: level,
	}

	gas := app.overviewGas()
	if gas.CapacityLitres == nil || *gas.CapacityLitres != 31 {
		t.Fatalf("expected Overview gas capacity 31L, got %#v", gas.CapacityLitres)
	}
	if gas.LevelLitres == nil || *gas.LevelLitres != 15.5 {
		t.Fatalf("expected level based on Overview capacity, got %#v", gas.LevelLitres)
	}
}

func TestOverviewGasFallsBackToMopekaCapacityWhenOverviewUnset(t *testing.T) {
	now := time.Now().UTC()
	app := &App{
		rawConfig: config.Config{
			Mopeka: config.MopekaConfig{TankCapacityLitres: 22, TankFillHeightMm: 200},
		},
		mopeka: &mopekaState{distanceMm: 100, lastSeen: now, hasReading: true},
	}

	gas := app.overviewGas()
	if gas.CapacityLitres == nil || *gas.CapacityLitres != 22 {
		t.Fatalf("expected Mopeka fallback capacity 22L, got %#v", gas.CapacityLitres)
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
