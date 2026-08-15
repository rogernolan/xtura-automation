package host

import "time"

// Snapshot is one best-effort read of host metrics. The default reader fills
// each field independently; an unreadable source leaves that field at its
// zero value rather than failing the whole snapshot.
type Snapshot struct {
	Model         string      `json:"model,omitempty"`
	Cores         int         `json:"cores"`
	Load          [3]float64  `json:"load"`
	Memory        Memory      `json:"memory"`
	Disk          []DiskUsage `json:"disk,omitempty"`
	TemperatureC  *float64    `json:"temperature_c,omitempty"`
	UptimeSeconds uint64      `json:"uptime_seconds,omitempty"`
	Power         PowerStatus `json:"power"`
}

// Metrics is the wire shape returned by GET /v1/pi/state and carried by the
// pi.state_changed SSE event. SampledAt is when the snapshot was last read;
// LastError/LastErrorAt are owned by the Manager, not the reader.
type Metrics struct {
	SampledAt time.Time `json:"sampled_at"`
	Snapshot
	LastError   string     `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
}

// Memory summarises /proc/meminfo. UsedPercent is derived from total and
// available; it is 0 when total is unknown.
type Memory struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// DiskUsage describes one mounted filesystem.
type DiskUsage struct {
	Mount       string  `json:"mount"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// PowerStatus decodes the Raspberry Pi throttle state. Status is "ok" when no
// current or latched issue is set, "warning" when any is set, and
// "unavailable" when the source could not be read. The four booleans describe
// current issues; OccurredSinceBoot lists latched conditions; RawThrottle is
// the vcgencmd hex string when that was the source.
type PowerStatus struct {
	Status            string   `json:"status"`
	UnderVoltage      bool     `json:"under_voltage,omitempty"`
	Throttled         bool     `json:"throttled,omitempty"`
	FrequencyCapped   bool     `json:"frequency_capped,omitempty"`
	SoftTempLimit     bool     `json:"soft_temp_limit,omitempty"`
	OccurredSinceBoot []string `json:"occurred_since_boot,omitempty"`
	RawThrottle       string   `json:"raw_throttle,omitempty"`
}
