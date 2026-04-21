package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"empirebus-tests/service/adapters/garmin"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/automation/scheduler"
	"empirebus-tests/service/config"
	domainheating "empirebus-tests/service/domains/heating"
)

type HeatingController interface {
	EnsureOn(context.Context) error
	EnsureOff(context.Context) error
	SetTargetTemperature(context.Context, float64) error
	CurrentState() domainheating.State
	Health() domainheating.AdapterHealth
}

type App struct {
	startedAt time.Time
	cfg       config.NormalizedConfig
	logger    *log.Logger
	adapter   HeatingController
	broker    *events.Broker

	mu               sync.RWMutex
	schedulerRunning bool
}

type HeatingStateView = domainheating.State
type ServiceHealthView = domainheating.ServiceHealth

type ProgramStatus struct {
	ID               string                       `json:"id"`
	Enabled          bool                         `json:"enabled"`
	Days             []time.Weekday              `json:"days"`
	Periods          []domainheating.HeatingPeriod `json:"periods"`
	ActivePeriod     domainheating.HeatingPeriod `json:"active_period"`
	NextPeriod       domainheating.HeatingPeriod `json:"next_period"`
	NextTransitionAt *time.Time                   `json:"next_transition_at,omitempty"`
	Action           scheduler.Action            `json:"action"`
}

func New(ctx context.Context, cfg config.NormalizedConfig, logger *log.Logger) *App {
	if logger == nil {
		logger = log.New(log.Writer(), "", log.LstdFlags)
	}
	broker := events.NewBroker(32)
	adapter := garmin.New(garmin.Config{
		WSURL:             cfg.Garmin.WSURL,
		Origin:            cfg.Garmin.Origin,
		HeartbeatInterval: cfg.Garmin.HeartbeatInterval,
		TraceWindow:       cfg.Garmin.TraceWindow,
		Logger:            logger,
	})
	adapter.Start(ctx)
	app := &App{
		startedAt: time.Now().UTC(),
		cfg:       cfg,
		logger:    logger,
		adapter:   adapter,
		broker:    broker,
	}
	go app.publishStateLoop(ctx)
	go app.schedulerLoop(ctx)
	return app
}

func (a *App) Broker() *events.Broker {
	return a.broker
}

func (a *App) HeatingState() domainheating.State {
	return a.adapter.CurrentState()
}

func (a *App) Health() domainheating.ServiceHealth {
	garminHealth := a.adapter.Health()
	status := "ok"
	if !garminHealth.Connected {
		status = "degraded"
	}
	a.mu.RLock()
	schedulerRunning := a.schedulerRunning
	a.mu.RUnlock()
	return domainheating.ServiceHealth{
		Status:           status,
		StartedAt:        a.startedAt,
		Garmin:           garminHealth,
		SchedulerRunning: schedulerRunning,
		ConfigLoaded:     true,
	}
}

func (a *App) EnsurePower(ctx context.Context, power string) error {
	switch power {
	case "on":
		return a.adapter.EnsureOn(ctx)
	case "off":
		return a.adapter.EnsureOff(ctx)
	default:
		return fmt.Errorf("unsupported power state %q", power)
	}
}

func (a *App) SetTargetTemperature(ctx context.Context, celsius float64) error {
	return a.adapter.SetTargetTemperature(ctx, celsius)
}

