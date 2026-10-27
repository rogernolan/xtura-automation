package notifications

import (
	"strings"
	"testing"
	"time"

	"empirebus-tests/service/domains/heating"
)

func TestAldePowerWaitsSixtySecondsAndSendsOnce(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	state := heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 2000, ShoreConnected: true, InputLimitA: 3.1, InputVoltageV: 233, ShoreWatts: 722.3, Session: 1}
	e := &AldePowerEvaluator{}
	for _, elapsed := range []time.Duration{0, 59 * time.Second} {
		if got := e.Evaluate(state, true, now.Add(elapsed)); len(got) != 0 {
			t.Fatalf("premature at %v: %+v", elapsed, got)
		}
	}
	got := e.Evaluate(state, true, now.Add(time.Minute))
	if len(got) != 1 || got[0].Kind != "alde_power" || !strings.Contains(got[0].Body, "3.1 A") || !strings.Contains(got[0].Body, "2 kW") {
		t.Fatalf("warning = %+v", got)
	}
	if got := e.Evaluate(state, true, now.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("duplicate: %+v", got)
	}
}

func TestAldePowerRecoveryAndUnknownLifecycle(t *testing.T) {
	for _, change := range []string{"sufficient", "off", "electric off", "disabled", "unknown", "session"} {
		t.Run(change, func(t *testing.T) {
			now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
			bad := heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 3000, Session: 1}
			e := &AldePowerEvaluator{}
			e.Evaluate(bad, true, now)
			changed, enabled := bad, true
			switch change {
			case "sufficient":
				changed.ShoreWatts = 3680
			case "off":
				changed.HeaterOn = false
			case "electric off":
				changed.ElectricWatts = 0
			case "disabled":
				enabled = false
			case "unknown":
				changed.Known = false
			case "session":
				changed.Session = 2
				bad.Session = 2
			}
			if got := e.Evaluate(changed, enabled, now.Add(30*time.Second)); len(got) != 0 {
				t.Fatal(got)
			}
			if got := e.Evaluate(bad, true, now.Add(60*time.Second)); len(got) != 0 {
				t.Fatalf("timer carried over: %+v", got)
			}
			if got := e.Evaluate(bad, true, now.Add(120*time.Second)); len(got) != 1 {
				t.Fatalf("new interval: %+v", got)
			}
			unknown := bad
			unknown.Known = false
			e.Evaluate(unknown, true, now.Add(121*time.Second))
			if got := e.Evaluate(bad, true, now.Add(200*time.Second)); len(got) != 0 {
				t.Fatalf("unknown rearmed sent warning: %+v", got)
			}
			safe := bad
			safe.HeaterOn = false
			e.Evaluate(safe, true, now.Add(201*time.Second))
			e.Evaluate(bad, true, now.Add(202*time.Second))
			got := e.Evaluate(bad, true, now.Add(262*time.Second))
			if len(got) != 1 || !strings.Contains(got[0].Body, "shore power") {
				t.Fatalf("rearmed unplug warning: %+v", got)
			}
		})
	}
}

func TestAldePowerSufficientAndDefaultEnabled(t *testing.T) {
	if !(Settings{}).PowerAlertEnabled() {
		t.Fatal("existing installs must default to enabled")
	}
	for _, watts := range []float64{0, 722.3, 4000} {
		e := &AldePowerEvaluator{}
		s := heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 722.3, ShoreWatts: watts}
		if watts == 0 {
			s.HeaterOn = false
		}
		e.Evaluate(s, true, time.Unix(100, 0))
		if got := e.Evaluate(s, true, time.Unix(200, 0)); len(got) != 0 {
			t.Fatalf("safe state produced alert: %+v", got)
		}
	}
}
