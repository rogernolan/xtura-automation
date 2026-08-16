# Staging Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a local simulated Garmin SERV environment on the Mac (`servsim`) plus a parallel staging environment on the Jones Pi, both deployable and testable before production.

**Architecture:** A new `cmd/servsim` binary plays the SERV side of the Garmin websocket protocol: it replays a recorded NDJSON capture for background state and echoes command state for a small set of controlled signals so command APIs complete. The Pi staging environment reuses the existing deploy scripts, parameterized by an `ENVIRONMENT` env var that swaps install root, config path, systemd unit, and health URL. No production service runtime behavior changes.

**Tech Stack:** Go 1.25 (`github.com/gorilla/websocket`), bash scripts, systemd. Module `empirebus-tests`.

## Global Constraints

- Go module `empirebus-tests`, Go `1.25.0`.
- `gorilla/websocket v1.5.3` is an existing dependency — do not add others.
- Keep production `deploy-on-pi.sh` behavior byte-for-byte unchanged when `ENVIRONMENT` is unset (default `prod`).
- No changes to `service/runtime`, `service/api`, `service/config` behavior. The only non-`cmd/servsim` production-package change is a pure additive export in `heating` (Task 1).
- SERV state frames use the observed shape `{"messagetype":16,"messagecmd":5,"size":8,"data":[<sigLo>,<sigHi>,<value>,0,0,0,0,0]}`.
- Signal `105` target temperature payload layout: `data[0..1]` = `105,0`, `data[3]` = `22`, `data[4..7]` = little-endian `int32` millikelvin absolute temp (`(celsius+273.15)*1000`).
- All tests must pass with `go test ./...` from the repo root.
- No web/JS changes (`web/static/app.js` untouched); the UI uses relative `/v1/...` paths so it works on any port.
- AGENTS.md: updates to `docs/garmin-empirbus-signals.md` must label servsim behavior as simulation/inference, not browser-confirmed evidence.

---

### Task 1: Export `DecodeTargetTemperature` from the heating package

**Files:**
- Modify: `heating/state.go` (add exported function after `decodeTargetTemperature`)
- Test: `heating/heating_test.go`

**Interfaces:**
- Produces: `heating.DecodeTargetTemperature(data []int) (float64, bool)` — decodes a signal `105` payload to the displayed 0.5 °C-grid temperature; `ok=false` when the payload is not a valid signal `105` frame. Later tasks (servsim echo model) depend on this exact signature.

- [ ] **Step 1: Write the failing test**

Append to `heating/heating_test.go`:

```go
func TestDecodeTargetTemperatureExported(t *testing.T) {
	t.Parallel()
	temp, ok := DecodeTargetTemperature([]int{105, 0, 0, 22, 12, 74, 4, 0})
	if !ok {
		t.Fatal("decode failed")
	}
	if temp != 8.0 {
		t.Fatalf("got %.1f want 8.0", temp)
	}
	if _, ok := DecodeTargetTemperature([]int{105, 0, 0, 21, 12, 74, 4, 0}); ok {
		t.Fatal("expected ok=false when data[3] is not 22")
	}
	if _, ok := DecodeTargetTemperature([]int{107, 0, 0, 22, 12, 74, 4, 0}); ok {
		t.Fatal("expected ok=false when the signal id is not 105")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./heating/ -run TestDecodeTargetTemperatureExported -v`
Expected: FAIL — `undefined: DecodeTargetTemperature`.

- [ ] **Step 3: Implement the exported function**

In `heating/state.go`, directly after the existing `decodeTargetTemperature` function, add:

```go
// DecodeTargetTemperature extracts the displayed setpoint from a signal 105
// payload, rounding to the observed 0.5 C grid. ok is false when the payload
// is not a valid signal 105 frame.
func DecodeTargetTemperature(data []int) (float64, bool) {
	_, tempC, ok := decodeTargetTemperature(data)
	return tempC, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./heating/ -run TestDecodeTargetTemperatureExported -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add heating/state.go heating/heating_test.go
git commit -m "feat: export DecodeTargetTemperature from heating"
```

---

### Task 2: NDJSON capture parsing for servsim replay

**Files:**
- Create: `cmd/servsim/capture.go`
- Test: `cmd/servsim/capture_test.go`

**Interfaces:**
- Consumes: `heating.WireFrame` (json tags `messagetype`, `messagecmd`, `size`, `data`) for frame re-marshalling when a record has no raw `message`.
- Produces: `type replayItem struct { delay time.Duration; message string }` and `func parseCapture(path string, maxGap time.Duration) ([]replayItem, error)`. Only `direction=="receive"` records are kept; gaps between consecutive kept records are capped at `maxGap`; negative gaps become 0. Later tasks consume `[]replayItem`.

- [ ] **Step 1: Write the failing test**

Create `cmd/servsim/capture_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/servsim/ -run 'TestParseCapture' -v`
Expected: FAIL — `undefined: parseCapture`.

- [ ] **Step 3: Implement `capture.go`**

