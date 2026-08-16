package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"empirebus-tests/heating"

	"github.com/gorilla/websocket"
)

func testServer(t *testing.T, capturePath string) string {
	t.Helper()
	items, err := parseCapture(capturePath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		capture: items,
		loop:    false,
		speed:   1,
		verbose: false,
		logger:  log.New(io.Discard, "", 0),
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func TestServerReplaysCaptureAndEchoesCommands(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.ndjson")
	data := strings.Join([]string{
		`{"at":"2026-08-15T14:00:00Z","direction":"event","event":"recording_started"}`,
		`{"at":"2026-08-15T14:00:00Z","direction":"receive","message":"{\"data\":[101,0,1,0,0,0,0,0],\"messagecmd\":5,\"messagetype\":16,\"size\":8}"}`,
		`{"at":"2026-08-15T14:00:00.2Z","direction":"receive","message":"{\"data\":[102,0,0,0,0,0,0,0],\"messagecmd\":5,\"messagetype\":16,\"size\":8}"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(capturePath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	wsURL := testServer(t, capturePath)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":96,"messagecmd":0,"size":2,"data":[0,0]}`))
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":17,"messagecmd":0,"size":3,"data":[101,0,3]}`))

	type frame struct {
		MessageType int   `json:"messagetype"`
		MessageCmd  int   `json:"messagecmd"`
		Data        []int `json:"data"`
	}
	var sawPowerOn, sawBusyIdle, sawSeed bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var f frame
		if err := json.Unmarshal(payload, &f); err != nil {
			continue
		}
		if len(f.Data) < 3 {
			continue
		}
		switch f.Data[0] {
		case 101:
			if f.Data[2] == 1 {
				sawPowerOn = true
			}
		case 102:
			if f.Data[2] == 0 {
				sawBusyIdle = true
			}
		case 105:
			if temp, ok := heating.DecodeTargetTemperature(f.Data); ok && temp == 20.0 {
				sawSeed = true
			}
		}
	}
	if !sawPowerOn || !sawBusyIdle || !sawSeed {
		t.Fatalf("missing expected frames: powerOn=%t busyIdle=%t seed=%t", sawPowerOn, sawBusyIdle, sawSeed)
	}
}

func TestServerConcurrentReplayAndCommands(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.ndjson")
	start := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	lines := []string{`{"at":"2026-08-15T14:00:00Z","direction":"event","event":"recording_started"}`}
	for i := 0; i < 300; i++ {
		at := start.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
		lines = append(lines, fmt.Sprintf(
			`{"at":%q,"direction":"receive","message":"{\"data\":[102,0,0,0,0,0,0,0],\"messagecmd\":5,\"messagetype\":16,\"size\":8}"}`,
			at))
	}
	if err := os.WriteFile(capturePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := parseCapture(capturePath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		capture: items,
		loop:    true,
		speed:   50,
		verbose: false,
		logger:  log.New(io.Discard, "", 0),
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	command := []byte(`{"messagetype":17,"messagecmd":0,"size":3,"data":[101,0,3]}`)
	go func() {
		for i := 0; i < 2000; i++ {
			if err := conn.WriteMessage(websocket.TextMessage, command); err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	_ = conn.SetReadDeadline(deadline)
	var lastErr error
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			lastErr = err
			break
		}
	}
	if time.Now().Before(deadline) {
		t.Fatalf("connection dropped while replay and command writes were in flight: %v", lastErr)
	}
}
