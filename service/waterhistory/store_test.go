package waterhistory

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObserveValidSample(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	fresh, grey := 72.0, 31.0
	store := New(Options{Directory: t.TempDir(), Retention: 7 * 24 * time.Hour}, func() time.Time { return now })
	changed, err := store.Observe(Sample{At: now.Add(-time.Minute), FreshPercent: &fresh, GreyPercent: &grey}, now)
	if err != nil || !changed {
		t.Fatalf("Observe() changed=%t err=%v", changed, err)
	}
	doc := store.Document(now)
	if len(doc.Samples) != 1 || *doc.Samples[0].FreshPercent != fresh || *doc.Samples[0].GreyPercent != grey {
		t.Fatalf("unexpected document: %+v", doc)
	}
}

func TestFillRequiresConfiguredThresholdAndSettling(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := New(Options{
		Directory:      t.TempDir(),
		Threshold:      10,
		SettlingPeriod: 15 * time.Minute,
		GroupingWindow: time.Hour,
		Retention:      7 * 24 * time.Hour,
	}, func() time.Time { return base })
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 59, 80)
	if got := len(store.Document(base.Add(14 * time.Minute)).Events); got != 0 {
		t.Fatalf("event before threshold/settling: %d", got)
	}
	observeBoth(t, store, base.Add(2*time.Minute), 60, 80)
	if got := len(store.Document(base.Add(14 * time.Minute)).Events); got != 0 {
		t.Fatalf("event before settling: %d", got)
	}
	observeBoth(t, store, base.Add(17*time.Minute), 60, 80)
	doc := store.Document(base.Add(17 * time.Minute))
	if len(doc.Events) != 1 || doc.Events[0].Kind != KindFill {
		t.Fatalf("unexpected events: %+v", doc.Events)
	}
}

func TestGreyEmptyIgnoresUnmatchedClose(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	changed, err := store.RecordGreyEmpty(base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unmatched close should not change state")
	}
	if got := len(store.Document(base.Add(time.Minute)).Events); got != 0 {
		t.Fatalf("unexpected events: %+v", store.Document(base.Add(time.Minute)).Events)
	}
}

func TestGreyEmptyOpenThenCloseRecordsEvent(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	changed, err := store.RecordGreyDischargeOpen(base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected open to persist")
	}
	changed, err = store.RecordGreyEmpty(base.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected close to record an event")
	}
	doc := store.Document(base.Add(2 * time.Minute))
	if len(doc.Events) != 1 || doc.Events[0].Kind != KindEmpty {
		t.Fatalf("unexpected events: %+v", doc.Events)
	}
	if doc.Events[0].Tank != TankGrey || doc.Events[0].From != 80 || doc.Events[0].To != 0 || doc.Events[0].Used != 80 {
		t.Fatalf("unexpected grey empty event: %+v", doc.Events[0])
	}
}

