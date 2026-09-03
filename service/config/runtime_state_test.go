package config

import (
	"os"
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

func TestHeatingRuntimeStateQuarantinesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	if err := os.WriteFile(path, []byte("mode: manual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := LoadHeatingRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != HeatingModeOff {
		t.Fatalf("got mode %q, want safe off mode", state.Mode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("corrupt state was not quarantined, stat error=%v", err)
	}
	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one quarantined state file, got %v (err=%v)", backups, err)
	}
}

func TestWaterRuntimeStateRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "water-runtime.yaml")
	openAt := time.Date(2026, 5, 6, 1, 0, 0, 0, time.UTC)
	completedAt := openAt.Add(30 * time.Minute)
	state := WaterRuntimeState{
		ScheduledOpening: &GreyWaterScheduledOpening{
			OpenAt:          openAt,
			LocalTime:       "03:00",
			Timezone:        "Europe/Rome",
			DurationMinutes: 30,
			Status:          GreyWaterSchedulePending,
		},
		LastScheduleMessage: "Grey water valve opened at 03:00 for 30 minutes.",
		LastCompletedAt:     &completedAt,
	}
	if err := SaveWaterRuntimeState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadWaterRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ScheduledOpening == nil {
		t.Fatal("expected scheduled opening")
	}
	if !loaded.ScheduledOpening.OpenAt.Equal(openAt) {
		t.Fatalf("got open_at %s", loaded.ScheduledOpening.OpenAt)
	}
	if loaded.ScheduledOpening.LocalTime != "03:00" || loaded.ScheduledOpening.Timezone != "Europe/Rome" {
		t.Fatalf("unexpected scheduled opening %#v", loaded.ScheduledOpening)
	}
	if loaded.ScheduledOpening.DurationMinutes != 30 {
		t.Fatalf("got duration %d", loaded.ScheduledOpening.DurationMinutes)
	}
	if loaded.LastScheduleMessage == "" || loaded.LastCompletedAt == nil || !loaded.LastCompletedAt.Equal(completedAt) {
		t.Fatalf("unexpected completion fields %#v", loaded)
	}
}

func TestWaterRuntimeStateValidateRejectsInvalidSchedule(t *testing.T) {
	t.Parallel()
	state := WaterRuntimeState{
		ScheduledOpening: &GreyWaterScheduledOpening{
			OpenAt:          time.Date(2026, 5, 6, 1, 0, 0, 0, time.UTC),
			LocalTime:       "25:00",
			Timezone:        "Europe/Rome",
			DurationMinutes: 0,
			Status:          GreyWaterSchedulePending,
		},
	}
	if err := state.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestWaterRuntimeStateQuarantinesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "water-runtime.yaml")
	if err := os.WriteFile(path, []byte("scheduled_opening: [corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := LoadWaterRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.ScheduledOpening != nil {
		t.Fatalf("got recovered schedule %#v, want empty state", state.ScheduledOpening)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("corrupt state was not quarantined, stat error=%v", err)
	}
	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one quarantined state file, got %v (err=%v)", backups, err)
	}
}
