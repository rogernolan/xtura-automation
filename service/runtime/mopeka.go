package runtime

import (
	"context"
	"math"
	"sync"
	"time"

	"empirebus-tests/service/adapters/btle"
	"empirebus-tests/service/domains/overview"
)

const mopekaStaleAfter = 5 * time.Minute

type mopekaState struct {
	mu          sync.Mutex
	distanceMm  float64
	batteryPct  float64
	tempC       float64
	quality     int
	lastSeen    time.Time
	hasReading  bool
	lastGas     overview.Gas
}

func (a *App) handleMopekaReading(reading btle.MopekaReading) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mopeka == nil {
		a.mopeka = &mopekaState{}
	}
	a.mopeka.mu.Lock()
	defer a.mopeka.mu.Unlock()
	a.mopeka.distanceMm = reading.DistanceMm
	a.mopeka.batteryPct = reading.BatteryPct
	a.mopeka.tempC = reading.TempC
	a.mopeka.quality = reading.Quality
	a.mopeka.lastSeen = a.now().UTC()
	a.mopeka.hasReading = true
}

func (a *App) overviewGas() overview.Gas {
	// Snapshot config first to avoid deadlock: always acquire a.mu before mopeka.mu.
	a.mu.RLock()
	cfg := a.rawConfig
	a.mu.RUnlock()

	if a.mopeka == nil {
		return overview.Gas{Status: "mopeka_not_configured"}
	}
	a.mopeka.mu.Lock()
	defer a.mopeka.mu.Unlock()

	if !a.mopeka.hasReading {
		return overview.Gas{Status: "mopeka_not_configured"}
	}

	if time.Since(a.mopeka.lastSeen) > mopekaStaleAfter {
		// Return last known values with stale status
		if a.mopeka.lastGas.UpdatedAt.IsZero() {
			return overview.Gas{Status: "stale"}
		}
		stale := a.mopeka.lastGas
		stale.Status = "stale"
		return stale
	}

	tankCapacity := cfg.Overview.GasTankCapacityLitres
	if tankCapacity <= 0 {
		tankCapacity = cfg.Mopeka.TankCapacityLitres
	}
	fillHeightMm := cfg.Mopeka.TankFillHeightMm

	if fillHeightMm == 0 {
		fillHeightMm = 290
	}

	// Distance is measured from the sensor (bottom of tank) to the liquid
	// surface. A smaller distance means more liquid. Empty = fillHeightMm,
	// full = 0mm.
	pct := (1.0 - a.mopeka.distanceMm/fillHeightMm) * 100.0
	pct = math.Max(0, math.Min(100, pct))

	litres := pct / 100.0 * tankCapacity

	status := "ok"

	gas := overview.Gas{
		Status:         status,
		LevelPercent:   &pct,
		LevelLitres:    &litres,
		CapacityLitres: &tankCapacity,
		BatteryPercent: &a.mopeka.batteryPct,
		TempC:          &a.mopeka.tempC,
		Quality:        &a.mopeka.quality,
		UpdatedAt:      a.mopeka.lastSeen,
	}
	// Cache for when we go stale
	a.mopeka.lastGas = gas
	return gas
}

func (a *App) startMopekaSim(ctx context.Context) {
	a.mu.RLock()
	cfg := a.rawConfig
	a.mu.RUnlock()
	if !simSwitchbotEnabled() || a.switchbot == nil || !cfg.Mopeka.Enabled {
		return
	}
	a.logger.Printf("mopeka simulation enabled: feeding synthetic readings")
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.mopekaSimTick()
			}
		}
	}()
}

func (a *App) mopekaSimTick() {
	a.mu.RLock()
	mac := a.rawConfig.Mopeka.MAC
	a.mu.RUnlock()
	now := a.nowUTC()
	// Simulate distance oscillating between 50mm and 200mm
	dist := 125.0 + 75.0*math.Sin(float64(now.Unix())/300.0)
	battery := 85.0
	voltage := 3.6
	a.switchbot.FeedMopeka(btle.MopekaReading{
		DistanceMm: dist,
		BatteryPct: battery,
		BatteryV:   voltage,
		TempC:      22.0,
		Quality:    3,
		MAC:        mac,
	})
}
