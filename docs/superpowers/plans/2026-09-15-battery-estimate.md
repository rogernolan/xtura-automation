# Battery Charge/Discharge Estimate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add estimated time remaining for charge/discharge to the overview page battery card, with smoothed current and proper handling of idle, near-full, and stale states.

**Architecture:** A new `battery_estimate.go` module in `service/runtime/` computes a `BatteryEstimate` from telemetry + config. It owns an EWMA current smoother (5-minute effective window) that is fed once per second from the existing publish loop. The `overviewDocument()` method calls the estimator instead of doing inline math. The frontend renders the new fields from the existing `overview.Battery` JSON, which gains `power_w`, `mode`, `charge_state`, and `eta_seconds` fields.

**Tech Stack:** Go 1.25, vanilla JS, standard `testing` package.

## Global Constraints

- Positive current = charging, negative = discharging (normalise once in the estimator if Garmin uses opposite)
- Idle deadband: ±2A (named constant `idleDeadbandA`)
- Battery capacity: 660Ah, nominal voltage: 12.8V, floor SOC: 20%, ready SOC: 95%, charge efficiency: 0.99
- EWMA smoothing half-life: ~5 minutes (alpha recalculated per tick at 1s intervals)
- Do not introduce a second Cerbo polling mechanism
- Do not derive power from V×I if authoritative value exists (it doesn't — compute from V×I using nominal voltage)
- No ETA when SOC ≤ floor, current in deadband, or inputs stale/unavailable
- Duration format: `< 1 minute`, `37m`, `1h 12m`, `6h 05m`, `1d 3h` — no seconds
- Config values live in `OverviewConfig`, not scattered constants

---

## File Map

| Action | File | Purpose |
|--------|------|---------|
| Create | `service/runtime/battery_estimate.go` | Estimate logic, EWMA smoother, duration formatting |
| Create | `service/runtime/battery_estimate_test.go` | Unit tests for estimate + formatting |
| Modify | `service/config/config.go:39-43` | Add battery config fields to `OverviewConfig` |
| Modify | `service/config/config.go:476-489` | Add defaults in `normalizeOverview` |
| Modify | `service/config/config.go:325-331` | Add validation for new fields |
| Modify | `service/domains/overview/types.go:22-28` | Extend `Battery` struct with new JSON fields |
| Modify | `service/runtime/app.go:81-128` | Add `batteryEstimate` field to `App` |
| Modify | `service/runtime/overview.go:14-68` | Replace inline ETA with estimator call; preserve battery fields in settings update |
| Modify | `service/runtime/overview_test.go` | Update existing tests, add new ones |
| Modify | `web/static/index.html:44-56` | Add detail text element for ETA |
| Modify | `web/static/app.js:780-790` | Render new battery fields |
| Modify | `config.example.yaml:51-55` | Add new config keys with comments |

---

### Task 1: Add battery config fields to OverviewConfig

**Files:**
- Modify: `service/config/config.go:39-43` (OverviewConfig struct)
- Modify: `service/config/config.go:476-489` (normalizeOverview)
- Modify: `service/config/config.go:325-331` (validation)
- Modify: `config.example.yaml:51-55`

**Interfaces:**
- Produces: `OverviewConfig.BatteryCapacityAh`, `BatteryNominalVoltage`, `BatteryFloorSOC`, `BatteryReadySOC`, `MultiplusMaxChargeCurrentA`, `ChargeEfficiency` fields

- [ ] **Step 1: Add fields to OverviewConfig struct**

In `service/config/config.go`, extend the `OverviewConfig` struct (line 39) to add:

```go
type OverviewConfig struct {
	UsableBatteryCapacityAh         float64   `yaml:"usable_battery_capacity_ah,omitempty"`
	GasTankCapacityLitres           float64   `yaml:"gas_tank_capacity_litres,omitempty"`
	Comfort                         []float64 `yaml:"comfort_thresholds,omitempty"`
	BatteryCapacityAh               float64   `yaml:"battery_capacity_ah,omitempty"`
	BatteryNominalVoltage           float64   `yaml:"battery_nominal_voltage,omitempty"`
	BatteryFloorSOC                 float64   `yaml:"battery_floor_soc,omitempty"`
	BatteryReadySOC                 float64   `yaml:"battery_ready_soc,omitempty"`
	MultiplusMaxChargeCurrentA      float64   `yaml:"multiplus_max_charge_current_a,omitempty"`
	ChargeEfficiency                float64   `yaml:"charge_efficiency,omitempty"`
}
```

- [ ] **Step 2: Add defaults in normalizeOverview**

In `service/config/config.go`, extend `normalizeOverview` (line 476) to set defaults after the existing `Comfort` default block:

```go
if out.BatteryCapacityAh == 0 {
	out.BatteryCapacityAh = 660
}
if out.BatteryNominalVoltage == 0 {
	out.BatteryNominalVoltage = 12.8
}
if out.BatteryFloorSOC == 0 {
	out.BatteryFloorSOC = 20
}
if out.BatteryReadySOC == 0 {
	out.BatteryReadySOC = 95
}
if out.MultiplusMaxChargeCurrentA == 0 {
	out.MultiplusMaxChargeCurrentA = 120
}
if out.ChargeEfficiency == 0 {
	out.ChargeEfficiency = 0.99
}
```

- [ ] **Step 3: Add validation**

In `service/config/config.go`, after the existing overview validation block (around line 331), add:

```go
if c.Overview.BatteryCapacityAh < 0 {
	problems = append(problems, "overview.battery_capacity_ah must not be negative")
}
if c.Overview.BatteryNominalVoltage < 0 {
	problems = append(problems, "overview.battery_nominal_voltage must not be negative")
}
if c.Overview.BatteryFloorSOC < 0 || c.Overview.BatteryFloorSOC > 100 {
	problems = append(problems, "overview.battery_floor_soc must be between 0 and 100")
}
if c.Overview.BatteryReadySOC < 0 || c.Overview.BatteryReadySOC > 100 {
	problems = append(problems, "overview.battery_ready_soc must be between 0 and 100")
}
if c.Overview.ChargeEfficiency < 0 || c.Overview.ChargeEfficiency > 1 {
	problems = append(problems, "overview.charge_efficiency must be between 0 and 1")
}
```

- [ ] **Step 4: Update config.example.yaml**

In `config.example.yaml`, extend the overview section:

```yaml
overview:
  usable_battery_capacity_ah: 100
  gas_tank_capacity_litres: 0
  comfort_thresholds: [10, 18, 24, 30]
  # Victron battery bank configuration
  battery_capacity_ah: 660
  battery_nominal_voltage: 12.8
  battery_floor_soc: 20
  battery_ready_soc: 95
  multiplus_max_charge_current_a: 120
  charge_efficiency: 0.99
```

- [ ] **Step 5: Run tests to verify no regressions**

Run: `go test ./service/config/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add service/config/config.go config.example.yaml
git commit -m "feat: add battery estimate config fields to OverviewConfig"
```

---

### Task 2: Create battery estimate module

**Files:**
- Create: `service/runtime/battery_estimate.go`

**Interfaces:**
- Produces: `BatteryEstimate` struct, `ComputeBatteryEstimate()` function, `BatteryEstimateSmoothing` EWMA tracker, `FormatBatteryDuration()` function

- [ ] **Step 1: Create the battery estimate file**

Create `service/runtime/battery_estimate.go`:

```go
package runtime

import (
	"fmt"
	"math"
	"time"
)

const (
	idleDeadbandA             = 2.0
	smoothingHalfLifeDuration = 5 * time.Minute
)

// BatteryMode represents the high-level battery state.
type BatteryMode string

const (
	BatteryModeCharging   BatteryMode = "charging"
	BatteryModeDischarging BatteryMode = "discharging"
	BatteryModeIdle       BatteryMode = "idle"
	BatteryModeUnknown    BatteryMode = "unknown"
)

// BatteryChargeState maps to MultiPlus charge phases when available.
type BatteryChargeState string

const (
	BatteryChargeStateBulk       BatteryChargeState = "bulk"
	BatteryChargeStateAbsorption BatteryChargeState = "absorption"
	BatteryChargeStateFloat      BatteryChargeState = "float"
	BatteryChargeStateStorage    BatteryChargeState = "storage"
	BatteryChargeStateUnknown    BatteryChargeState = ""
)

// BatteryConfig holds the configuration needed for battery estimates.
type BatteryConfig struct {
	CapacityAh          float64
	NominalVoltage      float64
	FloorSOC            float64
	ReadySOC            float64
	MaxChargeCurrentA   float64
	ChargeEfficiency    float64
}

// BatteryEstimate is the result of a battery estimate computation.
type BatteryEstimate struct {
	Mode            BatteryMode
	SOC             float64 // percent
	CurrentA        float64 // smoothed, positive = charging
	PowerW          float64 // computed from current × nominal voltage
	ChargeState     BatteryChargeState
	TargetSOC       float64 // the SOC target for ETA calculation
	EstimatedSeconds float64 // ETA in seconds, 0 = no estimate
	Available       bool    // false if inputs are stale or missing
}

// BatteryEstimateSmoothing maintains an EWMA of battery current.
type BatteryEstimateSmoothing struct {
	smoothed    float64
	initialized bool
	alpha       float64 // computed from half-life and tick interval
}

// NewBatteryEstimateSmoothing creates a smoother with the given half-life,
// assuming updates arrive at the given tick interval.
func NewBatteryEstimateSmoothing(halfLife time.Duration, tickInterval time.Duration) *BatteryEstimateSmoothing {
	// EWMA alpha = 1 - 2^(-tickInterval/halfLife)
	alpha := 1.0 - math.Pow(2, -float64(tickInterval)/float64(halfLife))
	return &BatteryEstimateSmoothing{alpha: alpha}
}

// Update incorporates a new current reading. Returns the smoothed value.
func (s *BatteryEstimateSmoothing) Update(current float64) float64 {
	if !s.initialized {
		s.smoothed = current
		s.initialized = true
		return s.smoothed
	}
	s.smoothed = s.alpha*current + (1-s.alpha)*s.smoothed
	return s.smoothed
}

// Smoothed returns the current smoothed value without updating.
func (s *BatteryEstimateSmoothing) Smoothed() float64 {
	return s.smoothed
}

// Reset clears the smoothing state.
func (s *BatteryEstimateSmoothing) Reset() {
	s.smoothed = 0
	s.initialized = false
}

// ComputeBatteryEstimate calculates the battery estimate from raw telemetry.
// A nil smoother disables smoothing and uses the raw current directly (used by
// tests and when the App has not been fully wired). The smoother is only fed
// when both SOC and current are present, so missing telemetry never pollutes
// the running average.
func ComputeBatteryEstimate(
	soc *float64,
	rawCurrent *float64,
	smoother *BatteryEstimateSmoothing,
	cfg BatteryConfig,
) BatteryEstimate {
	if soc == nil || rawCurrent == nil {
		return BatteryEstimate{Available: false}
	}

	socVal := *soc
	currentRaw := *rawCurrent

	// Update the EWMA smoother when one is available
	smoothedCurrent := currentRaw
	if smoother != nil {
		smoothedCurrent = smoother.Update(currentRaw)
	}

	// Compute power from smoothed current × nominal voltage
	powerW := smoothedCurrent * cfg.NominalVoltage

	// Determine mode using the idle deadband on the smoothed current
	var mode BatteryMode
	switch {
	case smoothedCurrent > idleDeadbandA:
		mode = BatteryModeCharging
	case smoothedCurrent < -idleDeadbandA:
		mode = BatteryModeDischarging
	default:
		mode = BatteryModeIdle
	}

	est := BatteryEstimate{
		Mode:     mode,
		SOC:      socVal,
		CurrentA: smoothedCurrent,
		PowerW:   powerW,
		Available: true,
	}

	// Compute ETA
	switch mode {
	case BatteryModeCharging:
		// Don't compute linear ETA at or above the ready SOC (top-of-charge region)
		if socVal >= cfg.ReadySOC {
			est.TargetSOC = cfg.ReadySOC
			return est
		}
		est.TargetSOC = cfg.ReadySOC
		requiredAh := cfg.CapacityAh * (cfg.ReadySOC - socVal) / 100.0
		effectiveCurrent := smoothedCurrent * cfg.ChargeEfficiency
		if effectiveCurrent > 0 {
			hours := requiredAh / effectiveCurrent
			est.EstimatedSeconds = hours * 3600.0
		}

	case BatteryModeDischarging:
		// Don't compute ETA if already at or below floor
		if socVal <= cfg.FloorSOC {
			return est
		}
		usableRemainingAh := cfg.CapacityAh * (socVal - cfg.FloorSOC) / 100.0
	 dischargeCurrent := math.Abs(smoothedCurrent)
		if dischargeCurrent > 0 {
			hours := usableRemainingAh / dischargeCurrent
			est.EstimatedSeconds = hours * 3600.0
		}
	}

	return est
}

// FormatBatteryDuration formats a duration for display.
// Examples: "< 1 minute", "37m", "1h 12m", "6h 05m", "1d 3h"
func FormatBatteryDuration(seconds float64) string {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return ""
	}
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return "< 1 minute"
	}
	totalMinutes := int(d.Minutes())
	if totalMinutes < 60 {
		return fmt.Sprintf("%dm", totalMinutes)
	}
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	days := hours / 24
	remainingHours := hours % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, remainingHours)
	}
	return fmt.Sprintf("%dh %02dm", hours, minutes)
}
```

- [ ] **Step 2: Run tests to verify compilation**

Run: `go build ./service/runtime/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add service/runtime/battery_estimate.go
git commit -m "feat: add battery estimate calculation module with EWMA smoothing"
```

---

### Task 3: Extend Battery struct and wire estimator into App

**Files:**
- Modify: `service/domains/overview/types.go:22-28`
- Modify: `service/runtime/app.go:81-128`
- Modify: `service/runtime/overview.go:14-68`

**Interfaces:**
- Consumes: `BatteryEstimate` from Task 2, `BatteryEstimateSmoothing` from Task 2
- Produces: Extended `overview.Battery` JSON, `batteryEstimate` field on `App`

- [ ] **Step 1: Extend the Battery struct**

In `service/domains/overview/types.go`, replace the `Battery` struct (line 22):

```go
type Battery struct {
	StateOfChargePercent *float64   `json:"state_of_charge_percent,omitempty"`
	CurrentA             *float64   `json:"current_a,omitempty"`
	PowerW               *float64   `json:"power_w,omitempty"`
	Mode                 string     `json:"mode"`
	ChargeState          string     `json:"charge_state,omitempty"`
	ETASeconds           *float64   `json:"eta_seconds,omitempty"`
	TargetSOC            *float64   `json:"target_soc,omitempty"`
	Status               string     `json:"status"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}
