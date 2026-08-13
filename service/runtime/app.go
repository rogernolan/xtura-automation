package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"empirebus-tests/heating"
	"empirebus-tests/service/adapters/garmin"
	"empirebus-tests/service/adapters/geotimezone"
	"empirebus-tests/service/adapters/teltonika"
	"empirebus-tests/service/adapters/tzfresolver"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/automation/scheduler"
	"empirebus-tests/service/config"
	domainheating "empirebus-tests/service/domains/heating"
	domainlights "empirebus-tests/service/domains/lights"
	domainlocation "empirebus-tests/service/domains/location"
	domainwater "empirebus-tests/service/domains/water"
	"empirebus-tests/service/recording"
	"empirebus-tests/service/tracking"
)

var recordingDirectory = "/var/lib/xtura/recordings"
var trackingDirectory = "/var/lib/xtura/tracks"

type HeatingController interface {
	EnsureOn(context.Context) error
	EnsureOff(context.Context) error
	SetTargetTemperature(context.Context, float64) error
	CurrentState() domainheating.State
	Health() domainheating.AdapterHealth
}

type LightsController interface {
	EnsureExteriorOn(context.Context) error
	EnsureExteriorOff(context.Context) error
	LightsState() domainlights.State
}

type WaterController interface {
	OpenGreyWaterValve(context.Context, time.Duration) error
	CloseGreyWaterValve(context.Context, time.Duration) error
	WaterState() domainwater.State
}

type LocationProvider interface {
	Poll(context.Context) (domainlocation.Fix, error)
}

type TimezoneResolver interface {
	Timezone(context.Context, float64, float64) (string, error)
}

type App struct {
	startedAt             time.Time
	cfg                   config.NormalizedConfig
	rawConfig             config.Config
	configPath            string
	revision              string
	modeState             config.HeatingRuntimeState
	runtimeStatePath      string
	waterRuntimeState     config.WaterRuntimeState
	waterRuntimeStatePath string
	logger                *log.Logger
	adapter               HeatingController
	lights                LightsController
	water                 WaterController
	location              LocationProvider
	timezoneResolver      TimezoneResolver
	broker                *events.Broker
	recording             *recording.Manager
	tracking              *tracking.Manager
	sleep                 func(time.Duration)
	now                   func() time.Time

	mu                 sync.RWMutex
	lightsState        domainlights.State
	waterState         domainwater.State
	locationState      domainlocation.State
	locationFixes      []domainlocation.Fix
	lastTimezoneSync   time.Time
	schedulerRunning   bool
	schedulerWake      chan struct{}
	waterSchedulerWake chan struct{}
}

