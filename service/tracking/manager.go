// Package tracking samples a location provider into per-session GeoJSON track
// files, optionally gated on the Garmin engine signal.
package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"empirebus-tests/heating"
	domainlocation "empirebus-tests/service/domains/location"
)

// Settings controls tracking behaviour. Directory is fixed at construction.
type Settings struct {
	WhenEngineOn   bool
	SampleInterval time.Duration
}

// State is the runtime tracking state.
type State struct {
	WhenEngineOn          bool       `json:"when_engine_on"`
	SampleIntervalSeconds float64    `json:"sample_interval_seconds"`
	EngineKnown           bool       `json:"engine_known"`
	EngineOn              bool       `json:"engine_on"`
	Tracking              bool       `json:"tracking"`
	CurrentFile           string     `json:"current_file,omitempty"`
	PointCount            int        `json:"point_count"`
	LastSampleAt          *time.Time `json:"last_sample_at,omitempty"`
	LastError             string     `json:"last_error,omitempty"`
	LastErrorAt           *time.Time `json:"last_error_at,omitempty"`
}

// FileInfo describes a track file for listing.
type FileInfo struct {
	Name       string     `json:"name"`
	Bytes      int64      `json:"bytes"`
	StartTime  *time.Time `json:"start_time,omitempty"`
	EndTime    *time.Time `json:"end_time,omitempty"`
	PointCount int        `json:"point_count"`
}

// ErrEngineMode reports a manual start/stop call made while tracking is gated
// on the engine. The UI hides manual controls in that mode.
var ErrEngineMode = errors.New("start/stop tracking is only available in manual mode")

const engineSignalID = 11

// activeTrack is the in-memory representation of the session file being
// written.
type activeTrack struct {
	name   string
	times  []time.Time
	points [][]float64
}

type Manager struct {
	mu       sync.Mutex
	dir      string
	poll     func(context.Context) (domainlocation.Fix, error)
	now      func() time.Time
	logger   *log.Logger
	onChange func(State)

	settings     Settings
	engineKnown  bool
	engineOn     bool
	lastSampleAt *time.Time
	lastError    string
	lastErrorAt  *time.Time
	track        *activeTrack

	wake chan struct{}
}

// New creates a tracking manager. nil now and logger fall back to sensible
// defaults; a nil poll fails every sample.
func New(dir string, poll func(context.Context) (domainlocation.Fix, error), now func() time.Time, logger *log.Logger) *Manager {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = log.Default()
	}
	if poll == nil {
		poll = func(context.Context) (domainlocation.Fix, error) {
			return domainlocation.Fix{}, errors.New("no location provider configured")
		}
	}
	return &Manager{dir: dir, poll: poll, now: now, logger: logger, wake: make(chan struct{}, 1)}
}

// SetOnChange installs a callback invoked after each sample and lifecycle
// transition.
func (m *Manager) SetOnChange(onChange func(State)) {
	m.mu.Lock()
	m.onChange = onChange
	m.mu.Unlock()
}

// Configure applies settings live. Switching between engine-gated and manual
// mode finalizes the active track so a session cannot leak across modes. A
// wake signal prompts Start to recreate its ticker so a sample-interval change
// takes effect immediately.
func (m *Manager) Configure(settings Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if settings.WhenEngineOn != m.settings.WhenEngineOn {
		m.finalizeLocked()
	}
	m.settings = settings
	m.signalWakeLocked()
	m.notifyLocked(m.snapshotLocked())
}

// StartRecording begins a new session in manual mode. If a session is already
// active it is left untouched. In engine mode it is a no-op; the HTTP layer
// rejects the call via ErrEngineMode.
func (m *Manager) StartRecording(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track == nil {
		m.beginSessionLocked(at)
	}
	m.notifyLocked(m.snapshotLocked())
}

// StopRecording finalizes the active session.
func (m *Manager) StopRecording() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track != nil {
		m.finalizeLocked()
	}
	m.notifyLocked(m.snapshotLocked())
}

// signalWakeLocked notifies Start's loop that settings changed. The buffered,
// non-blocking send is level-triggered: Start re-reads the current settings
// whenever it wakes, so a dropped signal is harmless as long as one is pending.
func (m *Manager) signalWakeLocked() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// ObserveFrame tracks engine state from Garmin receive frames for signal 11.
func (m *Manager) ObserveFrame(at time.Time, direction heating.Direction, raw string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if direction != heating.DirectionReceive {
		return
	}
	known, on, ok := engineSignalState(raw)
	if !ok {
		return
	}
	if known == m.engineKnown && on == m.engineOn {
		return
	}
	m.engineKnown = known
	m.engineOn = on
	if m.settings.WhenEngineOn {
		if on {
			m.beginSessionLocked(at.UTC())
		} else {
			m.finalizeLocked()
		}
	}
	m.notifyLocked(m.snapshotLocked())
}

