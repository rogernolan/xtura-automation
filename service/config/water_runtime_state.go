package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type GreyWaterScheduleStatus string

const (
	GreyWaterSchedulePending GreyWaterScheduleStatus = "pending"
	GreyWaterScheduleOpen    GreyWaterScheduleStatus = "open"
)

type GreyWaterScheduledOpening struct {
	OpenAt          time.Time               `yaml:"open_at" json:"open_at"`
	LocalTime       string                  `yaml:"local_time" json:"local_time"`
	Timezone        string                  `yaml:"timezone" json:"timezone"`
	DurationMinutes int                     `yaml:"duration_minutes" json:"duration_minutes"`
	Status          GreyWaterScheduleStatus `yaml:"status" json:"status"`
	OpenedAt        *time.Time              `yaml:"opened_at,omitempty" json:"opened_at,omitempty"`
}

type WaterRuntimeState struct {
	ScheduledOpening    *GreyWaterScheduledOpening `yaml:"scheduled_opening,omitempty" json:"scheduled_opening,omitempty"`
	LastScheduleMessage string                     `yaml:"last_schedule_message,omitempty" json:"last_schedule_message,omitempty"`
	LastCompletedAt     *time.Time                 `yaml:"last_completed_at,omitempty" json:"last_completed_at,omitempty"`
}

func LoadWaterRuntimeState(path string) (WaterRuntimeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WaterRuntimeState{}, nil
		}
		return WaterRuntimeState{}, err
	}
	var state WaterRuntimeState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return WaterRuntimeState{}, recoverCorruptState(path, "water runtime state", err)
	}
	if err := state.Validate(); err != nil {
		return WaterRuntimeState{}, recoverCorruptState(path, "water runtime state", err)
	}
	return state, nil
}

func SaveWaterRuntimeState(path string, state WaterRuntimeState) error {
	if err := state.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode water runtime state: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "water-runtime-state-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp water runtime state: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp water runtime state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp water runtime state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace water runtime state: %w", err)
	}
	return nil
}

func (s WaterRuntimeState) Validate() error {
	if s.ScheduledOpening == nil {
		return nil
	}
	return s.ScheduledOpening.Validate()
}

func (s GreyWaterScheduledOpening) Validate() error {
	if s.OpenAt.IsZero() {
		return fmt.Errorf("scheduled grey water opening requires open_at")
	}
	if _, _, err := ParseClockTime(s.LocalTime); err != nil {
		return fmt.Errorf("scheduled grey water opening local_time: %w", err)
	}
	if strings.TrimSpace(s.Timezone) == "" {
		return fmt.Errorf("scheduled grey water opening requires timezone")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("scheduled grey water opening timezone: %w", err)
	}
	if s.DurationMinutes <= 0 || s.DurationMinutes > 24*60 {
		return fmt.Errorf("scheduled grey water opening duration_minutes must be between 1 and 1440")
	}
	switch s.Status {
	case GreyWaterSchedulePending, GreyWaterScheduleOpen:
		return nil
	default:
		return fmt.Errorf("unsupported scheduled grey water opening status %q", s.Status)
	}
}

func ParseClockTime(value string) (hour int, minute int, err error) {
	value = strings.TrimSpace(value)
	if len(value) != len("15:04") || value[2] != ':' {
		return 0, 0, fmt.Errorf("must use HH:MM")
	}
	hour, err = strconv.Atoi(value[:2])
	if err != nil {
		return 0, 0, fmt.Errorf("must use HH:MM")
	}
	minute, err = strconv.Atoi(value[3:])
	if err != nil {
		return 0, 0, fmt.Errorf("must use HH:MM")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("must be a valid 24-hour time")
	}
	return hour, minute, nil
}
