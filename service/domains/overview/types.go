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
	StateOfChargePercent *float64   `json:"state_of_charge_percent,omitempty"`
	CurrentA             *float64   `json:"current_a,omitempty"`
	ETAHours             *float64   `json:"eta_hours,omitempty"`
	Status               string     `json:"status"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}

type Document struct {
	Status            string      `json:"status"`
	AldeTemperatureC  *float64    `json:"alde_temperature_c,omitempty"`
	Battery           Battery     `json:"battery"`
	FreshWaterPercent *float64    `json:"fresh_water_percent,omitempty"`
	GreyWaterPercent  *float64    `json:"grey_water_percent,omitempty"`
	Gas               Gas         `json:"gas"`
	Temperature       Temperature `json:"temperature"`
	UpdatedAt         *time.Time  `json:"updated_at,omitempty"`
}

// Temperature is the temperature panel: the big primary card plus the small
// sensor row. sensors[0] is the promoted primary.
type Temperature struct {
	Sensors   []TemperatureSensor `json:"sensors"`
	PrimaryID string              `json:"primary_id,omitempty"`
	Primary   *TemperaturePrimary `json:"primary,omitempty"`
}

// TemperatureSensor is one entry in the temperature panel.
type TemperatureSensor struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Source   string     `json:"source"`
	Temp     *float64   `json:"temp,omitempty"`
	Humidity *float64   `json:"humidity,omitempty"`
	Battery  *int       `json:"battery,omitempty"`
	Trend    string     `json:"trend"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

// TemperaturePrimary is the big-card payload with the 2h chart history.
type TemperaturePrimary struct {
	ID       string             `json:"id"`
	Temp     *float64           `json:"temp,omitempty"`
	Humidity *float64           `json:"humidity,omitempty"`
	Trend    string             `json:"trend"`
	History  []TemperaturePoint `json:"history"`
}

// TemperaturePoint is one chart sample for the primary card.
type TemperaturePoint struct {
	At   time.Time `json:"t"`
	Temp float64   `json:"temp"`
}

type Gas struct {
	Status         string     `json:"status"`
	LevelPercent   *float64   `json:"level_percent,omitempty"`
	LevelLitres    *float64   `json:"level_litres,omitempty"`
	CapacityLitres *float64   `json:"capacity_litres,omitempty"`
	BatteryPercent *float64   `json:"battery_percent,omitempty"`
	TempC          *float64   `json:"temp_c,omitempty"`
	Quality        *int       `json:"quality,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
