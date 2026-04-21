package garmin

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	rootheating "empirebus-tests/heating"
	domainheating "empirebus-tests/service/domains/heating"
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

	mu      sync.RWMutex
	session *rootheating.Session
	client  *rootheating.Client
	state   domainheating.State
	health  domainheating.AdapterHealth
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
		return
	}
	a.mu.Lock()
	a.closeSessionLocked()
	a.session = session
	a.client = rootheating.NewClient(session)
	a.health.Connected = true
	a.health.LastError = ""
	a.mu.Unlock()
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
		return
	}
	state := session.State()
	snapshot := snapshotFromRootState(state)
	a.mu.Lock()
	a.state = snapshot
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

func (a *Adapter) Health() domainheating.AdapterHealth {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.health
}

func (a *Adapter) EnsureOn(ctx context.Context) error {
	return a.withClient(func(client *rootheating.Client) error {
		return client.EnsureOn(ctx)
	})
}

func (a *Adapter) EnsureOff(ctx context.Context) error {
	return a.withClient(func(client *rootheating.Client) error {
		return client.EnsureOff(ctx)
	})
}

func (a *Adapter) SetTargetTemperature(ctx context.Context, celsius float64) error {
	return a.withClient(func(client *rootheating.Client) error {
		return client.SetTargetTemp(ctx, celsius)
	})
}

func (a *Adapter) withClient(fn func(*rootheating.Client) error) error {
	a.mu.RLock()
	client := a.client
	a.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("garmin adapter not connected")
	}
	if err := fn(client); err != nil {
		a.mu.Lock()
		a.state.LastCommandError = err.Error()
		a.mu.Unlock()
		return err
	}
	a.pollState()
	a.mu.Lock()
	a.state.LastCommandError = ""
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
