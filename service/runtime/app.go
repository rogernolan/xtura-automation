package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
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
	startedAt        time.Time
	cfg              config.NormalizedConfig
	rawConfig        config.Config
	configPath       string
	revision         string
	modeState        config.HeatingRuntimeState
	runtimeStatePath string
	logger           *log.Logger
	adapter          HeatingController
	broker           *events.Broker

	mu               sync.RWMutex
	schedulerRunning bool
	schedulerWake    chan struct{}
}

type HeatingStateView = domainheating.State
type ServiceHealthView = domainheating.ServiceHealth

var ErrScheduleRevisionConflict = errors.New("schedule revision conflict")

type ProgramStatus struct {
	ID               string                        `json:"id"`
	Enabled          bool                          `json:"enabled"`
	Days             []time.Weekday                `json:"days"`
	Periods          []domainheating.HeatingPeriod `json:"periods"`
	ActivePeriod     domainheating.HeatingPeriod   `json:"active_period"`
	NextPeriod       domainheating.HeatingPeriod   `json:"next_period"`
	NextTransitionAt *time.Time                    `json:"next_transition_at,omitempty"`
	Action           scheduler.Action              `json:"action"`
}

func New(ctx context.Context, rawConfig config.Config, configPath string, logger *log.Logger) (*App, error) {
	if logger == nil {
		logger = log.New(log.Writer(), "", log.LstdFlags)
	}
	cfg, err := rawConfig.Normalize()
	if err != nil {
		return nil, err
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
		startedAt:        time.Now().UTC(),
		cfg:              cfg,
		rawConfig:        rawConfig,
		configPath:       configPath,
		runtimeStatePath: runtimeStatePath(configPath),
		logger:           logger,
		adapter:          adapter,
		broker:           broker,
		schedulerWake:    make(chan struct{}, 1),
	}
	app.revision = readConfigRevision(configPath)
	if err := app.loadRuntimeState(); err != nil {
		return nil, err
	}
	go app.publishStateLoop(ctx)
	go app.schedulerLoop(ctx)
	return app, nil
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
	automation := a.automationSnapshot()
	statuses := make([]ProgramStatus, 0, len(automation.HeatingPrograms))
	for _, program := range automation.HeatingPrograms {
		status := ProgramStatus{
			ID:      program.ID,
			Enabled: program.Enabled,
			Days:    append([]time.Weekday(nil), program.Days...),
			Periods: append([]domainheating.HeatingPeriod(nil), program.Periods...),
			Action:  scheduler.Action{Kind: scheduler.ActionKindNoop},
		}
		calc, err := scheduler.Calculate(program, automation.Location, now)
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

func (a *App) HeatingSchedule() config.HeatingScheduleDocument {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rawConfig.HeatingScheduleDocument(a.revision)
}

func (a *App) UpdateHeatingSchedule(ctx context.Context, doc config.HeatingScheduleDocument) (config.HeatingScheduleDocument, error) {
	a.mu.RLock()
	currentRevision := a.revision
	currentConfig := a.rawConfig
	configPath := a.configPath
	a.mu.RUnlock()
	if strings.TrimSpace(configPath) == "" {
		return config.HeatingScheduleDocument{}, fmt.Errorf("config path is not configured")
	}
	if doc.Revision != "" && currentRevision != "" && doc.Revision != currentRevision {
		return config.HeatingScheduleDocument{}, ErrScheduleRevisionConflict
	}
	nextConfig, err := currentConfig.WithHeatingSchedule(doc)
	if err != nil {
		return config.HeatingScheduleDocument{}, err
	}
	nextNormalized, err := nextConfig.Normalize()
	if err != nil {
		return config.HeatingScheduleDocument{}, err
	}
	if err := config.SaveFile(configPath, nextConfig); err != nil {
		return config.HeatingScheduleDocument{}, err
	}
	revision := readConfigRevision(configPath)
	a.mu.Lock()
	a.rawConfig = nextConfig
	a.cfg = nextNormalized
	a.revision = revision
	a.mu.Unlock()
	a.reconcileCurrentState(ctx)
	a.signalSchedulerWake()
	out := nextConfig.HeatingScheduleDocument(revision)
	a.logger.Printf("heating schedule updated: programs=%d revision=%s timezone=%s", len(out.Programs), out.Revision, out.Timezone)
	a.broker.Publish(events.Event{
		Type:      "automation.schedule_updated",
		Timestamp: time.Now().UTC(),
		Payload:   out,
	})
	return out, nil
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

	if err := a.applyRuntimeMode(ctx, a.HeatingMode()); err != nil {
		a.logger.Printf("scheduler initial mode apply: %v", err)
	}

	for {
		if err := a.reconcileExpiredBoost(ctx); err != nil {
			a.logger.Printf("scheduler reconcile expired boost: %v", err)
		}
		mode := a.HeatingMode()
		if mode.Mode != config.HeatingModeSchedule {
			var wait <-chan time.Time
			if mode.Mode == config.HeatingModeBoost && mode.Boost != nil {
				timer := time.NewTimer(time.Until(mode.Boost.ExpiresAt))
				wait = timer.C
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-a.schedulerWake:
					timer.Stop()
					continue
				case <-wait:
					timer.Stop()
					continue
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-a.schedulerWake:
				continue
			}
		}
		automation := a.automationSnapshot()
		next, err := scheduler.Next(automation.HeatingPrograms, automation.Location, time.Now())
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-a.schedulerWake:
				continue
			case <-time.After(30 * time.Second):
				continue
			}
		}
		a.logger.Printf(
			"scheduler waiting: program=%s next_at=%s action=%s",
			next.Program.ID,
			next.Calculation.NextTransitionAt.UTC().Format(time.RFC3339),
			next.Calculation.Action.Kind,
		)
		wait := time.Until(next.Calculation.NextTransitionAt)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.schedulerWake:
			timer.Stop()
			continue
		case <-timer.C:
		}
		if a.HeatingMode().Mode != config.HeatingModeSchedule {
			continue
		}
		a.executeTransition(ctx, next)
	}
}

