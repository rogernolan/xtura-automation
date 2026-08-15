package runtime

import (
	"testing"
	"time"

	"empirebus-tests/service/config"
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