type HeatingStateView = domainheating.State
type ServiceHealthView = domainheating.ServiceHealth
type LocationStateView = domainlocation.State
type WaterStateView = domainwater.State

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
	recorder := recording.New(recordingDirectory, time.Now, logger)
	recorder.SetOnChange(func(state recording.State) {
		broker.Publish(events.Event{
			Type:      "recording.state_changed",
			Timestamp: time.Now().UTC(),
			Payload:   state,
		})
	})
	var location LocationProvider
	var timezoneResolver TimezoneResolver
	if cfg.Location.Enabled {
		location = teltonika.NewRUTX50(teltonika.RUTX50Config{
			Endpoint:           cfg.Location.RUTX50.Endpoint,
			LoginEndpoint:      cfg.Location.RUTX50.LoginEndpoint,
			Username:           cfg.Location.RUTX50.Username,
			Password:           cfg.Location.RUTX50.Password,
			PasswordFile:       cfg.Location.RUTX50.PasswordFile,
			AuthToken:          cfg.Location.RUTX50.AuthToken,
			InsecureSkipVerify: cfg.Location.RUTX50.InsecureSkipVerify,
			Timeout:            cfg.Location.RUTX50.Timeout,
		})
		if cfg.Location.Timezone.Provider == "tzf" {
			resolver, err := tzfresolver.New()
			if err != nil {
				return nil, err
			}
			timezoneResolver = resolver
		}
		if cfg.Location.Timezone.Provider == "geotimezone" {
			timezoneResolver = geotimezone.New(cfg.Location.Timezone.Endpoint, cfg.Location.Timezone.Timeout)
		}
	}
	var trackingManager *tracking.Manager
	if location != nil {
		trackingDir := cfg.Tracking.Dir
		if trackingDir == "" {
			trackingDir = trackingDirectory
		}
		trackingManager = tracking.New(trackingDir, location.Poll, time.Now, logger)
		trackingManager.Configure(tracking.Settings{
			Enabled:          cfg.Tracking.Enabled,
			OnlyWhenEngineOn: cfg.Tracking.OnlyWhenEngineOn,
			SampleInterval:   cfg.Tracking.SampleInterval,
		})
		trackingManager.SetOnChange(func(state tracking.State) {
			broker.Publish(events.Event{
				Type:      "tracking.state_changed",
				Timestamp: time.Now().UTC(),
				Payload:   state,
			})
		})
		trackingManager.Start(ctx)
	}
	recordFrame := recorder.Observe
	if trackingManager != nil {
		recordFrame = func(at time.Time, direction heating.Direction, raw string) {
			recorder.Observe(at, direction, raw)
			trackingManager.ObserveFrame(at, direction, raw)
		}
	}
	adapter := garmin.New(garmin.Config{
		WSURL:             cfg.Garmin.WSURL,
		Origin:            cfg.Garmin.Origin,
		HeartbeatInterval: cfg.Garmin.HeartbeatInterval,
		TraceWindow:       cfg.Garmin.TraceWindow,
		Logger:            logger,
		RecordFrame:       recordFrame,
	})
	adapter.Start(ctx)
	var lights LightsController
	if controller, ok := interface{}(adapter).(LightsController); ok {
		lights = controller
	}
	var water WaterController
	if controller, ok := interface{}(adapter).(WaterController); ok {
		water = controller
	}
	app := &App{
		startedAt:             time.Now().UTC(),
		cfg:                   cfg,
		rawConfig:             rawConfig,
		configPath:            configPath,
		runtimeStatePath:      runtimeStatePath(configPath),
		waterRuntimeStatePath: waterRuntimeStatePath(configPath),
		logger:                logger,
		adapter:               adapter,
		lights:                lights,
		water:                 water,
		location:              location,
		timezoneResolver:      timezoneResolver,
		broker:                broker,
		recording:             recorder,
		tracking:              trackingManager,
		now:                   time.Now,
		schedulerWake:         make(chan struct{}, 1),
		waterSchedulerWake:    make(chan struct{}, 1),
	}
	go func() {
		<-ctx.Done()
		recorder.Shutdown()
	}()
	app.revision = readConfigRevision(configPath)
	if err := app.loadRuntimeState(); err != nil {
		return nil, err
	}
	if err := app.loadWaterRuntimeState(); err != nil {
		return nil, err
	}
	app.locationState = domainlocation.State{
		Configured:         cfg.Location.Enabled,
		Provider:           cfg.Location.Provider,
		SystemTimezone:     currentSystemTimezone(),
		TimezoneUpdateMode: timezoneUpdateMode(cfg.Location.TimezoneUpdate),
	}
	go app.publishStateLoop(ctx)
	if cfg.Location.Enabled {
		go app.locationLoop(ctx)
	}
	go app.schedulerLoop(ctx)
	go app.waterSchedulerLoop(ctx)
	return app, nil
}

func (a *App) Broker() *events.Broker {
	return a.broker
}

func (a *App) RecordingState() recording.State {
	return a.recording.State()
}

func (a *App) StartRecording(_ context.Context, request recording.StartRequest) (recording.State, error) {
	return a.recording.Start(request)
}

func (a *App) StopRecording(_ context.Context) recording.State {
	return a.recording.Stop("stopped")
}

func (a *App) HeatingState() domainheating.State {
	return a.adapter.CurrentState()
}

func (a *App) LightsState() domainlights.State {
	var state domainlights.State
	if a.lights != nil {
		state = a.lights.LightsState()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	state.FlashInProgress = a.lightsState.FlashInProgress
	state.LastCommandError = a.lightsState.LastCommandError
	return state
}

func (a *App) LocationState() domainlocation.State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.locationState
}

