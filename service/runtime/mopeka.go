package runtime

import (
	"context"
	"math"
	"sync"
	"time"

	"empirebus-tests/service/adapters/btle"
	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/overview"
)

const (
	// mopekaStaleAfter is how long a Mopeka reading is considered fresh. The
	// Pro Check advertises continuously, so anything older than this means
	// the radio has stopped hearing the sensor.
	mopekaStaleAfter = 5 * time.Minute
	// mopekaLogInterval rate-limits the journal line. Mopeka advertises
	// roughly once a second, so logging every frame would bury the journal.
	mopekaLogInterval = 5 * time.Minute
	// mopekaQualityMinimum is the lowest ultrasonic quality that represents a
	// real liquid-surface echo. Quality 0 means the ping came straight back
	// off the tank floor, i.e. there is no liquid surface to measure.
	mopekaQualityMinimum = 1
)

type mopekaState struct {
	mu         sync.Mutex
	distanceMm float64
	batteryPct float64
	tempC      float64
	quality    int
	lastSeen   time.Time
	hasReading bool
	lastGas    overview.Gas
	lastLogged time.Time
}

func (a *App) handleMopekaReading(reading btle.MopekaReading) {
	a.mu.Lock()
	if a.mopeka == nil {
		a.mopeka = &mopekaState{}
	}
	now := a.now().UTC()
	logger := a.logger
	a.mopeka.mu.Lock()
	first := !a.mopeka.hasReading
	a.mopeka.distanceMm = reading.DistanceMm
	a.mopeka.batteryPct = reading.BatteryPct
	a.mopeka.tempC = reading.TempC
	a.mopeka.quality = reading.Quality
	a.mopeka.lastSeen = now
	a.mopeka.hasReading = true
	// MopekaReading.String() is the only thing that ever surfaces a raw
	// frame, so keep it on a timer rather than logging every advertisement.
	shouldLog := first || now.Sub(a.mopeka.lastLogged) >= mopekaLogInterval
	if shouldLog {
		a.mopeka.lastLogged = now
	}
	a.mopeka.mu.Unlock()
	a.mu.Unlock()

	if shouldLog && logger != nil {
		logger.Printf("mopeka reading: %s", reading.String())
	}
}

// overviewGas renders the Mopeka sensor as a tank fill percentage.
//
// The Pro Check is a bottom-mounted ultrasonic sensor firing upward, so
// DistanceMm is the depth of liquid standing above the sensor face: a full
// tank reads close to the tank's internal liquid height, an empty tank reads
// close to zero because the ping comes straight back off the floor. Fill is
// therefore distance/fillHeight, NOT its complement. The previous
// 1-distance/fillHeight reported the empty fraction instead, which agreed
// with reality only at exactly 0%, 50% and 100%.
func (a *App) overviewGas() overview.Gas {
	// Always acquire a.mu before mopeka.mu: handleMopekaReading nests them in
	// that order. Snapshot everything needed from a.mu in one pass, and read
	// the a.mopeka pointer under the lock since handleMopekaReading may be
	// publishing it for the first time concurrently.
	a.mu.RLock()
	raw := a.rawConfig
	normalized := a.cfg
	mopeka := a.mopeka
	now := time.Now().UTC()
	if a.now != nil {
		now = a.now().UTC()
	}
	a.mu.RUnlock()

	if !normalized.Mopeka.Enabled && !raw.Mopeka.Enabled {
		return overview.Gas{Status: overview.GasStatusNotConfigured}
	}
	if mopeka == nil {
		return overview.Gas{Status: overview.GasStatusNoData}
	}

	mopeka.mu.Lock()
	defer mopeka.mu.Unlock()

	if !mopeka.hasReading {
		// Configured, but not a single frame has ever been decoded. That is a
		// radio problem (wrong MAC, dead HCI socket, missing capabilities) and
		// must not masquerade as "not configured".
		return overview.Gas{Status: overview.GasStatusNoData}
	}

	age := now.Sub(mopeka.lastSeen)
	if age > mopekaStaleAfter {
		// Keep the last known values so the UI can still show what it last
		// saw, but flag the age so nothing presents it as live.
		stale := mopeka.lastGas
		stale.Status = overview.GasStatusStale
		seconds := int64(age / time.Second)
		stale.AgeSeconds = &seconds
		return stale
	}

	tankCapacity := raw.Overview.GasTankCapacityLitres
	if tankCapacity <= 0 {
		tankCapacity = raw.Mopeka.TankCapacityLitres
	}
	fillHeightMm := normalized.Mopeka.TankFillHeightMm
	if fillHeightMm <= 0 {
		fillHeightMm = raw.Mopeka.TankFillHeightMm
	}
	if fillHeightMm <= 0 {
		fillHeightMm = config.DefaultMopekaFillHeightMm
	}
	baseRadiusMm := config.MopekaBaseRadiusOrDefault(normalized.Mopeka)
	if raw.Mopeka.TankBaseRadiusMm != nil {
		baseRadiusMm = *raw.Mopeka.TankBaseRadiusMm
	}

	seconds := int64(age / time.Second)
	gas := overview.Gas{
		Status:         overview.GasStatusOK,
		BatteryPercent: &mopeka.batteryPct,
		TempC:          &mopeka.tempC,
		Quality:        &mopeka.quality,
		UpdatedAt:      mopeka.lastSeen,
		AgeSeconds:     &seconds,
		CapacityLitres: &tankCapacity,
	}

	// Quality 0 is the ultrasonic ping returning off the tank body with no
	// liquid in the way. There is no surface to measure, so there is no level
	// to report; publishing one would render "signal lost" as a real reading.
	if mopeka.quality < mopekaQualityMinimum {
		gas.Status = overview.GasStatusBadQuality
		mopeka.lastGas = gas
		return gas
	}

	pct := tankFillPercent(mopeka.distanceMm, baseRadiusMm, fillHeightMm)
	if pct > 100 {
		// More liquid standing than the tank can physically hold means
		// tank_fill_height_mm is miscalibrated. Say so rather than silently
		// pinning the bar at 100% forever.
		gas.Status = overview.GasStatusOutOfRange
	}
	pct = math.Max(0, math.Min(100, pct))
	litres := pct / 100.0 * tankCapacity
	gas.LevelPercent = &pct
	gas.LevelLitres = &litres

	// Cache for when we go stale.
	mopeka.lastGas = gas
	return gas
}

