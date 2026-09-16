package runtime

import (
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	idleDeadbandA             = 2.0
	smoothingHalfLifeDuration = 5 * time.Minute
)

// BatteryMode represents the high-level battery state.
type BatteryMode string

const (
	BatteryModeCharging    BatteryMode = "charging"
	BatteryModeDischarging BatteryMode = "discharging"
	BatteryModeIdle        BatteryMode = "idle"
	BatteryModeUnknown     BatteryMode = "unknown"
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
	CapacityAh        float64
	NominalVoltage    float64
	FloorSOC          float64
	ReadySOC          float64
	MaxChargeCurrentA float64
	ChargeEfficiency  float64
}

// BatteryEstimate is the result of a battery estimate computation.
type BatteryEstimate struct {
	Mode             BatteryMode
	SOC              float64 // percent
	CurrentA         float64 // smoothed, positive = charging
	PowerW           float64 // computed from current × nominal voltage
	ChargeState      BatteryChargeState
	TargetSOC        float64 // the SOC target for ETA calculation
	EstimatedSeconds float64 // ETA in seconds, 0 = no estimate
	Available        bool    // false if inputs are stale or missing
}

// BatteryEstimateSmoothing maintains an EWMA of battery current.
// It is safe for concurrent use.
type BatteryEstimateSmoothing struct {
	mu          sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.smoothed
}

// Initialized reports whether Update has been called at least once.
func (s *BatteryEstimateSmoothing) Initialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

// Reset clears the smoothing state.
func (s *BatteryEstimateSmoothing) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.smoothed = 0
	s.initialized = false
}

// ComputeBatteryEstimate calculates the battery estimate from raw telemetry.
// The smoother is fed externally (single writer from the publish loop) and
// this function reads it; nil or uninitialized means raw current is used directly.
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

	// Read the EWMA smoother when it has been seeded; otherwise use raw
	smoothedCurrent := currentRaw
	if smoother != nil && smoother.Initialized() {
		smoothedCurrent = smoother.Smoothed()
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
		Mode:      mode,
		SOC:       socVal,
		CurrentA:  smoothedCurrent,
		PowerW:    powerW,
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
		est.TargetSOC = cfg.FloorSOC
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
