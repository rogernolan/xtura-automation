package history

import (
	"bytes"
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

func TestLoadTailRepairsCorruptUnterminatedTailBeforeAppend(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 7*24*time.Hour, func() time.Time { return now }, nil)
	if err := store.Append("abc", sensors.Sample{At: now, Temp: 20}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "abc.ndjson")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("{\"t\":\"2026-08-16T10:01:00Z\"")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded := New(dir, 2*time.Hour, 7*24*time.Hour, func() time.Time { return now.Add(2 * time.Minute) }, nil)
	if _, err := reloaded.LoadTail("abc", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Append("abc", sensors.Sample{At: now.Add(2 * time.Minute), Temp: 21}); err != nil {
		t.Fatal(err)
	}
	samples, err := reloaded.LoadTail("abc", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[1].Temp != 21 {
		t.Fatalf("expected original and appended samples, got %#v", samples)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("2026-08-16T10:01:00Z")) {
		t.Fatalf("corrupt record remained in cleaned history: %q", data)
	}
}

func TestCompactRetainsThirtyDayWindowAndArchivesHourly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 30*24*time.Hour, func() time.Time { return now }, nil)
	old := now.Add(-31 * 24 * time.Hour)
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
		t.Fatalf("expected old sample removed from recent file, got %q", got)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "hourly", "abc-*.ndjson"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("expected one hourly archive, got %v (%v)", archives, err)
	}
	archive, err := os.ReadFile(archives[0])
	if err != nil {
		t.Fatalf("ReadFile archive: %v", err)
	}
	if !contains(string(archive), "temp\":1") {
		t.Fatalf("expected old sample in hourly archive, got %q", archive)
	}
}

func TestCompactDownsamplesRecentSamplesToTenMinutes(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 30*24*time.Hour, func() time.Time { return now }, nil)
	for i := 0; i < 6; i++ {
		at := now.Add(-30 * time.Minute).Add(time.Duration(i) * time.Minute)
		if err := store.Append("abc", sensors.Sample{At: at, Temp: float64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "abc.ndjson"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := len(testSplitLines(data)); got != 1 {
		t.Fatalf("expected one ten-minute bucket, got %d: %s", got, data)
	}
}

func TestCompactPreservesHourlyArchiveAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := New(dir, 2*time.Hour, 30*24*time.Hour, func() time.Time { return now }, nil)
	old := now.Add(-31 * 24 * time.Hour)
	if err := store.Append("abc", sensors.Sample{At: old, Temp: 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("first Compact: %v", err)
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("second Compact: %v", err)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "hourly", "abc-*.ndjson"))
	if len(archives) != 1 {
		t.Fatalf("expected one archive after repeated compaction, got %v", archives)
	}
	data, _ := os.ReadFile(archives[0])
	if got := len(testSplitLines(data)); got != 1 {
		t.Fatalf("expected one archived sample after repeated compaction, got %d", got)
	}
}

func testSplitLines(data []byte) [][]byte {
	var lines [][]byte
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) != 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
