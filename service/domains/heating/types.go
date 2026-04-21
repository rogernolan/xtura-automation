package heating

import (
	"fmt"
	"math"
	"time"
)

type PowerState string

const (
	PowerStateUnknown    PowerState = "unknown"
	PowerStateOff        PowerState = "off"
	PowerStateOn         PowerState = "on"
	PowerStateTransition PowerState = "transition"
)

type Mode string

const (
	ModeOff  Mode = "off"
	ModeHeat Mode = "heat"
)

type LocalTime struct {
	Hour   int
	Minute int
}

func (t LocalTime) Validate() error {
	if t.Hour < 0 || t.Hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23")
	}
	if t.Minute < 0 || t.Minute > 59 {
		return fmt.Errorf("minute must be between 0 and 59")
	}
	return nil
}

func (t LocalTime) Minutes() int {
	return t.Hour*60 + t.Minute
}

func (t LocalTime) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

type HeatingPeriod struct {
	Start         LocalTime `json:"start"`
	Mode          Mode      `json:"mode"`
	TargetCelsius *float64  `json:"target_celsius,omitempty"`
}

func (p HeatingPeriod) Validate() error {
	if err := p.Start.Validate(); err != nil {
		return fmt.Errorf("start %s: %w", p.Start, err)
	}
	switch p.Mode {
	case ModeOff:
		if p.TargetCelsius != nil {
			return fmt.Errorf("off periods must not set target_celsius")
		}
	case ModeHeat:
		if p.TargetCelsius == nil {
			return fmt.Errorf("heat periods must set target_celsius")
		}
	default:
		return fmt.Errorf("unsupported mode %q", p.Mode)
	}
	return nil
}

type HeatingProgram struct {
	ID      string         `json:"id"`
	Enabled bool           `json:"enabled"`
	Days    []time.Weekday `json:"days"`
	Periods []HeatingPeriod `json:"periods"`
}

func (p HeatingProgram) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("id is required")
	}
	if len(p.Days) == 0 {
		return fmt.Errorf("at least one day is required")
	}
	if len(p.Periods) == 0 {
		return fmt.Errorf("at least one period is required")
	}
	if p.Periods[0].Start.Minutes() != 0 {
		return fmt.Errorf("program must start at 00:00")
	}
	if err := p.Periods[0].Validate(); err != nil {
		return fmt.Errorf("period 0: %w", err)
	}
	seenDays := map[time.Weekday]struct{}{}
	for _, day := range p.Days {
		if _, ok := seenDays[day]; ok {
			return fmt.Errorf("duplicate day %s", day)
		}
		seenDays[day] = struct{}{}
	}
	prev := p.Periods[0]
	for i := 1; i < len(p.Periods); i++ {
		curr := p.Periods[i]
		if err := curr.Validate(); err != nil {
			return fmt.Errorf("period %d: %w", i, err)
		}
		if curr.Start.Minutes() <= prev.Start.Minutes() {
			return fmt.Errorf("period %d must start after the previous period", i)
		}
		if SameEffectiveState(prev, curr) {
			return fmt.Errorf("period %d is redundant with the previous period", i)
		}
		prev = curr
	}
	return nil
}

func (p HeatingProgram) AppliesOn(day time.Weekday) bool {
	for _, candidate := range p.Days {
		if candidate == day {
			return true
		}
	}
	return false
}

func SameEffectiveState(a, b HeatingPeriod) bool {
	if a.Mode != b.Mode {
		return false
	}
	if a.Mode == ModeOff {
		return true
	}
	if a.TargetCelsius == nil || b.TargetCelsius == nil {
		return false
	}
	return math.Abs(*a.TargetCelsius-*b.TargetCelsius) < 0.000001
}

type State struct {
	PowerState             PowerState `json:"power_state"`
	Ready                  bool       `json:"ready"`
	TargetTemperatureC     *float64   `json:"target_temperature_c,omitempty"`
	TargetTemperatureKnown bool       `json:"target_temperature_known"`
	LastUpdatedAt          *time.Time `json:"last_updated_at,omitempty"`
	LastCommandError       string     `json:"last_command_error,omitempty"`
}

type AdapterHealth struct {
	Connected   bool       `json:"connected"`
	LastFrameAt *time.Time `json:"last_frame_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

type ServiceHealth struct {
	Status           string        `json:"status"`
	StartedAt        time.Time     `json:"started_at"`
	Garmin           AdapterHealth `json:"garmin"`
	SchedulerRunning bool          `json:"scheduler_running"`
	ConfigLoaded     bool          `json:"config_loaded"`
}
