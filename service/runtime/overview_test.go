package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"empirebus-tests/service/config"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/domains/overview"
)

func TestOverviewDocumentEstimatesChargingTimeLinearly(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	app := &App{rawConfig: config.Config{Overview: config.OverviewConfig{UsableBatteryCapacityAh: 100}}, now: func() time.Time { return now }}
	soc, current := 40.0, 10.0
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current, UpdatedAt: &now})
	if doc.Battery.ETAHours == nil || *doc.Battery.ETAHours != 6 {
		t.Fatalf("expected 6h ETA, got %#v", doc.Battery.ETAHours)
	}
}

func TestOverviewDocumentDoesNotEstimateWhenNotCharging(t *testing.T) {
	soc, current := 40.0, -2.0
	app := &App{rawConfig: config.Config{Overview: config.OverviewConfig{UsableBatteryCapacityAh: 100}}}
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current})
	if doc.Battery.ETAHours != nil || doc.Battery.Status != "not_charging" {
		t.Fatalf("expected non-charging state, got %#v", doc.Battery)
	}
}

func TestOverviewDocumentExpiresOldTelemetry(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-31 * time.Second)
	temperature := 20.0
	app := &App{rawConfig: config.Config{}, now: func() time.Time { return now }}
	doc := app.overviewDocument(overview.Telemetry{AldeTemperatureC: &temperature, UpdatedAt: &old})
	if doc.Status != "stale" || doc.AldeTemperatureC != nil {
		t.Fatalf("expected stale telemetry to be unavailable, got status=%q temperature=%v", doc.Status, doc.AldeTemperatureC)
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
		Garmin:     config.GarminConfig{WSURL: "ws://localhost:8090/ws", HeartbeatInterval: 4 * time.Second},
		Automation: config.AutomationConfig{
			Timezone: "UTC",
			HeatingPrograms: []config.HeatingProgramConfig{{
				ID: "test",
				Days: []string{"mon"},
				Periods: []config.HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API:        config.APIConfig{Listen: ":8091"},
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
