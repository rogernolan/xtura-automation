package waterhistory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultThreshold   = 5
	defaultSettling    = 10 * time.Minute
	defaultGrouping    = time.Hour
	defaultRetention   = 30 * 24 * time.Hour
	sampleHeartbeat    = time.Minute
	chartAverageWindow = 5 * time.Minute
	chartDisplayBucket = time.Hour
	recentBucket       = 10 * time.Minute
	hourlyBucket       = time.Hour
	chartCacheFile     = "chart-samples.json"
)

type chartWindowSample struct {
	at    time.Time
	value float64
}

type chartCache struct {
	processed   int
	throughAt   *time.Time
	buckets     map[int64]int
	samples     []Point
	freshWindow []chartWindowSample
	greyWindow  []chartWindowSample
	freshSum    float64
	greySum     float64
}

type candidate struct {
	Tank      string    `json:"tank"`
	Kind      string    `json:"kind"`
	Baseline  float64   `json:"baseline"`
	Current   float64   `json:"current"`
	StartedAt time.Time `json:"started_at"`
	MovedAt   time.Time `json:"moved_at"`
}

type persistedState struct {
	LastSampleAt        *time.Time `json:"last_sample_at,omitempty"`
	FreshBase           *float64   `json:"fresh_base,omitempty"`
	GreyBase            *float64   `json:"grey_base,omitempty"`
	Fresh               *float64   `json:"fresh,omitempty"`
	Grey                *float64   `json:"grey,omitempty"`
	FreshCand           *candidate `json:"fresh_candidate,omitempty"`
	GreyCand            *candidate `json:"grey_candidate,omitempty"`
	GreyDischargeOpenAt *time.Time `json:"grey_discharge_open_at,omitempty"`
}

type Store struct {
	mu      sync.Mutex
	options Options
	now     func() time.Time
	samples []Point
	events  []Event
	state   persistedState
	chart   chartCache
}

func New(options Options, now func() time.Time) *Store {
	if options.Threshold <= 0 {
		options.Threshold = defaultThreshold
	}
	if options.SettlingPeriod <= 0 {
		options.SettlingPeriod = defaultSettling
	}
	if options.GroupingWindow <= 0 {
		options.GroupingWindow = defaultGrouping
	}
	if options.Retention <= 0 {
		options.Retention = defaultRetention
	}
	if now == nil {
		now = time.Now
	}
	return &Store{options: options, now: now}
}

func (s *Store) Observe(sample Sample, observedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sample.At.IsZero() {
		sample.At = observedAt
	}
	sample.At = sample.At.UTC()
	observedAt = observedAt.UTC()
	if sample.GreyPercent != nil && s.greySampleIsBeforeLatestEmptyLocked(sample.At) {
		sample.GreyPercent = nil
	}
	if sample.FreshPercent == nil && sample.GreyPercent == nil {
		return false, nil
	}
	for _, value := range []*float64{sample.FreshPercent, sample.GreyPercent} {
		if value != nil && (*value < 0 || *value > 100) {
			return false, fmt.Errorf("water percentage must be between 0 and 100")
		}
	}
	offlineObservation := false
	if s.state.LastSampleAt != nil && !sample.At.After(*s.state.LastSampleAt) {
		if sample.At.Equal(*s.state.LastSampleAt) {
			return false, nil
		}
		offlineObservation = true
		sample.At = observedAt
	}
	eventCount := len(s.events)
	s.state.LastSampleAt = timePtr(sample.At)
	point := Point{At: sample.At, FreshPercent: cloneFloat(sample.FreshPercent), GreyPercent: cloneFloat(sample.GreyPercent)}
	storeSample := s.shouldStoreSample(point)
	if storeSample {
		s.samples = append(s.samples, point)
	}
	if sample.FreshPercent != nil {
		s.observeTank(TankFresh, KindFill, *sample.FreshPercent, observedAt)
		if offlineObservation {
			s.commitCandidate(TankFresh, observedAt)
		}
	}
	if sample.GreyPercent != nil {
		s.observeGreySampleLocked(*sample.GreyPercent)
	}
	s.state.Fresh = cloneFloat(sample.FreshPercentOr(s.state.Fresh))
	s.state.Grey = cloneFloat(sample.GreyPercentOr(s.state.Grey))
	if err := s.persistObservationLocked(point, eventCount, storeSample); err != nil {
		return false, err
	}
	if storeSample {
		s.chartSamplesLocked()
		s.trimRawSamplesLocked(sample.At)
		if err := s.persistChartLocked(); err != nil {
			return false, err
		}
	}
	return storeSample || len(s.events) > eventCount, nil
}

