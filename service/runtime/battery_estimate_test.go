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
		{"one day one hours", 90000, "1d 1h"},
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
