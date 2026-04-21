package scheduler

import (
	"testing"
	"time"

	domainheating "empirebus-tests/service/domains/heating"
)

func TestCalculateSpringForwardTransitionsToFirstValidTimeAfterGap(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	heat := 20.0
	program := domainheating.HeatingProgram{
		ID:      "daily",
		Enabled: true,
		Days:    []time.Weekday{time.Sunday},
		Periods: []domainheating.HeatingPeriod{
			{Start: domainheating.LocalTime{Hour: 0, Minute: 0}, Mode: domainheating.ModeOff},
			{Start: domainheating.LocalTime{Hour: 1, Minute: 30}, Mode: domainheating.ModeHeat, TargetCelsius: &heat},
			{Start: domainheating.LocalTime{Hour: 8, Minute: 0}, Mode: domainheating.ModeOff},
		},
	}

	now := time.Date(2026, 3, 29, 0, 30, 0, 0, loc)
	calc, err := Calculate(program, loc, now)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if calc.Action.Kind != ActionKindEnsureOnAndSetTarget {
		t.Fatalf("expected ensure-on action, got %s", calc.Action.Kind)
	}
	got := calc.NextTransitionAt.In(loc)
	if got.Hour() != 2 || got.Minute() != 0 {
		t.Fatalf("expected first valid post-gap transition at 02:00, got %s", got.Format(time.RFC3339))
	}
}

func TestCalculateFallBackUsesFirstOccurrenceOnly(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	heat := 20.0
	program := domainheating.HeatingProgram{
		ID:      "daily",
		Enabled: true,
		Days:    []time.Weekday{time.Sunday},
		Periods: []domainheating.HeatingPeriod{
			{Start: domainheating.LocalTime{Hour: 0, Minute: 0}, Mode: domainheating.ModeOff},
			{Start: domainheating.LocalTime{Hour: 1, Minute: 30}, Mode: domainheating.ModeHeat, TargetCelsius: &heat},
			{Start: domainheating.LocalTime{Hour: 8, Minute: 0}, Mode: domainheating.ModeOff},
		},
	}

	now := time.Date(2026, 10, 25, 1, 15, 0, 0, time.UTC).In(loc)
	calc, err := Calculate(program, loc, now)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	got := calc.NextTransitionAt.In(loc)
	if got.Hour() != 8 || got.Minute() != 0 {
		t.Fatalf("expected next transition after the repeated hour to be 08:00, got %s", got.Format(time.RFC3339))
	}
}

func TestCalculateSkipsDaysWithoutProgramAndFindsNextHeatTransition(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	heat := 20.0
	program := domainheating.HeatingProgram{
		ID:      "weekday",
		Enabled: true,
		Days:    []time.Weekday{time.Monday},
		Periods: []domainheating.HeatingPeriod{
			{Start: domainheating.LocalTime{Hour: 0, Minute: 0}, Mode: domainheating.ModeOff},
			{Start: domainheating.LocalTime{Hour: 5, Minute: 30}, Mode: domainheating.ModeHeat, TargetCelsius: &heat},
			{Start: domainheating.LocalTime{Hour: 8, Minute: 0}, Mode: domainheating.ModeOff},
		},
	}

	now := time.Date(2026, 4, 21, 12, 0, 0, 0, loc) // Tuesday
	calc, err := Calculate(program, loc, now)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	got := calc.NextTransitionAt.In(loc)
	if got.Weekday() != time.Monday || got.Hour() != 5 || got.Minute() != 30 {
		t.Fatalf("expected next Monday 05:30, got %s", got.Format(time.RFC3339))
	}
}