```

- [ ] **Step 2: Add batteryEstimate to App struct**

In `service/runtime/app.go`, add a field to the `App` struct (after line 107):

```go
batteryEstimate *BatteryEstimateSmoothing
```

- [ ] **Step 3: Initialise the smoother in app construction**

In `service/runtime/app.go`, the `App` struct literal is constructed at line 263 (`app := &App{`), spanning lines 263-292. Add `batteryEstimate` to that literal:

```go
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
		host:                  hostManager,
		tracking:              trackingManager,
		overviewTelemetry:     overviewTelemetry,
		greyWaterDischarge:    greyWaterDischarge,
		now:                   time.Now,
		sensorsStore:          sensorStore,
		waterHistory:          waterStore,
		batteryEstimate:       NewBatteryEstimateSmoothing(smoothingHalfLifeDuration, time.Second),
		sensorStates:          make(map[string]*sensorState),
		lastHistory:           make(map[string]sensorStamp),
		sensorSettings:        cfg.Switchbot,
		notificationEvaluator: notifications.NewEvaluator(notifications.Settings{Alerts: cfg.Notifications.Alerts}),
		notificationSender:    notifications.NewSender(notifications.PushConfig{PublicKey: cfg.Notifications.VAPIDPublicKey, PrivateKey: cfg.Notifications.VAPIDPrivateKey, Subject: cfg.Notifications.Subject}, nil),
		schedulerWake:         make(chan struct{}, 1),
		waterSchedulerWake:    make(chan struct{}, 1),
	}