func TestGreyEmptyCloseIsIdempotent(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	if _, err := store.RecordGreyDischargeOpen(base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(base.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	changed, err := store.RecordGreyEmpty(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second close should be ignored")
	}
	if got := len(store.Document(base.Add(3 * time.Minute)).Events); got != 1 {
		t.Fatalf("got %d events", got)
	}
}

func TestGreyEmptyCloseIsIdempotentAcrossReloadAfterPersistedEventReplay(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	options := Options{Directory: dir, Threshold: 5, SettlingPeriod: 10 * time.Minute, GroupingWindow: time.Hour, Retention: 7 * 24 * time.Hour}
	store := New(options, func() time.Time { return base })
	observeBoth(t, store, base, 50, 80)
	openAt := base.Add(time.Minute)
	closeAt := base.Add(2 * time.Minute)
	if _, err := store.RecordGreyDischargeOpen(openAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(closeAt); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state.GreyDischargeOpenAt = timePtr(openAt)
	if err := writeState(filepath.Join(dir, "state.json"), state); err != nil {
		t.Fatal(err)
	}

	reloaded := New(options, func() time.Time { return closeAt.Add(time.Minute) })
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	changed, err := reloaded.RecordGreyEmpty(closeAt)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("replayed close should not append a duplicate event")
	}
	if got := len(reloaded.Document(closeAt.Add(time.Minute)).Events); got != 1 {
		t.Fatalf("expected one persisted grey-empty event after replay, got %d", got)
	}
}

func TestGreyEmptyReplayDoesNotClearNewerGreySample(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	openAt := base.Add(time.Minute)
	closeAt := base.Add(2 * time.Minute)
	if _, err := store.RecordGreyDischargeOpen(openAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(closeAt); err != nil {
		t.Fatal(err)
	}
	observeBoth(t, store, base.Add(3*time.Minute), 50, 16)

	changed, err := store.RecordGreyEmpty(closeAt)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("replayed close should not report a history change")
	}
	if store.state.Grey == nil || *store.state.Grey != 16 {
		t.Fatalf("replayed close should preserve newer grey state, got %#v", store.state.Grey)
	}
	if store.state.GreyBase == nil || *store.state.GreyBase != 16 {
		t.Fatalf("replayed close should rebase grey anomaly detection, got %#v", store.state.GreyBase)
	}
	if store.state.GreyDischargeOpenAt != nil {
		t.Fatalf("replayed close should not leave a pending open, got %v", store.state.GreyDischargeOpenAt)
	}
}

func TestGreyEmptyReplayDoesNotClearNewerPendingOpen(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	firstOpenAt := base.Add(time.Minute)
	firstCloseAt := base.Add(2 * time.Minute)
	secondOpenAt := base.Add(4 * time.Minute)
	secondCloseAt := base.Add(5 * time.Minute)
	if _, err := store.RecordGreyDischargeOpen(firstOpenAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(firstCloseAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyDischargeOpen(secondOpenAt); err != nil {
		t.Fatal(err)
	}

	changed, err := store.RecordGreyEmpty(firstCloseAt)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("replayed close should not report a history change")
	}
	if store.state.GreyDischargeOpenAt == nil || !store.state.GreyDischargeOpenAt.Equal(secondOpenAt) {
		t.Fatalf("replayed close should preserve newer pending open, got %v", store.state.GreyDischargeOpenAt)
	}
	observeBoth(t, store, secondCloseAt, 50, 22)
	changed, err = store.RecordGreyEmpty(secondCloseAt)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected newer pending open to remain active for its own close")
	}
	if got := len(store.Document(secondCloseAt).Events); got != 2 {
		t.Fatalf("expected both grey-empty events, got %d", got)
	}
}

func TestGreySampleOlderThanEmptyIsIgnored(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	openAt := base.Add(time.Minute)
	closeAt := base.Add(2 * time.Minute)
	if _, err := store.RecordGreyDischargeOpen(openAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(closeAt); err != nil {
		t.Fatal(err)
	}

	staleGrey := 40.0
	if _, err := store.Observe(Sample{At: openAt, GreyPercent: &staleGrey}, closeAt); err != nil {
		t.Fatal(err)
	}
	if store.state.Grey == nil || *store.state.Grey != 0 {
		t.Fatalf("stale pre-close grey sample overwrote empty state: %#v", store.state.Grey)
	}
}

func TestGreyPendingOpenPersistsAcrossReload(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	options := Options{Directory: dir, Threshold: 5, SettlingPeriod: 10 * time.Minute, GroupingWindow: time.Hour, Retention: 7 * 24 * time.Hour}
	store := New(options, func() time.Time { return base })
	observeBoth(t, store, base, 50, 80)
	if _, err := store.RecordGreyDischargeOpen(base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	reloaded := New(options, func() time.Time { return base.Add(2 * time.Minute) })
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	changed, err := reloaded.RecordGreyEmpty(base.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected persisted open to allow close")
	}
	doc := reloaded.Document(base.Add(2 * time.Minute))
	if len(doc.Events) != 1 || doc.Events[0].Tank != TankGrey || doc.Events[0].Kind != KindEmpty {
		t.Fatalf("unexpected reload events: %+v", doc.Events)
	}
}

func TestLongFillProducesOneEvent(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 56, 80)
	observeBoth(t, store, base.Add(3*time.Minute), 64, 80)
	observeBoth(t, store, base.Add(5*time.Minute), 90, 80)
	observeBoth(t, store, base.Add(16*time.Minute), 90, 80)
	if got := len(store.Document(base.Add(16 * time.Minute)).Events); got != 1 {
		t.Fatalf("got %d events", got)
	}
}

func TestObservationAppendsSamples(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store := New(Options{Directory: dir}, func() time.Time { return base })
	observeBoth(t, store, base, 50, 50)
	observeBoth(t, store, base.Add(time.Minute), 51, 51)
	data, err := os.ReadFile(dir + "/samples.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Fatalf("expected two appended rows, got %d", got)
	}
}

func TestCompactRetainsThirtyDayTenMinuteSamplesAndArchivesHourly(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh, grey := 70.0, 30.0
	old := base.Add(-31 * 24 * time.Hour)
	if err := writeNDJSON(filepath.Join(dir, "samples.ndjson"), []Point{
		{At: old, FreshPercent: &fresh, GreyPercent: &grey},
		{At: base.Add(-20 * time.Minute), FreshPercent: &fresh, GreyPercent: &grey},
		{At: base.Add(-19 * time.Minute), FreshPercent: &fresh, GreyPercent: &grey},
	}); err != nil {
		t.Fatalf("write samples: %v", err)
	}
	store := New(Options{Directory: dir, Retention: 30 * 24 * time.Hour}, func() time.Time { return base })
	if err := store.Compact(base); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "samples.ndjson"))
	if err != nil {
		t.Fatalf("read recent samples: %v", err)
	}
	if got := strings.Count(string(data), "\n"); got != 1 {
		t.Fatalf("expected one ten-minute recent sample, got %d: %s", got, data)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "hourly", "samples-*.ndjson"))
	if len(archives) != 1 {
		t.Fatalf("expected one hourly archive, got %v", archives)
	}
	archive, _ := os.ReadFile(archives[0])
	if !strings.Contains(string(archive), "2026-07-23T12:00:00Z") {
		t.Fatalf("expected old point in hourly archive, got %s", archive)
	}
}