func (a *App) TrackingSettings() tracking.Settings {
	if a.tracking == nil {
		return tracking.Settings{}
	}
	state := a.tracking.State()
	return tracking.Settings{
		Enabled:          state.Enabled,
		OnlyWhenEngineOn: state.OnlyWhenEngineOn,
		SampleInterval:   time.Duration(state.SampleIntervalSeconds * float64(time.Second)),
	}
}

func (a *App) UpdateTrackingSettings(ctx context.Context, settings tracking.Settings) (tracking.Settings, error) {
	a.mu.RLock()
	currentConfig := a.rawConfig
	configPath := a.configPath
	a.mu.RUnlock()
	if strings.TrimSpace(configPath) == "" {
		return tracking.Settings{}, fmt.Errorf("config path is not configured")
	}
	nextConfig := currentConfig
	onlyWhenEngineOn := settings.OnlyWhenEngineOn
	nextConfig.Tracking = config.TrackingConfig{
		Enabled:          settings.Enabled,
		OnlyWhenEngineOn: &onlyWhenEngineOn,
		SampleInterval:   settings.SampleInterval,
		Dir:              currentConfig.Tracking.Dir,
	}
	nextNormalized, err := nextConfig.Normalize()
	if err != nil {
		return tracking.Settings{}, err
	}
	if err := config.SaveFile(configPath, nextConfig); err != nil {
		return tracking.Settings{}, err
	}
	revision := readConfigRevision(configPath)
	a.mu.Lock()
	a.rawConfig = nextConfig
	a.cfg = nextNormalized
	a.revision = revision
	a.mu.Unlock()
	if a.tracking != nil {
		a.tracking.Configure(tracking.Settings{
			Enabled:          settings.Enabled,
			OnlyWhenEngineOn: settings.OnlyWhenEngineOn,
			SampleInterval:   settings.SampleInterval,
		})
	}
	out := tracking.Settings{
		Enabled:          nextNormalized.Tracking.Enabled,
		OnlyWhenEngineOn: nextNormalized.Tracking.OnlyWhenEngineOn,
		SampleInterval:   nextNormalized.Tracking.SampleInterval,
	}
	a.logger.Printf("tracking settings updated: enabled=%t only_when_engine_on=%t sample_interval=%s", out.Enabled, out.OnlyWhenEngineOn, out.SampleInterval)
	return out, nil
}

func (a *App) TrackingState() tracking.State {
	if a.tracking == nil {
		return tracking.State{}
	}
	return a.tracking.State()
}

func (a *App) TrackList() ([]tracking.FileInfo, error) {
	if a.tracking == nil {
		return nil, nil
	}
	return a.tracking.List()
}

func (a *App) TrackRead(name string) ([]byte, error) {
	if a.tracking == nil {
		return nil, fmt.Errorf("tracking manager not available")
	}
	return a.tracking.Read(name)
}

func (a *App) TrackDelete(name string) error {
	if a.tracking == nil {
		return fmt.Errorf("tracking manager not available")
	}
	return a.tracking.Delete(name)
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

func (a *App) locationLoop(ctx context.Context) {
	a.pollLocation(ctx)
	a.mu.RLock()
	pollInterval := a.cfg.Location.PollInterval
	a.mu.RUnlock()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollLocation(ctx)
		}
	}
}

