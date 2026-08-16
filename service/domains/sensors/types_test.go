package sensors

import (
	"testing"
	"time"
)

func TestNormalizeMAC(t *testing.T) {
	if got := NormalizeMAC("C5:65:68:81:84:32"); got != "c56568818432" {
		t.Fatalf("got %q", got)
	}
}

func TestSettingsValidate(t *testing.T) {
	valid := Settings{
		Enabled:   true,
		HCIDevice: "hci0",
		Sensors: []SensorConfig{
			{Name: "Main", MAC: "c5:65:68:81:84:32", Primary: true},
			{Name: "Outside", MAC: "d6:66:69:92:95:43"},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestSettingsValidateRejectsProblems(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Settings)
		want string
	}{
		{"bad hci", func(s *Settings) { s.HCIDevice = "bluetooth0" }, "hci_device"},
		{"empty name", func(s *Settings) { s.Sensors[0].Name = "  " }, "name is required"},
		{"bad mac", func(s *Settings) { s.Sensors[0].MAC = "c5:65:68" }, "must be a colon-separated MAC"},
		{"duplicate name", func(s *Settings) { s.Sensors[1].Name = "Main" }, "duplicates"},
		{"duplicate mac", func(s *Settings) { s.Sensors[1].MAC = s.Sensors[0].MAC }, "duplicates"},
		{"two primaries", func(s *Settings) { s.Sensors[1].Primary = true }, "at most one primary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Settings{
				Enabled:   true,
				HCIDevice: "hci0",
				Sensors: []SensorConfig{
					{Name: "Main", MAC: "c5:65:68:81:84:32", Primary: true},
					{Name: "Outside", MAC: "d6:66:69:92:95:43"},
				},
			}
			tc.mod(&s)
			if err := s.Validate(); err == nil || !contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTrendOf(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		delta float64
		want  Trend
	}{
		{"rising", 2.0, TrendRising},
		{"falling", -2.0, TrendFalling},
		{"steady", 0.1, TrendSteady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var samples []Sample
			for i := 0; i*60 < int(TrendWindow.Seconds()); i++ {
				at := now.Add(-time.Duration(i) * time.Minute)
				temp := 20.0
				if i*60 < int(TrendRecent.Seconds()) {
					temp += tc.delta
				}
				samples = append(samples, Sample{At: at, Temp: temp})
			}
			if got := TrendOf(samples, now); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrendOfUnavailableWithSparseHistory(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	if got := TrendOf(nil, now); got != TrendUnavailable {
		t.Fatalf("got %q, want unavailable", got)
	}
	// Only recent samples, no baseline bucket.
	samples := []Sample{{At: now.Add(-time.Minute), Temp: 20}}
	if got := TrendOf(samples, now); got != TrendUnavailable {
		t.Fatalf("got %q, want unavailable", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