func TestCompactPreservesWaterHourlyArchiveAcrossRuns(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh, grey := 70.0, 30.0
	old := base.Add(-31 * 24 * time.Hour)
	if err := writeNDJSON(filepath.Join(dir, "samples.ndjson"), []Point{{At: old, FreshPercent: &fresh, GreyPercent: &grey}}); err != nil {
		t.Fatalf("write samples: %v", err)
	}
	store := New(Options{Directory: dir, Retention: 30 * 24 * time.Hour}, func() time.Time { return base })
	if err := store.Compact(base); err != nil {
		t.Fatalf("first Compact: %v", err)
	}
	if err := store.Compact(base); err != nil {
		t.Fatalf("second Compact: %v", err)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "hourly", "samples-*.ndjson"))
	archive, _ := os.ReadFile(archives[0])
	if got := strings.Count(string(archive), "\n"); got != 1 {
		t.Fatalf("expected one archived point after repeated compaction, got %d", got)
	}
}

func TestCompactMergesSparseTankValuesInHourlyArchive(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh, grey := 70.0, 30.0
	if err := writeNDJSON(filepath.Join(dir, "samples.ndjson"), []Point{
		{At: base.Add(-31 * 24 * time.Hour), FreshPercent: &fresh},
		{At: base.Add(-31*24*time.Hour + 30*time.Minute), GreyPercent: &grey},
	}); err != nil {
		t.Fatalf("write samples: %v", err)
	}
	store := New(Options{Directory: dir, Retention: 30 * 24 * time.Hour}, func() time.Time { return base })
	if err := store.Compact(base); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "hourly", "samples-*.ndjson"))
	var points []Point
	if err := readNDJSON(archives[0], &points); err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if len(points) != 1 || points[0].FreshPercent == nil || points[0].GreyPercent == nil {
		t.Fatalf("expected both sparse tank values in one point, got %+v", points)
	}
}

