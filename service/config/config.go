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
	Automation AutomationConfig `yaml:"automation"`
	API        APIConfig        `yaml:"api"`
}

type GarminConfig struct {
	WSURL             string        `yaml:"ws_url"`
	Origin            string        `yaml:"origin,omitempty"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	TraceWindow       time.Duration `yaml:"trace_window,omitempty"`
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
	API        APIConfig
	Automation NormalizedAutomation
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
	if strings.TrimSpace(c.Automation.Timezone) == "" {
		problems = append(problems, "automation.timezone is required")
	} else if _, err := time.LoadLocation(c.Automation.Timezone); err != nil {
		problems = append(problems, fmt.Sprintf("automation.timezone: %v", err))
	}
	if strings.TrimSpace(c.API.Listen) == "" {
		problems = append(problems, "api.listen is required")
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
		return fmt.Errorf(strings.Join(problems, "; "))
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
		Garmin: c.Garmin,
		API:    c.API,
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
