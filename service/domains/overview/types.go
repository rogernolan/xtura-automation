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

type Settings struct {
	Comfort                 []float64 `json:"comfort_thresholds"`
	UsableBatteryCapacityAh float64   `json:"usable_battery_capacity_ah"`
	GasTankCapacityLitres   float64   `json:"gas_tank_capacity_litres"`
}

type Battery struct {
	StateOfChargePercent *float64 `json:"state_of_charge_percent,omitempty"`
	CurrentA             *float64 `json:"current_a,omitempty"`
	ETAHours             *float64 `json:"eta_hours,omitempty"`
	Status               string   `json:"status"`
}

type Document struct {
	AldeTemperatureC  *float64   `json:"alde_temperature_c,omitempty"`
	Battery           Battery    `json:"battery"`
	FreshWaterPercent *float64   `json:"fresh_water_percent,omitempty"`
	GreyWaterPercent  *float64   `json:"grey_water_percent,omitempty"`
	Gas               Gas        `json:"gas"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

type Gas struct {
	Status string `json:"status"`
}