Create `cmd/servsim/capture.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"empirebus-tests/heating"
)

type captureRecord struct {
	At        time.Time          `json:"at"`
	Direction string             `json:"direction"`
	Message   string             `json:"message,omitempty"`
	Frame     *heating.WireFrame `json:"frame,omitempty"`
}

type replayItem struct {
	delay   time.Duration
	message string
}

func parseCapture(path string, maxGap time.Duration) ([]replayItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	var items []replayItem
	var prev time.Time
	for {
		var rec captureRecord
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode capture record: %w", err)
		}
		if rec.Direction != "receive" {
			continue
		}
		message := rec.Message
		if message == "" && rec.Frame != nil {
			raw, err := json.Marshal(rec.Frame)
			if err != nil {
				return nil, fmt.Errorf("marshal capture frame: %w", err)
			}
			message = string(raw)
		}
		if message == "" {
			continue
		}
		var delay time.Duration
		if !prev.IsZero() && !rec.At.IsZero() {
			delay = rec.At.Sub(prev)
			if delay < 0 {
				delay = 0
			}
			if maxGap > 0 && delay > maxGap {
				delay = maxGap
			}
		}
		if !rec.At.IsZero() {
			prev = rec.At
		}
		items = append(items, replayItem{delay: delay, message: message})
	}
	return items, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/servsim/ -run 'TestParseCapture' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/servsim/capture.go cmd/servsim/capture_test.go
git commit -m "feat: parse NDJSON captures for servsim replay"
```

---

### Task 3: servsim echo model for command flows

**Files:**
- Create: `cmd/servsim/echo.go`
- Test: `cmd/servsim/echo_test.go`

**Interfaces:**
- Consumes: `heating.WireFrame` (fields `MessageType`, `MessageCmd`, `Data`); `heating.DecodeTargetTemperature(data []int) (float64, bool)` from Task 1.
- Produces: `type echoModel` with `newEchoModel() *echoModel`, `observe(wire heating.WireFrame)` (update internal state from replayed frames), and `onCommand(wire heating.WireFrame) []string` (returns raw frame strings to send to the service). Later tasks wire these into the websocket server.

Echo behavior (all emitted frames are `messagetype:16, messagecmd:5, size:8, data:[sigLo,sigHi,value,0,0,0,0,0]`):

- `messagecmd:0` with `data:[101,0,3]` → `[101,0,1]` then `[102,0,0]`, plus a seeded `105` at 20.0 °C when no target has been seen yet.
- `messagecmd:0` with `data:[101,0,5]` → `[101,0,0]`.
- `messagecmd:0` with `data:[47,0,3]` → `[47,0,1]`; `data:[48,0,3]` → `[48,0,1]`.
- `messagecmd:1` (press/release) with `data:[107,0,0]` (temp up release) → `105` at current target + 0.5; `data:[108,0,0]` → current target − 0.5. If no target known, seed 20.0 °C first.
- `messagecmd:1` with `data:[4,0,v]` → `[4,0,v]`; `data:[5,0,v]` → `[5,0,v]` (press `v=1`, release `v=0`).
- Everything else → empty list.

- [ ] **Step 1: Write the failing test**

