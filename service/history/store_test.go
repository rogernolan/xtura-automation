package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"empirebus-tests/service/domains/sensors"
)

func TestAppendAndRecent(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 7*24*time.Hour, func() time.Time { return now }, nil)

	hum := 55.0
	for i := 0; i < 5; i++ {
		at := now.Add(-time.Duration(5-i) * time.Minute)
		sample := sensors.Sample{At: at, Temp: 20 + float64(i)*0.1, Hum: &hum}
		if err := store.Append("abc", sample); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	samples := store.Recent("abc", now)
	if len(samples) != 5 {
		t.Fatalf("expected 5 recent samples, got %d", len(samples))
	}
	if samples[0].Temp != 20 {
		t.Fatalf("first sample temp: got %v", samples[0].Temp)
	}
}

func TestRecentTrimsOutsideWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, time.Hour, 7*24*time.Hour, func() time.Time { return now }, nil)
	for i := 0; i < 3; i++ {
		at := now.Add(-time.Duration(2*i+1) * time.Hour)
		if err := store.Append("abc", sensors.Sample{At: at, Temp: float64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if samples := store.Recent("abc", now); len(samples) != 1 {
		t.Fatalf("expected 1 recent sample after trimming, got %d", len(samples))
	}
}

func TestLoadTailAndSeedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 7*24*time.Hour, func() time.Time { return now }, nil)
	for i := 0; i < 3; i++ {
		at := now.Add(-time.Duration(3-i) * time.Minute)
		if err := store.Append("abc", sensors.Sample{At: at, Temp: 19 + float64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	reloaded := New(dir, 2*time.Hour, 7*24*time.Hour, func() time.Time { return now }, nil)
	samples, err := reloaded.LoadTail("abc", now)
	if err != nil {
		t.Fatalf("LoadTail: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("expected 3 loaded samples, got %d", len(samples))
	}
	reloaded.Seed("abc", samples)
	if got := reloaded.Recent("abc", now); len(got) != 3 {
		t.Fatalf("expected 3 recent after seed, got %d", len(got))
	}
}

func TestCompactRetainsWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 7*24*time.Hour, func() time.Time { return now }, nil)
	old := now.Add(-10 * 24 * time.Hour)
	new := now.Add(-time.Hour)
	if err := store.Append("abc", sensors.Sample{At: old, Temp: 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Append("abc", sensors.Sample{At: new, Temp: 2}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "abc.ndjson"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); len(got) == 0 || contains(got, "Temp\":1") || contains(got, "temp\":1") {
		t.Fatalf("expected old sample removed, got %q", got)
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
