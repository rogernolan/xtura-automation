package sensors

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Trend classifies the temperature change over the recent history window.
type Trend string

const (
	TrendRising      Trend = "rising"
	TrendFalling     Trend = "falling"
	TrendSteady      Trend = "steady"
	TrendUnavailable Trend = "unavailable"
)

// TrendWindow is the baseline history span used for trend calculation.
const TrendWindow = 30 * time.Minute

// TrendRecent is the trailing span compared against the older baseline bucket.
const TrendRecent = 5 * time.Minute

// TrendBaseline is the start of the older comparison bucket inside the window.
const TrendBaseline = 15 * time.Minute

// TrendThreshold is the temperature change that separates steady from a trend.
const TrendThreshold = 0.3

// AldeID is the fixed identity of the Garmin Alde temperature sensor.
const AldeID = "alde"

// Sample is one timestamped sensor reading.
type Sample struct {
	At   time.Time `json:"t"`
	Temp float64   `json:"temp"`
	Hum  *float64  `json:"hum,omitempty"`
}

// Reading is a decoded live reading for a sensor.
type Reading struct {
	Temp     float64  `json:"temp"`
	Humidity *float64 `json:"humidity,omitempty"`
	Battery  *int     `json:"battery,omitempty"`
}

// SensorConfig is a configured SwitchBot sensor.
type SensorConfig struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	Primary bool   `json:"primary,omitempty"`
}

// Settings is the runtime-editable switchbot configuration.
type Settings struct {
	Enabled   bool           `json:"enabled"`
	HCIDevice string         `json:"hci_device,omitempty"`
	Sensors   []SensorConfig `json:"sensors"`
}

var macPattern = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)

// NormalizeMAC lowercases and strips separators from a MAC address. It is the
// canonical sensor id for SwitchBot devices.
func NormalizeMAC(mac string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(mac), ":", ""))
}

// ID returns the canonical sensor identity derived from the MAC.
func (s SensorConfig) ID() string {
	return NormalizeMAC(s.MAC)
}

// SensorByMAC returns the first configured sensor whose MAC normalizes to id.
func (s Settings) SensorByMAC(mac string) (SensorConfig, bool) {
	id := NormalizeMAC(mac)
	for _, sensor := range s.Sensors {
		if sensor.ID() == id {
			return sensor, true
		}
	}
	return SensorConfig{}, false
}

// Validate checks the switchbot settings invariants.
func (s Settings) Validate() error {
	var problems []string
	if s.HCIDevice != "" && !strings.HasPrefix(s.HCIDevice, "hci") {
		problems = append(problems, fmt.Sprintf("switchbot.hci_device %q must name a bluetooth controller", s.HCIDevice))
	}
	seenNames := map[string]struct{}{}
	seenIDs := map[string]struct{}{}
	primaryCount := 0
	for i, sensor := range s.Sensors {
		if strings.TrimSpace(sensor.Name) == "" {
			problems = append(problems, fmt.Sprintf("switchbot.sensors[%d].name is required", i))
		} else if _, ok := seenNames[sensor.Name]; ok {
			problems = append(problems, fmt.Sprintf("switchbot.sensors[%d].name duplicates %q", i, sensor.Name))
		}
		seenNames[sensor.Name] = struct{}{}
		if !macPattern.MatchString(sensor.MAC) {
			problems = append(problems, fmt.Sprintf("switchbot.sensors[%d].mac %q must be a colon-separated MAC address", i, sensor.MAC))
			continue
		}
		id := NormalizeMAC(sensor.MAC)
		if _, ok := seenIDs[id]; ok {
			problems = append(problems, fmt.Sprintf("switchbot.sensors[%d].mac duplicates %q", i, sensor.MAC))
		}
		seenIDs[id] = struct{}{}
		if sensor.Primary {
			primaryCount++
		}
	}
	if primaryCount > 1 {
		problems = append(problems, "switchbot.sensors may have at most one primary")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// TrendOf classifies the temperature trend over the recent history window: the
// mean of the last five minutes compared with the mean of the 15-30 minute
// bucket. It returns TrendUnavailable when either bucket has no samples; it
// never fabricates a zero trend.
func TrendOf(samples []Sample, now time.Time) Trend {
	var recent []float64
	var baseline []float64
	recentStart := now.Add(-TrendRecent)
	baselineStart := now.Add(-TrendWindow)
	baselineEnd := now.Add(-TrendBaseline)
	for _, sample := range samples {
		at := sample.At
		switch {
		case at.After(recentStart) || at.Equal(recentStart):
			recent = append(recent, sample.Temp)
		case at.After(baselineStart) && at.Before(baselineEnd):
			baseline = append(baseline, sample.Temp)
		}
	}
	if len(recent) == 0 || len(baseline) == 0 {
		return TrendUnavailable
	}
	delta := mean(recent) - mean(baseline)
	switch {
	case delta >= TrendThreshold:
		return TrendRising
	case delta <= -TrendThreshold:
		return TrendFalling
	default:
		return TrendSteady
	}
}

func mean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
