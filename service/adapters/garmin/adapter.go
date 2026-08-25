package garmin

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	rootheating "empirebus-tests/heating"
	domainheating "empirebus-tests/service/domains/heating"
	domainlights "empirebus-tests/service/domains/lights"
	domainoverview "empirebus-tests/service/domains/overview"
	domainwater "empirebus-tests/service/domains/water"
)

type Config struct {
	WSURL             string
	Origin            string
	HeartbeatInterval time.Duration
	TraceWindow       time.Duration
	Logger            *log.Logger
	RecordFrame       func(time.Time, rootheating.Direction, string)
}

const (
	KindOpen  = "open"
	KindClose = "close"
)

type GreyWaterDischargeEvent struct {
	Kind string
	At   time.Time
}

type Adapter struct {
	cfg    Config
	logger *log.Logger

	mu          sync.RWMutex
	session     *rootheating.Session
	client      *rootheating.Client
	state       domainheating.State
	lightsState domainlights.State
	waterState  domainwater.State
	overview    domainoverview.Telemetry
	health      domainheating.AdapterHealth
	greyEvents  []GreyWaterDischargeEvent
}

func New(cfg Config) *Adapter {
	return &Adapter{cfg: cfg, logger: cfg.Logger}
}

func (a *Adapter) Start(ctx context.Context) {
	go a.loop(ctx)
}

func (a *Adapter) loop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			a.closeSession()
			return
		}
		if a.needsConnect() {
			a.tryConnect(ctx)
		}
		a.pollState()
		select {
		case <-ctx.Done():
			a.closeSession()
			return
		case <-ticker.C:
		}
	}
}

func (a *Adapter) needsConnect() bool {
	a.mu.RLock()
	session := a.session
	a.mu.RUnlock()
	if session == nil {
		return true
	}
	return session.Err() != nil
}

func (a *Adapter) tryConnect(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	if a.logger != nil {
		a.logger.Printf("garmin connect: ws_url=%s origin=%s", a.cfg.WSURL, a.cfg.Origin)
	}
	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             a.cfg.WSURL,
		Origin:            a.cfg.Origin,
		HeartbeatInterval: a.cfg.HeartbeatInterval,
		TraceWindow:       a.cfg.TraceWindow,
		Logger:            a.logger,
		RecordFrame:       a.cfg.RecordFrame,
	})
	if err := session.Connect(ctx); err != nil {
		a.mu.Lock()
		a.health.Connected = false
		a.health.LastError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin connect failed: ws_url=%s err=%v", a.cfg.WSURL, err)
		}
		return
	}
	a.mu.Lock()
	a.closeSessionLocked()
	a.session = session
	a.client = rootheating.NewClient(session)
	a.health.Connected = true
	a.health.LastError = ""
	a.mu.Unlock()
	if a.logger != nil {
		a.logger.Printf("garmin connect succeeded: ws_url=%s", a.cfg.WSURL)
	}
}

func (a *Adapter) pollState() {
	a.mu.RLock()
	session := a.session
	a.mu.RUnlock()
	if session == nil {
		return
	}
	if err := session.Err(); err != nil {
		a.mu.Lock()
		a.health.Connected = false
		a.health.LastError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin session error: %v", err)
		}
		return
	}
	state := session.State()
	snapshot := snapshotFromRootState(state)
	a.mu.RLock()
	currentLights := a.lightsState
	currentWater := a.waterState
	a.mu.RUnlock()
	lights := lightsSnapshotFromSession(session, currentLights)
	water := waterSnapshotFromSession(session, currentWater)
	overview := overviewTelemetryFromSession(session)
	edges := session.DrainReceivedSignalEdges()
	a.mu.Lock()
	a.state = snapshot
	a.lightsState = lights
	a.waterState = water
	a.overview = overview
	for _, edge := range edges {
		if !edge.On {
			continue
		}
		switch edge.Signal {
		case 4:
			a.greyEvents = append(a.greyEvents, GreyWaterDischargeEvent{Kind: KindOpen, At: edge.At})
		case 5:
			a.greyEvents = append(a.greyEvents, GreyWaterDischargeEvent{Kind: KindClose, At: edge.At})
		}
	}
	if !state.LastUpdated.IsZero() {
		last := state.LastUpdated
		a.health.LastFrameAt = &last
	}
	a.health.Connected = true
	a.health.LastError = ""
	a.mu.Unlock()
}

func (a *Adapter) DrainGreyWaterDischargeEvents() []GreyWaterDischargeEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.greyEvents) == 0 {
		return nil
	}
	events := append([]GreyWaterDischargeEvent(nil), a.greyEvents...)
	a.greyEvents = nil
	return events
}

func (a *Adapter) CurrentState() domainheating.State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *Adapter) LightsState() domainlights.State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lightsState
}

