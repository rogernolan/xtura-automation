// Package recording manages on-demand WebSocket traffic recordings.
package recording

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"empirebus-tests/heating"
)

type WaitFor string

const (
	WaitImmediate WaitFor = "immediate"
	WaitEngineOn  WaitFor = "engine_on"
	WaitHeatingOn WaitFor = "heating_on"
	WaitVictronOn WaitFor = "victron_on"
)

var ErrActive = errors.New("recording is already armed or active")

type StartRequest struct {
	WaitFor         WaitFor
	DurationMinutes int
}

type State struct {
	Status          string     `json:"status"`
	WaitFor         WaitFor    `json:"wait_for"`
	DurationMinutes int        `json:"duration_minutes"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FileName        string     `json:"file_name,omitempty"`
	LastFileName    string     `json:"last_file_name,omitempty"`
	Error           string     `json:"error,omitempty"`
}

type Manager struct {
	mu          sync.Mutex
	dir         string
	now         func() time.Time
	timeoutUnit time.Duration
	logger      *log.Logger

	state   State
	file    *os.File
	encoder *json.Encoder
	timer   *time.Timer
}

type record struct {
	At         time.Time          `json:"at"`
	Direction  string             `json:"direction"`
	Message    string             `json:"message,omitempty"`
	MessageLen int                `json:"message_len,omitempty"`
	Frame      *heating.WireFrame `json:"frame,omitempty"`
	Signal     *int               `json:"signal,omitempty"`
	Value      *int               `json:"value,omitempty"`
	Event      string             `json:"event,omitempty"`
	Error      string             `json:"error,omitempty"`
}

func New(dir string, now func() time.Time, logger *log.Logger) *Manager {
	return NewWithTimeout(dir, now, time.Minute, logger)
}

// NewWithTimeout permits tests to use a shorter duration unit than a minute.
func NewWithTimeout(dir string, now func() time.Time, timeoutUnit time.Duration, logger *log.Logger) *Manager {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Manager{
		dir:         dir,
		now:         now,
		timeoutUnit: timeoutUnit,
		logger:      logger,
		state:       State{Status: "idle"},
	}
}

func (m *Manager) Dir() string {
	return m.dir
}

func (m *Manager) Start(request StartRequest) (State, error) {
	if !validWaitFor(request.WaitFor) {
		return m.State(), fmt.Errorf("unsupported recording wait condition %q", request.WaitFor)
	}
	if request.DurationMinutes < 0 {
		return m.State(), fmt.Errorf("recording duration must not be negative")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Status == "armed" || m.state.Status == "recording" {
		return m.snapshotLocked(), ErrActive
	}
	m.state = State{
		Status:          "armed",
		WaitFor:         request.WaitFor,
		DurationMinutes: request.DurationMinutes,
		LastFileName:    m.state.LastFileName,
	}
	if request.WaitFor != WaitImmediate {
		return m.snapshotLocked(), nil
	}
	if err := m.beginLocked(); err != nil {
		return m.snapshotLocked(), err
	}
	return m.snapshotLocked(), nil
}

func (m *Manager) Stop(reason string) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(reason)
	return m.snapshotLocked()
}

func (m *Manager) Observe(at time.Time, direction heating.Direction, raw string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Status == "armed" {
		want := triggerSignal(m.state.WaitFor)
		if direction != heating.DirectionReceive || !isOnFrame(raw, want) {
			return
		}
		if err := m.beginLocked(); err != nil {
			return
		}
	}
	if m.state.Status != "recording" {
		return
	}
	m.writeLocked(webSocketRecord(at, direction, raw))
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked("service_shutdown")
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) beginLocked() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return m.failStartLocked(fmt.Errorf("create recording directory: %w", err))
	}
	file, fileName, err := m.createFileLocked()
	if err != nil {
		return m.failStartLocked(err)
	}
	m.file = file
	m.encoder = json.NewEncoder(file)
	startedAt := m.now().UTC()
	m.state.Status = "recording"
	m.state.StartedAt = &startedAt
	m.state.FileName = fileName
	m.state.LastFileName = fileName
	if !m.writeLocked(record{At: startedAt, Direction: "event", Event: "recording_started"}) {
		return fmt.Errorf("write recording start event: %s", m.state.Error)
	}
	if m.state.DurationMinutes > 0 {
		m.timer = time.AfterFunc(time.Duration(m.state.DurationMinutes)*m.timeoutUnit, func() {
			m.Stop("timeout")
		})
	}
	return nil
}

func (m *Manager) createFileLocked() (*os.File, string, error) {
	base := m.now().UTC().Format("20060102T150405Z")
	for suffix := 0; ; suffix++ {
		fileName := fmt.Sprintf("garmin-ws-%s.ndjson", base)
		if suffix > 0 {
			fileName = fmt.Sprintf("garmin-ws-%s-%d.ndjson", base, suffix)
		}
		file, err := os.OpenFile(filepath.Join(m.dir, fileName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			return file, fileName, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return nil, "", fmt.Errorf("create recording file: %w", err)
	}
}

func (m *Manager) stopLocked(reason string) {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.state.Status != "recording" && m.state.Status != "armed" {
		return
	}
	if m.state.Status == "recording" {
		m.writeLocked(record{At: m.now().UTC(), Direction: "event", Event: reason})
	}
	if m.file != nil {
		if err := m.file.Close(); err != nil {
			m.setErrorLocked(fmt.Errorf("close recording file: %w", err))
		}
		m.file = nil
		m.encoder = nil
	}
	m.state.Status = "idle"
	m.state.WaitFor = ""
	m.state.DurationMinutes = 0
	m.state.StartedAt = nil
	m.state.FileName = ""
}

func (m *Manager) writeLocked(value record) bool {
	if m.encoder == nil {
		return false
	}
	if err := m.encoder.Encode(value); err != nil {
		m.setErrorLocked(fmt.Errorf("write recording: %w", err))
		if m.timer != nil {
			m.timer.Stop()
			m.timer = nil
		}
		if m.file != nil {
			_ = m.file.Close()
			m.file = nil
		}
		m.encoder = nil
		m.state.Status = "idle"
		m.state.StartedAt = nil
		m.state.FileName = ""
		return false
	}
	return true
}

func (m *Manager) failStartLocked(err error) error {
	m.setErrorLocked(err)
	m.state.Status = "idle"
	m.state.WaitFor = ""
	m.state.DurationMinutes = 0
	m.state.StartedAt = nil
	m.state.FileName = ""
	return err
}

func (m *Manager) setErrorLocked(err error) {
	m.state.Error = err.Error()
	m.logger.Printf("recording manager: %v", err)
}

func (m *Manager) snapshotLocked() State {
	state := m.state
	if state.StartedAt != nil {
		startedAt := *state.StartedAt
		state.StartedAt = &startedAt
	}
	return state
}

func validWaitFor(wait WaitFor) bool {
	switch wait {
	case WaitImmediate, WaitEngineOn, WaitHeatingOn, WaitVictronOn:
		return true
	default:
		return false
	}
}

func triggerSignal(wait WaitFor) int {
	switch wait {
	case WaitEngineOn:
		return 11
	case WaitHeatingOn:
		return 101
	case WaitVictronOn:
		return 197
	default:
		return -1
	}
}

func isOnFrame(raw string, want int) bool {
	frame, err := heating.ParseWireFrame(raw)
	return err == nil && len(frame.Data) >= 3 && frame.Data[0]|frame.Data[1]<<8 == want && frame.Data[2]&1 != 0
}

func webSocketRecord(at time.Time, direction heating.Direction, raw string) record {
	entry := record{
		At:         at.UTC(),
		Direction:  string(direction),
		Message:    raw,
		MessageLen: len(raw),
	}
	frame, err := heating.ParseWireFrame(raw)
	if err != nil {
		entry.Error = fmt.Sprintf("parse frame: %v", err)
		return entry
	}
	entry.Frame = &frame
	if len(frame.Data) > 0 {
		signal := frame.Data[0]
		if len(frame.Data) > 1 {
			signal |= frame.Data[1] << 8
		}
		entry.Signal = &signal
	}
	if len(frame.Data) > 2 {
		value := frame.Data[2]
		entry.Value = &value
	}
	return entry
}