// Start launches the sampling goroutine using the sample interval. Interval
// changes applied via Configure take effect immediately: Configure signals a
// wake channel and Start recreates the ticker when it observes a change.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	interval := m.settings.SampleInterval
	m.mu.Unlock()
	go func() {
		defer m.Shutdown()
		ticker := time.NewTicker(nonZeroInterval(interval))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Sample(ctx)
			case <-m.wake:
			}
			m.mu.Lock()
			next := m.settings.SampleInterval
			m.mu.Unlock()
			if next != interval {
				ticker.Stop()
				ticker = time.NewTicker(nonZeroInterval(next))
				interval = next
			}
		}
	}()
}

func nonZeroInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 5 * time.Second
	}
	return interval
}

// Sample polls the location provider once and appends a point to the active
// session track. In engine mode it is skipped until the engine is known and
// on; in manual mode it is skipped unless a session is active.
func (m *Manager) Sample(ctx context.Context) {
	m.mu.Lock()
	if m.settings.WhenEngineOn && (!m.engineKnown || !m.engineOn) {
		m.mu.Unlock()
		return
	}
	if !m.settings.WhenEngineOn && m.track == nil {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	fix, err := m.poll(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings.WhenEngineOn && (!m.engineKnown || !m.engineOn) {
		return
	}
	if !m.settings.WhenEngineOn && m.track == nil {
		return
	}
	at := m.now().UTC()
	if err != nil {
		m.recordErrorLocked(at, err)
		m.notifyLocked(m.snapshotLocked())
		return
	}
	m.appendSampleLocked(fix, at)
	m.notifyLocked(m.snapshotLocked())
}

// State returns a snapshot of the runtime state.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// Shutdown finalizes the active track.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track == nil {
		return
	}
	m.finalizeLocked()
	m.notifyLocked(m.snapshotLocked())
}