func (s *Store) RecordGreyDischargeOpen(at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at = at.UTC()
	if s.state.GreyDischargeOpenAt != nil && s.state.GreyDischargeOpenAt.Equal(at) {
		return false, nil
	}
	s.state.GreyDischargeOpenAt = timePtr(at)
	if err := s.persistObservationLocked(Point{}, len(s.events), false); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) RecordGreyEmpty(at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at = at.UTC()
	if s.hasGreyEmptyEventAtLocked(at) {
		if s.clearGreyDischargeStateLocked(at) {
			if err := s.persistObservationLocked(Point{}, len(s.events), false); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if s.state.GreyDischargeOpenAt == nil {
		return false, nil
	}
	eventStart := len(s.events)
	from := 0.0
	if s.state.Grey != nil {
		from = *s.state.Grey
	}
	to := 0.0
	s.events = append(s.events, Event{At: at, Tank: TankGrey, Kind: KindEmpty, From: from, To: to, Used: from})
	s.clearGreyDischargeStateLocked(at)
	if err := s.persistObservationLocked(Point{}, eventStart, false); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) shouldStoreSample(point Point) bool {
	if len(s.samples) == 0 {
		return true
	}
	last := s.samples[len(s.samples)-1]
	if !sameLevel(last.FreshPercent, point.FreshPercent) || !sameLevel(last.GreyPercent, point.GreyPercent) {
		return true
	}
	return point.At.Sub(last.At) >= sampleHeartbeat
}

func sameLevel(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *Store) observeGreySampleLocked(value float64) {
	if s.state.GreyDischargeOpenAt != nil {
		return
	}
	if s.state.GreyBase == nil {
		s.state.GreyBase = cloneFloat(&value)
		return
	}
	if value > *s.state.GreyBase {
		s.state.GreyBase = cloneFloat(&value)
		return
	}
	if *s.state.GreyBase-value < s.options.Threshold {
		return
	}
	if s.options.Logf != nil {
		s.options.Logf("grey level dropped from %.1f to %.1f without a pending discharge open", *s.state.GreyBase, value)
	}
	s.state.GreyBase = cloneFloat(&value)
}

func (s *Store) hasGreyEmptyEventAtLocked(at time.Time) bool {
	for index := len(s.events) - 1; index >= 0; index-- {
		event := s.events[index]
		if event.Tank == TankGrey && event.Kind == KindEmpty && event.At.Equal(at) {
			return true
		}
	}
	return false
}

func (s *Store) greySampleIsBeforeLatestEmptyLocked(at time.Time) bool {
	for index := len(s.events) - 1; index >= 0; index-- {
		event := s.events[index]
		if event.Tank == TankGrey && event.Kind == KindEmpty {
			return at.Before(event.At)
		}
	}
	return false
}

func (s *Store) clearGreyDischargeStateLocked(completedAt time.Time) bool {
	changed := false
	if s.state.GreyDischargeOpenAt != nil && !s.state.GreyDischargeOpenAt.After(completedAt) {
		s.state.GreyDischargeOpenAt = nil
		changed = true
	}
	if s.state.LastSampleAt == nil || !s.state.LastSampleAt.After(completedAt) {
		to := 0.0
		if s.state.Grey == nil || *s.state.Grey != to {
			changed = true
		}
		s.state.Grey = cloneFloat(&to)
		s.state.GreyBase = cloneFloat(&to)
		s.state.GreyCand = nil
	} else {
		// A sample received after the close belongs to the current tank state.
		// Keep that state, but rebase anomaly detection so the next sample is
		// compared with it rather than with the pre-discharge level.
		s.state.GreyBase = cloneFloat(s.state.Grey)
		s.state.GreyCand = nil
	}
	return changed
}

func (s *Store) commitCandidate(tank string, at time.Time) {
	var cand *candidate
	if tank == TankFresh {
		cand = s.state.FreshCand
	} else {
		cand = s.state.GreyCand
	}
	if cand == nil {
		return
	}
	s.events = append(s.events, Event{At: at, Tank: tank, Kind: cand.Kind, From: cand.Baseline, To: cand.Current, Used: abs(cand.Current - cand.Baseline)})
	base := cloneFloat(&cand.Current)
	if tank == TankFresh {
		s.state.FreshBase, s.state.FreshCand = base, nil
	} else {
		s.state.GreyBase, s.state.GreyCand = base, nil
	}
}

func (sample Sample) FreshPercentOr(previous *float64) *float64 {
	if sample.FreshPercent != nil {
		return sample.FreshPercent
	}
	return previous
}
func (sample Sample) GreyPercentOr(previous *float64) *float64 {
	if sample.GreyPercent != nil {
		return sample.GreyPercent
	}
	return previous
}

func (s *Store) observeTank(tank, kind string, value float64, at time.Time) {
	base := s.state.FreshBase
	cand := s.state.FreshCand
	if tank == TankGrey {
		base, cand = s.state.GreyBase, s.state.GreyCand
	}
	if base == nil {
		base = cloneFloat(&value)
		if tank == TankFresh {
			s.state.FreshBase = base
		} else {
			s.state.GreyBase = base
		}
		return
	}
	if cand == nil {
		if (kind == KindFill && value < baseValue(base)) || (kind == KindEmpty && value > baseValue(base)) {
			base = cloneFloat(&value)
		}
		moved := (kind == KindFill && value-baseValue(base) >= s.options.Threshold) || (kind == KindEmpty && baseValue(base)-value >= s.options.Threshold)
		if moved {
			cand = &candidate{Tank: tank, Kind: kind, Baseline: baseValue(base), Current: value, StartedAt: at, MovedAt: at}
		}
	} else {
		directionOK := (kind == KindFill && value >= cand.Current) || (kind == KindEmpty && value <= cand.Current)
		if !directionOK {
			cand = nil
			base = cloneFloat(&value)
		} else {
			if value != cand.Current {
				cand.MovedAt = at
			}
			cand.Current = value
			if at.Sub(cand.MovedAt) >= s.options.SettlingPeriod {
				s.events = append(s.events, Event{At: at, Tank: tank, Kind: kind, From: cand.Baseline, To: value, Used: abs(value - cand.Baseline)})
				base = cloneFloat(&value)
				cand = nil
			}
		}
	}
	if tank == TankFresh {
		s.state.FreshBase, s.state.FreshCand = base, cand
	} else {
		s.state.GreyBase, s.state.GreyCand = base, cand
	}
}

func (s *Store) Document(now time.Time) Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.UTC().Add(-s.options.Retention)
	doc := Document{}
	for _, sample := range s.samples {
		if !sample.At.Before(cutoff) {
			doc.Samples = append(doc.Samples, sample)
		}
	}
	for _, sample := range s.chartSamplesLocked() {
		if !sample.At.Before(cutoff) {
			doc.ChartSamples = append(doc.ChartSamples, sample)
		}
	}
	for _, event := range s.events {
		if !event.At.Before(cutoff) {
			doc.Events = append(doc.Events, event)
		}
	}
	if s.state.Fresh != nil {
		doc.Fresh = s.summaryLocked(TankFresh, *s.state.Fresh, now)
	}
	if s.state.Grey != nil {
		doc.Grey = s.summaryLocked(TankGrey, *s.state.Grey, now)
	}
	doc.Markers = s.groupMarkers(doc.Events)
	return doc
}

// chartSamplesLocked incrementally prepares the small, render-ready history
// sent to every client. Raw samples are append-only, so only the new suffix
// needs to pass through the moving average and display bucketing.
func (s *Store) chartSamplesLocked() []Point {
	if s.chart.buckets == nil {
		s.chart.buckets = make(map[int64]int)
	}
	for s.chart.processed < len(s.samples) {
		sample := s.samples[s.chart.processed]
		s.chart.processed++
		s.chart.throughAt = timePtr(sample.At)
		s.updateChartFieldLocked(sample, sample.FreshPercent, true)
		s.updateChartFieldLocked(sample, sample.GreyPercent, false)
	}
	return s.chart.samples
}

func (s *Store) seedChartWindowsLocked(limit int) {
	s.chart.freshWindow = nil
	s.chart.greyWindow = nil
	s.chart.freshSum = 0
	s.chart.greySum = 0
	if limit > len(s.samples) {
		limit = len(s.samples)
	}
	for _, sample := range s.samples[:limit] {
		s.addChartWindowLocked(sample.At, sample.FreshPercent, true)
		s.addChartWindowLocked(sample.At, sample.GreyPercent, false)
	}
}

func (s *Store) addChartWindowLocked(at time.Time, value *float64, fresh bool) {
	if value == nil {
		return
	}
	rounded := math.Round(*value)
	window := &s.chart.freshWindow
	sum := &s.chart.freshSum
	if !fresh {
		window = &s.chart.greyWindow
		sum = &s.chart.greySum
	}
	*window = append(*window, chartWindowSample{at: at, value: rounded})
	*sum += rounded
	cutoff := at.Add(-chartAverageWindow)
	for len(*window) > 0 && (*window)[0].at.Before(cutoff) {
		*sum -= (*window)[0].value
		*window = (*window)[1:]
	}
}

func (s *Store) updateChartFieldLocked(sample Point, value *float64, fresh bool) {
	if value == nil {
		return
	}
	s.addChartWindowLocked(sample.At, value, fresh)
	window := &s.chart.freshWindow
	sum := &s.chart.freshSum
	if !fresh {
		window = &s.chart.greyWindow
		sum = &s.chart.greySum
	}
	average := *sum / float64(len(*window))
	bucket := sample.At.UnixNano() / int64(chartDisplayBucket)
	index, exists := s.chart.buckets[bucket]
	if !exists {
		index = len(s.chart.samples)
		s.chart.buckets[bucket] = index
		s.chart.samples = append(s.chart.samples, Point{At: sample.At})
	} else if sample.At.After(s.chart.samples[index].At) {
		s.chart.samples[index].At = sample.At
	}
	if fresh {
		s.chart.samples[index].FreshPercent = cloneFloat(&average)
	} else {
		s.chart.samples[index].GreyPercent = cloneFloat(&average)
	}
}

func (s *Store) resetChartCacheLocked() {
	s.chart = chartCache{}
}

func (s *Store) trimRawSamplesLocked(at time.Time) {
	cutoff := at.Add(-chartAverageWindow)
	kept := s.samples[:0]
	for _, sample := range s.samples {
		if !sample.At.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	s.samples = kept
	s.chart.processed = len(s.samples)
}

func (s *Store) summaryLocked(tank string, current float64, now time.Time) Summary {
	var latest *Event
	for i := range s.events {
		if s.events[i].Tank == tank && (latest == nil || s.events[i].At.After(latest.At)) {
			latest = &s.events[i]
		}
	}
	if latest == nil {
		return Summary{}
	}
	at := latest.At
	days := now.UTC().Sub(at).Hours() / 24
	used := latest.To - current
	if tank == TankGrey {
		used = current - latest.To
	}
	if used < 0 {
		used = 0
	}
	return Summary{EventAt: &at, DaysSince: &days, UsedPercent: &used}
}

func (s *Store) groupMarkers(events []Event) []Marker {
	for i := range events {
		for j := i + 1; j < len(events); j++ {
			if events[j].At.Before(events[i].At) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
	var markers []Marker
	for _, event := range events {
		if len(markers) > 0 && event.At.Sub(markers[len(markers)-1].At) <= s.options.GroupingWindow && event.Tank != markers[len(markers)-1].Events[0].Tank {
			markers[len(markers)-1].Events = append(markers[len(markers)-1].Events, event)
			if event.At.After(markers[len(markers)-1].At) {
				markers[len(markers)-1].At = event.At
			}
			continue
		}
		markers = append(markers, Marker{At: event.At, Events: []Event{event}})
	}
	return markers
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := readNDJSON(filepath.Join(s.options.Directory, "samples.ndjson"), &s.samples); err != nil {
		return err
	}
	if err := readNDJSON(filepath.Join(s.options.Directory, "events.ndjson"), &s.events); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(s.options.Directory, "state.json"))
	if os.IsNotExist(err) {
		if err := s.compactLoadedSamplesLocked(); err != nil {
			return err
		}
		return s.initializeChartCacheLocked()
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return err
	}
	if err := s.compactLoadedSamplesLocked(); err != nil {
		return err
	}
	return s.initializeChartCacheLocked()
}

func (s *Store) initializeChartCacheLocked() error {
	if err := s.loadChartCacheLocked(); err == nil {
		replayFrom := 0
		if s.chart.throughAt != nil {
			for replayFrom < len(s.samples) && !s.samples[replayFrom].At.After(*s.chart.throughAt) {
				replayFrom++
			}
		}
		s.seedChartWindowsLocked(replayFrom)
		s.chart.processed = replayFrom
		s.chartSamplesLocked()
		if s.chart.processed > replayFrom {
			if err := s.persistChartLocked(); err != nil {
				return err
			}
		}
		s.trimRawSamplesLocked(s.latestSampleAtLocked())
		return nil
	}
	s.resetChartCacheLocked()
	s.chartSamplesLocked()
	s.trimRawSamplesLocked(s.latestSampleAtLocked())
	return s.persistLoadedCacheLocked()
}

func (s *Store) persistLoadedCacheLocked() error {
	if s.options.Directory == "" {
		return nil
	}
	if _, err := os.Stat(s.options.Directory); err != nil {
		if os.IsNotExist(err) || isPermission(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(filepath.Join(s.options.Directory, chartCacheFile)); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return nil
	}
	if err := s.persistChartLocked(); err != nil {
		if isPermission(err) {
			return nil
		}
		return err
	}
	if err := s.persistRawSamplesLocked(); err != nil && !isPermission(err) {
		return err
	}
	return nil
}

func isPermission(err error) bool {
	return err != nil && (os.IsPermission(err) || errors.Is(err, fs.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied"))
}

func (s *Store) latestSampleAtLocked() time.Time {
	if len(s.samples) == 0 {
		return s.now().UTC()
	}
	return s.samples[len(s.samples)-1].At
}

func (s *Store) loadChartCacheLocked() error {
	data, err := os.ReadFile(filepath.Join(s.options.Directory, chartCacheFile))
	if err != nil {
		return err
	}
	var persisted struct {
		ThroughAt *time.Time `json:"through_at,omitempty"`
		Samples   []Point    `json:"samples"`
	}
	if err := json.Unmarshal(data, &persisted); err == nil && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		s.chart.throughAt = persisted.ThroughAt
		s.chart.samples = persisted.Samples
	} else {
		var legacy []Point
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		s.chart.throughAt = nil
		s.chart.samples = nil
	}
	s.chart.buckets = make(map[int64]int, len(s.chart.samples))
	for index, sample := range s.chart.samples {
		s.chart.buckets[sample.At.UnixNano()/int64(chartDisplayBucket)] = index
	}
	s.chart.processed = 0
	return nil
}

func (s *Store) persistChartLocked() error {
	if s.options.Directory == "" {
		return nil
	}
	if err := os.MkdirAll(s.options.Directory, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		ThroughAt *time.Time `json:"through_at,omitempty"`
		Samples   []Point    `json:"samples"`
	}{ThroughAt: s.chart.throughAt, Samples: s.chart.samples})
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.options.Directory, chartCacheFile), data, 0o644)
}

func (s *Store) persistRawSamplesLocked() error {
	if s.options.Directory == "" {
		return nil
	}
	return writeNDJSON(filepath.Join(s.options.Directory, "samples.ndjson"), s.samples)
}

func (s *Store) compactLoadedSamplesLocked() error {
	compacted := compactSamples(s.samples)
	s.samples = compacted
	s.resetChartCacheLocked()
	return nil
}

func (s *Store) Compact(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.UTC().Add(-s.options.Retention)
	var all []Point
	if s.options.Directory != "" {
		if err := readNDJSON(filepath.Join(s.options.Directory, "samples.ndjson"), &all); err != nil {
			return err
		}
	}
	if len(all) == 0 {
		all = append(all, s.samples...)
	}
	recent := bucketPoints(all, cutoff, recentBucket)
	if s.options.Directory != "" {
		if err := s.archivePointsLocked(all, cutoff); err != nil {
			return err
		}
	}
	s.samples = recent
	s.resetChartCacheLocked()
	s.chartSamplesLocked()
	chartCutoff := cutoff
	chartKept := s.chart.samples[:0]
	for _, sample := range s.chart.samples {
		if !sample.At.Before(chartCutoff) {
			chartKept = append(chartKept, sample)
		}
	}
	s.chart.samples = chartKept
	s.chart.buckets = make(map[int64]int, len(chartKept))
	for index, sample := range chartKept {
		s.chart.buckets[sample.At.UnixNano()/int64(chartDisplayBucket)] = index
	}
	if err := s.persistChartLocked(); err != nil {
		return err
	}
	if s.options.Directory != "" {
		if err := s.persistLocked(); err != nil {
			return err
		}
	}
	s.trimRawSamplesLocked(s.latestSampleAtLocked())
	return nil
}

func (s *Store) archivePointsLocked(points []Point, cutoff time.Time) error {
	dir := filepath.Join(s.options.Directory, "hourly")
	archives := make(map[string][]Point)
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "samples-") || filepath.Ext(entry.Name()) != ".ndjson" {
				continue
			}
			var stored []Point
			if err := readNDJSON(filepath.Join(dir, entry.Name()), &stored); err != nil {
				return err
			}
			archives[entry.Name()] = stored
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, point := range points {
		if point.At.Before(cutoff) {
			name := "samples-" + point.At.UTC().Format("2006-01") + ".ndjson"
			archives[name] = append(archives[name], point)
		}
	}
	for name, points := range archives {
		if err := writePointsAtomic(filepath.Join(dir, name), bucketPoints(points, time.Time{}, hourlyBucket)); err != nil {
			return err
		}
	}
	return nil
}

func bucketPoints(points []Point, cutoff time.Time, bucket time.Duration) []Point {
	buckets := make(map[int64]Point)
	for _, point := range points {
		if !cutoff.IsZero() && point.At.Before(cutoff) {
			continue
		}
		key := point.At.Truncate(bucket).UnixNano()
		previous, ok := buckets[key]
		if !ok {
			buckets[key] = point
			continue
		}
		if point.At.After(previous.At) {
			previous.At = point.At
		}
		if point.FreshPercent != nil {
			previous.FreshPercent = cloneFloat(point.FreshPercent)
		}
		if point.GreyPercent != nil {
			previous.GreyPercent = cloneFloat(point.GreyPercent)
		}
		buckets[key] = previous
	}
	out := make([]Point, 0, len(buckets))
	for _, point := range buckets {
		out = append(out, point)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func writePointsAtomic(path string, points []Point) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var data bytes.Buffer
	for _, point := range points {
		encoded, err := json.Marshal(point)
		if err != nil {
			return err
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	return writeFileAtomic(path, data.Bytes(), 0o644)
}

func writeEventsAtomic(path string, events []Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var data bytes.Buffer
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	return writeFileAtomic(path, data.Bytes(), 0o644)
}

func compactSamples(samples []Point) []Point {
	kept := make([]Point, 0, len(samples))
	for _, sample := range samples {
		if len(kept) == 0 {
			kept = append(kept, sample)
			continue
		}
		last := kept[len(kept)-1]
		if !sameLevel(last.FreshPercent, sample.FreshPercent) || !sameLevel(last.GreyPercent, sample.GreyPercent) || sample.At.Sub(last.At) >= sampleHeartbeat {
			kept = append(kept, sample)
		}
	}
	return kept
}

func (s *Store) persistLocked() error {
	if s.options.Directory == "" {
		return nil
	}
	if err := os.MkdirAll(s.options.Directory, 0o755); err != nil {
		return err
	}
	if err := writePointsAtomic(filepath.Join(s.options.Directory, "samples.ndjson"), s.samples); err != nil {
		return err
	}
	if err := writeEventsAtomic(filepath.Join(s.options.Directory, "events.ndjson"), s.events); err != nil {
		return err
	}
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.options.Directory, "state.json"), data, 0o644)
}

func (s *Store) persistObservationLocked(sample Point, eventStart int, storeSample bool) error {
	if s.options.Directory == "" {
		return nil
	}
	if err := os.MkdirAll(s.options.Directory, 0o755); err != nil {
		return err
	}
	if storeSample {
		if err := appendNDJSON(filepath.Join(s.options.Directory, "samples.ndjson"), sample); err != nil {
			return err
		}
	}
	for _, event := range s.events[eventStart:] {
		if err := appendNDJSON(filepath.Join(s.options.Directory, "events.ndjson"), event); err != nil {
			return err
		}
	}
	return writeState(filepath.Join(s.options.Directory, "state.json"), s.state)
}

func appendNDJSON(path string, value interface{}) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	return err
}

func writeState(path string, state persistedState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".water-history-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func readNDJSON(path string, target interface{}) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	var lines []json.RawMessage
	lineStart := int64(0)
	for scanner.Scan() {
		line := append(json.RawMessage(nil), scanner.Bytes()...)
		lines = append(lines, line)
		lineStart += int64(len(line)) + 1
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	switch out := target.(type) {
	case *[]Point:
		for index, line := range lines {
			var value Point
			if err := json.Unmarshal(line, &value); err != nil {
				log.Printf("water history: skipping unparsable line in %s: %v", path, err)
				if index == len(lines)-1 && lineStart-1 == info.Size() {
					if repairErr := truncateCorruptTail(path, lineStart-int64(len(line))-1); repairErr != nil {
						log.Printf("water history: unable to repair corrupt tail in %s: %v", path, repairErr)
					}
				}
				continue
			}
			*out = append(*out, value)
		}
	case *[]Event:
		for index, line := range lines {
			var value Event
			if err := json.Unmarshal(line, &value); err != nil {
				log.Printf("water history: skipping unparsable line in %s: %v", path, err)
				if index == len(lines)-1 && lineStart-1 == info.Size() {
					if repairErr := truncateCorruptTail(path, lineStart-int64(len(line))-1); repairErr != nil {
						log.Printf("water history: unable to repair corrupt tail in %s: %v", path, repairErr)
					}
				}
				continue
			}
			*out = append(*out, value)
		}
	}
	return nil
}

func truncateCorruptTail(path string, offset int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(offset)
}

func writeNDJSON(path string, values interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var encoded []interface{}
	switch items := values.(type) {
	case []Point:
		for _, item := range items {
			encoded = append(encoded, item)
		}
	case []Event:
		for _, item := range items {
			encoded = append(encoded, item)
		}
	}
	for _, value := range encoded {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
func timePtr(value time.Time) *time.Time { return &value }
func baseValue(value *float64) float64   { return *value }
func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
