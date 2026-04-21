package scheduler

import (
	"errors"
	"fmt"
	"time"

	domainheating "empirebus-tests/service/domains/heating"
)

var ErrNoUpcomingTransition = errors.New("no upcoming transition found")

type ActionKind string

const (
	ActionKindNoop                ActionKind = "noop"
	ActionKindEnsureOnAndSetTarget ActionKind = "ensure_on_and_set_target"
	ActionKindSetTarget            ActionKind = "set_target"
	ActionKindEnsureOff            ActionKind = "ensure_off"
)

type Action struct {
	Kind          ActionKind `json:"kind"`
	TargetCelsius *float64   `json:"target_celsius,omitempty"`
}

type Calculation struct {
	ProgramID         string                      `json:"program_id"`
	ActivePeriod      domainheating.HeatingPeriod `json:"active_period"`
	ActiveIndex       int                         `json:"active_index"`
	NextPeriod        domainheating.HeatingPeriod `json:"next_period"`
	NextIndex         int                         `json:"next_index"`
	NextTransitionAt  time.Time                   `json:"next_transition_at"`
	Action            Action                      `json:"action"`
}

type ProgramCalculation struct {
	Program     domainheating.HeatingProgram
	Calculation Calculation
}

func Calculate(program domainheating.HeatingProgram, loc *time.Location, now time.Time) (Calculation, error) {
	if loc == nil {
		return Calculation{}, fmt.Errorf("location is required")
	}
	if err := program.Validate(); err != nil {
		return Calculation{}, err
	}
	nowLocal := now.In(loc)
	activePeriods := periodsForDay(program, nowLocal.Weekday())
	starts, err := resolveDayStarts(dateOnly(nowLocal), activePeriods, loc)
	if err != nil {
		return Calculation{}, err
	}
	activeIndex := 0
	for i := range starts {
		if !starts[i].After(now) {
			activeIndex = i
		}
	}
	active := activePeriods[activeIndex]
	nextPeriods, nextIndex, nextAt, err := nextDistinctTransition(program, loc, now, active)
	if err != nil {
		return Calculation{}, err
	}
	next := nextPeriods[nextIndex]
	return Calculation{
		ProgramID:        program.ID,
		ActivePeriod:     active,
		ActiveIndex:      activeIndex,
		NextPeriod:       next,
		NextIndex:        nextIndex,
		NextTransitionAt: nextAt,
		Action:           deriveAction(active, next),
	}, nil
}

func Next(programs []domainheating.HeatingProgram, loc *time.Location, now time.Time) (ProgramCalculation, error) {
	var (
		best ProgramCalculation
		set  bool
	)
	for _, program := range programs {
		if !program.Enabled {
			continue
		}
		calc, err := Calculate(program, loc, now)
		if err != nil {
			if errors.Is(err, ErrNoUpcomingTransition) {
				continue
			}
			return ProgramCalculation{}, err
		}
		if !set || calc.NextTransitionAt.Before(best.Calculation.NextTransitionAt) {
			best = ProgramCalculation{Program: program, Calculation: calc}
			set = true
		}
	}
	if !set {
		return ProgramCalculation{}, ErrNoUpcomingTransition
	}
	return best, nil
}

func periodsForDay(program domainheating.HeatingProgram, day time.Weekday) []domainheating.HeatingPeriod {
	if program.Enabled && program.AppliesOn(day) {
		return program.Periods
	}
	return []domainheating.HeatingPeriod{{
		Start: domainheating.LocalTime{Hour: 0, Minute: 0},
		Mode:  domainheating.ModeOff,
	}}
}

func nextDistinctTransition(program domainheating.HeatingProgram, loc *time.Location, now time.Time, current domainheating.HeatingPeriod) ([]domainheating.HeatingPeriod, int, time.Time, error) {
	nowLocal := now.In(loc)
	currentDate := dateOnly(nowLocal)
	for dayOffset := 0; dayOffset < 8; dayOffset++ {
		day := currentDate.AddDate(0, 0, dayOffset)
		periods := periodsForDay(program, day.Weekday())
		starts, err := resolveDayStarts(day, periods, loc)
		if err != nil {
			return nil, 0, time.Time{}, err
		}
		for i := range starts {
			if !starts[i].After(now) {
				continue
			}
			if domainheating.SameEffectiveState(current, periods[i]) {
				continue
			}
			return periods, i, starts[i], nil
		}
	}
	return nil, 0, time.Time{}, ErrNoUpcomingTransition
}

func resolveDayStarts(day time.Time, periods []domainheating.HeatingPeriod, loc *time.Location) ([]time.Time, error) {
	starts := make([]time.Time, 0, len(periods))
	for i, period := range periods {
		start, err := resolveLocalTime(day, period.Start, loc)
		if err != nil {
			return nil, fmt.Errorf("period %d: %w", i, err)
		}
		starts = append(starts, start)
	}
	return starts, nil
}

func resolveLocalTime(day time.Time, local domainheating.LocalTime, loc *time.Location) (time.Time, error) {
	if err := local.Validate(); err != nil {
		return time.Time{}, err
	}
	targetDate := dateOnly(day.In(loc))
	startOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, loc)
	targetMinutes := local.Minutes()
	for step := 0; step <= 30*60; step++ {
		candidate := startOfDay.Add(time.Duration(step) * time.Minute)
		candidateLocal := candidate.In(loc)
		if ymd(candidateLocal) != ymd(targetDate) {
			continue
		}
		minutes := candidateLocal.Hour()*60 + candidateLocal.Minute()
		if minutes >= targetMinutes {
			return candidate, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not resolve %s on %s", local, targetDate.Format("2006-01-02"))
}

func deriveAction(from, to domainheating.HeatingPeriod) Action {
	switch from.Mode {
	case domainheating.ModeOff:
		switch to.Mode {
		case domainheating.ModeOff:
			return Action{Kind: ActionKindNoop}
		case domainheating.ModeHeat:
			return Action{Kind: ActionKindEnsureOnAndSetTarget, TargetCelsius: to.TargetCelsius}
		}
	case domainheating.ModeHeat:
		switch to.Mode {
		case domainheating.ModeOff:
			return Action{Kind: ActionKindEnsureOff}
		case domainheating.ModeHeat:
			return Action{Kind: ActionKindSetTarget, TargetCelsius: to.TargetCelsius}
		}
	}
	return Action{Kind: ActionKindNoop}
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func ymd(t time.Time) string {
	return t.Format("2006-01-02")
}
