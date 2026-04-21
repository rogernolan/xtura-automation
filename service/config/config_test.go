package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFileAndNormalize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(`
garmin:
  ws_url: ws://192.168.1.1:8888/ws
  heartbeat_interval: 4s
automation:
  timezone: Europe/London
  heating_programs:
    - id: weekday
      days: ["mon", "tue", "wed", "thu", "fri"]
      periods:
        - start: "00:00"
          mode: "off"
        - start: "05:30"
          mode: "heat"
          target_celsius: 20.0
        - start: "08:00"
          mode: "off"
    - id: weekend
      days: ["sat", "sun"]
      periods:
        - start: "00:00"
          mode: "off"
api:
  listen: 0.0.0.0:8080
`)), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	normalized, err := cfg.Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got := len(normalized.Automation.HeatingPrograms); got != 2 {
		t.Fatalf("expected 2 normalized programs, got %d", got)
	}
	if normalized.Automation.Location.String() != "Europe/London" {
		t.Fatalf("unexpected location %s", normalized.Automation.Location)
	}
}

func TestValidateRejectsRedundantPeriods(t *testing.T) {
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{{
				ID:   "bad",
				Days: []string{"mon"},
				Periods: []HeatingPeriodConfig{
					{Start: "00:00", Mode: "off"},
					{Start: "08:00", Mode: "off"},
				},
			}},
		},
		API: APIConfig{Listen: ":8080"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "redundant") {
		t.Fatalf("expected redundant-period validation error, got %v", err)
	}
}

func TestValidateRejectsMissingHeatTarget(t *testing.T) {
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{{
				ID:   "bad",
				Days: []string{"mon"},
				Periods: []HeatingPeriodConfig{
					{Start: "00:00", Mode: "off"},
					{Start: "05:30", Mode: "heat"},
				},
			}},
		},
		API: APIConfig{Listen: ":8080"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "target_celsius") {
		t.Fatalf("expected target validation error, got %v", err)
	}
}

func TestValidateRejectsOverlappingProgramDays(t *testing.T) {
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{
				{
					ID:   "a",
					Days: []string{"mon"},
					Periods: []HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
				},
				{
					ID:   "b",
					Days: []string{"monday"},
					Periods: []HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
				},
			},
		},
		API: APIConfig{Listen: ":8080"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected overlapping-day validation error, got %v", err)
	}
}
