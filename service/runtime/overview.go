package runtime

import (
	"context"
	"fmt"
	"math"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/overview"
)

func (a *App) Overview() overview.Document {
	telemetry := overview.Telemetry{}
	if a.overviewTelemetry != nil {
		telemetry = a.overviewTelemetry()
	}
	return a.overviewDocument(telemetry)
}

func (a *App) overviewDocument(telemetry overview.Telemetry) overview.Document {
	settings := config.NormalizeOverview(a.rawConfig.Overview)
	doc := overview.Document{
		AldeTemperatureC:  telemetry.AldeTemperatureC,
		FreshWaterPercent: telemetry.FreshWaterPercent,
		GreyWaterPercent:  telemetry.GreyWaterPercent,
		UpdatedAt:         telemetry.UpdatedAt,
		Gas:               overview.Gas{Status: "mopeka_not_configured"},
		Battery:           overview.Battery{StateOfChargePercent: telemetry.BatteryStateOfChargePercent, CurrentA: telemetry.BatteryCurrentA, Status: "unavailable"},
	}
	if telemetry.BatteryCurrentA != nil {
		if *telemetry.BatteryCurrentA > 0 {
			doc.Battery.Status = "charging"
			if telemetry.BatteryStateOfChargePercent != nil && settings.UsableBatteryCapacityAh > 0 && *telemetry.BatteryStateOfChargePercent < 100 {
				eta := settings.UsableBatteryCapacityAh * (1 - *telemetry.BatteryStateOfChargePercent/100) / *telemetry.BatteryCurrentA
				if eta >= 0 && math.IsInf(eta, 0) == false && !math.IsNaN(eta) {
					doc.Battery.ETAHours = &eta
				}
			}
		} else {
			doc.Battery.Status = "not_charging"
		}
	}
	return doc
}

func (a *App) OverviewSettings() overview.Settings {
	settings := config.NormalizeOverview(a.rawConfig.Overview)
	return overview.Settings{Comfort: append([]float64(nil), settings.Comfort...), UsableBatteryCapacityAh: settings.UsableBatteryCapacityAh, GasTankCapacityLitres: settings.GasTankCapacityLitres}
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
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.mu.RLock()
	next := a.rawConfig
	path := a.configPath
	a.mu.RUnlock()
	if path == "" {
		return overview.Settings{}, fmt.Errorf("config path is not configured")
	}
	next.Overview = config.OverviewConfig{Comfort: append([]float64(nil), settings.Comfort...), UsableBatteryCapacityAh: settings.UsableBatteryCapacityAh, GasTankCapacityLitres: settings.GasTankCapacityLitres}
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
