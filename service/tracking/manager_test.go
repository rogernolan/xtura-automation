package tracking_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"empirebus-tests/heating"
	domainlocation "empirebus-tests/service/domains/location"
	"empirebus-tests/service/tracking"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

type fakePoll struct {
	mu    sync.Mutex
	calls int
	fixes []domainlocation.Fix
	errs  []error
}

func newFakePoll() *fakePoll { return &fakePoll{} }

func (p *fakePoll) add(fix domainlocation.Fix, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fixes = append(p.fixes, fix)
	p.errs = append(p.errs, err)
}

func (p *fakePoll) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *fakePoll) poll(_ context.Context) (domainlocation.Fix, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.calls
	p.calls++
	if len(p.fixes) == 0 {
		return domainlocation.Fix{}, nil
	}
	if i >= len(p.fixes) {
		i = len(p.fixes) - 1
	}
	return p.fixes[i], p.errs[i]
}

type stateRecorder struct {
	mu     sync.Mutex
	states []tracking.State
}

func (r *stateRecorder) onChange(s tracking.State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *stateRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func engineFrame(signal, value int) string {
	return fmt.Sprintf(`{"messagetype":16,"messagecmd":0,"size":3,"data":[%d,%d,%d]}`,
		signal&0xff, signal>>8, value)
}

func fix(lat, lon float64, altitude *float64, at time.Time) domainlocation.Fix {
	return domainlocation.Fix{Latitude: lat, Longitude: lon, Altitude: altitude, UpdatedAt: at}
}

func alt(v float64) *float64 { return &v }

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

type parsedTrack struct {
	Times  []time.Time
	Coords [][]float64
}

func parseFeature(t *testing.T, data []byte) parsedTrack {
	t.Helper()
	var feature struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string      `json:"type"`
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	}
	if err := json.Unmarshal(data, &feature); err != nil {
		t.Fatalf("unmarshal track: %v", err)
	}
	if feature.Type != "Feature" || feature.Geometry.Type != "LineString" {
		t.Fatalf("unexpected feature: %+v", feature)
	}
	rawTimes, _ := feature.Properties["times"].([]any)
	var times []time.Time
	for _, raw := range rawTimes {
		s, _ := raw.(string)
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse time %q: %v", s, err)
		}
		times = append(times, tm)
	}
	if len(times) != len(feature.Geometry.Coordinates) {
		t.Fatalf("times(%d) != coordinates(%d)", len(times), len(feature.Geometry.Coordinates))
	}
	return parsedTrack{Times: times, Coords: feature.Geometry.Coordinates}
}

