package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	domainwater "empirebus-tests/service/domains/water"
)

var ErrWaterCommandInProgress = errors.New("water command already in progress")

const greyWaterValveHoldDuration = 5 * time.Second

func (a *App) WaterState() domainwater.State {
	var state domainwater.State
	if a.water != nil {
		state = a.water.WaterState()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	state.CommandInProgress = a.waterState.CommandInProgress
	state.LastCommandError = a.waterState.LastCommandError
	state.ScheduledOpening = cloneWaterScheduledOpening(a.waterState.ScheduledOpening)
	state.LastScheduleMessage = a.waterState.LastScheduleMessage
	state.LastScheduleCompletedAt = cloneTimePtr(a.waterState.LastScheduleCompletedAt)
	return state
}

func (a *App) OpenGreyWaterValve(ctx context.Context) error {
	return a.holdGreyWaterValve(ctx, domainwater.ValveDirectionOpening)
}

func (a *App) CloseGreyWaterValve(ctx context.Context) error {
	return a.holdGreyWaterValve(ctx, domainwater.ValveDirectionClosing)
}

func (a *App) holdGreyWaterValve(ctx context.Context, direction domainwater.ValveDirection) (err error) {
	if a.water == nil {
		err = fmt.Errorf("water controller not configured")
		a.setWaterCommandError(err.Error())
		return err
	}
	a.mu.Lock()
	if a.waterState.CommandInProgress {
		a.mu.Unlock()
		a.setWaterCommandError(ErrWaterCommandInProgress.Error())
		return ErrWaterCommandInProgress
	}
	a.waterState.CommandInProgress = true
	a.waterState.LastCommandError = ""
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.waterState.CommandInProgress = false
		if err != nil {
			a.waterState.LastCommandError = err.Error()
		} else {
			a.waterState.LastCommandError = ""
		}
		a.mu.Unlock()
	}()

	switch direction {
	case domainwater.ValveDirectionOpening:
		return a.water.OpenGreyWaterValve(ctx, greyWaterValveHoldDuration)
	case domainwater.ValveDirectionClosing:
		return a.water.CloseGreyWaterValve(ctx, greyWaterValveHoldDuration)
	default:
		err = fmt.Errorf("unsupported water valve direction %q", direction)
		return err
	}
}

func (a *App) setWaterCommandError(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.waterState.LastCommandError = message
}

func waterRuntimeStatePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return configPath + ".water-runtime.yaml"
}

func (a *App) ScheduleGreyWaterOpening(ctx context.Context, localTime string, duration time.Duration) (domainwater.State, error) {
	if duration <= 0 {
		return domainwater.State{}, fmt.Errorf("duration must be greater than zero")
	}
	durationMinutes := int(duration / time.Minute)
	if time.Duration(durationMinutes)*time.Minute != duration {
		return domainwater.State{}, fmt.Errorf("duration must be whole minutes")
	}
	hour, minute, err := config.ParseClockTime(localTime)
	if err != nil {
		return domainwater.State{}, err
	}
	timezoneName := a.currentAutomationTimezone()
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		return domainwater.State{}, err
	}
	now := a.nowTime()
	nowLocal := now.In(loc)
	openAtLocal := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, minute, 0, 0, loc)
	if !openAtLocal.After(now) {
		openAtLocal = openAtLocal.AddDate(0, 0, 1)
	}
	scheduled := config.GreyWaterScheduledOpening{
		OpenAt:          openAtLocal.UTC(),
		LocalTime:       fmt.Sprintf("%02d:%02d", hour, minute),
		Timezone:        timezoneName,
		DurationMinutes: durationMinutes,
		Status:          config.GreyWaterSchedulePending,
	}
	next := config.WaterRuntimeState{ScheduledOpening: &scheduled}
	if err := a.saveAndApplyWaterRuntimeState(ctx, next); err != nil {
		return domainwater.State{}, err
	}
	a.signalWaterSchedulerWake()
	return a.WaterState(), nil
}

func (a *App) CancelGreyWaterOpening(ctx context.Context) (domainwater.State, error) {
	next := a.currentWaterRuntimeState()
	next.ScheduledOpening = nil
	if err := a.saveAndApplyWaterRuntimeState(ctx, next); err != nil {
		return domainwater.State{}, err
	}
	a.signalWaterSchedulerWake()
	return a.WaterState(), nil
}

func (a *App) loadWaterRuntimeState() error {
	path := a.waterRuntimeStatePath
	if path == "" {
		return nil
	}
	state, recovered, err := config.LoadWaterRuntimeStateWithRecovery(path)
	if err != nil {
		return err
	}
	if recovered {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := a.CloseGreyWaterValve(closeCtx); err != nil {
			a.logger.Printf("water runtime state recovery: failed to close grey water valve: %v", err)
		}
		cancel()
		if err := config.SaveWaterRuntimeState(path, state); err != nil {
			a.logger.Printf("water runtime state recovery: failed to save safe state: %v", err)
		}
	}
	a.applyWaterRuntimeState(state)
	return nil
}

func (a *App) currentWaterRuntimeState() config.WaterRuntimeState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneWaterRuntimeState(a.waterRuntimeState)
}

func (a *App) saveAndApplyWaterRuntimeState(_ context.Context, state config.WaterRuntimeState) error {
	a.mu.RLock()
	path := a.waterRuntimeStatePath
	a.mu.RUnlock()
	if path == "" {
		return fmt.Errorf("water runtime state path is not configured")
	}
	if err := config.SaveWaterRuntimeState(path, state); err != nil {
		return err
	}
	a.applyWaterRuntimeState(state)
	a.publishWaterStateChanged()
	return nil
}

