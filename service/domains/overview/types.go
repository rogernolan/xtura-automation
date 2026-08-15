package overview

import "time"

// Telemetry is the latest source-confirmed overview data received from Garmin.
// Nil fields have not received a valid scalar status frame in this session.
type Telemetry struct {
	AldeTemperatureC            *float64   `json:"alde_temperature_c,omitempty"`
	FreshWaterPercent           *float64   `json:"fresh_water_percent,omitempty"`
	GreyWaterPercent            *float64   `json:"grey_water_percent,omitempty"`
	BatteryCurrentA             *float64   `json:"battery_current_a,omitempty"`
	BatteryStateOfChargePercent *float64   `json:"battery_state_of_charge_percent,omitempty"`
	UpdatedAt                   *time.Time `json:"updated_at,omitempty"`
}
