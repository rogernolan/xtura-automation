package runtime

import (
	"context"
	"fmt"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/overview"
)

const overviewStaleAfter = 30 * time.Second

func (a *App) Overview() overview.Document {
	telemetry := overview.Telemetry{}
	if a.overviewTelemetry != nil {
		telemetry = a.overviewTelemetry()
	}
	doc := a.overviewDocument(telemetry)
	if a.adapter != nil && !a.adapter.Health().Connected && doc.Status == "available" {
		expireOverviewDocument(&doc)
	}
	return doc
}

func (a *App) overviewDocument(telemetry overview.Telemetry) overview.Document {
	settings := config.NormalizeOverview(a.overviewConfig())
	now := time.Now().UTC()
	if a.now != nil {
		now = a.now().UTC()
	}
	status := "unavailable"
	if telemetry.UpdatedAt != nil {
		status = "available"
		if now.Sub(*telemetry.UpdatedAt) > overviewStaleAfter {
			status = "stale"
		}
	}
	doc := overview.Document{
		Status:            status,
		AldeTemperatureC:  telemetry.AldeTemperatureC,
		FreshWaterPercent: telemetry.FreshWaterPercent,
		GreyWaterPercent:  telemetry.GreyWaterPercent,
		UpdatedAt:         telemetry.UpdatedAt,
		Gas:               a.overviewGas(),
		Temperature:       a.temperatureDocument(telemetry),
	}
	estCfg := BatteryConfig{
		CapacityAh:        settings.BatteryCapacityAh,
		NominalVoltage:    settings.BatteryNominalVoltage,
		FloorSOC:          settings.BatteryFloorSOC,
		ReadySOC:          settings.BatteryReadySOC,
		MaxChargeCurrentA: settings.MultiplusMaxChargeCurrentA,
		ChargeEfficiency:  settings.ChargeEfficiency,
	}
	est := ComputeBatteryEstimate(
		telemetry.BatteryStateOfChargePercent,
		telemetry.BatteryCurrentA,
		a.batteryEstimate,
		estCfg,
	)

	doc.Battery = overview.Battery{
		StateOfChargePercent: telemetry.BatteryStateOfChargePercent,
		CurrentA:             telemetry.BatteryCurrentA,
		UpdatedAt:            telemetry.UpdatedAt,
	}

	if !est.Available {
		doc.Battery.Status = "unavailable"
	} else {
		doc.Battery.Status = string(est.Mode)
		doc.Battery.Mode = string(est.Mode)
		powerW := est.PowerW
		doc.Battery.PowerW = &powerW

		switch est.Mode {
		case BatteryModeCharging:
			if est.SOC >= estCfg.ReadySOC {
				doc.Battery.ChargeState = "topping_off"
			} else if est.EstimatedSeconds > 0 {
				doc.Battery.ETASeconds = &est.EstimatedSeconds
				targetSOC := estCfg.ReadySOC
				doc.Battery.TargetSOC = &targetSOC
			}
		case BatteryModeDischarging:
			if est.EstimatedSeconds > 0 {
				doc.Battery.ETASeconds = &est.EstimatedSeconds
				targetSOC := estCfg.FloorSOC
				doc.Battery.TargetSOC = &targetSOC
			}
		}
	}
	if status == "stale" {
		expireOverviewDocument(&doc)
	}
	return doc
}

func expireOverviewDocument(doc *overview.Document) {
	doc.Status = "stale"
}

func (a *App) overviewConfig() config.OverviewConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	settings := a.rawConfig.Overview
	settings.Comfort = append([]float64(nil), settings.Comfort...)
	return settings
}

func (a *App) OverviewSettings() overview.Settings {
	settings := config.NormalizeOverview(a.overviewConfig())
	return overview.Settings{
		Comfort:                 append([]float64(nil), settings.Comfort...),
		UsableBatteryCapacityAh: settings.UsableBatteryCapacityAh,
		GasTankCapacityLitres:   settings.GasTankCapacityLitres,
		BatteryCapacityAh:       settings.BatteryCapacityAh,
	}
}

func (a *App) UpdateOverviewSettings(ctx context.Context, settings overview.Settings) (overview.Settings, error) {
	if len(settings.Comfort) != 4 {
		return overview.Settings{}, fmt.Errorf("overview.comfort_thresholds must contain four ordered values")
	}
	for i := 1; i < len(settings.Comfort); i++ {
		if settings.Comfort[i] <= settings.Comfort[i-1] {
			return overview.Settings{}, fmt.Errorf("overview.comfort_thresholds must be ordered")
		}
	}
	if settings.UsableBatteryCapacityAh <= 0 {
		return overview.Settings{}, fmt.Errorf("overview.usable_battery_capacity_ah must be greater than zero")
	}
	if settings.GasTankCapacityLitres < 0 {
		return overview.Settings{}, fmt.Errorf("overview.gas_tank_capacity_litres must not be negative")
	}
	if settings.BatteryCapacityAh < 0 {
		return overview.Settings{}, fmt.Errorf("overview.battery_capacity_ah must not be negative")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.mu.RLock()
	next := a.rawConfig
	path := a.configPath
	a.mu.RUnlock()
	if path == "" {
		return overview.Settings{}, fmt.Errorf("config path is not configured")
	}
	ov := next.Overview
	ov.Comfort = append([]float64(nil), settings.Comfort...)
	ov.UsableBatteryCapacityAh = settings.UsableBatteryCapacityAh
	ov.GasTankCapacityLitres = settings.GasTankCapacityLitres
	ov.BatteryCapacityAh = settings.BatteryCapacityAh
	next.Overview = ov
	normalized, err := next.Normalize()
	if err != nil {
		return overview.Settings{}, err
	}
	if err := config.SaveFile(path, next); err != nil {
		return overview.Settings{}, err
	}
	a.mu.Lock()
	a.rawConfig = next
	a.cfg = normalized
	a.revision = readConfigRevision(path)
	a.mu.Unlock()
	out := a.OverviewSettings()
	a.broker.Publish(events.Event{Type: "overview.state_changed", Timestamp: a.now().UTC(), Payload: a.Overview()})
	return out, nil
}
