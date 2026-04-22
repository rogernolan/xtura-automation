package runtime

import (
	"context"
	"fmt"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
)

func runtimeStatePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return configPath + ".runtime.yaml"
}

func (a *App) HeatingMode() config.HeatingRuntimeState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneRuntimeState(a.modeState)
}

func (a *App) SetHeatingModeSchedule(ctx context.Context) (config.HeatingRuntimeState, error) {
	state := config.DefaultHeatingRuntimeState()
	state.Mode = config.HeatingModeSchedule
	return a.setHeatingMode(ctx, state)
}

func (a *App) SetHeatingModeOff(ctx context.Context) (config.HeatingRuntimeState, error) {
	state := config.DefaultHeatingRuntimeState()
	state.Mode = config.HeatingModeOff
	return a.setHeatingMode(ctx, state)
}

func (a *App) SetHeatingModeManual(ctx context.Context, targetCelsius float64) (config.HeatingRuntimeState, error) {
	state := config.DefaultHeatingRuntimeState()
	state.Mode = config.HeatingModeManual
	state.ManualTargetCelsius = &targetCelsius
	return a.setHeatingMode(ctx, state)
}

func (a *App) SetHeatingModeBoost(ctx context.Context, targetCelsius float64, duration time.Duration) (config.HeatingRuntimeState, error) {
	if duration <= 0 {
		return config.HeatingRuntimeState{}, fmt.Errorf("boost duration must be greater than zero")
	}
	base := a.baseRuntimeState()
	expiresAt := time.Now().UTC().Add(duration)
	state := config.DefaultHeatingRuntimeState()
	state.Mode = config.HeatingModeBoost
	state.Boost = &config.HeatingBoostState{
		TargetCelsius:             targetCelsius,
		ExpiresAt:                 expiresAt,
		ResumeMode:                base.Mode,
		ResumeManualTargetCelsius: cloneFloat64Ptr(base.ManualTargetCelsius),
	}
	return a.setHeatingMode(ctx, state)
}

func (a *App) setHeatingMode(ctx context.Context, state config.HeatingRuntimeState) (config.HeatingRuntimeState, error) {
	state.UpdatedAt = time.Now().UTC()
	if err := state.Validate(); err != nil {
		return config.HeatingRuntimeState{}, err
	}
	a.mu.RLock()
	path := a.runtimeStatePath
	a.mu.RUnlock()
	if path == "" {
		return config.HeatingRuntimeState{}, fmt.Errorf("runtime state path is not configured")
	}
	if err := config.SaveHeatingRuntimeState(path, state); err != nil {
		return config.HeatingRuntimeState{}, err
	}
	a.mu.Lock()
	a.modeState = cloneRuntimeState(state)
	a.mu.Unlock()
	if err := a.applyRuntimeMode(ctx, state); err != nil {
		return config.HeatingRuntimeState{}, err
	}
	a.logger.Printf("heating mode changed: %s", formatHeatingModeLog(state))
	a.signalSchedulerWake()
	a.broker.Publish(events.Event{
		Type:      "heating.mode_changed",
		Timestamp: time.Now().UTC(),
		Payload:   state,
	})
	return state, nil
}

func (a *App) loadRuntimeState() error {
	path := a.runtimeStatePath
	if path == "" {
		a.modeState = config.DefaultHeatingRuntimeState()
		return nil
	}
	state, err := config.LoadHeatingRuntimeState(path)
	if err != nil {
		return err
	}
	if expired, collapsed := collapseExpiredBoost(state, time.Now().UTC()); expired {
		a.logger.Printf("heating boost expired on startup: restoring %s", formatHeatingModeLog(collapsed))
		state = collapsed
		if err := config.SaveHeatingRuntimeState(path, state); err != nil {
			return err
		}
	}
	a.modeState = cloneRuntimeState(state)
	return nil
}

func collapseExpiredBoost(state config.HeatingRuntimeState, now time.Time) (bool, config.HeatingRuntimeState) {
	if state.Mode != config.HeatingModeBoost || state.Boost == nil {
		return false, state
	}
	if now.Before(state.Boost.ExpiresAt) {
		return false, state
	}
	next := config.DefaultHeatingRuntimeState()
	next.Mode = state.Boost.ResumeMode
	next.ManualTargetCelsius = cloneFloat64Ptr(state.Boost.ResumeManualTargetCelsius)
	next.UpdatedAt = now.UTC()
	return true, next
}