func (a *App) HeatingPrograms(now time.Time) []ProgramStatus {
	statuses := make([]ProgramStatus, 0, len(a.cfg.Automation.HeatingPrograms))
	for _, program := range a.cfg.Automation.HeatingPrograms {
		status := ProgramStatus{
			ID:      program.ID,
			Enabled: program.Enabled,
			Days:    append([]time.Weekday(nil), program.Days...),
			Periods: append([]domainheating.HeatingPeriod(nil), program.Periods...),
			Action:  scheduler.Action{Kind: scheduler.ActionKindNoop},
		}
		calc, err := scheduler.Calculate(program, a.cfg.Automation.Location, now)
		if err == nil {
			status.ActivePeriod = calc.ActivePeriod
			status.NextPeriod = calc.NextPeriod
			nextAt := calc.NextTransitionAt
			status.NextTransitionAt = &nextAt
			status.Action = calc.Action
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func (a *App) publishStateLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	var last domainheating.State
	lastConnected := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := a.adapter.CurrentState()
			if current != last {
				last = current
				a.broker.Publish(events.Event{
					Type:      "heating.state_changed",
					Timestamp: time.Now().UTC(),
					Payload:   current,
				})
			}
			health := a.adapter.Health()
			if health.Connected != lastConnected {
				lastConnected = health.Connected
				a.broker.Publish(events.Event{
					Type:      "service.connection_changed",
					Timestamp: time.Now().UTC(),
					Payload:   health,
				})
			}
		}
	}
}

func (a *App) schedulerLoop(ctx context.Context) {
	a.mu.Lock()
	a.schedulerRunning = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.schedulerRunning = false
		a.mu.Unlock()
	}()

	a.reconcileCurrentState(ctx)

	for {
		next, err := scheduler.Next(a.cfg.Automation.HeatingPrograms, a.cfg.Automation.Location, time.Now())
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}
		wait := time.Until(next.Calculation.NextTransitionAt)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		a.executeTransition(ctx, next)
	}
}

func (a *App) reconcileCurrentState(ctx context.Context) {
	now := time.Now()
	for _, program := range a.cfg.Automation.HeatingPrograms {
		if !program.Enabled || !program.AppliesOn(now.In(a.cfg.Automation.Location).Weekday()) {
			continue
		}
		calc, err := scheduler.Calculate(program, a.cfg.Automation.Location, now)
		if err != nil {
			a.logger.Printf("scheduler reconcile: %v", err)
			continue
		}
		if err := a.applyPeriod(ctx, calc.ActivePeriod); err != nil {
			a.logger.Printf("scheduler reconcile program=%s: %v", program.ID, err)
		}
	}
}

func (a *App) executeTransition(ctx context.Context, next scheduler.ProgramCalculation) {
	correlationID := fmt.Sprintf("sched-%d", time.Now().UnixNano())
	a.broker.Publish(events.Event{
		Type:          "automation.run_started",
		Timestamp:     time.Now().UTC(),
		CorrelationID: correlationID,
		Payload: map[string]interface{}{
			"program_id":         next.Program.ID,
			"next_transition_at": next.Calculation.NextTransitionAt,
			"action":             next.Calculation.Action,
		},
	})
	err := a.applyPeriod(ctx, next.Calculation.NextPeriod)
	if err != nil {
		a.logger.Printf("scheduler program=%s: %v", next.Program.ID, err)
		a.broker.Publish(events.Event{
			Type:          "automation.run_failed",
			Timestamp:     time.Now().UTC(),
			CorrelationID: correlationID,
			Payload: map[string]interface{}{
				"program_id": next.Program.ID,
				"error":      err.Error(),
			},
		})
		return
	}
	a.broker.Publish(events.Event{
		Type:          "automation.run_succeeded",
		Timestamp:     time.Now().UTC(),
		CorrelationID: correlationID,
		Payload: map[string]interface{}{
			"program_id": next.Program.ID,
			"action":     next.Calculation.Action,
		},
	})
}

func (a *App) applyPeriod(ctx context.Context, period domainheating.HeatingPeriod) error {
	switch period.Mode {
	case domainheating.ModeOff:
		return a.adapter.EnsureOff(ctx)
	case domainheating.ModeHeat:
		if err := a.adapter.EnsureOn(ctx); err != nil {
			return err
		}
		if period.TargetCelsius == nil {
			return fmt.Errorf("missing target temperature for heat period")
		}
		return a.adapter.SetTargetTemperature(ctx, *period.TargetCelsius)
	default:
		return fmt.Errorf("unsupported period mode %q", period.Mode)
	}
}