Create `cmd/servsim/echo_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"

	"empirebus-tests/heating"
)

type echoFrame struct {
	MessageType int   `json:"messagetype"`
	MessageCmd  int   `json:"messagecmd"`
	Data        []int `json:"data"`
}

func parseEchoFrames(t *testing.T, raws []string) []echoFrame {
	t.Helper()
	frames := make([]echoFrame, 0, len(raws))
	for _, raw := range raws {
		var frame echoFrame
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			t.Fatalf("unmarshal echo frame %q: %v", raw, err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func tempOf(t *testing.T, frames []echoFrame) []float64 {
	t.Helper()
	var temps []float64
	for _, frame := range frames {
		if len(frame.Data) >= 8 && frame.Data[0] == 105 {
			temp, ok := heating.DecodeTargetTemperature(frame.Data)
			if !ok {
				t.Fatalf("frame %v is not a decodable 105 frame", frame.Data)
			}
			temps = append(temps, temp)
		}
	}
	return temps
}

func cmd(typ, cmd int, data []int) heating.WireFrame {
	return heating.WireFrame{MessageType: typ, MessageCmd: cmd, Size: len(data), Data: data}
}

func TestEchoModelHeatingPowerOnSeedsTarget(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{101, 0, 3})))
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if frames[0].Data[0] != 101 || frames[0].Data[2] != 1 {
		t.Fatalf("frame 0 = %v, want 101 value 1", frames[0].Data)
	}
	if frames[1].Data[0] != 102 || frames[1].Data[2] != 0 {
		t.Fatalf("frame 1 = %v, want 102 value 0", frames[1].Data)
	}
	temps := tempOf(t, frames)
	if len(temps) != 1 || temps[0] != 20.0 {
		t.Fatalf("got temps %v, want [20]", temps)
	}
}

func TestEchoModelHeatingPowerOff(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{101, 0, 5})))
	if len(frames) != 1 || frames[0].Data[0] != 101 || frames[0].Data[2] != 0 {
		t.Fatalf("got %v, want single 101 value 0 frame", frames)
	}
}

func TestEchoModelTempAdjustsFromObservedBaseline(t *testing.T) {
	model := newEchoModel()
	model.observe(cmd(16, 5, []int{105, 0, 0, 22, 12, 74, 4, 0})) // 8.0 C
	up := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{107, 0, 0})))
	temps := tempOf(t, up)
	if len(temps) != 1 || temps[0] != 8.5 {
		t.Fatalf("got temps %v, want [8.5]", temps)
	}
	down := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{108, 0, 0})))
	temps = tempOf(t, down)
	if len(temps) != 1 || temps[0] != 8.0 {
		t.Fatalf("got temps %v, want [8.0]", temps)
	}
}

func TestEchoModelTempSeedsWhenUnknown(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{108, 0, 0})))
	temps := tempOf(t, frames)
	if len(temps) != 1 || temps[0] != 19.5 {
		t.Fatalf("got temps %v, want [19.5] (seeded 20.0 then -0.5)", temps)
	}
}

func TestEchoModelValveAndLights(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{4, 0, 1})))
	if len(frames) != 1 || frames[0].Data[0] != 4 || frames[0].Data[2] != 1 {
		t.Fatalf("valve press: got %v, want 4 value 1", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{4, 0, 0})))
	if len(frames) != 1 || frames[0].Data[0] != 4 || frames[0].Data[2] != 0 {
		t.Fatalf("valve release: got %v, want 4 value 0", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{5, 0, 1})))
	if len(frames) != 1 || frames[0].Data[0] != 5 || frames[0].Data[2] != 1 {
		t.Fatalf("valve close press: got %v, want 5 value 1", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{47, 0, 3})))
	if len(frames) != 1 || frames[0].Data[0] != 47 || frames[0].Data[2] != 1 {
		t.Fatalf("lights on: got %v, want 47 value 1", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{48, 0, 3})))
	if len(frames) != 1 || frames[0].Data[0] != 48 || frames[0].Data[2] != 1 {
		t.Fatalf("lights off: got %v, want 48 value 1", frames)
	}
}

func TestEchoModelIgnoresBootstrapHeartbeatAndUnknown(t *testing.T) {
	model := newEchoModel()
	if got := model.onCommand(cmd(96, 0, []int{0, 0})); len(got) != 0 {
		t.Fatalf("bootstrap produced frames: %v", got)
	}
	if got := model.onCommand(cmd(128, 0, []int{0})); len(got) != 0 {
		t.Fatalf("heartbeat produced frames: %v", got)
	}
	if got := model.onCommand(cmd(17, 0, []int{200, 0, 3})); len(got) != 0 {
		t.Fatalf("unknown signal produced frames: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/servsim/ -run 'TestEchoModel' -v`
Expected: FAIL — `undefined: echoModel`.

- [ ] **Step 3: Implement `echo.go`**

Create `cmd/servsim/echo.go`:

```go
package main

import (
	"encoding/json"
	"math"
	"sync"

	"empirebus-tests/heating"
)

const (
	simSignalValveOpen  = 4
	simSignalValveClose = 5
	simSignalPower      = 101
	simSignalBusy       = 102
	simSignalTargetTemp = 105
	simSignalTempUp     = 107
	simSignalTempDown   = 108
	simSignalLightsOn   = 47
	simSignalLightsOff  = 48
)

type echoModel struct {
	mu          sync.Mutex
	heatingOn   bool
	targetKnown bool
	targetC     float64
}

func newEchoModel() *echoModel {
	return &echoModel{}
}

func (m *echoModel) observe(wire heating.WireFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(wire.Data) < 3 {
		return
	}
	signal := wire.Data[0] | (wire.Data[1] << 8)
	switch signal {
	case simSignalPower:
		m.heatingOn = wire.Data[2] == 1
	case simSignalTargetTemp:
		if temp, ok := heating.DecodeTargetTemperature(wire.Data); ok {
			m.targetKnown = true
			m.targetC = temp
		}
	}
}

func (m *echoModel) onCommand(wire heating.WireFrame) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wire.MessageType != 17 || len(wire.Data) < 3 {
		return nil
	}
	signal := wire.Data[0] | (wire.Data[1] << 8)
	value := wire.Data[2]
	switch wire.MessageCmd {
	case 0:
		switch {
		case signal == simSignalPower && value == 3:
			m.heatingOn = true
			out := []string{stateFrame(simSignalPower, 1), stateFrame(simSignalBusy, 0)}
			if !m.targetKnown {
				m.seedTargetLocked()
				out = append(out, targetFrame(m.targetC))
			}
			return out
		case signal == simSignalPower && value == 5:
			m.heatingOn = false
			return []string{stateFrame(simSignalPower, 0)}
		case signal == simSignalLightsOn && value == 3:
			return []string{stateFrame(simSignalLightsOn, 1)}
		case signal == simSignalLightsOff && value == 3:
			return []string{stateFrame(simSignalLightsOff, 1)}
		}
	case 1:
		switch {
		case signal == simSignalTempUp && value == 0:
			if !m.targetKnown {
				m.seedTargetLocked()
			}
			m.targetC += 0.5
			return []string{targetFrame(m.targetC)}
		case signal == simSignalTempDown && value == 0:
			if !m.targetKnown {
				m.seedTargetLocked()
			}
			m.targetC -= 0.5
			return []string{targetFrame(m.targetC)}
		case signal == simSignalValveOpen || signal == simSignalValveClose:
			return []string{stateFrame(signal, value)}
		}
	}
	return nil
}

func (m *echoModel) seedTargetLocked() {
	m.targetKnown = true
	m.targetC = 20.0
}

func stateFrame(signal, value int) string {
	raw := heating.WireFrame{
		MessageType: 16,
		MessageCmd:  5,
		Size:        8,
		Data:        []int{signal & 0xff, (signal >> 8) & 0xff, value, 0, 0, 0, 0, 0},
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(payload)
}

func targetFrame(temp float64) string {
	raw := int32(math.Round((temp + 273.15) * 1000))
	rawData := []int{
		105, 0, 0, 22,
		int(byte(raw)), int(byte(raw >> 8)), int(byte(raw >> 16)), int(byte(raw >> 24)),
	}
	payload, err := json.Marshal(heating.WireFrame{MessageType: 16, MessageCmd: 5, Size: 8, Data: rawData})
	if err != nil {
		return ""
	}
	return string(payload)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/servsim/ -run 'TestEchoModel' -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add cmd/servsim/echo.go cmd/servsim/echo_test.go
git commit -m "feat: add servsim echo model for command flows"
```