func TestEngineOnlySessionLifecycle(t *testing.T) {
	dir := t.TempDir()
	onAt := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(onAt)
	poll := newFakePoll()
	poll.add(fix(51.065375, 0.854362, nil, time.Time{}), nil)
	poll.add(fix(51.0655, 0.8544, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, OnlyWhenEngineOn: true, SampleInterval: 5 * time.Second})

	state := manager.State()
	if !state.Enabled || !state.OnlyWhenEngineOn || state.EngineKnown || state.Tracking {
		t.Fatalf("initial state = %+v", state)
	}
	manager.Sample(context.Background())
	if got := poll.callCount(); got != 0 {
		t.Fatalf("sampled %d times while engine state unknown", got)
	}

	manager.ObserveFrame(onAt, heating.DirectionReceive, engineFrame(11, 1))
	state = manager.State()
	if !state.EngineKnown || !state.EngineOn || !state.Tracking {
		t.Fatalf("state after engine-on = %+v", state)
	}
	wantName := "track-" + onAt.Format("20060102T150405Z") + ".geojson"
	if state.CurrentFile != wantName {
		t.Fatalf("current file = %q, want %q", state.CurrentFile, wantName)
	}

	clock.set(onAt.Add(5 * time.Second))
	manager.Sample(context.Background())
	clock.set(onAt.Add(10 * time.Second))
	manager.Sample(context.Background())
	if got := manager.State().PointCount; got != 2 {
		t.Fatalf("point count = %d, want 2", got)
	}
	track := parseFeature(t, readFile(t, filepath.Join(dir, wantName)))
	if len(track.Times) != 2 || len(track.Coords) != 2 {
		t.Fatalf("track has %d times / %d coords", len(track.Times), len(track.Coords))
	}
	if !track.Times[0].Equal(onAt.Add(5*time.Second)) || !track.Times[1].Equal(onAt.Add(10*time.Second)) {
		t.Fatalf("track times = %v", track.Times)
	}

	manager.ObserveFrame(onAt.Add(15*time.Second), heating.DirectionReceive, engineFrame(11, 0))
	state = manager.State()
	if state.EngineOn || state.Tracking || state.CurrentFile != "" || state.PointCount != 0 {
		t.Fatalf("state after engine-off = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(dir, wantName)); err != nil {
		t.Fatalf("finalized track missing: %v", err)
	}
}

func TestContinuousDayRotationAndResume(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(day1)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	manager.Sample(context.Background())
	clock.set(day1.Add(time.Minute))
	manager.Sample(context.Background())

	day1Name := "track-2026-08-13.geojson"
	if got := manager.State().PointCount; got != 2 {
		t.Fatalf("point count = %d, want 2", got)
	}
	if _, err := os.Stat(filepath.Join(dir, day1Name)); err != nil {
		t.Fatalf("day 1 track missing: %v", err)
	}

	day2 := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	clock.set(day2)
	manager.Sample(context.Background())
	day2Name := "track-2026-08-14.geojson"
	if got := manager.State().PointCount; got != 1 {
		t.Fatalf("point count after rotation = %d, want 1", got)
	}
	if manager.State().CurrentFile != day2Name {
		t.Fatalf("current file = %q, want %q", manager.State().CurrentFile, day2Name)
	}

	clock2 := newFakeClock(day2.Add(2 * time.Hour))
	manager2 := tracking.New(dir, poll.poll, clock2.now, discardLogger())
	manager2.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	manager2.Sample(context.Background())
	if got := manager2.State().PointCount; got != 2 {
		t.Fatalf("resumed point count = %d, want 2", got)
	}
	track := parseFeature(t, readFile(t, filepath.Join(dir, day2Name)))
	if len(track.Times) != 2 || !track.Times[0].Equal(day2) || !track.Times[1].Equal(day2.Add(2*time.Hour)) {
		t.Fatalf("resumed times = %v", track.Times)
	}
}

func TestUnknownEngineStateBlocksSamplingInEngineOnlyMode(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, OnlyWhenEngineOn: true, SampleInterval: 5 * time.Second})

	for i := 0; i < 3; i++ {
		manager.Sample(context.Background())
	}
	if got := poll.callCount(); got != 0 {
		t.Fatalf("polled %d times while engine state unknown", got)
	}

	manager.ObserveFrame(start, heating.DirectionReceive, engineFrame(11, 1))
	manager.Sample(context.Background())
	if got := poll.callCount(); got != 1 {
		t.Fatalf("polled %d times, want 1", got)
	}

	manager.ObserveFrame(start.Add(time.Second), heating.DirectionReceive, engineFrame(11, 0))
	manager.Sample(context.Background())
	manager.Sample(context.Background())
	if got := poll.callCount(); got != 1 {
		t.Fatalf("polled %d times after engine-off, want 1", got)
	}
}

