package runtime

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	"empirebus-tests/service/recording"
)

func TestAppStopsRecordingOnContextShutdown(t *testing.T) {
	dir := t.TempDir()
	previousDir := recordingDirectory
	recordingDirectory = dir
	t.Cleanup(func() { recordingDirectory = previousDir })

	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	app, err := New(rootCtx, testRecordingConfig(), "", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := app.Broker().Subscribe()
	t.Cleanup(unsubscribe)

	state, err := app.StartRecording(context.Background(), recording.StartRequest{
		WaitFor:         recording.WaitImmediate,
		DurationMinutes: 1,
	})
	if err != nil || state.Status != "recording" {
		t.Fatalf("start = %#v, %v", state, err)
	}
	if got := app.RecordingState().Status; got != "recording" {
		t.Fatalf("recording state = %q", got)
	}
	waitForRecordingStatus(t, events, "recording")

	cancel()
	waitForRecordingStatus(t, events, "idle")

	path := filepath.Join(dir, state.FileName)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > 0 && strings.Contains(lines[len(lines)-1], `"event":"service_shutdown"`) {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("recording %q did not end with service_shutdown", path)
}

func waitForRecordingStatus(t *testing.T, stream <-chan events.Event, want string) {
	t.Helper()
	timeout := time.After(time.Second)
	for {
		select {
		case event := <-stream:
			if event.Type != "recording.state_changed" {
				continue
			}
			state, ok := event.Payload.(recording.State)
			if ok && state.Status == want {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for recording status %q", want)
		}
	}
}

func testRecordingConfig() config.Config {
	enabled := true
	return config.Config{
		Garmin: config.GarminConfig{
			WSURL:             "ws://127.0.0.1:1/ws",
			HeartbeatInterval: time.Hour,
		},
		Automation: config.AutomationConfig{
			Timezone: "UTC",
			HeatingPrograms: []config.HeatingProgramConfig{{
				ID:      "test",
				Enabled: &enabled,
				Days:    []string{"monday"},
				Periods: []config.HeatingPeriodConfig{{Start: "00:00", Mode: "off"}},
			}},
		},
		API: config.APIConfig{Listen: "127.0.0.1:0"},
	}
}