func (a *App) baseRuntimeState() config.HeatingRuntimeState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return baseRuntimeState(a.modeState)
}

func baseRuntimeState(state config.HeatingRuntimeState) config.HeatingRuntimeState {
	if state.Mode == config.HeatingModeBoost && state.Boost != nil {
		base := config.DefaultHeatingRuntimeState()
		base.Mode = state.Boost.ResumeMode
		base.ManualTargetCelsius = cloneFloat64Ptr(state.Boost.ResumeManualTargetCelsius)
		base.UpdatedAt = state.UpdatedAt
		return base
	}
	return cloneRuntimeState(state)
}

func cloneRuntimeState(state config.HeatingRuntimeState) config.HeatingRuntimeState {
	out := state
	out.ManualTargetCelsius = cloneFloat64Ptr(state.ManualTargetCelsius)
	if state.Boost != nil {
		boost := *state.Boost
		boost.ResumeManualTargetCelsius = cloneFloat64Ptr(state.Boost.ResumeManualTargetCelsius)
		out.Boost = &boost
	}
	return out
}

func cloneFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func (a *App) applyRuntimeMode(ctx context.Context, state config.HeatingRuntimeState) error {
	switch state.Mode {
	case config.HeatingModeSchedule:
		a.reconcileCurrentState(ctx)
		return nil
	case config.HeatingModeOff:
		return a.adapter.EnsureOff(ctx)
	case config.HeatingModeManual:
		if state.ManualTargetCelsius == nil {
			return fmt.Errorf("manual mode requires manual_target_celsius")
		}
		if err := a.adapter.EnsureOn(ctx); err != nil {
			return err
		}
		return a.adapter.SetTargetTemperature(ctx, *state.ManualTargetCelsius)
	case config.HeatingModeBoost:
		if state.Boost == nil {
			return fmt.Errorf("boost mode requires boost")
		}
		if err := a.adapter.EnsureOn(ctx); err != nil {
			return err
		}
		return a.adapter.SetTargetTemperature(ctx, state.Boost.TargetCelsius)
	default:
		return fmt.Errorf("unsupported heating mode %q", state.Mode)
	}
}

func (a *App) signalSchedulerWake() {
	select {
	case a.schedulerWake <- struct{}{}:
	default:
	}
}

func (a *App) reconcileExpiredBoost(ctx context.Context) error {
	a.mu.RLock()
	state := cloneRuntimeState(a.modeState)
	path := a.runtimeStatePath
	a.mu.RUnlock()
	expired, collapsed := collapseExpiredBoost(state, time.Now().UTC())
	if !expired {
		return nil
	}
	if err := config.SaveHeatingRuntimeState(path, collapsed); err != nil {
		return err
	}
	a.mu.Lock()
	a.modeState = cloneRuntimeState(collapsed)
	a.mu.Unlock()
	if err := a.applyRuntimeMode(ctx, collapsed); err != nil {
		return err
	}
	a.logger.Printf("heating boost expired: restoring %s", formatHeatingModeLog(collapsed))
	a.broker.Publish(events.Event{
		Type:      "heating.mode_changed",
		Timestamp: time.Now().UTC(),
		Payload:   collapsed,
	})
	return nil
}

func formatHeatingModeLog(state config.HeatingRuntimeState) string {
	switch state.Mode {
	case config.HeatingModeSchedule:
		return "schedule"
	case config.HeatingModeOff:
		return "off"
	case config.HeatingModeManual:
		if state.ManualTargetCelsius != nil {
			return fmt.Sprintf("manual target=%.1fC", *state.ManualTargetCelsius)
		}
		return "manual"
	case config.HeatingModeBoost:
		if state.Boost != nil {
			return fmt.Sprintf(
				"boost target=%.1fC until=%s resume=%s",
				state.Boost.TargetCelsius,
				state.Boost.ExpiresAt.UTC().Format(time.RFC3339),
				state.Boost.ResumeMode,
			)
		}
		return "boost"
	default:
		return string(state.Mode)
	}
}