func TestCompactPrunesInMemoryChartWithoutPersistence(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	fresh, grey := 70.0, 30.0
	store := New(Options{Retention: 30 * 24 * time.Hour}, func() time.Time { return base })
	if _, err := store.Observe(Sample{At: base.Add(-31 * 24 * time.Hour), FreshPercent: &fresh, GreyPercent: &grey}, base); err != nil {
		t.Fatalf("old Observe: %v", err)
	}
	if _, err := store.Observe(Sample{At: base, FreshPercent: &fresh, GreyPercent: &grey}, base); err != nil {
		t.Fatalf("recent Observe: %v", err)
	}
	if len(store.chart.samples) != 2 {
		t.Fatalf("expected two in-memory chart samples before compact, got %d", len(store.chart.samples))
	}
	if err := store.Compact(base); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(store.chart.samples) != 1 {
		t.Fatalf("expected old in-memory chart sample pruned, got %d", len(store.chart.samples))
	}
}

func TestDocumentBuildsAppendOnlyChartCache(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 80.4, 20.4)
	observeBoth(t, store, base.Add(2*time.Minute), 79.6, 21.6)
	doc := store.Document(base.Add(2 * time.Minute))
	if len(doc.ChartSamples) != 1 {
		t.Fatalf("expected one hourly chart sample, got %+v", doc.ChartSamples)
	}
	if got := *doc.ChartSamples[0].FreshPercent; math.Abs(got-80) > 0.001 {
		t.Fatalf("expected rounded five-minute average of fresh readings, got %v", got)
	}
	if got := *doc.ChartSamples[0].GreyPercent; math.Abs(got-21) > 0.001 {
		t.Fatalf("expected rounded five-minute average of grey readings, got %v", got)
	}

	observeBoth(t, store, base.Add(30*time.Minute), 70.4, 30.4)
	doc = store.Document(base.Add(30 * time.Minute))
	if len(doc.ChartSamples) != 1 {
		t.Fatalf("expected one hourly chart sample, got %+v", doc.ChartSamples)
	}
	if got := *doc.ChartSamples[0].FreshPercent; math.Abs(got-70) > 0.001 {
		t.Fatalf("expected latest five-minute average in the bucket, got %v", got)
	}
	if store.chart.processed != len(store.samples) {
		t.Fatalf("expected chart cache to process all samples, processed %d of %d", store.chart.processed, len(store.samples))
	}

	observeBoth(t, store, base.Add(61*time.Minute), 69.6, 31.6)
	store.Document(base.Add(61 * time.Minute))
	if store.chart.processed != len(store.samples) {
		t.Fatalf("expected cache to append-process the rolling raw window, processed %d of %d", store.chart.processed, len(store.samples))
	}
	if len(store.samples) != 1 {
		t.Fatalf("expected only the current five-minute raw window to remain, got %d samples", len(store.samples))
	}
}

func TestChartCacheSurvivesRestartWithoutRawHistory(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	options := Options{Directory: dir, Retention: 7 * 24 * time.Hour}
	store := New(options, func() time.Time { return base })
	observeBoth(t, store, base, 80, 20)
	observeBoth(t, store, base.Add(time.Minute), 81, 19)
	observeBoth(t, store, base.Add(30*time.Minute), 70, 30)
	want := store.Document(base.Add(30 * time.Minute))

	reloaded := New(options, func() time.Time { return base.Add(31 * time.Minute) })
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got := reloaded.Document(base.Add(31 * time.Minute))
	if len(got.ChartSamples) != len(want.ChartSamples) || *got.ChartSamples[0].FreshPercent != *want.ChartSamples[0].FreshPercent {
		t.Fatalf("chart cache changed across restart: got %+v want %+v", got.ChartSamples, want.ChartSamples)
	}
	if len(reloaded.samples) != 1 || reloaded.chart.processed != len(reloaded.samples) {
		t.Fatalf("expected only rolling raw samples after restart, got %d raw and processed=%d", len(reloaded.samples), reloaded.chart.processed)
	}
}