---

### Task 4: servsim websocket server and CLI

**Files:**
- Create: `cmd/servsim/server.go`
- Create: `cmd/servsim/main.go`
- Test: `cmd/servsim/server_test.go`

**Interfaces:**
- Consumes: `parseCapture`, `replayItem`, `echoModel` from Tasks 2-3; `heating.ParseWireFrame`.
- Produces: `type Server struct { addr string; capture []replayItem; loop bool; speed float64; verbose bool; logger *log.Logger }` with `handler() http.Handler` and `serve(ctx context.Context) error`; `cmd/servsim` binary with flags `-listen`, `-capture`, `-loop`, `-speed`, `-max-gap`, `-verbose`.

- [ ] **Step 1: Write the failing end-to-end test**

Create `cmd/servsim/server_test.go`:

```go
package main

import (
	"encoding/json"
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
```

> Note: `sawObservedBusy` was removed during plan review; the assertions above (`sawPowerOn && sawBusyIdle && sawSeed`) are the ones that matter.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/servsim/ -run TestServerReplaysCaptureAndEchoesCommands -v`
Expected: FAIL — `undefined: Server`.

- [ ] **Step 3: Implement `server.go`**

Create `cmd/servsim/server.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"empirebus-tests/heating"

	"github.com/gorilla/websocket"
)

type Server struct {
	addr    string
	capture []replayItem
	loop    bool
	speed   float64
	verbose bool
	logger  *log.Logger
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleConn)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "servsim: fake Garmin SERV")
	})
	return mux
}

func (s *Server) serve(ctx context.Context) error {
	httpServer := &http.Server{Addr: s.addr, Handler: s.handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	return httpServer.ListenAndServe()
}

func (s *Server) handleConn(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Printf("servsim upgrade: %v", err)
		return
	}
	defer conn.Close()
	model := newEchoModel()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.replayLoop(ctx, conn, model)
	s.readLoop(ctx, conn, model)
}

func (s *Server) replayLoop(ctx context.Context, conn *websocket.Conn, model *echoModel) {
	for {
		for _, item := range s.capture {
			delay := time.Duration(float64(item.delay) / s.speed)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(item.message)); err != nil {
				return
			}
			if wire, err := heating.ParseWireFrame(item.message); err == nil {
				model.observe(wire)
			}
			if s.verbose {
				s.logger.Printf("servsim replay: %s", item.message)
			}
		}
		if !s.loop {
			return
		}
	}
}

func (s *Server) readLoop(ctx context.Context, conn *websocket.Conn, model *echoModel) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		raw := string(payload)
		if s.verbose {
			s.logger.Printf("servsim recv: %s", raw)
		}
		wire, err := heating.ParseWireFrame(raw)
		if err != nil {
			continue
		}
		for _, out := range model.onCommand(wire) {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(out)); err != nil {
				return
			}
			if s.verbose {
				s.logger.Printf("servsim echo: %s", out)
			}
		}
	}
}
```

- [ ] **Step 4: Implement `main.go`**

Create `cmd/servsim/main.go`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", ":8090", "websocket listen address")
	capturePath := flag.String("capture", "", "path to an NDJSON capture to replay")
	loop := flag.Bool("loop", false, "replay the capture repeatedly")
	speed := flag.Float64("speed", 1.0, "replay pacing multiplier (higher = faster)")
	maxGap := flag.Duration("max-gap", 10*time.Second, "maximum inter-frame delay during replay")
	verbose := flag.Bool("verbose", false, "log inbound and outbound frames")
	flag.Parse()

	if *capturePath == "" {
		fmt.Fprintln(os.Stderr, "servsim: -capture is required")
		flag.Usage()
		os.Exit(2)
	}
	if *speed <= 0 {
		fmt.Fprintln(os.Stderr, "servsim: -speed must be positive")
		os.Exit(2)
	}
	items, err := parseCapture(*capturePath, *maxGap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "servsim: load capture: %v\n", err)
		os.Exit(1)
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "servsim: capture contains no receive frames to replay")
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "servsim ", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &Server{
		addr:    *listen,
		capture: items,
		loop:    *loop,
		speed:   *speed,
		verbose: *verbose,
		logger:  logger,
	}
	logger.Printf("replaying %d receive frames from %s on %s (loop=%t)", len(items), *capturePath, *listen, *loop)
	if err := srv.serve(ctx); err != nil && err != http.ErrServerClosed {
		logger.Printf("servsim exited: %v", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/servsim/ -v`