func (a *Adapter) WaterState() domainwater.State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.waterState
}

func (a *Adapter) OverviewTelemetry() domainoverview.Telemetry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.overview
}

func (a *Adapter) Health() domainheating.AdapterHealth {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.health
}

func (a *Adapter) EnsureOn(ctx context.Context) error {
	return a.withHeatingCommand(func(client *rootheating.Client) error {
		return client.EnsureOn(ctx)
	})
}

func (a *Adapter) EnsureOff(ctx context.Context) error {
	return a.withHeatingCommand(func(client *rootheating.Client) error {
		return client.EnsureOff(ctx)
	})
}

func (a *Adapter) SetTargetTemperature(ctx context.Context, celsius float64) error {
	return a.withHeatingCommand(func(client *rootheating.Client) error {
		return client.SetTargetTemp(ctx, celsius)
	})
}

func (a *Adapter) EnsureExteriorOn(ctx context.Context) error {
	return a.ensureExteriorState(ctx, 47, true)
}

func (a *Adapter) EnsureExteriorOff(ctx context.Context) error {
	return a.ensureExteriorState(ctx, 48, true)
}

func (a *Adapter) OpenGreyWaterValve(ctx context.Context, hold time.Duration) error {
	return a.holdWaterButton(ctx, 4, hold)
}

func (a *Adapter) CloseGreyWaterValve(ctx context.Context, hold time.Duration) error {
	return a.holdWaterButton(ctx, 5, hold)
}

func (a *Adapter) withClient(fn func(*rootheating.Client) error) error {
	a.mu.RLock()
	client := a.client
	a.mu.RUnlock()
	if client == nil {
		if a.logger != nil {
			a.logger.Printf("garmin command rejected: adapter not connected")
		}
		return fmt.Errorf("garmin adapter not connected")
	}
	return fn(client)
}

func (a *Adapter) withHeatingCommand(fn func(*rootheating.Client) error) error {
	if err := a.withClient(fn); err != nil {
		a.mu.Lock()
		a.state.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command failed: %v", err)
		}
		return err
	}
	a.pollState()
	a.mu.Lock()
	a.state.LastCommandError = ""
	a.mu.Unlock()
	return nil
}

func (a *Adapter) ensureExteriorState(ctx context.Context, signal int, wantOn bool) error {
	a.mu.RLock()
	client := a.client
	session := a.session
	a.mu.RUnlock()
	if client == nil || session == nil {
		err := fmt.Errorf("garmin adapter not connected")
		a.mu.Lock()
		a.lightsState.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command rejected: adapter not connected")
		}
		return err
	}
	sendAt, err := client.SendSimpleCommandAt(ctx, signal, 3)
	if err != nil {
		a.mu.Lock()
		a.lightsState.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command failed: %v", err)
		}
		return err
	}
	if _, err := session.WaitForSignalIsOnAfter(ctx, signal, wantOn, sendAt); err != nil {
		a.mu.Lock()
		a.lightsState.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command failed: %v", err)
		}
		return err
	}
	a.pollState()
	a.mu.Lock()
	a.lightsState.LastCommandError = ""
	a.mu.Unlock()
	return nil
}

func (a *Adapter) holdWaterButton(ctx context.Context, signal int, hold time.Duration) error {
	a.mu.RLock()
	client := a.client
	session := a.session
	a.mu.RUnlock()
	if client == nil || session == nil {
		err := fmt.Errorf("garmin adapter not connected")
		a.mu.Lock()
		a.waterState.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command rejected: adapter not connected")
		}
		return err
	}
	pressedAt, err := client.HoldButton(ctx, signal, hold)
	if err != nil {
		a.mu.Lock()
		a.waterState.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command failed: %v", err)
		}
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := session.WaitForSignalIsOnAfter(waitCtx, signal, false, pressedAt); err != nil {
		a.mu.Lock()
		a.waterState.LastCommandError = err.Error()
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Printf("garmin command failed: %v", err)
		}
		return err
	}
	a.pollState()
	a.mu.Lock()
	a.waterState.LastCommandError = ""
	a.mu.Unlock()
	return nil
}

func (a *Adapter) closeSession() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closeSessionLocked()
}

func (a *Adapter) closeSessionLocked() {
	if a.session != nil {
		_ = a.session.Close()
	}
	a.session = nil
	a.client = nil
}

func snapshotFromRootState(state rootheating.HeaterState) domainheating.State {
	out := domainheating.State{
		PowerState:             mapPowerState(state.PowerState),
		Ready:                  state.Ready(),
		TargetTemperatureKnown: state.TargetTempKnown,
	}
	if state.TargetTempKnown {
		temp := state.TargetTempC
		out.TargetTemperatureC = &temp
	}
	if !state.LastUpdated.IsZero() {
		last := state.LastUpdated
		out.LastUpdatedAt = &last
	}
	return out
}