// tankVolumeBelowMm returns the volume of liquid, in cubic millimetres,
// standing below levelMm in a tank whose interior is a hemispherical base of
// baseRadiusMm topped by a straight cylinder of the same radius.
//
// A level at or below baseRadiusMm is still up inside the dome, where the tank
// is far narrower than the full bore, and the cross-section grows with the
// square of the level. Treating that region as linear overstates it badly:
// 146mm of liquid in a 150mm dome of R=150mm holds 6.8L, not the 10.3L that a
// straight cylinder of the same height and radius would.
//
// A baseRadiusMm of 0 means a straight-walled tank, where volume is simply
// proportional to height.
func tankVolumeBelowMm(levelMm, baseRadiusMm float64) float64 {
	if baseRadiusMm <= 0 {
		return levelMm
	}
	if levelMm <= baseRadiusMm {
		// Spherical cap of height levelMm cut from the bottom of a sphere of
		// radius baseRadiusMm: pi*h^2*(R - h/3). The -h/3 term is exactly the
		// part a linear approximation gets wrong.
		return math.Pi * levelMm * levelMm * (baseRadiusMm - levelMm/3)
	}
	hemi := 2.0 / 3.0 * math.Pi * math.Pow(baseRadiusMm, 3)
	return hemi + math.Pi*baseRadiusMm*baseRadiusMm*(levelMm-baseRadiusMm)
}

// tankFillPercent converts a liquid level into a fill percentage by volume,
// which is what the Mopeka app reports and what a level gauge is expected to
// mean. A dome-shaped tank and a straight one disagree by up to 10 points at
// the same level, so the geometry has to be modelled rather than assumed away.
func tankFillPercent(levelMm, baseRadiusMm, totalHeightMm float64) float64 {
	if totalHeightMm <= 0 {
		return 0
	}
	return tankVolumeBelowMm(levelMm, baseRadiusMm) / tankVolumeBelowMm(totalHeightMm, baseRadiusMm) * 100
}

func (a *App) startMopekaSim(ctx context.Context) {
	a.mu.RLock()
	cfg := a.rawConfig
	a.mu.RUnlock()
	if !simSwitchbotEnabled() || a.switchbot == nil || !cfg.Mopeka.Enabled {
		return
	}
	a.logger.Printf("mopeka simulation enabled: feeding synthetic readings")
	goSafe(a.logger, "mopeka_sim_tick", func() {
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
	})
}

func (a *App) mopekaSimTick() {
	a.mu.RLock()
	mac := a.rawConfig.Mopeka.MAC
	a.mu.RUnlock()
	now := a.nowUTC()
	// Simulate liquid depth between 50mm and 200mm in a 290mm tank.
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