```

The smoother is created once with a 5-minute half-life and 1-second tick. Note that the estimator already tolerates a nil smoother, so existing tests that build `&App{...}` without `batteryEstimate` keep working (they exercise the raw-current path).

- [ ] **Step 4: Replace inline ETA logic in overviewDocument**

In `service/runtime/overview.go`, the `overview.Document{...}` literal at line 41 currently sets `Battery: overview.Battery{StateOfChargePercent: ..., CurrentA: ..., Status: "unavailable", UpdatedAt: ...}` inline. **Remove that `Battery:` field from the Document literal** (it is now built by the estimator below), so the literal inside `overviewDocument` becomes:

```go
	doc := overview.Document{
		Status:            status,
		AldeTemperatureC:  telemetry.AldeTemperatureC,
		FreshWaterPercent: telemetry.FreshWaterPercent,
		GreyWaterPercent:  telemetry.GreyWaterPercent,
		UpdatedAt:         telemetry.UpdatedAt,
		Gas:               a.overviewGas(),
		Temperature:       a.temperatureDocument(telemetry),
	}
```

Then replace the inline battery block (the `if telemetry.BatteryCurrentA != nil { ... }` block, currently lines 51-63) with:

```go
estCfg := BatteryConfig{
	CapacityAh:        settings.BatteryCapacityAh,
	NominalVoltage:    settings.BatteryNominalVoltage,
	FloorSOC:          settings.BatteryFloorSOC,
	ReadySOC:          settings.BatteryReadySOC,
	MaxChargeCurrentA: settings.MultiplusMaxChargeCurrentA,
	ChargeEfficiency:  settings.ChargeEfficiency,
}
est := ComputeBatteryEstimate(
	telemetry.BatteryStateOfChargePercent,
	telemetry.BatteryCurrentA,
	a.batteryEstimate,
	estCfg,
)