func TestSampleWritesValidGeoJSONWithAlignedTimes(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.065375, 0.854362, nil, time.Time{}), nil)
	poll.add(fix(51.0655, 0.8544, nil, time.Time{}), nil)
	poll.add(fix(51.0656, 0.85445, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, OnlyWhenEngineOn: true, SampleInterval: 5 * time.Second})
	manager.ObserveFrame(start, heating.DirectionReceive, engineFrame(11, 1))
	for i := 1; i <= 3; i++ {
		clock.set(start.Add(time.Duration(i) * 5 * time.Second))
		manager.Sample(context.Background())
	}

	name := "track-" + start.Format("20060102T150405Z") + ".geojson"
	data := readFile(t, filepath.Join(dir, name))
	var raw struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string      `json:"type"`
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("track is not valid JSON: %v", err)
	}
	if raw.Type != "Feature" || raw.Geometry.Type != "LineString" {
		t.Fatalf("unexpected feature: %+v", raw)
	}
	if raw.Properties["name"] != name {
		t.Fatalf("name = %v", raw.Properties["name"])
	}
	if raw.Properties["point_count"].(float64) != 3 {
		t.Fatalf("point_count = %v", raw.Properties["point_count"])
	}
	if raw.Properties["start_time"] != "2026-08-13T09:40:05Z" || raw.Properties["end_time"] != "2026-08-13T09:40:15Z" {
		t.Fatalf("start/end = %v / %v", raw.Properties["start_time"], raw.Properties["end_time"])
	}
	if raw.Properties["sample_interval_seconds"].(float64) != 5 {
		t.Fatalf("sample_interval_seconds = %v", raw.Properties["sample_interval_seconds"])
	}
	times := parseFeature(t, data).Times
	if len(times) != len(raw.Geometry.Coordinates) {
		t.Fatalf("times and coordinates not aligned")
	}
	for i, tm := range times {
		if !tm.Equal(start.Add(time.Duration(i+1) * 5 * time.Second)) {
			t.Fatalf("time[%d] = %v", i, tm)
		}
	}
}

func TestAltitudeStoredAsThirdCoordinateElementWhenPresent(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.065375, 0.854362, alt(7), time.Time{}), nil)
	poll.add(fix(51.0655, 0.8544, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	manager.Sample(context.Background())
	clock.set(start.Add(5 * time.Second))
	manager.Sample(context.Background())

	track := parseFeature(t, readFile(t, filepath.Join(dir, "track-2026-08-13.geojson")))
	if len(track.Coords) != 2 {
		t.Fatalf("coords = %v", track.Coords)
	}
	if len(track.Coords[0]) != 3 || track.Coords[0][0] != 0.854362 || track.Coords[0][1] != 51.065375 || track.Coords[0][2] != 7 {
		t.Fatalf("coords[0] = %v, want [lon, lat, alt]", track.Coords[0])
	}
	if len(track.Coords[1]) != 2 || track.Coords[1][0] != 0.8544 || track.Coords[1][1] != 51.0655 {
		t.Fatalf("coords[1] = %v, want [lon, lat]", track.Coords[1])
	}
}

func TestAtomicRewriteLeavesValidFileAfterEachSample(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	path := filepath.Join(dir, "track-2026-08-13.geojson")
	for i := 0; i < 5; i++ {
		clock.set(start.Add(time.Duration(i) * 5 * time.Second))
		manager.Sample(context.Background())
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".tmp" {
				t.Fatalf("leftover temp file %s after sample %d", entry.Name(), i)
			}
		}
		track := parseFeature(t, readFile(t, path))
		if len(track.Times) != i+1 || len(track.Coords) != i+1 {
			t.Fatalf("after sample %d: %d times / %d coords", i, len(track.Times), len(track.Coords))
		}
	}
}