func TestRestartReplaysRawSamplesNewerThanChartWatermark(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	options := Options{Directory: dir, Retention: 7 * 24 * time.Hour}
	store := New(options, func() time.Time { return base })
	observeBoth(t, store, base, 50, 50)
	observeBoth(t, store, base.Add(time.Minute), 51, 49)

	// Simulate a crash after the raw append but before the chart cache write.
	fresh, grey := 52.0, 48.0
	if err := appendNDJSON(filepath.Join(dir, "samples.ndjson"), Point{At: base.Add(2 * time.Minute), FreshPercent: &fresh, GreyPercent: &grey}); err != nil {
		t.Fatal(err)
	}

	reloaded := New(options, func() time.Time { return base.Add(3 * time.Minute) })
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if !reloaded.chart.throughAt.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("chart watermark = %v, want %v", reloaded.chart.throughAt, base.Add(2*time.Minute))
	}
	if got := *reloaded.chart.samples[0].FreshPercent; math.Abs(got-51) > 0.001 {
		t.Fatalf("replayed chart value = %v, want 51", got)
	}
}

func TestIdenticalObservationsUseOneMinuteHeartbeats(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 50)
	observeBoth(t, store, base.Add(30*time.Second), 50, 50)
	observeBoth(t, store, base.Add(time.Minute), 50, 50)
	observeBoth(t, store, base.Add(90*time.Second), 50, 50)
	observeBoth(t, store, base.Add(2*time.Minute), 50, 50)

	if got := len(store.Document(base.Add(2 * time.Minute)).Samples); got != 3 {
		t.Fatalf("expected initial sample plus two one-minute heartbeats, got %d", got)
	}
}

func TestOppositeMovementClosesCandidate(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 56, 80)
	observeBoth(t, store, base.Add(2*time.Minute), 54, 80)
	observeBoth(t, store, base.Add(20*time.Minute), 54, 80)
	if got := len(store.Document(base.Add(20 * time.Minute)).Events); got != 0 {
		t.Fatalf("got %d events", got)
	}
}

func TestGreyLevelDropWithoutOpenLogsAndCreatesNoEvent(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	var logs []string
	store := New(Options{
		Directory:      t.TempDir(),
		Threshold:      5,
		SettlingPeriod: 10 * time.Minute,
		GroupingWindow: time.Hour,
		Retention:      7 * 24 * time.Hour,
		Logf: func(format string, args ...interface{}) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}, func() time.Time { return base })
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 50, 74)
	doc := store.Document(base.Add(time.Minute))
	if len(doc.Events) != 0 {
		t.Fatalf("unexpected grey heuristic event: %+v", doc.Events)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one anomaly log, got %v", logs)
	}
	if !strings.Contains(strings.ToLower(logs[0]), "grey") || !strings.Contains(strings.ToLower(logs[0]), "open") {
		t.Fatalf("unexpected log message: %q", logs[0])
	}
}

func TestGreyLevelCumulativeDropWithoutOpenLogsOnceAtThreshold(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	var logs []string
	store := New(Options{
		Directory:      t.TempDir(),
		Threshold:      5,
		SettlingPeriod: 10 * time.Minute,
		GroupingWindow: time.Hour,
		Retention:      7 * 24 * time.Hour,
		Logf: func(format string, args ...interface{}) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}, func() time.Time { return base })
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 50, 77)
	observeBoth(t, store, base.Add(2*time.Minute), 50, 74)
	observeBoth(t, store, base.Add(3*time.Minute), 50, 71)

	if got := len(store.Document(base.Add(3 * time.Minute)).Events); got != 0 {
		t.Fatalf("unexpected grey heuristic event: %+v", store.Document(base.Add(3*time.Minute)).Events)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one cumulative anomaly log, got %v", logs)
	}
	if !strings.Contains(logs[0], "80.0") || !strings.Contains(logs[0], "74.0") {
		t.Fatalf("expected cumulative log to reference threshold-crossing drop, got %q", logs[0])
	}
}

