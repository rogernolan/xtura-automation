package recording_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"empirebus-tests/heating"
	"empirebus-tests/service/recording"
)

func TestManagerStartsImmediatelyAndWritesWebSocketRecords(t *testing.T) {
	dir := t.TempDir()
	manager := recording.New(dir, time.Now, log.New(io.Discard, "", 0))

	state, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1})
	if err != nil || state.Status != "recording" {
		t.Fatalf("start = %#v, %v", state, err)
	}
	manager.Observe(time.Date(2026, 8, 12, 14, 30, 0, 0, time.UTC), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[101,0,1]}`)
	manager.Stop("stopped")

	records := readNDJSON(t, filepath.Join(dir, state.FileName))
	if records[1]["direction"] != "receive" || records[1]["signal"].(float64) != 101 {
		t.Fatal(records)
	}
}

func TestManagerStartsOnlyAfterNewMatchingOnFrame(t *testing.T) {
	manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
	if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitVictronOn, DurationMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	manager.Observe(time.Now(), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[196,0,1]}`)
	if got := manager.State().Status; got != "armed" {
		t.Fatalf("status = %q", got)
	}
	manager.Observe(time.Now(), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[197,0,1]}`)
	if got := manager.State().Status; got != "recording" {
		t.Fatalf("status = %q", got)
	}
}

func TestStopOverridesArmedStateAndIsIdempotent(t *testing.T) {
	manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
	if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitEngineOn, DurationMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	manager.Stop("stopped")
	if got := manager.Stop("stopped").Status; got != "idle" {
		t.Fatalf("status = %q", got)
	}
}

func TestZeroDurationHasNoTimer(t *testing.T) {
	manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
	if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 0}); err != nil {
		t.Fatal(err)
	}
	if got := manager.State().Status; got != "recording" {
		t.Fatalf("status = %q", got)
	}
}

func TestTimeoutStopsRecording(t *testing.T) {
	manager := recording.NewWithTimeout(t.TempDir(), time.Now, time.Millisecond, log.New(io.Discard, "", 0))
	if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if manager.State().Status == "idle" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("status = %q", manager.State().Status)
}

func TestShutdownWritesServiceShutdown(t *testing.T) {
	manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
	state, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1})
	if err != nil {
		t.Fatal(err)
	}
	manager.Shutdown()
	if got := lastEvent(t, filepath.Join(manager.Dir(), state.FileName)); got != "service_shutdown" {
		t.Fatalf("event = %q", got)
	}
}

func TestStartRejectsInvalidRequestAndSecondActiveRecorder(t *testing.T) {
	manager := recording.New(t.TempDir(), time.Now, log.New(io.Discard, "", 0))
	if _, err := manager.Start(recording.StartRequest{WaitFor: "bad", DurationMinutes: 1}); err == nil {
		t.Fatal("expected wait validation error")
	}
	if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(recording.StartRequest{WaitFor: recording.WaitImmediate, DurationMinutes: 1}); !errors.Is(err, recording.ErrActive) {
		t.Fatalf("err = %v", err)
	}
}

func readNDJSON(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var records []map[string]interface{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func lastEvent(t *testing.T, path string) string {
	t.Helper()
	records := readNDJSON(t, path)
	if len(records) == 0 {
		t.Fatal("no records")
	}
	event, _ := records[len(records)-1]["event"].(string)
	return event
}