func TestListReadDeleteAndPathTraversalRejection(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	manager.Sample(context.Background())
	name := "track-2026-08-13.geojson"
	expected := readFile(t, filepath.Join(dir, name))

	for _, bad := range []string{"../track-x.geojson", "/etc/passwd", "track-x.txt", "other.geojson", "track-", "", "track-a.geojson/x"} {
		if _, err := manager.Read(bad); err == nil {
			t.Fatalf("Read(%q) should be rejected", bad)
		}
		if err := manager.Delete(bad); err == nil {
			t.Fatalf("Delete(%q) should be rejected", bad)
		}
	}

	infoList, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infoList) != 1 {
		t.Fatalf("list = %+v", infoList)
	}
	info := infoList[0]
	if info.Name != name || info.Bytes != int64(len(expected)) || info.PointCount != 1 {
		t.Fatalf("info = %+v", info)
	}
	if info.StartTime == nil || info.EndTime == nil || !info.StartTime.Equal(start) || !info.EndTime.Equal(start) {
		t.Fatalf("info times = %v / %v", info.StartTime, info.EndTime)
	}

	data, err := manager.Read(name)
	if err != nil || string(data) != string(expected) {
		t.Fatalf("read = %v, %v", string(data), err)
	}

	if err := manager.Delete(name); err != nil {
		t.Fatal(err)
	}
	infoList, err = manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infoList) != 0 {
		t.Fatalf("list after delete = %+v", infoList)
	}
	if err := manager.Delete(name); err == nil {
		t.Fatal("deleting a missing file should error")
	}
	if _, err := manager.Read(name); err == nil {
		t.Fatal("reading a missing file should error")
	}
}

func TestSampleGeneratesTrackFileInTempDirectory(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.065375, 0.854362, alt(7), time.Time{}), nil)
	poll.add(fix(51.0655, 0.8544, nil, time.Time{}), nil)
	poll.add(fix(51.0656, 0.85445, alt(6), time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	for i := 1; i <= 3; i++ {
		clock.set(start.Add(time.Duration(i) * 5 * time.Second))
		manager.Sample(context.Background())
	}

	track := parseFeature(t, readFile(t, filepath.Join(dir, "track-2026-08-13.geojson")))
	if len(track.Times) != 3 || len(track.Coords) != 3 {
		t.Fatalf("track has %d points", len(track.Times))
	}
	if len(track.Coords[0]) != 3 || len(track.Coords[1]) != 2 || len(track.Coords[2]) != 3 {
		t.Fatalf("mixed altitudes not preserved: %v", track.Coords)
	}
}

func TestPollFailureRecordsLastErrorAndContinues(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(domainlocation.Fix{}, errors.New("router unreachable"))
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	poll.add(domainlocation.Fix{}, errors.New("router unreachable again"))
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, OnlyWhenEngineOn: true, SampleInterval: 5 * time.Second})
	manager.ObserveFrame(start, heating.DirectionReceive, engineFrame(11, 1))

	manager.Sample(context.Background())
	state := manager.State()
	if state.LastError == "" || state.LastErrorAt == nil || state.PointCount != 0 || !state.Tracking {
		t.Fatalf("state after poll failure = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(dir, state.CurrentFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file should not be created on poll failure, err=%v", err)
	}

	clock.set(start.Add(5 * time.Second))
	manager.Sample(context.Background())
	state = manager.State()
	if state.LastError != "" || state.LastErrorAt != nil || state.PointCount != 1 {
		t.Fatalf("state after recovery = %+v", state)
	}
	name := "track-" + start.Format("20060102T150405Z") + ".geojson"
	track := parseFeature(t, readFile(t, filepath.Join(dir, name)))
	if len(track.Times) != 1 {
		t.Fatalf("track split unexpectedly: %v", track.Times)
	}

	clock.set(start.Add(10 * time.Second))
	manager.Sample(context.Background())
	state = manager.State()
	if state.LastError == "" || state.PointCount != 1 {
		t.Fatalf("state after second failure = %+v", state)
	}
	track = parseFeature(t, readFile(t, filepath.Join(dir, name)))
	if len(track.Times) != 1 {
		t.Fatalf("track split on poll failure: %v", track.Times)
	}
}

func TestConfigureDisableFinalizesActiveTrack(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 5 * time.Second})
	manager.Sample(context.Background())
	if state := manager.State(); !state.Tracking || state.PointCount != 1 {
		t.Fatalf("state before disable = %+v", state)
	}
	before := poll.callCount()

	manager.Configure(tracking.Settings{Enabled: false, SampleInterval: 5 * time.Second})
	state := manager.State()
	if state.Tracking || state.CurrentFile != "" || state.PointCount != 0 {
		t.Fatalf("state after disable = %+v", state)
	}
	manager.Sample(context.Background())
	if got := poll.callCount(); got != before {
		t.Fatal("sampled while disabled")
	}
}

