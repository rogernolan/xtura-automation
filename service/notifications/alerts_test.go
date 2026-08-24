package notifications

import (
	"testing"
	"time"
)

func TestEvaluatorCrossingAndRearm(t *testing.T) {
	now := time.Unix(0, 0)
	settings := Settings{Alerts: []Alert{{ID: "hot", Name: "Cabin hot", SensorID: "alde", HighCelsius: ptr(20)}}}
	e := NewEvaluator(settings)
	if got := e.Evaluate("alde", "Alde", 19, now); len(got) != 0 {
		t.Fatalf("initial safe notifications = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 21, now.Add(time.Minute)); len(got) != 1 || got[0].Side != "high" {
		t.Fatalf("crossing notifications = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 22, now.Add(2*time.Minute)); len(got) != 0 {
		t.Fatalf("crossing repeat notifications = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 19, now.Add(3*time.Minute)); len(got) != 0 {
		t.Fatalf("rearm notifications = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 21, now.Add(4*time.Minute)); len(got) != 0 {
		t.Fatalf("second crossing within debounce window = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 19, now.Add(5*time.Minute)); len(got) != 0 {
		t.Fatalf("re-arm after suppressed crossing = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 21, now.Add(9*time.Minute)); len(got) != 0 {
		t.Fatalf("third crossing within debounce window = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 19, now.Add(10*time.Minute)); len(got) != 0 {
		t.Fatalf("re-arm after third crossing = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 21, now.Add(11*time.Minute)); len(got) != 1 {
		t.Fatalf("second crossing after debounce window = %#v", got)
	}
}

func TestEvaluatorRepeatHonorsTenMinuteDebounce(t *testing.T) {
	now := time.Unix(0, 0)
	settings := Settings{Alerts: []Alert{{ID: "cold", Name: "Cabin cold", SensorID: "alde", LowCelsius: ptr(10), Mode: DeliveryRepeat}}}
	e := NewEvaluator(settings)
	if got := e.Evaluate("alde", "Alde", 8, now); len(got) != 1 {
		t.Fatalf("initial notification = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 12, now.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("re-arm before re-cross = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 8, now.Add(2*time.Minute)); len(got) != 0 {
		t.Fatalf("re-cross within debounce window = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 8, now.Add(5*time.Minute)); len(got) != 0 {
		t.Fatalf("repeat before debounce window = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 8, now.Add(10*time.Minute)); len(got) != 1 {
		t.Fatalf("repeat after debounce window = %#v", got)
	}

	if got := e.Evaluate("alde", "Alde", 12, now.Add(11*time.Minute)); len(got) != 0 {
		t.Fatalf("re-arm after repeat alert = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 8, now.Add(12*time.Minute)); len(got) != 0 {
		t.Fatalf("second re-cross within debounce window = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 12, now.Add(14*time.Minute)); len(got) != 0 {
		t.Fatalf("re-arm after suppressed re-cross = %#v", got)
	}
	if got := e.Evaluate("alde", "Alde", 8, now.Add(20*time.Minute)); len(got) != 1 {
		t.Fatalf("re-cross after debounce window = %#v", got)
	}
}

func TestEvaluatorHighAndLowSidesAreIndependent(t *testing.T) {
	now := time.Unix(0, 0)
	settings := Settings{Alerts: []Alert{{ID: "range", Name: "Cabin range", SensorID: "alde", HighCelsius: ptr(20), LowCelsius: ptr(10)}}}
	e := NewEvaluator(settings)
	got := e.Evaluate("alde", "Alde", 21, now)
	if len(got) != 1 || got[0].Side != "high" {
		t.Fatalf("high notification = %#v", got)
	}
	got = e.Evaluate("alde", "Alde", 8, now.Add(time.Minute))
	if len(got) != 1 || got[0].Side != "low" {
		t.Fatalf("low notification after high notification = %#v", got)
	}
}

func TestSettingsValidate(t *testing.T) {
	known := map[string]struct{}{"alde": {}, "abc": {}}
	cases := []struct {
		name     string
		settings Settings
	}{
		{"no limit", Settings{Alerts: []Alert{{ID: "a", SensorID: "alde"}}}},
		{"unknown sensor", Settings{Alerts: []Alert{{ID: "a", SensorID: "missing", HighCelsius: ptr(20)}}}},
		{"reversed limits", Settings{Alerts: []Alert{{ID: "a", SensorID: "alde", LowCelsius: ptr(20), HighCelsius: ptr(10)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.settings.Validate(known); err == nil {
				t.Fatal("Validate returned nil")
			}
		})
	}
}

func TestEvaluatorReportsOfflineAfterThirtyMinutes(t *testing.T) {
	now := time.Unix(0, 0)
	e := NewEvaluator(Settings{Alerts: []Alert{{ID: "a", Name: "Cabin", SensorID: "alde", HighCelsius: ptr(20)}}})
	names := map[string]string{"alde": "Alde"}
	e.Evaluate("alde", "Alde", 19, now)
	if got := e.CheckOffline(now.Add(30*time.Minute), names); len(got) != 0 {
		t.Fatalf("at timeout notifications = %#v", got)
	}
	got := e.CheckOffline(now.Add(30*time.Minute+time.Second), names)
	if len(got) != 1 || got[0].Side != "offline" {
		t.Fatalf("offline notifications = %#v", got)
	}
	if got := e.CheckOffline(now.Add(31*time.Minute), names); len(got) != 0 {
		t.Fatalf("offline repeat notifications = %#v", got)
	}
	e.Evaluate("alde", "Alde", 19, now.Add(32*time.Minute))
	if got := e.CheckOffline(now.Add(63*time.Minute), names); len(got) != 1 {
		t.Fatalf("offline rearm notifications = %#v", got)
	}
}

func ptr(v float64) *float64 { return &v }
