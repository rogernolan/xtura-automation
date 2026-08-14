package location

import "time"

type Fix struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Altitude  *float64  `json:"altitude,omitempty"`
	Source    string    `json:"source,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type State struct {
	Configured         bool       `json:"configured"`
	Known              bool       `json:"known"`
	Provider           string     `json:"provider,omitempty"`
	Latitude           float64    `json:"latitude"`
	Longitude          float64    `json:"longitude"`
	Altitude           *float64   `json:"altitude,omitempty"`
	IsMoving           bool       `json:"is_moving"`
	MovementMeters     float64    `json:"movement_meters,omitempty"`
	Timezone           string     `json:"timezone,omitempty"`
	SystemTimezone     string     `json:"system_timezone,omitempty"`
	TimezoneUpdatedAt  *time.Time `json:"timezone_updated_at,omitempty"`
	LastUpdatedAt      *time.Time `json:"last_updated_at,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	LastErrorAt        *time.Time `json:"last_error_at,omitempty"`
	TimezoneUpdateMode string     `json:"timezone_update_mode,omitempty"`
}