func TestOnChangeNotifiedOnTransitionsAndSamples(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, OnlyWhenEngineOn: true, SampleInterval: 5 * time.Second})
	rec := &stateRecorder{}
	manager.SetOnChange(rec.onChange)

	manager.ObserveFrame(start, heating.DirectionReceive, engineFrame(11, 1))
	if got := rec.count(); got != 1 {
		t.Fatalf("notifications after engine-on = %d, want 1", got)
	}
	manager.ObserveFrame(start.Add(time.Second), heating.DirectionReceive, engineFrame(11, 1))
	manager.ObserveFrame(start.Add(2*time.Second), heating.DirectionReceive, engineFrame(101, 1))
	manager.ObserveFrame(start.Add(3*time.Second), heating.DirectionSend, engineFrame(11, 1))
	manager.ObserveFrame(start.Add(4*time.Second), heating.DirectionReceive, `{"messagetype":16,"messagecmd":0,"size":3,"data":[11]}`)
	if got := rec.count(); got != 1 {
		t.Fatalf("notifications after non-transitions = %d, want 1", got)
	}
	manager.Sample(context.Background())
	if got := rec.count(); got != 2 {
		t.Fatalf("notifications after sample = %d, want 2", got)
	}
	manager.ObserveFrame(start.Add(5*time.Second), heating.DirectionReceive, engineFrame(11, 0))
	if got := rec.count(); got != 3 {
		t.Fatalf("notifications after engine-off = %d, want 3", got)
	}
}

func TestStartSamplesOnIntervalAndFinalizesOnCancel(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx)
	defer cancel()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if manager.State().PointCount >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := manager.State().PointCount; got < 1 {
		t.Fatalf("point count = %d after start", got)
	}
	cancel()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !manager.State().Tracking {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if state := manager.State(); state.Tracking {
		t.Fatalf("track still active after cancel: %+v", state)
	}
}

func TestConfigureChangesSampleIntervalLive(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 13, 9, 40, 0, 0, time.UTC)
	clock := newFakeClock(start)
	poll := newFakePoll()
	poll.add(fix(51.0, 0.85, nil, time.Time{}), nil)
	manager := tracking.New(dir, poll.poll, clock.now, discardLogger())
	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 30 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := manager.State().PointCount; got != 0 {
			t.Fatalf("sampled %d times before interval change, want 0", got)
		}
		time.Sleep(5 * time.Millisecond)
	}

	manager.Configure(tracking.Settings{Enabled: true, SampleInterval: 10 * time.Millisecond})

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := manager.State().PointCount; got > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := manager.State().PointCount; got < 1 {
		t.Fatal("no sample within deadline after live interval change")
	}
}

func TestStateAndFileInfoJSONShape(t *testing.T) {
	now := time.Date(2026, 8, 13, 9, 40, 5, 0, time.UTC)
	state := tracking.State{
		Enabled:               true,
		OnlyWhenEngineOn:      true,
		SampleIntervalSeconds: 5,
		EngineKnown:           true,
		EngineOn:              true,
		Tracking:              true,
		CurrentFile:           "track-x.geojson",
		PointCount:            2,
		LastSampleAt:          &now,
		LastError:             "boom",
		LastErrorAt:           &now,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"enabled", "only_when_engine_on", "sample_interval_seconds", "engine_known", "engine_on", "tracking", "current_file", "point_count", "last_sample_at", "last_error", "last_error_at"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("state JSON missing key %q: %s", key, data)
		}
	}

	info := tracking.FileInfo{Name: "track-x.geojson", Bytes: 12, StartTime: &now, EndTime: &now, PointCount: 2}
	data, err = json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "bytes", "start_time", "end_time", "point_count"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("file info JSON missing key %q: %s", key, data)
		}
	}
}
