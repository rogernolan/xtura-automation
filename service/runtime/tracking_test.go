package runtime

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	"empirebus-tests/service/tracking"
)

func TestAppTrackingWiring(t *testing.T) {
	server := newGPSServer(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	trackDir := t.TempDir()
	app := newTrackingTestApp(t, server.URL, configPath, trackDir)

	events, unsubscribe := app.Broker().Subscribe()
	t.Cleanup(unsubscribe)

	waitForCondition(t, "location fix", func() bool {
		return app.LocationState().Known
	})
	if got := app.LocationState().Altitude; got == nil || *got != 120.5 {
		t.Fatalf("location altitude = %v, want 120.5", got)
	}
	settings := app.TrackingSettings()
	if !settings.Enabled || settings.OnlyWhenEngineOn || settings.SampleInterval != time.Second {
		t.Fatalf("tracking settings = %#v", settings)
	}
	if got := app.TrackingDirectory(); got != trackDir {
		t.Fatalf("tracking directory = %q, want %q", got, trackDir)
	}
	waitForTrackingStateEvent(t, events)
	waitForCondition(t, "second tracking sample", func() bool {
		return app.TrackingState().PointCount >= 2
	})
	infos, err := app.TrackList()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("track list = %d entries, want 1", len(infos))
	}
	if infos[0].PointCount < 2 {
		t.Fatalf("track point count = %d", infos[0].PointCount)
	}
	data, err := app.TrackRead(infos[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "LineString") {
		t.Fatalf("track file %s missing LineString: %s", infos[0].Name, data)
	}
}

func TestUpdateTrackingSettings(t *testing.T) {
	server := newGPSServer(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	app := newTrackingTestApp(t, server.URL, configPath, t.TempDir())

	updated, err := app.UpdateTrackingSettings(context.Background(), tracking.Settings{
		Enabled:          true,
		OnlyWhenEngineOn: false,
		SampleInterval:   2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SampleInterval != 2*time.Second || !updated.Enabled || updated.OnlyWhenEngineOn {
		t.Fatalf("updated settings = %#v", updated)
	}
	if got := app.TrackingSettings(); got.SampleInterval != 2*time.Second {
		t.Fatalf("app settings sample interval = %s", got.SampleInterval)
	}
	if got := app.TrackingState().SampleIntervalSeconds; got != 2.0 {
		t.Fatalf("manager sample interval seconds = %v", got)
	}
	persisted, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), "sample_interval: 2s") {
		t.Fatalf("config does not contain sample_interval: 2s:\n%s", persisted)
	}
	if _, err := app.UpdateTrackingSettings(context.Background(), tracking.Settings{
		Enabled:        true,
		SampleInterval: 2 * time.Hour,
	}); err == nil {
		t.Fatal("expected validation error for out-of-range sample interval")
	}
	persistedAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(persistedAfter) != string(persisted) {
		t.Fatalf("config changed after rejected update:\n%s", persistedAfter)
	}

	zeroed, err := app.UpdateTrackingSettings(context.Background(), tracking.Settings{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if zeroed.SampleInterval != 5*time.Second {
		t.Fatalf("zero interval should normalize to 5s, got %s", zeroed.SampleInterval)
	}
	if got := app.TrackingState().SampleIntervalSeconds; got != 5.0 {
		t.Fatalf("manager sample interval seconds = %v, want 5.0", got)
	}
}

func TestConcurrentConfigWritersSerialize(t *testing.T) {
	server := newGPSServer(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	app := newTrackingTestApp(t, server.URL, configPath, t.TempDir())

	for i := 0; i < 10; i++ {
		interval := time.Duration(5+i) * time.Second
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = app.UpdateTrackingSettings(context.Background(), tracking.Settings{
				Enabled:        true,
				SampleInterval: interval,
			})
		}()
		go func() {
			defer wg.Done()
			_, _ = app.UpdateHeatingSchedule(context.Background(), config.HeatingScheduleDocument{
				Timezone: "UTC",
				Programs: []config.HeatingScheduleProgramDocument{{
					ID:      "concurrent",
					Enabled: true,
					Days:    []string{"tuesday"},
					Periods: []config.HeatingSchedulePeriodDocument{{Start: "00:00", Mode: "off"}},
				}},
			})
		}()
		wg.Wait()
		persisted, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(persisted), "sample_interval:") {
			t.Fatalf("tracking change lost after concurrent writes:\n%s", persisted)
		}
		if !strings.Contains(string(persisted), "concurrent") {
			t.Fatalf("heating change lost after concurrent writes:\n%s", persisted)
		}
	}
}

func TestTrackingMethodsWithoutLocation(t *testing.T) {
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	app, err := New(rootCtx, testRecordingConfig(), "", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := app.TrackingSettings(); got != (tracking.Settings{}) {
		t.Fatalf("tracking settings = %#v", got)
	}
	if got := app.TrackingState(); got != (tracking.State{}) {
		t.Fatalf("tracking state = %#v", got)
	}
	infos, err := app.TrackList()
	if err != nil || len(infos) != 0 {
		t.Fatalf("track list = %#v, %v", infos, err)
	}
	if _, err := app.TrackRead("track-2026-01-01.geojson"); err == nil {
		t.Fatal("expected TrackRead error without tracking manager")
	}
	if err := app.TrackDelete("track-2026-01-01.geojson"); err == nil {
		t.Fatal("expected TrackDelete error without tracking manager")
	}
}

func newTrackingTestApp(t *testing.T, serverURL, configPath, trackDir string) *App {
	t.Helper()
	onlyWhenEngineOn := false
	cfg := config.Config{
		Garmin: config.GarminConfig{
			WSURL:             "ws://127.0.0.1:1/ws",
			HeartbeatInterval: time.Hour,
		},
		Location: config.LocationConfig{
			Enabled:  true,
			Provider: "rutx50",
			RUTX50: config.RUTX50LocationConfig{
				Endpoint:  serverURL,
				AuthToken: "test-token",
				Timeout:   2 * time.Second,
			},
			Timezone: config.TimezoneLookupConfig{Provider: "none"},
		},
		Tracking: config.TrackingConfig{
			Enabled:          true,
			OnlyWhenEngineOn: &onlyWhenEngineOn,
			SampleInterval:   time.Second,
			Dir:              trackDir,
		},
		Automation: config.AutomationConfig{
			Timezone: "UTC",
			HeatingPrograms: []config.HeatingProgramConfig{{
				ID:      "test",
				Enabled: &onlyWhenEngineOn,
				Days:    []string{"monday"},
				Periods: []config.HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: config.APIConfig{Listen: "127.0.0.1:0"},
	}
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	app, err := New(rootCtx, cfg, configPath, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func newGPSServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"gps":{"latitude":"45.0","longitude":"9.0","altitude":"120.5"}}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func waitForCondition(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func waitForTrackingStateEvent(t *testing.T, stream <-chan events.Event) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-stream:
			if event.Type != "tracking.state_changed" {
				continue
			}
			state, ok := event.Payload.(tracking.State)
			if ok && state.Enabled {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for tracking.state_changed event")
		}
	}
}