Expected: PASS (all capture, echo, and server tests).

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: all packages `ok`.

- [ ] **Step 7: Commit**

```bash
git add cmd/servsim/server.go cmd/servsim/main.go cmd/servsim/server_test.go
git commit -m "feat: add servsim fake SERV websocket server"
```

---

### Task 5: Simulated environment config and run script

**Files:**
- Create: `config.sim.yaml`
- Create: `scripts/sim/run-sim.sh`

**Interfaces:**
- Consumes: `cmd/servsim` binary (flags `-listen`, `-capture`, `-loop`), `cmd/empirebusd` binary (flag `-config`).

- [ ] **Step 1: Create `config.sim.yaml`**

```yaml
garmin:
  # Simulated SERV via scripts/sim/run-sim.sh / cmd/servsim.
  ws_url: ws://localhost:8090/ws
  heartbeat_interval: 4s
  trace_window: 3s

api:
  listen: 0.0.0.0:8091

automation:
  timezone: Europe/London
  heating_programs:
    - id: sim-off
      days: ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
      periods:
        - start: "00:00"
          mode: "off"
```

- [ ] **Step 2: Create `scripts/sim/run-sim.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

CAPTURE="${1:-}"
if [[ -z "${CAPTURE}" ]]; then
  CAPTURE="$(ls -1 "${REPO_ROOT}"/captures/garmin-ws-*.ndjson 2>/dev/null | tail -1 || true)"
fi
if [[ -z "${CAPTURE}" ]]; then
  echo "no capture found; pass one explicitly, e.g.:" >&2
  echo "  ${0} captures/garmin-ws-YYYYMMDD-HHMMSS.ndjson" >&2
  exit 1
fi
if [[ ! -f "${CAPTURE}" ]]; then
  echo "capture not found: ${CAPTURE}" >&2
  exit 1
fi

BUILD_DIR="$(mktemp -d)"
cd "${REPO_ROOT}"

echo "==> Building servsim and empirebusd"
go build -o "${BUILD_DIR}/servsim" ./cmd/servsim
go build -o "${BUILD_DIR}/empirebusd" ./cmd/empirebusd

echo "==> Starting servsim (capture=${CAPTURE})"
"${BUILD_DIR}/servsim" -listen :8090 -capture "${CAPTURE}" &
SERVSIM_PID=$!
trap 'kill "${SERVSIM_PID}" 2>/dev/null || true; rm -rf "${BUILD_DIR}"' EXIT

for _ in {1..50}; do
  if curl --silent --output /dev/null --max-time 1 http://127.0.0.1:8090/; then
    break
  fi
  sleep 0.1
done

echo "==> Starting empirebusd (config=config.sim.yaml)"
echo "    UI/API: http://localhost:8091"
"${BUILD_DIR}/empirebusd" -config ./config.sim.yaml
```

- [ ] **Step 3: Make the script executable and syntax-check it**

Run:
```bash
chmod +x scripts/sim/run-sim.sh
bash -n scripts/sim/run-sim.sh
```
Expected: no output from `bash -n` (exit 0).

- [ ] **Step 4: Verify the config loads**

Run:
```bash
go run ./cmd/empirebusd -config ./config.sim.yaml >/tmp/empirebusd-sim.log 2>&1 &
SIM_PID=$!
sleep 2
kill "${SIM_PID}" 2>/dev/null || true
rg -n "empirebusd starting|listen" /tmp/empirebusd-sim.log
```
Expected: the log contains `empirebusd starting: config=... listen=0.0.0.0:8091` and `garmin target: ws_url=ws://localhost:8090/ws`. The daemon tries to connect to the sim SERV (which is not running here), which is expected — the config validation must pass.

- [ ] **Step 5: Manual smoke test**

With a capture file available (for example the repo-root `garmin-ws-20260815T142323Z.ndjson` copied into the worktree), run:

```bash
./scripts/sim/run-sim.sh <path-to-capture>
```

In another terminal:
```bash
curl --silent http://127.0.0.1:8091/v1/health
curl --silent -X POST http://127.0.0.1:8091/v1/heating/power -d '{"state":"on"}'
curl --silent http://127.0.0.1:8091/v1/heating/state
curl --silent http://127.0.0.1:8091/v1/water/grey-valve/open -X POST
curl --silent http://127.0.0.1:8091/v1/lights/external/flash -X POST -d '{"count":1}'
```
Expected: health returns 200 JSON; power-on returns 200; heating state reports on; valve and flash commands return 200. Stop the sim with Ctrl-C.