func (a *App) reconcileCurrentState(ctx context.Context) {
	if a.HeatingMode().Mode != config.HeatingModeSchedule {
		return
	}
	now := time.Now()
	automation := a.automationSnapshot()
	for _, program := range automation.HeatingPrograms {
		if !program.Enabled || !program.AppliesOn(now.In(automation.Location).Weekday()) {
			continue
		}
		calc, err := scheduler.Calculate(program, automation.Location, now)
		if err != nil {
			a.logger.Printf("scheduler reconcile: %v", err)
			continue
		}
		if err := a.applyPeriod(ctx, calc.ActivePeriod); err != nil {
			a.logger.Printf("scheduler reconcile program=%s: %v", program.ID, err)
		}
	}
}

func (a *App) automationSnapshot() config.NormalizedAutomation {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := config.NormalizedAutomation{
		Location:        a.cfg.Automation.Location,
		HeatingPrograms: make([]domainheating.HeatingProgram, 0, len(a.cfg.Automation.HeatingPrograms)),
	}
	for _, program := range a.cfg.Automation.HeatingPrograms {
		out.HeatingPrograms = append(out.HeatingPrograms, cloneHeatingProgram(program))
	}
	return out
}

func cloneHeatingProgram(program domainheating.HeatingProgram) domainheating.HeatingProgram {
	out := program
	out.Days = append([]time.Weekday(nil), program.Days...)
	out.Periods = make([]domainheating.HeatingPeriod, 0, len(program.Periods))
	for _, period := range program.Periods {
		cloned := period
		if period.TargetCelsius != nil {
			target := *period.TargetCelsius
			cloned.TargetCelsius = &target
		}
		out.Periods = append(out.Periods, cloned)
	}
	return out
}

func readConfigRevision(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano)
}

func (a *App) executeTransition(ctx context.Context, next scheduler.ProgramCalculation) {
	correlationID := fmt.Sprintf("sched-%d", time.Now().UnixNano())
	a.logger.Printf(
		"scheduler executing: program=%s action=%s at=%s",
		next.Program.ID,
		next.Calculation.Action.Kind,
		next.Calculation.NextTransitionAt.UTC().Format(time.RFC3339),
	)
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
	a.logger.Printf("scheduler succeeded: program=%s action=%s", next.Program.ID, next.Calculation.Action.Kind)
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
