package waterhistory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultThreshold = 5
	defaultSettling  = 10 * time.Minute
	defaultGrouping  = time.Hour
	defaultRetention = 7 * 24 * time.Hour
)

type candidate struct {
	Tank      string    `json:"tank"`
	Kind      string    `json:"kind"`
	Baseline  float64   `json:"baseline"`
	Current   float64   `json:"current"`
	StartedAt time.Time `json:"started_at"`
	MovedAt   time.Time `json:"moved_at"`
}

type persistedState struct {
	LastSampleAt *time.Time `json:"last_sample_at,omitempty"`
	FreshBase    *float64   `json:"fresh_base,omitempty"`
	GreyBase     *float64   `json:"grey_base,omitempty"`
	Fresh        *float64   `json:"fresh,omitempty"`
	Grey         *float64   `json:"grey,omitempty"`
	FreshCand    *candidate `json:"fresh_candidate,omitempty"`
	GreyCand     *candidate `json:"grey_candidate,omitempty"`
}

type Store struct {
	mu      sync.Mutex
	options Options
	now     func() time.Time
	samples []Point
	events  []Event
	state   persistedState
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
	s.samples = append(s.samples, Point{At: sample.At, FreshPercent: cloneFloat(sample.FreshPercent), GreyPercent: cloneFloat(sample.GreyPercent)})
	if sample.FreshPercent != nil {
		s.observeTank(TankFresh, KindFill, *sample.FreshPercent, observedAt)
		if offlineObservation {
			s.commitCandidate(TankFresh, observedAt)
		}
	}
	if sample.GreyPercent != nil {
		s.observeTank(TankGrey, KindEmpty, *sample.GreyPercent, observedAt)
		if offlineObservation {
			s.commitCandidate(TankGrey, observedAt)
		}
	}
	s.state.Fresh = cloneFloat(sample.FreshPercentOr(s.state.Fresh))
	s.state.Grey = cloneFloat(sample.GreyPercentOr(s.state.Grey))
	if err := s.persistObservationLocked(s.samples[len(s.samples)-1], eventCount); err != nil {
		return false, err
	}
	return true, nil
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
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.state)
}

func (s *Store) Compact(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.UTC().Add(-s.options.Retention)
	kept := s.samples[:0]
	for _, sample := range s.samples {
		if !sample.At.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	s.samples = kept
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	if s.options.Directory == "" {
		return nil
	}
	if err := os.MkdirAll(s.options.Directory, 0o755); err != nil {
		return err
	}
	if err := writeNDJSON(filepath.Join(s.options.Directory, "samples.ndjson"), s.samples); err != nil {
		return err
	}
	if err := writeNDJSON(filepath.Join(s.options.Directory, "events.ndjson"), s.events); err != nil {
		return err
	}
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.options.Directory, "state.json"), data, 0o644)
}

func (s *Store) persistObservationLocked(sample Point, eventStart int) error {
	if s.options.Directory == "" {
		return nil
	}
	if err := os.MkdirAll(s.options.Directory, 0o755); err != nil {
		return err
	}
	if err := appendNDJSON(filepath.Join(s.options.Directory, "samples.ndjson"), sample); err != nil {
		return err
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
	return os.WriteFile(path, data, 0o644)
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
	scanner := bufio.NewScanner(file)
	var lines []json.RawMessage
	for scanner.Scan() {
		lines = append(lines, append(json.RawMessage(nil), scanner.Bytes()...))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	switch out := target.(type) {
	case *[]Point:
		for _, line := range lines {
			var value Point
			if err := json.Unmarshal(line, &value); err != nil {
				return err
			}
			*out = append(*out, value)
		}
	case *[]Event:
		for _, line := range lines {
			var value Event
			if err := json.Unmarshal(line, &value); err != nil {
				return err
			}
			*out = append(*out, value)
		}
	}
	return nil
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