func mapPowerState(state rootheating.PowerState) domainheating.PowerState {
	switch state {
	case rootheating.PowerOff:
		return domainheating.PowerStateOff
	case rootheating.PowerOn:
		return domainheating.PowerStateOn
	case rootheating.PowerTransition:
		return domainheating.PowerStateTransition
	default:
		return domainheating.PowerStateUnknown
	}
}

func lightsSnapshotFromSession(session *rootheating.Session, current domainlights.State) domainlights.State {
	on, onKnown, onAt := session.SignalIsOn(47)
	off, offKnown, offAt := session.SignalIsOn(48)
	switch {
	case onKnown && on && (!offKnown || onAt.After(offAt)):
		current.ExternalKnown = true
		current.ExternalOn = true
		current.LastUpdatedAt = &onAt
	case offKnown && off && (!onKnown || offAt.After(onAt) || offAt.Equal(onAt)):
		current.ExternalKnown = true
		current.ExternalOn = false
		current.LastUpdatedAt = &offAt
	}
	return current
}

func waterSnapshotFromSession(session *rootheating.Session, current domainwater.State) domainwater.State {
	open, openKnown, openAt := session.SignalIsOn(4)
	close, closeKnown, closeAt := session.SignalIsOn(5)
	switch {
	case openKnown && open && (!closeKnown || openAt.After(closeAt)):
		current.ValveKnown = true
		current.ValveMoving = true
		current.ValveDirection = domainwater.ValveDirectionOpening
		current.LastUpdatedAt = stableTimePointer(current.LastUpdatedAt, openAt)
	case closeKnown && close && (!openKnown || closeAt.After(openAt)):
		current.ValveKnown = true
		current.ValveMoving = true
		current.ValveDirection = domainwater.ValveDirectionClosing
		current.LastUpdatedAt = stableTimePointer(current.LastUpdatedAt, closeAt)
	case openKnown && (!closeKnown || openAt.After(closeAt) || openAt.Equal(closeAt)):
		current.ValveKnown = true
		current.ValveMoving = false
		current.ValveDirection = domainwater.ValveDirectionNone
		current.LastUpdatedAt = stableTimePointer(current.LastUpdatedAt, openAt)
	case closeKnown:
		current.ValveKnown = true
		current.ValveMoving = false
		current.ValveDirection = domainwater.ValveDirectionNone
		current.LastUpdatedAt = stableTimePointer(current.LastUpdatedAt, closeAt)
	}
	return current
}

func stableTimePointer(current *time.Time, next time.Time) *time.Time {
	if current != nil && current.Equal(next) {
		return current
	}
	return &next
}

func overviewTelemetryFromSession(session *rootheating.Session) domainoverview.Telemetry {
	var telemetry domainoverview.Telemetry
	telemetry.AldeTemperatureC = overviewScalar(session, rootheating.SignalHeatingActualTemp, 22, func(raw int32) float64 {
		return float64(raw)/1000 - 273.15
	}, &telemetry.UpdatedAt)
	telemetry.FreshWaterPercent = overviewScalar(session, rootheating.SignalFreshWaterPercent, 14, milliUnits, &telemetry.UpdatedAt)
	telemetry.GreyWaterPercent = overviewScalar(session, rootheating.SignalGreyWaterPercent, 14, milliUnits, &telemetry.UpdatedAt)
	telemetry.BatteryCurrentA = overviewScalar(session, rootheating.SignalBatteryCurrentA, 6, milliUnits, &telemetry.UpdatedAt)
	telemetry.BatteryStateOfChargePercent = overviewScalar(session, rootheating.SignalBatteryStateOfChargePercent, 14, milliUnits, &telemetry.UpdatedAt)
	return telemetry
}

func overviewScalar(session *rootheating.Session, signal, valueType int, convert func(int32) float64, updatedAt **time.Time) *float64 {
	frame, at, ok := session.LatestReceivedSignal(signal)
	if !ok || frame.MessageType != 16 || frame.MessageCmd != 5 || len(frame.Data) < 8 || frame.Data[3] != valueType {
		return nil
	}
	raw, ok := signedInt32LittleEndian(frame.Data[4:8])
	if !ok {
		return nil
	}
	if *updatedAt == nil || at.After(**updatedAt) {
		atCopy := at
		*updatedAt = &atCopy
	}
	value := convert(raw)
	return &value
}

func signedInt32LittleEndian(data []int) (int32, bool) {
	if len(data) != 4 {
		return 0, false
	}
	var value uint32
	for index, item := range data {
		if item < 0 || item > 255 {
			return 0, false
		}
		value |= uint32(item) << (8 * index)
	}
	return int32(value), true
}

func milliUnits(raw int32) float64 {
	return float64(raw) / 1000
}