doc.Battery = overview.Battery{
	StateOfChargePercent: telemetry.BatteryStateOfChargePercent,
	CurrentA:            telemetry.BatteryCurrentA,
	UpdatedAt:           telemetry.UpdatedAt,
}

if !est.Available {
	doc.Battery.Status = "unavailable"
} else {
	doc.Battery.Status = string(est.Mode)
	doc.Battery.Mode = string(est.Mode)
	powerW := est.PowerW
	doc.Battery.PowerW = &powerW

	switch est.Mode {
	case BatteryModeCharging:
		if est.SOC >= estCfg.ReadySOC {
			doc.Battery.ChargeState = "topping_off"
		} else if est.EstimatedSeconds > 0 {
			doc.Battery.ETASeconds = &est.EstimatedSeconds
			targetSOC := est.TargetSOC
			doc.Battery.TargetSOC = &targetSOC
		}
	case BatteryModeDischarging:
		if est.EstimatedSeconds > 0 {
			doc.Battery.ETASeconds = &est.EstimatedSeconds
			targetSOC := estCfg.FloorSOC
			doc.Battery.TargetSOC = &targetSOC
		}
	}
}
```

- [ ] **Step 5: Preserve battery config fields in UpdateOverviewSettings**

`UpdateOverviewSettings` (in `service/runtime/overview.go`, around line 111) currently replaces `next.Overview` with a brand-new `config.OverviewConfig` built from only the three legacy settings fields. After this feature that would silently wipe the battery config fields on every settings save. Fix it by starting from the existing `next.Overview` and overriding only the three legacy fields:

```go
	existing := next.Overview
	next.Overview = config.OverviewConfig{
		UsableBatteryCapacityAh: settings.UsableBatteryCapacityAh,
		GasTankCapacityLitres:   settings.GasTankCapacityLitres,
		Comfort:                 append([]float64(nil), settings.Comfort...),
		BatteryCapacityAh:       existing.BatteryCapacityAh,
		BatteryNominalVoltage:   existing.BatteryNominalVoltage,
		BatteryFloorSOC:         existing.BatteryFloorSOC,
		BatteryReadySOC:         existing.BatteryReadySOC,
		MultiplusMaxChargeCurrentA: existing.MultiplusMaxChargeCurrentA,
		ChargeEfficiency:        existing.ChargeEfficiency,
	}
