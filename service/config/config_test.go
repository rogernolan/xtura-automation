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
  ws_url: ws://172.16.11.7:8888/ws
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

func TestValidateAllowsAdjacentPeriodsWithSameEffectiveState(t *testing.T) {
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
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected adjacent matching periods to validate, got %v", err)
	}
}

func TestNormalizeLocationDefaultsRUTX50(t *testing.T) {
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Location: LocationConfig{
			Enabled: true,
			RUTX50: RUTX50LocationConfig{
				InsecureSkipVerify: true,
			},
			TimezoneUpdate: TimezoneUpdateConfig{
				Enabled:      true,
				UpdateConfig: true,
			},
		},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{{
				ID:      "weekday",
				Days:    []string{"mon"},
				Periods: []HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: APIConfig{Listen: ":8080"},
	}
	normalized, err := cfg.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if !normalized.Location.Enabled {
		t.Fatal("expected location to be enabled")
	}
	if normalized.Location.Provider != "rutx50" {
		t.Fatalf("got provider %q", normalized.Location.Provider)
	}
	if normalized.Location.RUTX50.Endpoint != "http://192.168.51.1/api/gps/position/status" {
		t.Fatalf("got endpoint %q", normalized.Location.RUTX50.Endpoint)
	}
	if normalized.Location.RUTX50.LoginEndpoint != "http://192.168.51.1/api/login" {
		t.Fatalf("got login endpoint %q", normalized.Location.RUTX50.LoginEndpoint)
	}
	if normalized.Location.PollInterval != 5*time.Minute {
		t.Fatalf("got poll interval %s", normalized.Location.PollInterval)
	}
	if normalized.Location.Timezone.Provider != "tzf" {
		t.Fatalf("got timezone provider %q", normalized.Location.Timezone.Provider)
	}
	if normalized.Location.Movement.Window != 15*time.Minute {
		t.Fatalf("got movement window %s", normalized.Location.Movement.Window)
	}
	if normalized.Location.Movement.MinDistanceMeters != 250 {
		t.Fatalf("got movement distance %f", normalized.Location.Movement.MinDistanceMeters)
	}
}

func TestValidateRejectsTimezoneUpdateWithoutAction(t *testing.T) {
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Location: LocationConfig{
			Enabled:        true,
			TimezoneUpdate: TimezoneUpdateConfig{Enabled: true},
		},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{{
				ID:      "weekday",
				Days:    []string{"mon"},
				Periods: []HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: APIConfig{Listen: ":8080"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "location.timezone_update.command") {
		t.Fatalf("expected timezone update validation error, got %v", err)
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

func TestValidateRejectsHeatTargetOutsideSafeRange(t *testing.T) {
	target := 25.0
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{{
				ID:   "bad",
				Days: []string{"mon"},
				Periods: []HeatingPeriodConfig{
					{Start: "00:00", Mode: "off"},
					{Start: "05:30", Mode: "heat", TargetCelsius: &target},
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
					ID:      "a",
					Days:    []string{"mon"},
					Periods: []HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
				},
				{
					ID:      "b",
					Days:    []string{"monday"},
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

func TestHeatingScheduleDocumentRoundTrip(t *testing.T) {
	cfg := Config{
		Garmin: GarminConfig{WSURL: "ws://example", HeartbeatInterval: 4 * time.Second},
		Automation: AutomationConfig{
			Timezone: "Europe/London",
			HeatingPrograms: []HeatingProgramConfig{{
				ID:      "weekday",
				Enabled: ptrBool(false),
				Days:    []string{"mon"},
				Periods: []HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: APIConfig{Listen: ":8080"},
	}
	doc := cfg.HeatingScheduleDocument("rev-1")
	if doc.Revision != "rev-1" {
		t.Fatalf("got revision %q", doc.Revision)
	}
	next, err := cfg.WithHeatingSchedule(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(next.Automation.HeatingPrograms); got != 1 {
		t.Fatalf("got %d programs", got)
	}
	if next.Automation.HeatingPrograms[0].Enabled == nil || *next.Automation.HeatingPrograms[0].Enabled {
		t.Fatalf("expected disabled program to round-trip")
	}
}

func ptrBool(v bool) *bool {
	return &v
}