> `POST /v1/heating/power` takes the body `{"state":"on"}` / `{"state":"off"}` (confirmed in `docs/control-safety.md`). If a request returns `400`, the error body names the expected field.

- [ ] **Step 6: Commit**

```bash
git add config.sim.yaml scripts/sim/run-sim.sh
git commit -m "feat: add simulated environment config and run script"
```

---

### Task 6: Staging systemd unit and example config

**Files:**
- Create: `ops/systemd/empirebusd-staging.service`
- Create: `config.staging.example.yaml`

**Interfaces:**
- Consumes: nothing new. Produces files referenced by Task 7's `ENVIRONMENT=staging` table.

- [ ] **Step 1: Create `ops/systemd/empirebusd-staging.service`**

```ini
[Unit]
Description=EmpireBus Heating Service (staging)
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
Type=simple
User=xtura
Group=xtura
WorkingDirectory=/opt/xtura-staging/current
ExecStart=/opt/xtura-staging/current/empirebusd -config /var/lib/xtura-staging/config.yaml
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Create `config.staging.example.yaml`**

```yaml
garmin:
  # Same SERV as production. The staging instance is a parallel, real-hardware
  # instance for build verification: anything you command from the staging UI
  # affects the real heater/valves. Exercise full command flows in the Mac
  # simulation instead.
  ws_url: ws://172.16.11.7:8888/ws
  heartbeat_interval: 4s
  trace_window: 3s

location:
  enabled: false

api:
  listen: 0.0.0.0:8080

automation:
  timezone: Europe/London
  # All-off by default. Replace with a short test pattern when you want to
  # verify schedule behaviour in staging.
  heating_programs:
    - id: everyday-default
      days: ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
      periods:
        - start: "00:00"
          mode: "off"
```

- [ ] **Step 3: Verify the staging config loads**

Run:
```bash
go run ./cmd/empirebusd -config ./config.staging.example.yaml >/tmp/empirebusd-staging.log 2>&1 &
SIM_PID=$!
sleep 2
kill "${SIM_PID}" 2>/dev/null || true
rg -n "empirebusd starting|garmin target" /tmp/empirebusd-staging.log
```
Expected: the log shows `listen=0.0.0.0:8080` and `ws_url=ws://172.16.11.7:8888/ws`; config validation passes (garmin connect failures are expected and fine — it retries).

- [ ] **Step 4: Commit**

```bash
git add ops/systemd/empirebusd-staging.service config.staging.example.yaml
git commit -m "ops: add staging systemd unit and config example"
```

---

### Task 7: Environment support in deploy scripts

**Files:**
- Modify: `scripts/deploy/deploy-on-pi.sh`
- Modify: `scripts/deploy/run-deploy-from-mac.sh`

**Interfaces:**
- Consumes: `ops/systemd/empirebusd-staging.service` and `config.staging.example.yaml` from Task 6.
- Produces: `ENVIRONMENT` env var (values `prod` | `staging`, default `prod`) honored by `deploy-on-pi.sh`; `ENVIRONMENT` forwarded by `run-deploy-from-mac.sh`.

- [ ] **Step 1: Parameterize `scripts/deploy/deploy-on-pi.sh`**

Replace the constant block at the top of the file (currently lines 4-14) with:

```bash
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_PATH="${REPO_ROOT}/scripts/deploy/$(basename "${BASH_SOURCE[0]}")"
GO_BIN="${GO_BIN:-go}"
SUDOERS_TIMEZONE_SOURCE="${REPO_ROOT}/ops/sudoers/xtura-timezone"
SUDOERS_TIMEZONE_DEST="/etc/sudoers.d/xtura-timezone"

ENVIRONMENT="${ENVIRONMENT:-prod}"
case "${ENVIRONMENT}" in
  prod)
    INSTALL_ROOT="/opt/xtura"
    CONFIG_PATH="/var/lib/xtura/config.yaml"
    DATA_DIR="/var/lib/xtura"
    SERVICE_NAME="empirebusd"
    SERVICE_UNIT_SOURCE="${REPO_ROOT}/ops/systemd/empirebusd.service"
    SERVICE_UNIT_DEST="/etc/systemd/system/empirebusd.service"
    HEALTH_URL="http://127.0.0.1/v1/health"
    ;;
  staging)
    INSTALL_ROOT="/opt/xtura-staging"
    CONFIG_PATH="/var/lib/xtura-staging/config.yaml"
    DATA_DIR="/var/lib/xtura-staging"
    SERVICE_NAME="empirebusd-staging"
    SERVICE_UNIT_SOURCE="${REPO_ROOT}/ops/systemd/empirebusd-staging.service"
    SERVICE_UNIT_DEST="/etc/systemd/system/empirebusd-staging.service"
    HEALTH_URL="http://127.0.0.1:8080/v1/health"
    ;;
  *)
    echo "unsupported ENVIRONMENT: ${ENVIRONMENT} (expected prod or staging)" >&2
    exit 1
    ;;
esac
```