```

- [ ] **Step 6: Run tests to verify compilation**

Run: `go build ./service/runtime/...` and `go build ./service/domains/overview/...`
Expected: PASS

Note: after this task the *test* package fails to compile because `overview_test.go` still references the removed `ETAHours` field. That is fixed in Task 5; do not run `go test` yet.

- [ ] **Step 7: Commit**

```bash
git add service/domains/overview/types.go service/runtime/app.go service/runtime/overview.go
git commit -m "feat: wire battery estimate into overview document"
```

---

### Task 4: Add battery estimate tests

**Files:**
- Create: `service/runtime/battery_estimate_test.go`

**Interfaces:**
- Consumes: `ComputeBatteryEstimate`, `FormatBatteryDuration`, `BatteryEstimateSmoothing` from Task 2

- [ ] **Step 1: Create battery_estimate_test.go**

Create `service/runtime/battery_estimate_test.go` with the following test cases:

```go
package runtime

import (
	"math"
	"testing"
	"time"
)

var testBatteryCfg = BatteryConfig{
	CapacityAh:        660,
	NominalVoltage:    12.8,
	FloorSOC:          20,
	ReadySOC:          95,
	MaxChargeCurrentA: 120,
	ChargeEfficiency:  0.99,
}

func newTestSmoother() *BatteryEstimateSmoothing {
	// Use a very high alpha (fast tracking) for deterministic tests
	return &BatteryEstimateSmoothing{alpha: 1.0, initialized: false}
}

func TestBatteryEstimateDischargingToFloor(t *testing.T) {
	soc := 80.0
	current := -30.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, &current, smoother, testBatteryCfg)

	if est.Mode != BatteryModeDischarging {
		t.Fatalf("expected discharging mode, got %q", est.Mode)
	}
	// usableRemaining = 660 * (80 - 20) / 100 = 396Ah
	// hours = 396 / 30 = 13.2h = 47520s
	if est.EstimatedSeconds < 47500 || est.EstimatedSeconds > 47540 {
		t.Fatalf("expected ~47520s ETA, got %v", est.EstimatedSeconds)
	}
	if est.TargetSOC != 20 {
		t.Fatalf("expected target SOC 20, got %v", est.TargetSOC)
	}
}

func TestBatteryEstimateChargingToReady(t *testing.T) {
	soc := 50.0
	current := 100.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, &current, smoother, testBatteryCfg)

	if est.Mode != BatteryModeCharging {
		t.Fatalf("expected charging mode, got %q", est.Mode)
	}
	// requiredAh = 660 * (95 - 50) / 100 = 297Ah
	// effectiveCurrent = 100 * 0.99 = 99A
	// hours = 297 / 99 = 3.0h = 10800s
	if est.EstimatedSeconds < 10790 || est.EstimatedSeconds > 10810 {
		t.Fatalf("expected ~10800s ETA, got %v", est.EstimatedSeconds)
	}
}

func TestBatteryEstimateNearFullNoLinearETA(t *testing.T) {
	soc := 97.0
	current := 5.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, &current, smoother, testBatteryCfg)

	if est.Mode != BatteryModeCharging {
		t.Fatalf("expected charging mode, got %q", est.Mode)
	}
	if est.EstimatedSeconds != 0 {
		t.Fatalf("expected no linear ETA above ready SOC, got %v", est.EstimatedSeconds)
	}
}

