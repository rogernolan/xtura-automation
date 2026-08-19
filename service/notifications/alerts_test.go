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
	if got := e.Evaluate("alde", "Alde", 21, now.Add(4*time.Minute)); len(got) != 1 {
		t.Fatalf("second crossing notifications = %#v", got)
	}
}

func TestEvaluatorRepeatEveryFiveMinutes(t *testing.T) {
	now := time.Unix(0, 0)
	settings := Settings{Alerts: []Alert{{ID: "cold", Name: "Cabin cold", SensorID: "alde", LowCelsius: ptr(10), Mode: DeliveryRepeat}}}
	e := NewEvaluator(settings)
	for i, want := range []int{1, 0, 0, 0, 0, 1} {
		got := e.Evaluate("alde", "Alde", 8, now.Add(time.Duration(i)*time.Minute))
		if len(got) != want {
			t.Fatalf("minute %d notifications = %#v, want %d", i, got, want)
		}
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

func ptr(v float64) *float64 { return &v }