func TestGreyLevelRiseWithoutOpenIsNormal(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	var logs []string
	store := New(Options{
		Directory:      t.TempDir(),
		Threshold:      5,
		SettlingPeriod: 10 * time.Minute,
		GroupingWindow: time.Hour,
		Retention:      7 * 24 * time.Hour,
		Logf: func(format string, args ...interface{}) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}, func() time.Time { return base })
	observeBoth(t, store, base, 50, 20)
	observeBoth(t, store, base.Add(time.Minute), 50, 26)
	if len(store.Document(base.Add(time.Minute)).Events) != 0 {
		t.Fatalf("unexpected events after upward grey change: %+v", store.Document(base.Add(time.Minute)).Events)
	}
	if len(logs) != 0 {
		t.Fatalf("unexpected anomaly logs: %v", logs)
	}
}

func TestReconnectObservedEventUsesObservationTime(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	oldSource := base.Add(-time.Hour)
	fresh, grey := 56.0, 80.0
	if _, err := store.Observe(Sample{At: oldSource, FreshPercent: &fresh, GreyPercent: &grey}, base.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	doc := store.Document(base.Add(11 * time.Minute))
	if len(doc.Events) != 1 || !doc.Events[0].At.Equal(base.Add(11*time.Minute)) {
		t.Fatalf("unexpected event: %+v", doc.Events)
	}
}

func TestStartupLevelDoesNotCreateEvent(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 100, 0)
	observeBoth(t, store, base.Add(20*time.Minute), 100, 0)
	if got := len(store.Document(base.Add(20 * time.Minute)).Events); got != 0 {
		t.Fatalf("got %d events", got)
	}
}

func TestRestartLoadsSamplesAndEvents(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	options := Options{Directory: dir, Threshold: 5, SettlingPeriod: 10 * time.Minute, GroupingWindow: time.Hour, Retention: 7 * 24 * time.Hour}
	store := New(options, func() time.Time { return base })
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 56, 80)
	observeBoth(t, store, base.Add(11*time.Minute), 56, 80)
	reloaded := New(options, func() time.Time { return base.Add(12 * time.Minute) })
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	doc := reloaded.Document(base.Add(12 * time.Minute))
	if len(doc.Samples) != 1 || len(doc.Events) != 1 {
		t.Fatalf("unexpected reload: %+v", doc)
	}
}

func TestLoadSkipsCorruptNDJSONRecords(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh, grey := 50.0, 80.0
	sample, err := json.Marshal(Point{At: base, FreshPercent: &fresh, GreyPercent: &grey})
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(Event{At: base.Add(time.Hour), Tank: TankGrey, Kind: KindEmpty, From: 80, Used: 80})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "samples.ndjson"), []byte(string(sample)+"\n\x00corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(string(event)+"\n\x00corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := New(Options{Directory: dir}, func() time.Time { return base.Add(2 * time.Hour) })
	if err := store.Load(); err != nil {
		t.Fatalf("Load should tolerate corrupt records: %v", err)
	}
	doc := store.Document(base.Add(2 * time.Hour))
	if len(doc.Samples) != 1 || len(doc.Events) != 1 {
		t.Fatalf("expected valid records to survive corrupt records, got %d samples and %d events", len(doc.Samples), len(doc.Events))
	}
	for _, name := range []string{"samples.ndjson", "events.ndjson"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "corrupt") || strings.ContainsRune(string(data), '\x00') {
			t.Fatalf("%s still contains corrupt data: %q", name, data)
		}
	}
}

func TestLoadRepairsCorruptUnterminatedTailBeforeAppend(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh, grey := 50.0, 80.0
	sample, err := json.Marshal(Point{At: base, FreshPercent: &fresh, GreyPercent: &grey})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "samples.ndjson"), []byte(string(sample)+"\n{\"t\":\"2026-08-23T12:01:00Z\""), 0o644); err != nil {
		t.Fatal(err)
	}

	store := New(Options{Directory: dir}, func() time.Time { return base.Add(2 * time.Minute) })
	if err := store.Load(); err != nil {
		t.Fatalf("Load should tolerate and repair a truncated record: %v", err)
	}
	newFresh, newGrey := 51.0, 80.0
	if _, err := store.Observe(Sample{At: base.Add(2 * time.Minute), FreshPercent: &newFresh, GreyPercent: &newGrey}, base.Add(2*time.Minute)); err != nil {
		t.Fatalf("append after repair: %v", err)
	}

	reloaded := New(Options{Directory: dir}, func() time.Time { return base.Add(3 * time.Minute) })
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reload after append: %v", err)
	}
	if got := len(reloaded.Document(base.Add(3 * time.Minute)).Samples); got != 2 {
		t.Fatalf("expected original and appended samples after repair, got %d", got)
	}
}

