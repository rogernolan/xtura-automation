package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	domainheating "empirebus-tests/service/domains/heating"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Garmin     GarminConfig     `yaml:"garmin"`
	Host       HostConfig       `yaml:"host,omitempty"`
	Location   LocationConfig   `yaml:"location,omitempty"`
	Tracking   TrackingConfig   `yaml:"tracking,omitempty"`
	Automation AutomationConfig `yaml:"automation"`
	API        APIConfig        `yaml:"api"`
}

type GarminConfig struct {
	WSURL             string        `yaml:"ws_url"`
	Origin            string        `yaml:"origin,omitempty"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	TraceWindow       time.Duration `yaml:"trace_window,omitempty"`
}

// HostConfig controls the Pi status host metric sampler.
type HostConfig struct {
	SampleInterval time.Duration `yaml:"sample_interval,omitempty"`
}

type LocationConfig struct {
	Enabled        bool                 `yaml:"enabled,omitempty"`
	Provider       string               `yaml:"provider,omitempty"`
	PollInterval   time.Duration        `yaml:"poll_interval,omitempty"`
	RUTX50         RUTX50LocationConfig `yaml:"rutx50,omitempty"`
	Timezone       TimezoneLookupConfig `yaml:"timezone,omitempty"`
	TimezoneUpdate TimezoneUpdateConfig `yaml:"timezone_update,omitempty"`
	Movement       MovementConfig       `yaml:"movement,omitempty"`
}

type RUTX50LocationConfig struct {
	Endpoint           string        `yaml:"endpoint,omitempty"`
	LoginEndpoint      string        `yaml:"login_endpoint,omitempty"`
	Username           string        `yaml:"username,omitempty"`
	Password           string        `yaml:"password,omitempty"`
	PasswordFile       string        `yaml:"password_file,omitempty"`
	AuthToken          string        `yaml:"auth_token,omitempty"`
	InsecureSkipVerify bool          `yaml:"insecure_skip_verify,omitempty"`
	Timeout            time.Duration `yaml:"timeout,omitempty"`
}

type TimezoneLookupConfig struct {
	Provider string        `yaml:"provider,omitempty"`
	Endpoint string        `yaml:"endpoint,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

type TimezoneUpdateConfig struct {
	Enabled      bool          `yaml:"enabled,omitempty"`
	Interval     time.Duration `yaml:"interval,omitempty"`
	Command      []string      `yaml:"command,omitempty"`
	UpdateConfig bool          `yaml:"update_config,omitempty"`
}

type MovementConfig struct {
	Window            time.Duration `yaml:"window,omitempty"`
	MinDistanceMeters float64       `yaml:"min_distance_meters,omitempty"`
}

type TrackingConfig struct {
	WhenEngineOn   *bool         `yaml:"when_engine_on,omitempty"`
	SampleInterval time.Duration `yaml:"sample_interval,omitempty"`
	Dir            string        `yaml:"dir,omitempty"`
}

type AutomationConfig struct {
	Timezone        string                 `yaml:"timezone"`
	HeatingPrograms []HeatingProgramConfig `yaml:"heating_programs"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
}

type HeatingProgramConfig struct {
	ID      string                `yaml:"id"`
	Enabled *bool                 `yaml:"enabled,omitempty"`
	Days    []string              `yaml:"days"`
	Periods []HeatingPeriodConfig `yaml:"periods"`
}

type HeatingPeriodConfig struct {
	Start         string   `yaml:"start"`
	Mode          string   `yaml:"mode"`
	TargetCelsius *float64 `yaml:"target_celsius,omitempty"`
}

type HeatingScheduleDocument struct {
	Timezone string                           `json:"timezone"`
	Programs []HeatingScheduleProgramDocument `json:"programs"`
	Revision string                           `json:"revision,omitempty"`
}

type HeatingScheduleProgramDocument struct {
	ID      string                          `json:"id"`
	Enabled bool                            `json:"enabled"`
	Days    []string                        `json:"days"`
	Periods []HeatingSchedulePeriodDocument `json:"periods"`
}

type HeatingSchedulePeriodDocument struct {
	Start         string   `json:"start"`
	Mode          string   `json:"mode"`
	TargetCelsius *float64 `json:"target_celsius,omitempty"`
}

type NormalizedConfig struct {
	Garmin     GarminConfig
	Host       NormalizedHost
	Location   NormalizedLocation
	Tracking   NormalizedTracking
	API        APIConfig
	Automation NormalizedAutomation
}

// NormalizedHost is the resolved host metric sampler config.
type NormalizedHost struct {
	SampleInterval time.Duration
}

type NormalizedLocation struct {
	Enabled        bool
	Provider       string
	PollInterval   time.Duration
	RUTX50         RUTX50LocationConfig
	Timezone       TimezoneLookupConfig
	TimezoneUpdate TimezoneUpdateConfig
	Movement       MovementConfig
}

type NormalizedTracking struct {
	WhenEngineOn   bool
	SampleInterval time.Duration
	Dir            string
}

type NormalizedAutomation struct {
	Location        *time.Location
	HeatingPrograms []domainheating.HeatingProgram
}

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveFile(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	var problems []string
	if strings.TrimSpace(c.Garmin.WSURL) == "" {
		problems = append(problems, "garmin.ws_url is required")
	}
	if c.Garmin.HeartbeatInterval <= 0 {
		problems = append(problems, "garmin.heartbeat_interval must be greater than zero")
	}
	if c.Host.SampleInterval < 0 {
		problems = append(problems, "host.sample_interval must not be negative")
	}
	if c.Location.Enabled {
		provider := strings.TrimSpace(c.Location.Provider)
		if provider == "" {
			provider = "rutx50"
		}
		if provider != "rutx50" {
			problems = append(problems, fmt.Sprintf("location.provider %q is not supported", c.Location.Provider))
		}
		if c.Location.PollInterval < 0 {
			problems = append(problems, "location.poll_interval must not be negative")
		}
		if c.Location.RUTX50.Timeout < 0 {
			problems = append(problems, "location.rutx50.timeout must not be negative")
		}
		timezoneProvider := strings.TrimSpace(c.Location.Timezone.Provider)
		if timezoneProvider == "" {
			timezoneProvider = "tzf"
		}
		if timezoneProvider != "tzf" && timezoneProvider != "geotimezone" && timezoneProvider != "none" {
			problems = append(problems, fmt.Sprintf("location.timezone.provider %q is not supported", c.Location.Timezone.Provider))
		}
		if c.Location.Timezone.Timeout < 0 {
			problems = append(problems, "location.timezone.timeout must not be negative")
		}
		if c.Location.TimezoneUpdate.Interval < 0 {
			problems = append(problems, "location.timezone_update.interval must not be negative")
		}
		if c.Location.TimezoneUpdate.Enabled && !c.Location.TimezoneUpdate.UpdateConfig && len(c.Location.TimezoneUpdate.Command) == 0 {
			problems = append(problems, "location.timezone_update.command is required unless location.timezone_update.update_config is true")
		}
		if c.Location.Movement.Window < 0 {
			problems = append(problems, "location.movement.window must not be negative")
		}
		if c.Location.Movement.MinDistanceMeters < 0 {
			problems = append(problems, "location.movement.min_distance_meters must not be negative")
		}
	}
	if strings.TrimSpace(c.Automation.Timezone) == "" {
		problems = append(problems, "automation.timezone is required")
	} else if _, err := time.LoadLocation(c.Automation.Timezone); err != nil {
		problems = append(problems, fmt.Sprintf("automation.timezone: %v", err))
	}
	if strings.TrimSpace(c.API.Listen) == "" {
		problems = append(problems, "api.listen is required")
	}
	if c.Tracking.SampleInterval != 0 && (c.Tracking.SampleInterval < 1*time.Second || c.Tracking.SampleInterval > 3600*time.Second) {
		problems = append(problems, "tracking.sample_interval must be between 1s and 3600s")
	}
	if (c.Tracking.WhenEngineOn != nil || c.Tracking.SampleInterval != 0 || c.Tracking.Dir != "") && !c.Location.Enabled {
		problems = append(problems, "tracking requires location.enabled")
	}
	if len(c.Automation.HeatingPrograms) == 0 {
		problems = append(problems, "automation.heating_programs must contain at least one program")
	}

	seenIDs := map[string]struct{}{}
	dayOwner := map[time.Weekday]string{}
	for i, program := range c.Automation.HeatingPrograms {
		if strings.TrimSpace(program.ID) == "" {
			problems = append(problems, fmt.Sprintf("automation.heating_programs[%d].id is required", i))
			continue
		}
		if _, ok := seenIDs[program.ID]; ok {
			problems = append(problems, fmt.Sprintf("automation.heating_programs[%d].id duplicates %q", i, program.ID))
			continue
		}
		seenIDs[program.ID] = struct{}{}
		normalized, err := normalizeHeatingProgram(program)
		if err != nil {
			problems = append(problems, fmt.Sprintf("automation.heating_programs[%d]: %v", i, err))
			continue
		}
		if err := normalized.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("automation.heating_programs[%d]: %v", i, err))
			continue
		}
		if normalized.Enabled {
			for _, day := range normalized.Days {
				if owner, ok := dayOwner[day]; ok {
					problems = append(problems, fmt.Sprintf("automation.heating_programs[%d].days overlaps %s with %q", i, day, owner))
					continue
				}
				dayOwner[day] = normalized.ID
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) Normalize() (NormalizedConfig, error) {
	if err := c.Validate(); err != nil {
		return NormalizedConfig{}, err
	}
	loc, err := time.LoadLocation(c.Automation.Timezone)
	if err != nil {
		return NormalizedConfig{}, err
	}
	out := NormalizedConfig{
		Garmin:   c.Garmin,
		Host:     normalizeHost(c.Host),
		Location: normalizeLocation(c.Location),
		Tracking: normalizeTracking(c.Tracking),
		API:      c.API,
		Automation: NormalizedAutomation{
			Location:        loc,
			HeatingPrograms: make([]domainheating.HeatingProgram, 0, len(c.Automation.HeatingPrograms)),
		},
	}
	for i, program := range c.Automation.HeatingPrograms {
		normalized, err := normalizeHeatingProgram(program)
		if err != nil {
			return NormalizedConfig{}, fmt.Errorf("automation.heating_programs[%d]: %w", i, err)
		}
		out.Automation.HeatingPrograms = append(out.Automation.HeatingPrograms, normalized)
	}
	return out, nil
}

func normalizeLocation(in LocationConfig) NormalizedLocation {
	out := NormalizedLocation{
		Enabled:      in.Enabled,
		Provider:     strings.TrimSpace(in.Provider),
		PollInterval: in.PollInterval,
		RUTX50: RUTX50LocationConfig{
			Endpoint:           strings.TrimSpace(in.RUTX50.Endpoint),
			LoginEndpoint:      strings.TrimSpace(in.RUTX50.LoginEndpoint),
			Username:           strings.TrimSpace(in.RUTX50.Username),
			Password:           in.RUTX50.Password,
			PasswordFile:       strings.TrimSpace(in.RUTX50.PasswordFile),
			AuthToken:          strings.TrimSpace(in.RUTX50.AuthToken),
			InsecureSkipVerify: in.RUTX50.InsecureSkipVerify,
			Timeout:            in.RUTX50.Timeout,
		},
		Timezone: TimezoneLookupConfig{
			Provider: strings.TrimSpace(in.Timezone.Provider),
			Endpoint: strings.TrimSpace(in.Timezone.Endpoint),
			Timeout:  in.Timezone.Timeout,
		},
		TimezoneUpdate: TimezoneUpdateConfig{
			Enabled:      in.TimezoneUpdate.Enabled,
			Interval:     in.TimezoneUpdate.Interval,
			Command:      append([]string(nil), in.TimezoneUpdate.Command...),
			UpdateConfig: in.TimezoneUpdate.UpdateConfig,
		},
		Movement: MovementConfig{
			Window:            in.Movement.Window,
			MinDistanceMeters: in.Movement.MinDistanceMeters,
		},
	}
	if out.Provider == "" {
		out.Provider = "rutx50"
	}
	if out.PollInterval == 0 {
		out.PollInterval = 5 * time.Minute
	}
	if out.RUTX50.Endpoint == "" {
		out.RUTX50.Endpoint = "http://192.168.51.1/api/gps/position/status"
	}
	if out.RUTX50.LoginEndpoint == "" {
		out.RUTX50.LoginEndpoint = "http://192.168.51.1/api/login"
	}
	if out.RUTX50.Timeout == 0 {
		out.RUTX50.Timeout = 10 * time.Second
	}
	if out.Timezone.Provider == "" {
		out.Timezone.Provider = "tzf"
	}
	if out.Timezone.Timeout == 0 {
		out.Timezone.Timeout = 10 * time.Second
	}
	if out.TimezoneUpdate.Interval == 0 {
		out.TimezoneUpdate.Interval = time.Hour
	}
	if out.Movement.Window == 0 {
		out.Movement.Window = 15 * time.Minute
	}
	if out.Movement.MinDistanceMeters == 0 {
		out.Movement.MinDistanceMeters = 250
	}
	return out
}

func normalizeHost(in HostConfig) NormalizedHost {
	out := NormalizedHost{SampleInterval: in.SampleInterval}
	if out.SampleInterval <= 0 {
		out.SampleInterval = 5 * time.Second
	}
	return out
}

func normalizeTracking(in TrackingConfig) NormalizedTracking {
	out := NormalizedTracking{
		SampleInterval: in.SampleInterval,
		Dir:            strings.TrimSpace(in.Dir),
	}
	if in.WhenEngineOn == nil {
		out.WhenEngineOn = true
	} else {
		out.WhenEngineOn = *in.WhenEngineOn
	}
	if out.SampleInterval == 0 {
		out.SampleInterval = 5 * time.Second
	}
	if out.Dir == "" {
		out.Dir = "/var/lib/xtura/tracks"
	}
	return out
}

func (c Config) HeatingScheduleDocument(revision string) HeatingScheduleDocument {
	programs := make([]HeatingScheduleProgramDocument, 0, len(c.Automation.HeatingPrograms))
	for _, program := range c.Automation.HeatingPrograms {
		enabled := true
		if program.Enabled != nil {
			enabled = *program.Enabled
		}
		periods := make([]HeatingSchedulePeriodDocument, 0, len(program.Periods))
		for _, period := range program.Periods {
			periods = append(periods, HeatingSchedulePeriodDocument{
				Start:         period.Start,
				Mode:          period.Mode,
				TargetCelsius: period.TargetCelsius,
			})
		}
		programs = append(programs, HeatingScheduleProgramDocument{
			ID:      program.ID,
			Enabled: enabled,
			Days:    append([]string(nil), program.Days...),
			Periods: periods,
		})
	}
	return HeatingScheduleDocument{
		Timezone: c.Automation.Timezone,
		Programs: programs,
		Revision: revision,
	}
}

func (c Config) WithHeatingSchedule(doc HeatingScheduleDocument) (Config, error) {
	next := c
	next.Automation.Timezone = strings.TrimSpace(doc.Timezone)
	next.Automation.HeatingPrograms = make([]HeatingProgramConfig, 0, len(doc.Programs))
	for _, program := range doc.Programs {
		enabled := program.Enabled
		periods := make([]HeatingPeriodConfig, 0, len(program.Periods))
		for _, period := range program.Periods {
			periods = append(periods, HeatingPeriodConfig{
				Start:         strings.TrimSpace(period.Start),
				Mode:          strings.TrimSpace(period.Mode),
				TargetCelsius: period.TargetCelsius,
			})
		}
		next.Automation.HeatingPrograms = append(next.Automation.HeatingPrograms, HeatingProgramConfig{
			ID:      strings.TrimSpace(program.ID),
			Enabled: &enabled,
			Days:    append([]string(nil), program.Days...),
			Periods: periods,
		})
	}
	if err := next.Validate(); err != nil {
		return Config{}, err
	}
	return next, nil
}

func normalizeHeatingProgram(program HeatingProgramConfig) (domainheating.HeatingProgram, error) {
	enabled := true
	if program.Enabled != nil {
		enabled = *program.Enabled
	}
	days, err := parseDays(program.Days)
	if err != nil {
		return domainheating.HeatingProgram{}, err
	}
	periods := make([]domainheating.HeatingPeriod, 0, len(program.Periods))
	for i, period := range program.Periods {
		start, err := parseLocalTime(period.Start)
		if err != nil {
			return domainheating.HeatingProgram{}, fmt.Errorf("period %d start: %w", i, err)
		}
		mode, err := parseMode(period.Mode)
		if err != nil {
			return domainheating.HeatingProgram{}, fmt.Errorf("period %d mode: %w", i, err)
		}
		periods = append(periods, domainheating.HeatingPeriod{
			Start:         start,
			Mode:          mode,
			TargetCelsius: period.TargetCelsius,
		})
	}
	return domainheating.HeatingProgram{
		ID:      strings.TrimSpace(program.ID),
		Enabled: enabled,
		Days:    days,
		Periods: periods,
	}, nil
}

func parseDays(days []string) ([]time.Weekday, error) {
	if len(days) == 0 {
		return nil, fmt.Errorf("days is required")
	}
	out := make([]time.Weekday, 0, len(days))
	for i, raw := range days {
		day, ok := weekdayByName(strings.TrimSpace(strings.ToLower(raw)))
		if !ok {
			return nil, fmt.Errorf("day %d: unsupported weekday %q", i, raw)
		}
		out = append(out, day)
	}
	return out, nil
}

func parseLocalTime(raw string) (domainheating.LocalTime, error) {
	parsed, err := time.Parse("15:04", raw)
	if err != nil {
		return domainheating.LocalTime{}, fmt.Errorf("must use HH:MM: %w", err)
	}
	return domainheating.LocalTime{Hour: parsed.Hour(), Minute: parsed.Minute()}, nil
}

func parseMode(raw string) (domainheating.Mode, error) {
	switch domainheating.Mode(strings.TrimSpace(strings.ToLower(raw))) {
	case domainheating.ModeOff:
		return domainheating.ModeOff, nil
	case domainheating.ModeHeat:
		return domainheating.ModeHeat, nil
	default:
		return "", fmt.Errorf("unsupported mode %q", raw)
	}
}

func weekdayByName(raw string) (time.Weekday, bool) {
	switch raw {
	case "sun", "sunday":
		return time.Sunday, true
	case "mon", "monday":
		return time.Monday, true
	case "tue", "tues", "tuesday":
		return time.Tuesday, true
	case "wed", "wednesday":
		return time.Wednesday, true
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, true
	case "fri", "friday":
		return time.Friday, true
	case "sat", "saturday":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}