Then make these three edits in the same file:

1. Add an environment banner right after `cd "${REPO_ROOT}"`:

```bash
echo "==> Deploying ${SERVICE_NAME} to the ${ENVIRONMENT} environment"
```

2. Replace the install-root mkdir/chown line:

```bash
sudo mkdir -p "${RELEASES_DIR}" "${DATA_DIR}"
```

and

```bash
sudo chown -R xtura:xtura "${INSTALL_ROOT}" "${DATA_DIR}"
```

(these replace the current `sudo mkdir -p "${RELEASES_DIR}" /var/lib/xtura` and `sudo chown -R xtura:xtura "${INSTALL_ROOT}" /var/lib/xtura` lines).

3. Replace the health-check URL `http://127.0.0.1/v1/health` with `"${HEALTH_URL}"` in the `curl --fail --silent --show-error --max-time 2 http://127.0.0.1/v1/health` line.

- [ ] **Step 2: Forward `ENVIRONMENT` in `scripts/deploy/run-deploy-from-mac.sh`**

Replace the header and ssh command with:

```bash
#!/usr/bin/env bash
set -euo pipefail

PI_HOST="${PI_HOST:-jones-pi.taile19bc2.ts.net}"
PI_USER="${PI_USER:-$(id -un)}"
PI_PORT="${PI_PORT:-22}"
REMOTE_REPO="${REMOTE_REPO:-/home/${PI_USER}/development/xtura-automation}"
ENVIRONMENT="${ENVIRONMENT:-prod}"
TARGET_SHA="${1:-HEAD}"

ssh -p "${PI_PORT}" "${PI_USER}@${PI_HOST}" "\
  cd '${REMOTE_REPO}' && \
  ENVIRONMENT='${ENVIRONMENT}' ./scripts/deploy/deploy-on-pi.sh '${TARGET_SHA}'"
```

- [ ] **Step 3: Syntax-check both scripts**

Run:
```bash
bash -n scripts/deploy/deploy-on-pi.sh
bash -n scripts/deploy/run-deploy-from-mac.sh
```
Expected: no output (exit 0).

- [ ] **Step 4: Verify ENVIRONMENT validation without running a deploy**

Run:
```bash
ENVIRONMENT=bogus bash scripts/deploy/deploy-on-pi.sh 2>&1 | head -1 || true
```
Expected: output `unsupported ENVIRONMENT: bogus (expected prod or staging)` and exit 1. Confirm the default (no `ENVIRONMENT`) still prints the prod paths:

```bash
rg -n 'INSTALL_ROOT|CONFIG_PATH|DATA_DIR|HEALTH_URL|SERVICE_NAME=' scripts/deploy/deploy-on-pi.sh | head
```
Expected: the prod values appear under the `prod)` case and the script defaults `ENVIRONMENT=prod`.

> The real staging deploy (install on the Pi) is out of scope for this task's automated verification; it will be done with the user against the Jones Pi. Verify no stray `sudo mkdir`/`chown`/health references to the old hard-coded `/var/lib/xtura` or port 80 remain in the prod path by reading the final file.

- [ ] **Step 5: Commit**

```bash
git add scripts/deploy/deploy-on-pi.sh scripts/deploy/run-deploy-from-mac.sh
git commit -m "ops: support staging environment in deploy scripts"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/garmin-empirbus-signals.md`
- Modify: `docs/codex-notes.md`

- [ ] **Step 1: Add the simulated environment section to `README.md`**

Insert before the `## Deployment` heading:

`````markdown
## Simulated environment (Mac)

For development without touching the real motorhome, run the service against a
fake Garmin SERV (`cmd/servsim`) that replays a recorded NDJSON capture and
echoes command state for the heater, valves, and lights:

```bash
./scripts/sim/run-sim.sh                  # uses the newest captures/garmin-ws-*.ndjson
./scripts/sim/run-sim.sh captures/my.ndjson
```

This starts `servsim` on `ws://localhost:8090/ws` and `empirebusd` on
`http://localhost:8091` with `config.sim.yaml`. Exercise command flows there:
power on/off, set target temperature, grey-valve open/close, and the exterior
light flash. `servsim -help` lists options (`-loop` replays the capture
repeatedly; `-speed` changes replay pacing). Because the fake SERV echo is
simulation behavior, not browser-confirmed evidence, see the simulation note in
[garmin-empirbus-signals.md](docs/garmin-empirbus-signals.md).
`````

- [ ] **Step 2: Add the staging section to `README.md`**

Inside `## Deployment`, before the `### GitHub Actions Attempt` heading, add:

