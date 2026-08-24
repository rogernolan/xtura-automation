package waterhistory

import (
	"math"
	"os"
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

func TestFillRequiresFivePointsAndTenMinutesSettled(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 56, 80)
	if got := len(store.Document(base.Add(5 * time.Minute)).Events); got != 0 {
		t.Fatalf("event before settling: %d", got)
	}
	observeBoth(t, store, base.Add(11*time.Minute), 56, 80)
	doc := store.Document(base.Add(11 * time.Minute))
	if len(doc.Events) != 1 || doc.Events[0].Kind != KindFill {
		t.Fatalf("unexpected events: %+v", doc.Events)
	}
}

func TestGreyEmptyRequiresFivePointsAndTenMinutesSettled(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(time.Minute), 50, 74)
	observeBoth(t, store, base.Add(11*time.Minute), 50, 74)
	doc := store.Document(base.Add(11 * time.Minute))
	if len(doc.Events) != 1 || doc.Events[0].Kind != KindEmpty {
		t.Fatalf("unexpected events: %+v", doc.Events)
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

func TestSubthresholdMovementDoesNotCreateEvent(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 50, 80)
	observeBoth(t, store, base.Add(20*time.Minute), 54, 80)
	if got := len(store.Document(base.Add(31 * time.Minute)).Events); got != 0 {
		t.Fatalf("got %d events", got)
	}
}

func TestNormalUsageAdvancesSettledBaseline(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	observeBoth(t, store, base, 100, 0)
	observeBoth(t, store, base.Add(time.Minute), 20, 80)
	observeBoth(t, store, base.Add(2*time.Minute), 90, 70)
	observeBoth(t, store, base.Add(13*time.Minute), 90, 70)
	doc := store.Document(base.Add(13 * time.Minute))
	if len(doc.Events) != 2 {
		t.Fatalf("expected fill and empty after normal usage, got %+v", doc.Events)
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
	if store.chart.processed != len(store.samples) || store.chart.processed != 4 {
		t.Fatalf("expected cache to append-process only the new sample, processed %d of %d", store.chart.processed, len(store.samples))
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
	if len(doc.Samples) != 3 || len(doc.Events) != 1 {
		t.Fatalf("unexpected reload: %+v", doc)
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
	observeBoth(t, store, base.Add(time.Minute), 56, 74)
	observeBoth(t, store, base.Add(11*time.Minute), 56, 74)
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
	observeBoth(t, store, base.Add(time.Minute), 56, 74)
	observeBoth(t, store, base.Add(11*time.Minute), 56, 74)
	fresh, grey := 40.0, 90.0
	if _, err := store.Observe(Sample{At: base.Add(12 * time.Minute), FreshPercent: &fresh, GreyPercent: &grey}, base.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	doc := store.Document(base.Add(12 * time.Minute))
	if doc.Fresh.UsedPercent == nil || math.Abs(*doc.Fresh.UsedPercent-16) > 0.001 || doc.Grey.UsedPercent == nil || math.Abs(*doc.Grey.UsedPercent-16) > 0.001 {
		t.Fatalf("unexpected summaries: %+v", doc)
	}
}