// List returns track files in the directory with metadata parsed from each.
func (m *Manager) List() ([]FileInfo, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list tracks: %w", err)
	}
	var infos []FileInfo
	for _, entry := range entries {
		if !validTrackName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fileInfo := FileInfo{Name: entry.Name(), Bytes: info.Size()}
		if data, err := os.ReadFile(filepath.Join(m.dir, entry.Name())); err == nil {
			if times, points, ok := parseTrackFile(data); ok {
				if len(times) > 0 {
					start := times[0]
					end := times[len(times)-1]
					fileInfo.StartTime = &start
					fileInfo.EndTime = &end
				}
				fileInfo.PointCount = len(points)
			}
		}
		infos = append(infos, fileInfo)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// Read returns the raw bytes of a track file. Names that do not match the
// track pattern are rejected.
func (m *Manager) Read(name string) ([]byte, error) {
	if !validTrackName(name) {
		return nil, fmt.Errorf("invalid track name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(m.dir, name))
	if err != nil {
		return nil, fmt.Errorf("read track %s: %w", name, err)
	}
	return data, nil
}

// Delete removes a track file. Names that do not match the track pattern are
// rejected. Deleting the active track finalizes it so a later sample cannot
// resurrect the removed file.
func (m *Manager) Delete(name string) error {
	if !validTrackName(name) {
		return fmt.Errorf("invalid track name %q", name)
	}
	m.mu.Lock()
	isActive := m.track != nil && m.track.name == name
	m.mu.Unlock()
	if err := os.Remove(filepath.Join(m.dir, name)); err != nil {
		return fmt.Errorf("delete track %s: %w", name, err)
	}
	if isActive {
		m.mu.Lock()
		if m.track != nil && m.track.name == name {
			m.finalizeLocked()
		}
		m.notifyLocked(m.snapshotLocked())
		m.mu.Unlock()
	}
	return nil
}

func (m *Manager) appendSampleLocked(fix domainlocation.Fix, at time.Time) {
	if m.settings.WhenEngineOn && m.track == nil {
		m.beginSessionLocked(at)
	}
	if m.track == nil {
		return
	}
	position := []float64{fix.Longitude, fix.Latitude}
	if fix.Altitude != nil {
		position = append(position, *fix.Altitude)
	}
	m.track.points = append(m.track.points, position)
	m.track.times = append(m.track.times, at)
	if err := m.writeTrackLocked(); err != nil {
		m.track.points = m.track.points[:len(m.track.points)-1]
		m.track.times = m.track.times[:len(m.track.times)-1]
		m.recordErrorLocked(at, err)
		return
	}
	sampleAt := at
	m.lastSampleAt = &sampleAt
	m.lastError = ""
	m.lastErrorAt = nil
}

func (m *Manager) beginSessionLocked(at time.Time) {
	m.track = &activeTrack{
		name: fmt.Sprintf("track-%s.geojson", at.UTC().Format("20060102T150405Z")),
	}
}

func (m *Manager) finalizeLocked() {
	m.track = nil
}

func (m *Manager) recordErrorLocked(at time.Time, err error) {
	m.lastError = err.Error()
	atCopy := at
	m.lastErrorAt = &atCopy
	m.logger.Printf("tracking: %v", err)
}

// writeTrackLocked atomically writes the active track. A track with fewer than
// two positions is not written: RFC 7946 requires a LineString to have at
// least two positions, so the file only appears once a second fix arrives.
func (m *Manager) writeTrackLocked() error {
	if m.track == nil {
		return nil
	}
	if len(m.track.points) < 2 {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("create track directory: %w", err)
	}
	data, err := json.MarshalIndent(buildFeature(m.track, m.settings), "", "  ")
	if err != nil {
		return fmt.Errorf("encode track: %w", err)
	}
	data = append(data, '\n')
	target := filepath.Join(m.dir, m.track.name)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write track: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("rename track: %w", err)
	}
	return nil
}

func (m *Manager) snapshotLocked() State {
	state := State{
		WhenEngineOn:          m.settings.WhenEngineOn,
		SampleIntervalSeconds: m.settings.SampleInterval.Seconds(),
		EngineKnown:           m.engineKnown,
		EngineOn:              m.engineOn,
		LastSampleAt:          m.lastSampleAt,
		LastError:             m.lastError,
		LastErrorAt:           m.lastErrorAt,
	}
	if m.track != nil {
		state.Tracking = true
		state.CurrentFile = m.track.name
		state.PointCount = len(m.track.times)
	}
	if state.LastSampleAt != nil {
		sampleAt := *state.LastSampleAt
		state.LastSampleAt = &sampleAt
	}
	if state.LastErrorAt != nil {
		errorAt := *state.LastErrorAt
		state.LastErrorAt = &errorAt
	}
	return state
}

func (m *Manager) notifyLocked(state State) {
	onChange := m.onChange
	if onChange != nil {
		onChange(state)
	}
}

type trackProperties struct {
	Name                  string   `json:"name"`
	StartTime             string   `json:"start_time"`
	EndTime               string   `json:"end_time"`
	PointCount            int      `json:"point_count"`
	SampleIntervalSeconds float64  `json:"sample_interval_seconds"`
	Times                 []string `json:"times"`
}

type trackGeometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

type trackFeature struct {
	Type       string          `json:"type"`
	Properties trackProperties `json:"properties"`
	Geometry   trackGeometry   `json:"geometry"`
}

func buildFeature(track *activeTrack, settings Settings) trackFeature {
	times := make([]string, 0, len(track.times))
	for _, t := range track.times {
		times = append(times, t.UTC().Format(time.RFC3339))
	}
	var startTime, endTime string
	if len(times) > 0 {
		startTime = times[0]
		endTime = times[len(times)-1]
	}
	return trackFeature{
		Type: "Feature",
		Properties: trackProperties{
			Name:                  track.name,
			StartTime:             startTime,
			EndTime:               endTime,
			PointCount:            len(track.points),
			SampleIntervalSeconds: settings.SampleInterval.Seconds(),
			Times:                 times,
		},
		Geometry: trackGeometry{Type: "LineString", Coordinates: track.points},
	}
}

func parseTrackFile(data []byte) ([]time.Time, [][]float64, bool) {
	var feature trackFeature
	if err := json.Unmarshal(data, &feature); err != nil {
		return nil, nil, false
	}
	if feature.Type != "Feature" || feature.Geometry.Type != "LineString" {
		return nil, nil, false
	}
	if len(feature.Properties.Times) != len(feature.Geometry.Coordinates) {
		return nil, nil, false
	}
	times := make([]time.Time, 0, len(feature.Properties.Times))
	for _, s := range feature.Properties.Times {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, nil, false
		}
		times = append(times, t)
	}
	for _, pos := range feature.Geometry.Coordinates {
		if len(pos) != 2 && len(pos) != 3 {
			return nil, nil, false
		}
	}
	return times, feature.Geometry.Coordinates, true
}

func engineSignalState(raw string) (known, on bool, ok bool) {
	frame, err := heating.ParseWireFrame(raw)
	if err != nil || len(frame.Data) < 3 || frame.Data[0]|frame.Data[1]<<8 != engineSignalID {
		return false, false, false
	}
	return true, frame.Data[2]&1 != 0, true
}

func validTrackName(name string) bool {
	if name == "" || filepath.Base(name) != name {
		return false
	}
	if !strings.HasPrefix(name, "track-") || !strings.HasSuffix(name, ".geojson") {
		return false
	}
	return true
}
