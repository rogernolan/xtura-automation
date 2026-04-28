package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHeatingRuntimeStateRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	manual := 19.0
	state := HeatingRuntimeState{
		Mode:                HeatingModeManual,
		ManualTargetCelsius: &manual,
		UpdatedAt:           time.Now().UTC().Round(0),
	}
	if err := SaveHeatingRuntimeState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHeatingRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != HeatingModeManual {
		t.Fatalf("got mode %q", loaded.Mode)
	}
	if loaded.ManualTargetCelsius == nil || *loaded.ManualTargetCelsius != 19.0 {
		t.Fatalf("unexpected manual target %#v", loaded.ManualTargetCelsius)
	}
}

func TestHeatingRuntimeStateValidateRejectsMissingManualTarget(t *testing.T) {
	t.Parallel()
	state := HeatingRuntimeState{Mode: HeatingModeManual}
	if err := state.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestHeatingRuntimeStateValidateRejectsTargetOutsideSafeRange(t *testing.T) {
	t.Parallel()
	for _, target := range []float64{4.5, 25.0} {
		target := target
		t.Run("manual", func(t *testing.T) {
			t.Parallel()
			state := HeatingRuntimeState{Mode: HeatingModeManual, ManualTargetCelsius: &target}
			if err := state.Validate(); err == nil {
				t.Fatalf("expected validation error for %.1fC", target)
			}
		})
		t.Run("boost", func(t *testing.T) {
			t.Parallel()
			state := HeatingRuntimeState{
				Mode: HeatingModeBoost,
				Boost: &HeatingBoostState{
					TargetCelsius: target,
					ExpiresAt:     time.Now().UTC().Add(time.Hour),
					ResumeMode:    HeatingModeSchedule,
				},
			}
			if err := state.Validate(); err == nil {
				t.Fatalf("expected validation error for %.1fC", target)
			}
		})
	}
}