func (a *App) pollLocation(ctx context.Context) {
	if a.location == nil {
		return
	}
	a.mu.RLock()
	provider := a.cfg.Location.Provider
	pollTimeout := a.cfg.Location.RUTX50.Timeout
	timezoneTimeout := a.cfg.Location.Timezone.Timeout
	timezoneUpdate := a.cfg.Location.TimezoneUpdate
	movementWindow := a.cfg.Location.Movement.Window
	minDistanceMeters := a.cfg.Location.Movement.MinDistanceMeters
	a.mu.RUnlock()
	pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	fix, err := a.location.Poll(pollCtx)
	now := time.Now().UTC()
	if err != nil {
		a.mu.Lock()
		a.locationState.Configured = true
		a.locationState.Provider = provider
		a.locationState.LastError = err.Error()
		a.locationState.LastErrorAt = &now
		a.locationState.SystemTimezone = currentSystemTimezone()
		a.locationState.TimezoneUpdateMode = timezoneUpdateMode(timezoneUpdate)
		a.mu.Unlock()
		a.logger.Printf("location poll failed: %v", err)
		return
	}
	timezoneName := a.currentAutomationTimezone()
	if a.timezoneResolver != nil {
		resolveCtx, resolveCancel := context.WithTimeout(ctx, timezoneTimeout)
		resolved, resolveErr := a.timezoneResolver.Timezone(resolveCtx, fix.Latitude, fix.Longitude)
		resolveCancel()
		if resolveErr != nil {
			err = fmt.Errorf("timezone lookup: %w", resolveErr)
			a.logger.Printf("location timezone lookup failed: %v", err)
		} else {
			timezoneName = resolved
		}
	}
	var timezoneUpdatedAt *time.Time
	if err == nil && timezoneName != "" {
		if updated, updateErr := a.maybeUpdateTimezone(ctx, timezoneName, now); updateErr != nil {
			err = fmt.Errorf("timezone update: %w", updateErr)
			a.logger.Printf("location timezone update failed: %v", err)
		} else if updated {
			timezoneUpdatedAt = &now
		}
	}
	a.mu.Lock()
	a.locationFixes = recentLocationFixes(append(a.locationFixes, fix), fix.UpdatedAt, movementWindow)
	movementMeters := cumulativeMovementMeters(a.locationFixes)
	isMoving := movementMeters >= minDistanceMeters
	a.locationState = domainlocation.State{
		Configured:         true,
		Known:              true,
		Provider:           provider,
		Latitude:           fix.Latitude,
		Longitude:          fix.Longitude,
		Altitude:           fix.Altitude,
		IsMoving:           isMoving,
		MovementMeters:     movementMeters,
		Timezone:           timezoneName,
		SystemTimezone:     currentSystemTimezone(),
		TimezoneUpdatedAt:  timezoneUpdatedAt,
		LastUpdatedAt:      &fix.UpdatedAt,
		TimezoneUpdateMode: timezoneUpdateMode(timezoneUpdate),
	}
	if err != nil {
		a.locationState.LastError = err.Error()
		a.locationState.LastErrorAt = &now
	}
	a.mu.Unlock()
}

func recentLocationFixes(fixes []domainlocation.Fix, now time.Time, window time.Duration) []domainlocation.Fix {
	if window <= 0 {
		window = 15 * time.Minute
	}
	cutoff := now.Add(-window)
	start := 0
	for start < len(fixes) && fixes[start].UpdatedAt.Before(cutoff) {
		start++
	}
	out := append([]domainlocation.Fix(nil), fixes[start:]...)
	return out
}

func cumulativeMovementMeters(fixes []domainlocation.Fix) float64 {
	var total float64
	for i := 1; i < len(fixes); i++ {
		total += distanceMeters(fixes[i-1], fixes[i])
	}
	return total
}

func distanceMeters(a, b domainlocation.Fix) float64 {
	const earthRadiusMeters = 6371000
	lat1 := degreesToRadians(a.Latitude)
	lat2 := degreesToRadians(b.Latitude)
	dLat := degreesToRadians(b.Latitude - a.Latitude)
	dLon := degreesToRadians(b.Longitude - a.Longitude)
	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)
	h := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLon*sinLon
	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(h))
}

func degreesToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func (a *App) maybeUpdateTimezone(ctx context.Context, timezoneName string, now time.Time) (bool, error) {
	if _, err := time.LoadLocation(timezoneName); err != nil {
		return false, err
	}
	a.mu.RLock()
	cfg := a.cfg.Location.TimezoneUpdate
	lastSync := a.lastTimezoneSync
	currentConfigTZ := a.rawConfig.Automation.Timezone
	a.mu.RUnlock()
	if !cfg.Enabled {
		return false, nil
	}
	if !lastSync.IsZero() && now.Sub(lastSync) < cfg.Interval && currentConfigTZ == timezoneName {
		return false, nil
	}
	if cfg.UpdateConfig && currentConfigTZ != timezoneName {
		if err := a.updateConfigTimezone(timezoneName); err != nil {
			return false, err
		}
	}
	if len(cfg.Command) > 0 && currentSystemTimezone() != timezoneName {
		args := append([]string(nil), cfg.Command[1:]...)
		args = append(args, timezoneName)
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := exec.CommandContext(runCtx, cfg.Command[0], args...).Run(); err != nil {
			return false, err
		}
	}
	a.mu.Lock()
	a.lastTimezoneSync = now
	a.mu.Unlock()
	return true, nil
}

