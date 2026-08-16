package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCapture(t *testing.T, path string, lines ...string) {
	t.Helper()
	data := ""
	for _, line := range lines {
		data += line + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseCaptureSkipsEventsAndSendsAndCapsGaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.ndjson")
	writeCapture(t, path,
		`{"at":"2026-08-15T14:00:00Z","direction":"event","event":"recording_started"}`,
		`{"at":"2026-08-15T14:00:00Z","direction":"receive","message":"{\"data\":[101,0,1,0,0,0,0,0],\"messagecmd\":5,\"messagetype\":16,\"size\":8}"}`,
		`{"at":"2026-08-15T14:00:00.5Z","direction":"receive","message":"{\"data\":[4,0,1,0,0,0,0,0],\"messagecmd\":5,\"messagetype\":16,\"size\":8}"}`,
		`{"at":"2026-08-15T14:00:00.7Z","direction":"send","message":"{\"messagetype\":96,\"messagecmd\":0,\"size\":1,\"data\":[0]}"}`,
		`{"at":"2026-08-15T14:00:05Z","direction":"receive","message":"{\"data\":[5,0,0,0,0,0,0,0],\"messagecmd\":5,\"messagetype\":16,\"size\":8}"}`,
	)

	items, err := parseCapture(path, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[0].delay != 0 {
		t.Fatalf("first delay = %v, want 0", items[0].delay)
	}
	if items[1].delay != 500*time.Millisecond {
		t.Fatalf("second delay = %v, want 500ms", items[1].delay)
	}
	if items[2].delay != 1*time.Second {
		t.Fatalf("third delay = %v, want capped 1s", items[2].delay)
	}
	if items[2].message == "" || items[1].message == "" {
		t.Fatal("expected raw messages to be preserved")
	}
}

func TestParseCaptureFallsBackToFrameField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.ndjson")
	writeCapture(t, path,
		`{"at":"2026-08-15T14:00:00Z","direction":"receive","frame":{"messagetype":16,"messagecmd":5,"size":8,"data":[105,0,0,22,30,121,4,0]}}`,
	)

	items, err := parseCapture(path, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].message == "" {
		t.Fatal("expected a message re-marshalled from the frame field")
	}
}
