package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	domainheating "empirebus-tests/service/domains/heating"

	"gopkg.in/yaml.v3"
)

type HeatingMode string

const (
	HeatingModeSchedule HeatingMode = "schedule"
	HeatingModeOff      HeatingMode = "off"
	HeatingModeManual   HeatingMode = "manual"
	HeatingModeBoost    HeatingMode = "boost"
)

type HeatingBoostState struct {
	TargetCelsius             float64     `yaml:"target_celsius" json:"target_celsius"`
	ExpiresAt                 time.Time   `yaml:"expires_at" json:"expires_at"`
	ResumeMode                HeatingMode `yaml:"resume_mode" json:"resume_mode"`
	ResumeManualTargetCelsius *float64    `yaml:"resume_manual_target_celsius,omitempty" json:"resume_manual_target_celsius,omitempty"`
}

type HeatingRuntimeState struct {
	Mode                HeatingMode        `yaml:"mode" json:"mode"`
	ManualTargetCelsius *float64           `yaml:"manual_target_celsius,omitempty" json:"manual_target_celsius,omitempty"`
	Boost               *HeatingBoostState `yaml:"boost,omitempty" json:"boost,omitempty"`
	UpdatedAt           time.Time          `yaml:"updated_at" json:"updated_at"`
}

func DefaultHeatingRuntimeState() HeatingRuntimeState {
	return HeatingRuntimeState{
		Mode:      HeatingModeSchedule,
		UpdatedAt: time.Now().UTC(),
	}
}

func LoadHeatingRuntimeState(path string) (HeatingRuntimeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultHeatingRuntimeState(), nil
		}
		return HeatingRuntimeState{}, err
	}
	var state HeatingRuntimeState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return recoveredHeatingRuntimeState(), recoverCorruptState(path, "heating runtime state", err)
	}
	if err := state.Validate(); err != nil {
		return recoveredHeatingRuntimeState(), recoverCorruptState(path, "heating runtime state", err)
	}
	return state, nil
}

func recoveredHeatingRuntimeState() HeatingRuntimeState {
	return HeatingRuntimeState{Mode: HeatingModeOff, UpdatedAt: time.Now().UTC()}
}

func recoverCorruptState(path, kind string, cause error) error {
	backup := fmt.Sprintf("%s.corrupt-%s", path, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("%s: %w (quarantine failed: %v)", kind, cause, err)
	}
	log.Printf("%s: moved corrupt file to %s: %v", kind, backup, cause)
	return nil
}

func SaveHeatingRuntimeState(path string, state HeatingRuntimeState) error {
	if err := state.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "runtime-state-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp runtime state: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp runtime state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp runtime state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace runtime state: %w", err)
	}
	return nil
}

func (s HeatingRuntimeState) Validate() error {
	switch s.Mode {
	case HeatingModeSchedule:
		if s.ManualTargetCelsius != nil || s.Boost != nil {
			return fmt.Errorf("schedule mode must not set manual_target_celsius or boost")
		}
	case HeatingModeOff:
		if s.ManualTargetCelsius != nil || s.Boost != nil {
			return fmt.Errorf("off mode must not set manual_target_celsius or boost")
		}
	case HeatingModeManual:
		if s.ManualTargetCelsius == nil {
			return fmt.Errorf("manual mode requires manual_target_celsius")
		}
		if err := domainheating.ValidateTargetCelsius(*s.ManualTargetCelsius); err != nil {
			return err
		}
		if s.Boost != nil {
			return fmt.Errorf("manual mode must not set boost")
		}
	case HeatingModeBoost:
		if s.Boost == nil {
			return fmt.Errorf("boost mode requires boost")
		}
		if err := domainheating.ValidateTargetCelsius(s.Boost.TargetCelsius); err != nil {
			return err
		}
		if s.Boost.ResumeMode == HeatingModeBoost {
			return fmt.Errorf("boost resume_mode must not be boost")
		}
		if s.Boost.ResumeManualTargetCelsius != nil {
			if err := domainheating.ValidateTargetCelsius(*s.Boost.ResumeManualTargetCelsius); err != nil {
				return fmt.Errorf("resume_manual_target_celsius: %w", err)
			}
		}
	default:
		return fmt.Errorf("unsupported heating mode %q", s.Mode)
	}
	return nil
}