func TestBatteryEstimateIdleWithinDeadband(t *testing.T) {
	soc := 68.0
	current := -1.2
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, &current, smoother, testBatteryCfg)

	if est.Mode != BatteryModeIdle {
		t.Fatalf("expected idle mode, got %q", est.Mode)
	}
	if est.EstimatedSeconds != 0 {
		t.Fatalf("expected no ETA when idle, got %v", est.EstimatedSeconds)
	}
}

func TestBatteryEstimateBelowFloorSOC(t *testing.T) {
	soc := 15.0
	current := -30.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, &current, smoother, testBatteryCfg)

	if est.Mode != BatteryModeDischarging {
		t.Fatalf("expected discharging mode, got %q", est.Mode)
	}
	if est.EstimatedSeconds != 0 {
		t.Fatalf("expected no ETA when below floor, got %v", est.EstimatedSeconds)
	}
}

func TestBatteryEstimateMissingSOC(t *testing.T) {
	current := -30.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(nil, &current, smoother, testBatteryCfg)

	if est.Available {
		t.Fatalf("expected unavailable when SOC is nil")
	}
}

func TestBatteryEstimateMissingCurrent(t *testing.T) {
	soc := 68.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, nil, smoother, testBatteryCfg)

	if est.Available {
		t.Fatalf("expected unavailable when current is nil")
	}
}

func TestBatteryEstimateSmoothingDampensSpike(t *testing.T) {
	smoother := NewBatteryEstimateSmoothing(5*time.Minute, time.Second)

	// Establish a baseline of -30A discharge
	for i := 0; i < 60; i++ {
		smoother.Update(-30.0)
	}
	baseline := smoother.Smoothed()
	if baseline > -29 || baseline < -31 {
		t.Fatalf("expected baseline near -30A, got %v", baseline)
	}

	// Spike to -200A for 5 seconds (e.g. water pump starting)
	for i := 0; i < 5; i++ {
		smoother.Update(-200.0)
	}
	afterSpike := smoother.Smoothed()
	// The smoothed value should not jump dramatically
	if afterSpike < -80 {
		t.Fatalf("smoothing should dampen spike, got %v (expected > -80)", afterSpike)
	}

	// Return to -30A, should recover within ~30 seconds
	for i := 0; i < 30; i++ {
		smoother.Update(-30.0)
	}
	recovered := smoother.Smoothed()
	if recovered < -35 || recovered > -25 {
		t.Fatalf("smoothing should recover, got %v (expected near -30)", recovered)
	}
}

func TestBatteryEstimatePowerComputation(t *testing.T) {
	soc := 68.0
	current := 50.0
	smoother := newTestSmoother()
	est := ComputeBatteryEstimate(&soc, &current, smoother, testBatteryCfg)

	expectedPower := 50.0 * 12.8 // = 640W
	if math.Abs(est.PowerW-expectedPower) > 0.01 {
		t.Fatalf("expected power %.1fW, got %.1fW", expectedPower, est.PowerW)
	}
}