`````markdown
### Staging environment (Jones Pi)

A second, parallel service instance on the Jones Pi that shares the SERV with
production but runs on its own port, config, and systemd unit:

- releases in `/opt/xtura-staging/releases/<git-sha>`, active link at
  `/opt/xtura-staging/current`
- writable service config at `/var/lib/xtura-staging/config.yaml`
- `empirebusd-staging.service`, HTTP on `:8080`

Setup once on the Pi (mirrors production):

```bash
sudo mkdir -p /opt/xtura-staging /var/lib/xtura-staging
sudo cp ~/development/xtura-automation/config.staging.example.yaml /var/lib/xtura-staging/config.yaml
sudo chown -R xtura:xtura /opt/xtura-staging /var/lib/xtura-staging
```

Deploy a build to staging (from the Pi's git checkout) or trigger it from the
Mac:

```bash
ENVIRONMENT=staging ./scripts/deploy/deploy-on-pi.sh            # on the Pi
ENVIRONMENT=staging ./scripts/deploy/run-deploy-from-mac.sh <sha>   # from the Mac
```

Verify with `curl http://127.0.0.1:8080/v1/health` (or open
`http://jones-pi:8080/`). To promote a verified build to production, deploy the
same SHA with the default environment.

**Staging talks to the real SERV.** Commands issued from the staging UI affect
the real heater and valves; use it for read/build verification and the Mac
simulation for command testing. The SERV's tolerance for two concurrent
websocket clients is unverified — if staging's connection ever drops
production's, point staging's `garmin.ws_url` at a `servsim` instance instead.
`````

- [ ] **Step 3: Add the simulation note to `docs/garmin-empirbus-signals.md`**

Add a new section before `## Code Red Module Data`:

```markdown
## Simulation note: `servsim` command echo

`cmd/servsim` is a fake SERV for local development. It replays a recorded
NDJSON capture for background state and echoes command state for a small set of
signals so command APIs complete offline. This is **simulation behavior, not
browser-confirmed evidence**; it exists so the repo can develop and test
without the motorhome.

| Command frame | Echoed frames |
| --- | --- |
| `messagetype=17, messagecmd=0, data=[101,0,3]` | `16/5` `[101,0,1]` then `[102,0,0]`, plus a seeded `[105,...,20.0C]` when no target is known |
| `messagetype=17, messagecmd=0, data=[101,0,5]` | `16/5` `[101,0,0]` |
| `messagetype=17, messagecmd=0, data=[47,0,3]` | `16/5` `[47,0,1]` |
| `messagetype=17, messagecmd=0, data=[48,0,3]` | `16/5` `[48,0,1]` |
| `messagetype=17, messagecmd=1, data=[107,0,0]` (temp up release) | `16/5` `[105,0,0,22,...]` at +0.5C |
| `messagetype=17, messagecmd=1, data=[108,0,0]` (temp down release) | `16/5` `[105,0,0,22,...]` at -0.5C |
| `messagetype=17, messagecmd=1, data=[4,0,v]` | `16/5` `[4,0,v]` |
| `messagetype=17, messagecmd=1, data=[5,0,v]` | `16/5` `[5,0,v]` |

Echo state frames use the observed SERV state shape (`messagetype=16,
messagecmd=5, size=8, data=[sigLo,sigHi,value,0,0,0,0,0]`) and the signal `105`
payload layout above, matching the replayed frames in `garmin-ws-*.ndjson` and
the `Heating*.har` captures. Source: `cmd/servsim/echo.go`. The 20.0C seed is a
convenience so `SetTargetTemp` can read a baseline; it is not an observed SERV
value.
```

- [ ] **Step 4: Update `docs/codex-notes.md`**

In the Run/Test table add:

```markdown
| Run simulated env | `./scripts/sim/run-sim.sh` | Starts `servsim` (fake SERV) and `empirebusd` against `config.sim.yaml`; no real hardware involved. |
```

In the Deploy table add:

```markdown
| Staging deploy trigger | `ENVIRONMENT=staging ./scripts/deploy/run-deploy-from-mac.sh` | SSHes to the Pi and deploys to the staging env (`/opt/xtura-staging`, `:8080`). |
```

In the Repo Map table add:

```markdown
| `cmd/servsim` | Fake Garmin SERV for the local simulated environment; replays NDJSON captures and echoes command state. |
| `config.sim.yaml` | Config that points `empirebusd` at the local `servsim`. |
| `scripts/sim/run-sim.sh` | Starts the simulated environment on the Mac. |
```

- [ ] **Step 5: Verify the final state**

Run: `go test ./...`
Expected: all packages `ok`.

Run: `bash -n scripts/deploy/deploy-on-pi.sh scripts/deploy/run-deploy-from-mac.sh scripts/sim/run-sim.sh`
Expected: no output (exit 0).

Review the README sections render correctly (the nested code fences must be closed) by reading the modified areas.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/garmin-empirbus-signals.md docs/codex-notes.md
git commit -m "docs: document simulated environment and staging deployment"
```

---

## Final Verification

- [ ] `go test ./...` passes from the repo root.
- [ ] `bash -n` passes on all three touched shell scripts.
- [ ] Manual Mac smoke test (Task 5 Step 5) passes.
- [ ] Real staging deploy on the Jones Pi done with the user: `ENVIRONMENT=staging ./scripts/deploy/run-deploy-from-mac.sh <sha>`; confirm `empirebusd-staging` serves `:8080/v1/health`, production stays healthy, and note SERV concurrent-client behavior.
- [ ] All commits are on `feature/staging-deployment` in the worktree.
