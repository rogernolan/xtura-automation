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
)

type Config struct {
	WSURL             string
	Origin            string
	HeartbeatInterval time.Duration
	TraceWindow       time.Duration
	Logger            *log.Logger
}

type Adapter struct {
	cfg    Config
	logger *log.Logger

	mu          sync.RWMutex
	session     *rootheating.Session
	client      *rootheating.Client
	state       domainheating.State
	lightsState domainlights.State
	health      domainheating.AdapterHealth
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
	a.mu.RUnlock()
	lights := lightsSnapshotFromSession(session, currentLights)
	a.mu.Lock()
	a.state = snapshot
	a.lightsState = lights
	if !state.LastUpdated.IsZero() {
		last := state.LastUpdated
		a.health.LastFrameAt = &last
	}
	a.health.Connected = true
	a.health.LastError = ""
	a.mu.Unlock()
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