func (a *App) applyWaterRuntimeState(state config.WaterRuntimeState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.waterRuntimeState = cloneWaterRuntimeState(state)
	a.waterState.ScheduledOpening = domainScheduledOpening(state.ScheduledOpening)
	a.waterState.LastScheduleMessage = state.LastScheduleMessage
	a.waterState.LastScheduleCompletedAt = cloneTimePtr(state.LastCompletedAt)
}

func (a *App) waterSchedulerLoop(ctx context.Context) {
	for {
		state := a.currentWaterRuntimeState()
		scheduled := state.ScheduledOpening
		if scheduled == nil {
			select {
			case <-ctx.Done():
				return
			case <-a.waterSchedulerWake:
				continue
			}
		}
		wait := time.Until(scheduled.OpenAt)
		if scheduled.Status == config.GreyWaterScheduleOpen {
			wait = time.Until(scheduled.OpenAt.Add(time.Duration(scheduled.DurationMinutes) * time.Minute))
		}
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.waterSchedulerWake:
			timer.Stop()
			continue
		case <-timer.C:
		}
		if err := a.executeDueGreyWaterOpening(ctx); err != nil {
			a.logger.Printf("scheduled grey water opening failed: %v", err)
		}
	}
}

func (a *App) executeDueGreyWaterOpening(ctx context.Context) error {
	state := a.currentWaterRuntimeState()
	scheduled := state.ScheduledOpening
	if scheduled == nil {
		return nil
	}
	now := a.nowTime().UTC()
	duration := time.Duration(scheduled.DurationMinutes) * time.Minute
	if scheduled.Status == config.GreyWaterSchedulePending {
		if now.Before(scheduled.OpenAt) {
			return nil
		}
		if now.After(scheduled.OpenAt.Add(duration)) {
			state.ScheduledOpening = nil
			state.LastScheduleMessage = fmt.Sprintf("Missed scheduled grey water opening at %s.", scheduled.LocalTime)
			completedAt := now
			state.LastCompletedAt = &completedAt
			_ = a.saveAndApplyWaterRuntimeState(ctx, state)
			return nil
		}
		openedAt := now
		scheduled.Status = config.GreyWaterScheduleOpen
		scheduled.OpenedAt = &openedAt
		state.ScheduledOpening = scheduled
		if err := a.saveAndApplyWaterRuntimeState(ctx, state); err != nil {
			return err
		}
		if err := a.OpenGreyWaterValve(ctx); err != nil {
			return a.finishGreyWaterSchedule(ctx, *scheduled, fmt.Sprintf("Scheduled grey water opening failed: %v", err), now)
		}
	}
	closeAt := scheduled.OpenAt.Add(duration)
	wait := closeAt.Sub(a.nowTime())
	if wait > 0 {
		if err := a.sleepContext(ctx, wait); err != nil {
			return err
		}
	}
	if err := a.CloseGreyWaterValve(ctx); err != nil {
		return a.finishGreyWaterSchedule(ctx, *scheduled, fmt.Sprintf("Scheduled grey water close failed: %v", err), a.nowTime().UTC())
	}
	message := fmt.Sprintf("Grey water valve opened at %s for %d minutes.", scheduled.LocalTime, scheduled.DurationMinutes)
	return a.finishGreyWaterSchedule(ctx, *scheduled, message, a.nowTime().UTC())
}

func (a *App) finishGreyWaterSchedule(ctx context.Context, _ config.GreyWaterScheduledOpening, message string, at time.Time) error {
	state := a.currentWaterRuntimeState()
	state.ScheduledOpening = nil
	state.LastScheduleMessage = message
	completedAt := at.UTC()
	state.LastCompletedAt = &completedAt
	return a.saveAndApplyWaterRuntimeState(ctx, state)
}

func (a *App) sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	if a.sleep != nil {
		a.sleep(duration)
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a *App) signalWaterSchedulerWake() {
	if a.waterSchedulerWake == nil {
		return
	}
	select {
	case a.waterSchedulerWake <- struct{}{}:
	default:
	}
}

func (a *App) publishWaterStateChanged() {
	if a.broker == nil {
		return
	}
	a.broker.Publish(events.Event{
		Type:      "water.state_changed",
		Timestamp: a.nowTime().UTC(),
		Payload:   a.WaterState(),
	})
}

func (a *App) nowTime() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func cloneWaterRuntimeState(state config.WaterRuntimeState) config.WaterRuntimeState {
	out := state
	if state.ScheduledOpening != nil {
		scheduled := *state.ScheduledOpening
		scheduled.OpenedAt = cloneTimePtr(state.ScheduledOpening.OpenedAt)
		out.ScheduledOpening = &scheduled
	}
	out.LastCompletedAt = cloneTimePtr(state.LastCompletedAt)
	return out
}

func domainScheduledOpening(scheduled *config.GreyWaterScheduledOpening) *domainwater.ScheduledOpening {
	if scheduled == nil {
		return nil
	}
	return &domainwater.ScheduledOpening{
		OpenAt:          scheduled.OpenAt,
		LocalTime:       scheduled.LocalTime,
		Timezone:        scheduled.Timezone,
		DurationMinutes: scheduled.DurationMinutes,
		Status:          string(scheduled.Status),
		OpenedAt:        cloneTimePtr(scheduled.OpenedAt),
	}
}

func cloneWaterScheduledOpening(scheduled *domainwater.ScheduledOpening) *domainwater.ScheduledOpening {
	if scheduled == nil {
		return nil
	}
	out := *scheduled
	out.OpenedAt = cloneTimePtr(scheduled.OpenedAt)
	return &out
}

func cloneTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