func (a *App) updateConfigTimezone(timezoneName string) error {
	a.mu.RLock()
	nextConfig := a.rawConfig
	configPath := a.configPath
	a.mu.RUnlock()
	if strings.TrimSpace(configPath) == "" {
		return fmt.Errorf("config path is not configured")
	}
	nextConfig.Automation.Timezone = timezoneName
	nextNormalized, err := nextConfig.Normalize()
	if err != nil {
		return err
	}
	if err := config.SaveFile(configPath, nextConfig); err != nil {
		return err
	}
	revision := readConfigRevision(configPath)
	a.mu.Lock()
	a.rawConfig = nextConfig
	a.cfg = nextNormalized
	a.revision = revision
	a.mu.Unlock()
	a.signalSchedulerWake()
	out := nextConfig.HeatingScheduleDocument(revision)
	a.broker.Publish(events.Event{
		Type:      "automation.schedule_updated",
		Timestamp: time.Now().UTC(),
		Payload:   out,
	})
	return nil
}

func (a *App) currentAutomationTimezone() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rawConfig.Automation.Timezone
}

func currentSystemTimezone() string {
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if timezoneName := timezoneFromLocaltimeTarget(target); timezoneName != "" {
			return timezoneName
		}
	}
	data, err := os.ReadFile("/etc/timezone")
	if err == nil && strings.TrimSpace(string(data)) != "" {
		return strings.TrimSpace(string(data))
	}
	return time.Local.String()
}

func timezoneFromLocaltimeTarget(target string) string {
	const marker = "zoneinfo/"
	index := strings.LastIndex(target, marker)
	if index < 0 {
		return ""
	}
	timezoneName := strings.TrimSpace(target[index+len(marker):])
	if timezoneName == "" {
		return ""
	}
	if _, err := time.LoadLocation(timezoneName); err != nil {
		return ""
	}
	return timezoneName
}

func timezoneUpdateMode(cfg config.TimezoneUpdateConfig) string {
	if !cfg.Enabled {
		return "disabled"
	}
	if cfg.UpdateConfig && len(cfg.Command) > 0 {
		return "config_and_command"
	}
	if cfg.UpdateConfig {
		return "config"
	}
	return "command"
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
	if err := domainheating.ValidateTargetCelsius(celsius); err != nil {
		return err
	}
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
	var lastLocation domainlocation.State
	var lastWater domainwater.State
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
			location := a.LocationState()
			if location != lastLocation {
				lastLocation = location
				a.broker.Publish(events.Event{
					Type:      "location.state_changed",
					Timestamp: time.Now().UTC(),
					Payload:   location,
				})
			}
			water := a.WaterState()
			if water != lastWater {
				lastWater = water
				a.broker.Publish(events.Event{
					Type:      "water.state_changed",
					Timestamp: time.Now().UTC(),
					Payload:   water,
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
				a.logger.Printf("scheduler next transition unavailable: err=%v mode=%s", err, a.HeatingMode().Mode)
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
			a.logger.Printf("scheduler reconcile calculate failed: program=%s now=%s err=%v", program.ID, now.UTC().Format(time.RFC3339), err)
			continue
		}
		if err := a.applyPeriod(ctx, calc.ActivePeriod); err != nil {
			a.logger.Printf(
				"scheduler reconcile apply failed: program=%s mode=%s target=%s err=%v",
				program.ID,
				calc.ActivePeriod.Mode,
				formatTargetCelsius(calc.ActivePeriod.TargetCelsius),
				err,
			)
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
		a.logger.Printf(
			"scheduler execution failed: program=%s action=%s next_mode=%s next_target=%s err=%v",
			next.Program.ID,
			next.Calculation.Action.Kind,
			next.Calculation.NextPeriod.Mode,
			formatTargetCelsius(next.Calculation.NextPeriod.TargetCelsius),
			err,
		)
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

func formatTargetCelsius(target *float64) string {
	if target == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1fC", *target)
}