func TestLoadCompactsIdenticalSamples(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh, grey := 50.0, 50.0
	if err := writeNDJSON(dir+"/samples.ndjson", []Point{
		{At: base, FreshPercent: &fresh, GreyPercent: &grey},
		{At: base.Add(30 * time.Second), FreshPercent: &fresh, GreyPercent: &grey},
		{At: base.Add(time.Minute), FreshPercent: &fresh, GreyPercent: &grey},
	}); err != nil {
		t.Fatal(err)
	}
	store := New(Options{Directory: dir}, func() time.Time { return base.Add(time.Minute) })
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Document(base.Add(time.Minute)).Samples); got != 2 {
		t.Fatalf("expected duplicate legacy sample to be compacted, got %d", got)
	}
}

func TestGroupedMarkersKeepIndependentEvents(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	if _, err := store.RecordGreyDischargeOpen(base.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(base.Add(11 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	observeBoth(t, store, base.Add(time.Minute), 56, 80)
	observeBoth(t, store, base.Add(11*time.Minute), 56, 80)
	doc := store.Document(base.Add(11 * time.Minute))
	if len(doc.Events) != 2 || len(doc.Markers) != 1 || len(doc.Markers[0].Events) != 2 {
		t.Fatalf("unexpected grouping: %+v", doc)
	}
}

func testStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	return New(Options{Directory: t.TempDir(), Threshold: 5, SettlingPeriod: 10 * time.Minute, GroupingWindow: time.Hour, Retention: 7 * 24 * time.Hour}, func() time.Time { return now })
}

func observeBoth(t *testing.T, store *Store, at time.Time, freshValue, greyValue float64) {
	t.Helper()
	fresh, grey := freshValue, greyValue
	if _, err := store.Observe(Sample{At: at, FreshPercent: &fresh, GreyPercent: &grey}, at); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryUsage(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	if _, err := store.RecordGreyDischargeOpen(base.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordGreyEmpty(base.Add(11 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	observeBoth(t, store, base.Add(time.Minute), 56, 80)
	observeBoth(t, store, base.Add(11*time.Minute), 56, 80)
	fresh, grey := 40.0, 16.0
	if _, err := store.Observe(Sample{At: base.Add(12 * time.Minute), FreshPercent: &fresh, GreyPercent: &grey}, base.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	doc := store.Document(base.Add(12 * time.Minute))
	if doc.Fresh.UsedPercent == nil || math.Abs(*doc.Fresh.UsedPercent-16) > 0.001 || doc.Grey.UsedPercent == nil || math.Abs(*doc.Grey.UsedPercent-16) > 0.001 {
		t.Fatalf("unexpected summaries: %+v", doc)
	}
}

func TestFreshPredictionUsesTwelveHourLinearFit(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base.Add(12*time.Hour))
	store.events = []Event{{At: base, Tank: TankFresh, Kind: KindFill, To: 80}}
	current := 60.0
	store.state.Fresh = &current
	first, middle, last := 80.0, 70.0, 60.0
	store.samples = []Point{
		{At: base, FreshPercent: &first},
		{At: base.Add(6 * time.Hour), FreshPercent: &middle},
		{At: base.Add(12 * time.Hour), FreshPercent: &last},
	}

	doc := store.Document(base.Add(12 * time.Hour))
	if got, want := doc.Fresh.Prediction, "Based on 12 hours fresh water usage data, predict 10% in 1 day 6 hours"; got != want {
		t.Fatalf("prediction = %q, want %q", got, want)
	}
}