func TestFormatBatteryDuration(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected string
	}{
		{"zero", 0, "< 1 minute"},
		{"thirty seconds", 30, "< 1 minute"},
		{"forty five seconds", 45, "< 1 minute"},
		{"one minute", 60, "1m"},
		{"thirty seven minutes", 2220, "37m"},
		{"one hour twelve minutes", 4320, "1h 12m"},
		{"six hours five minutes", 21900, "6h 05m"},
		{"one day three hours", 90000, "1d 3h"},
		{"negative", -100, ""},
		{"NaN", math.NaN(), ""},
		{"Inf", math.Inf(1), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatBatteryDuration(tt.seconds)
			if got != tt.expected {
				t.Fatalf("FormatBatteryDuration(%v) = %q, want %q", tt.seconds, got, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./service/runtime/ -run TestBattery -v`
Expected: All tests PASS

- [ ] **Step 3: Run all runtime tests to check for regressions**

Run: `go test ./service/runtime/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add service/runtime/battery_estimate_test.go
git commit -m "test: add battery estimate and duration formatting tests"
```

---

### Task 5: Update existing overview tests

**Files:**
- Modify: `service/runtime/overview_test.go`

**Interfaces:**
- Consumes: Updated `overview.Battery` struct from Task 3

- [ ] **Step 1: Update TestOverviewDocumentEstimatesChargingTimeLinearly**

The existing test checks `doc.Battery.ETAHours` which no longer exists. Update to check `doc.Battery.ETASeconds` and `doc.Battery.Mode`:

```go
func TestOverviewDocumentEstimatesChargingTimeLinearly(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	app := &App{
		rawConfig: config.Config{Overview: config.OverviewConfig{
			UsableBatteryCapacityAh: 100,
			BatteryCapacityAh:       100,
			BatteryNominalVoltage:   12.8,
			BatteryFloorSOC:         20,
			BatteryReadySOC:         95,
			ChargeEfficiency:        0.99,
		}},
		now:               func() time.Time { return now },
		batteryEstimate:   NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	soc, current := 40.0, 10.0
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current, UpdatedAt: &now})
	if doc.Battery.Mode != "charging" {
		t.Fatalf("expected charging mode, got %q", doc.Battery.Mode)
	}
	if doc.Battery.ETASeconds == nil {
		t.Fatalf("expected ETA seconds, got nil")
	}
	// requiredAh = 100 * (95-40)/100 = 55Ah
	// effective = 10 * 0.99 = 9.9A
	// hours = 55/9.9 ≈ 5.556h ≈ 20000s
	eta := *doc.Battery.ETASeconds
	if eta < 19990 || eta > 20010 {
		t.Fatalf("expected ~20000s ETA, got %v", eta)
	}
}
```

- [ ] **Step 2: Update TestOverviewDocumentDoesNotEstimateWhenNotCharging**

```go
func TestOverviewDocumentDoesNotEstimateWhenNotCharging(t *testing.T) {
	soc, current := 40.0, -2.0
	app := &App{
		rawConfig: config.Config{Overview: config.OverviewConfig{
			BatteryCapacityAh:     660,
			BatteryNominalVoltage: 12.8,
			BatteryFloorSOC:       20,
			BatteryReadySOC:       95,
			ChargeEfficiency:      0.99,
		}},
		batteryEstimate: NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current})
	// -2A is inside the idle deadband
	if doc.Battery.Mode != "idle" {
		t.Fatalf("expected idle mode, got %q", doc.Battery.Mode)
	}
	if doc.Battery.ETASeconds != nil {
		t.Fatalf("expected no ETA when idle, got %v", doc.Battery.ETASeconds)
	}
}
```

- [ ] **Step 3: Add test for discharging ETA**

```go
func TestOverviewDocumentEstimatesDischargeTime(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	app := &App{
		rawConfig: config.Config{Overview: config.OverviewConfig{
			BatteryCapacityAh:     660,
			BatteryNominalVoltage: 12.8,
			BatteryFloorSOC:       20,
			BatteryReadySOC:       95,
			ChargeEfficiency:      0.99,
		}},
		now:               func() time.Time { return now },
		batteryEstimate:   NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	soc, current := 80.0, -30.0
	doc := app.overviewDocument(overview.Telemetry{BatteryStateOfChargePercent: &soc, BatteryCurrentA: &current, UpdatedAt: &now})
	if doc.Battery.Mode != "discharging" {
		t.Fatalf("expected discharging mode, got %q", doc.Battery.Mode)
	}
	if doc.Battery.ETASeconds == nil {
		t.Fatalf("expected ETA seconds, got nil")
	}
	// usableRemaining = 660 * (80-20)/100 = 396Ah
	// hours = 396/30 = 13.2h = 47520s
	eta := *doc.Battery.ETASeconds
	if eta < 47500 || eta > 47540 {
		t.Fatalf("expected ~47520s ETA, got %v", eta)
	}
}
```

- [ ] **Step 4: Add test for stale data**

```go
func TestOverviewDocumentBatteryUnavailableWhenMissing(t *testing.T) {
	app := &App{
		rawConfig:       config.Config{Overview: config.OverviewConfig{}},
		batteryEstimate: NewBatteryEstimateSmoothing(5*time.Minute, time.Second),
	}
	doc := app.overviewDocument(overview.Telemetry{})
	if doc.Battery.Status != "unavailable" {
		t.Fatalf("expected unavailable status, got %q", doc.Battery.Status)
	}
}
```

- [ ] **Step 5: Run all tests**

Run: `go test ./service/runtime/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add service/runtime/overview_test.go
git commit -m "test: update overview tests for new battery estimate model"
```

---

### Task 6: Update frontend HTML and JavaScript

**Files:**
- Modify: `web/static/index.html:44-56`
- Modify: `web/static/app.js:780-790`
- Modify: `web/static/app.js:622-627`

**Interfaces:**
- Consumes: `overview.Battery` JSON with `mode`, `eta_seconds`, `target_soc`, `power_w`, `charge_state`

- [ ] **Step 1: Update the battery card HTML**

In `web/static/index.html`, replace the battery card (lines 47-56) with:

```html
<button id="batterySocCard" class="overview-card overview-charging-card" type="button" data-overview-route="#/more/settings">
  <div class="overview-card-heading"><span>Battery</span><span id="batteryState" class="state-text">Loading</span></div>
  <div class="overview-charging-row">
    <strong id="batterySoc" class="overview-value">--</strong>
    <strong id="batteryCurrent" class="overview-charging-current">--</strong>
  </div>
  <div class="supply-bar" aria-hidden="true"><span id="batteryBar"></span></div>
  <p id="batteryPower" class="detail-text"></p>
  <p id="timeToFull" class="detail-text"></p>
  <p id="batteryLastSeen" class="detail-text last-seen-text" hidden></p>
</button>
```

- [ ] **Step 2: Add formatBatteryDuration to app.js**

In `web/static/app.js`, after the `formatBatteryCurrent` function (line 627), add:

```javascript
function formatBatteryDuration(seconds) {
  if (seconds === null || seconds === undefined || !Number.isFinite(Number(seconds))) return "";
  const s = Number(seconds);
  if (s < 60) return "< 1 minute";
  const totalMinutes = Math.floor(s / 60);
  if (totalMinutes < 60) return `${totalMinutes}m`;
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  if (days > 0) return `${days}d ${remainingHours}h`;
  return `${hours}h ${String(minutes).padStart(2, "0")}m`;
}

function formatBatteryPower(watts) {
  if (watts === null || watts === undefined || !Number.isFinite(Number(watts))) return "";
  const w = Number(watts);
  const sign = w >= 0 ? "+" : "";
  return `${sign}${(w / 1000).toFixed(2)} kW`;
}
```

- [ ] **Step 3: Update renderOverview to use new battery fields**

In `web/static/app.js`, replace the battery rendering block (lines 785-790) with:

```javascript
if (byId("batterySoc")) byId("batterySoc").textContent = overviewPercent(doc.battery && doc.battery.state_of_charge_percent);
if (byId("batteryCurrent")) byId("batteryCurrent").textContent = doc.battery && doc.battery.current_a !== undefined ? formatBatteryCurrent(doc.battery.current_a) : "--";
if (byId("batteryState")) {
  const b = doc.battery;
  let stateText = "N/A";
  if (b) {
    if (stale || b.status === "unavailable") stateText = "N/A";
    else if (b.mode === "charging" && b.charge_state === "topping_off") stateText = "Charging";
    else if (b.mode === "charging") stateText = "Charging";
    else if (b.mode === "discharging") stateText = "Discharging";
    else if (b.mode === "idle") stateText = "Idle";
    else stateText = "N/A";
  }
  byId("batteryState").textContent = stateText;
}
if (byId("batteryPower")) {
  byId("batteryPower").textContent = doc.battery && doc.battery.power_w !== undefined ? formatBatteryPower(doc.battery.power_w) : "";
}
if (byId("timeToFull")) {
  const b = doc.battery;
  let detailText = "";
  if (b && !stale && b.status !== "unavailable") {
    if (b.mode === "charging" && b.charge_state === "topping_off") {
      detailText = "Topping off";
    } else if (b.eta_seconds !== undefined && b.eta_seconds !== null) {
      const targetLabel = b.target_soc !== undefined ? `${Number(b.target_soc).toFixed(0)}%` : "";
      detailText = `${formatBatteryDuration(b.eta_seconds)} to ${targetLabel}`;
    }
  }
  byId("timeToFull").textContent = detailText;
}
if (byId("batteryBar")) { const pct = doc.battery && doc.battery.state_of_charge_percent; byId("batteryBar").style.width = pct === undefined || pct === null ? "0%" : `${Math.max(0, Math.min(100, Number(pct)))}%`; }
applyLastSeen("batteryLastSeen", doc.battery && doc.battery.updated_at);
```

Note: when the document is stale, the ETA/detail line is cleared so an old estimate is never shown as current. The SOC and current values remain visible with the existing "Last seen" indicator.

- [ ] **Step 4: Run JavaScript tests and lint**

Run: `npm test && npm run lint`
Expected: PASS (may need to update test expectations for new DOM elements; ESLint must stay clean)

- [ ] **Step 5: Run sim to verify visual appearance**

Run: `./scripts/sim/run-sim.sh`
Expected: Overview page shows battery card with mode, power, ETA, and duration

- [ ] **Step 6: Commit**

```bash
git add web/static/index.html web/static/app.js
git commit -m "feat: render battery estimate on overview page"
```

---

### Task 7: Final verification

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Run all JavaScript tests and lint**

Run: `npm test && npm run lint`
Expected: ALL PASS

- [ ] **Step 3: Run the full sim**

Run: `./scripts/sim/run-sim.sh`
Expected: Overview page shows battery estimate with:
- SOC percentage
- Current with sign (+/-)
- Power in kW
- Mode: Charging / Discharging / Idle
- ETA to target SOC (or "Topping off" above 95%)
- Progress bar
- "Last seen" indicator when stale

- [ ] **Step 4: Verify edge cases in the UI**

Check the sim with different capture data to verify:
- Charging state shows "Charging" and ETA to 95%
- Discharging state shows "Discharging" and ETA to 20%
- Idle (small current) shows "Idle" with no ETA
- Stale data shows "Last seen" and no stale ETA
- Above 95% shows "Topping off" instead of misleading linear ETA

- [ ] **Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address review feedback on battery estimate feature"
```
