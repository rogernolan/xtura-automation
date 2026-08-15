package host

import (
	"context"
	"errors"
	"log"
	"sync"
	"testing"
	"time"
)

var fixedNow = func() time.Time {
	return time.Date(2026, 8, 14, 9, 40, 0, 0, time.UTC)
}

func testSnapshot() Snapshot {
	return Snapshot{
		Model:         "Raspberry Pi Zero 2 W",
		Cores:         4,
		Load:          [3]float64{0.5, 0.3, 0.2},
		UptimeSeconds: 100,
		Power:         PowerStatus{Status: "ok"},
	}
}

func collectPublished(t *testing.T, manager *Manager) (*sync.Mutex, *[]Metrics) {
	t.Helper()
	var mu sync.Mutex
	var published []Metrics
	manager.SetOnChange(func(metrics Metrics) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, metrics)
	})
	return &mu, &published
}

func TestManagerPublishesFirstSample(t *testing.T) {
	manager := New(time.Second, func() (Snapshot, error) { return testSnapshot(), nil }, fixedNow, log.Default())
	mu, published := collectPublished(t, manager)

	manager.Sample()

	mu.Lock()
	defer mu.Unlock()
	if len(*published) != 1 {
		t.Fatalf("published = %d events, want 1", len(*published))
	}
	if (*published)[0].SampledAt != fixedNow() {
		t.Fatalf("sampled at = %v", (*published)[0].SampledAt)
	}
	if (*published)[0].Model != "Raspberry Pi Zero 2 W" || (*published)[0].Cores != 4 {
		t.Fatalf("published metrics = %#v", (*published)[0])
	}
}

func TestManagerSuppressesUnchangedSnapshot(t *testing.T) {
	manager := New(time.Second, func() (Snapshot, error) { return testSnapshot(), nil }, fixedNow, log.Default())
	mu, published := collectPublished(t, manager)

	manager.Sample()
	manager.Sample()

	mu.Lock()
	defer mu.Unlock()
	if len(*published) != 1 {
		t.Fatalf("published = %d events, want 1", len(*published))
	}
}

func TestManagerPublishesChangedSnapshot(t *testing.T) {
	uptime := uint64(100)
	manager := New(time.Second, func() (Snapshot, error) {
		snap := testSnapshot()
		snap.UptimeSeconds = uptime
		return snap, nil
	}, fixedNow, log.Default())
	mu, published := collectPublished(t, manager)

	manager.Sample()
	uptime = 101
	manager.Sample()

	mu.Lock()
	defer mu.Unlock()
	if len(*published) != 2 {
		t.Fatalf("published = %d events, want 2", len(*published))
	}
}

func TestManagerRecordsErrorAndKeepsSnapshot(t *testing.T) {
	fail := false
	readErr := errors.New("read failed")
	manager := New(time.Second, func() (Snapshot, error) {
		if fail {
			return Snapshot{}, readErr
		}
		return testSnapshot(), nil
	}, fixedNow, log.Default())

	manager.Sample()
	fail = true
	manager.Sample()

	state := manager.State()
	if state.LastError != "read failed" {
		t.Fatalf("last error = %q", state.LastError)
	}
	if state.LastErrorAt == nil || *state.LastErrorAt != fixedNow() {
		t.Fatalf("last error at = %v", state.LastErrorAt)
	}
	if state.Model != "Raspberry Pi Zero 2 W" {
		t.Fatalf("previous snapshot not preserved: %#v", state)
	}
}

func TestManagerStateBeforeFirstSample(t *testing.T) {
	manager := New(time.Second, func() (Snapshot, error) { return testSnapshot(), nil }, fixedNow, log.Default())
	state := manager.State()
	if !state.SampledAt.IsZero() {
		t.Fatalf("sampled at before first sample = %v", state.SampledAt)
	}
}

func TestManagerStartSamplesOnInterval(t *testing.T) {
	var mu sync.Mutex
	count := 0
	manager := New(10*time.Millisecond, func() (Snapshot, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return testSnapshot(), nil
	}, fixedNow, log.Default())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager.Start(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := count
		mu.Unlock()
		if got >= 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := count
	mu.Unlock()
	t.Fatalf("samples = %d, want >= 3", got)
}
