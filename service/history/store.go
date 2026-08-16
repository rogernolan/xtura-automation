// Package history stores per-sensor temperature history in an NDJSON file per
// sensor plus an in-memory ring buffer for the recent window.
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"empirebus-tests/service/domains/sensors"
)

// DefaultWindow is how much recent history the store keeps in memory.
const DefaultWindow = 2 * time.Hour

// DefaultRetention is how long persisted history is kept before compaction.
const DefaultRetention = 7 * 24 * time.Hour

// Store keeps per-sensor sample history.
type Store struct {
	mu        sync.Mutex
	dir       string
	window    time.Duration
	retention time.Duration
	now       func() time.Time
	logger    *log.Logger

	ring  map[string][]sensors.Sample
	files map[string]*os.File
}

// New creates a store rooted at dir. now may be nil for time.Now.
func New(dir string, window time.Duration, retention time.Duration, now func() time.Time, logger *log.Logger) *Store {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Store{
		dir:       dir,
		window:    window,
		retention: retention,
		now:       now,
		logger:    logger,
		ring:      make(map[string][]sensors.Sample),
		files:     make(map[string]*os.File),
	}
}

// Dir returns the persistence directory.
func (s *Store) Dir() string {
	return s.dir
}

// Seed prepopulates the in-memory ring for id from a startup load; it does not
// touch disk.
func (s *Store) Seed(id string, samples []sensors.Sample) {
	cutoff := s.now().Add(-s.window)
	kept := make([]sensors.Sample, 0, len(samples))
	for _, sample := range samples {
		if sample.At.Before(cutoff) {
			continue
		}
		kept = append(kept, sample)
	}
	s.mu.Lock()
	s.ring[id] = kept
	s.mu.Unlock()
}

// Append records a sample for id and persists it to <id>.ndjson.
func (s *Store) Append(id string, sample sensors.Sample) error {
	now := s.now()
	if sample.At.IsZero() {
		sample.At = now
	}
	sample.At = sample.At.UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendRingLocked(id, sample)
	return s.persistLocked(id, sample)
}

func (s *Store) appendRingLocked(id string, sample sensors.Sample) {
	cutoff := sample.At.Add(-s.window)
	samples := s.ring[id]
	samples = append(samples, sample)
	trimmed := samples[:0]
	for _, existing := range samples {
		if existing.At.After(cutoff) || existing.At.Equal(cutoff) {
			trimmed = append(trimmed, existing)
		}
	}
	s.ring[id] = trimmed
}

func (s *Store) persistLocked(id string, sample sensors.Sample) error {
	file := s.files[id]
	if file == nil {
		var err error
		file, err = s.openFileLocked(id)
		if err != nil {
			return err
		}
		s.files[id] = file
	}
	line, err := json.Marshal(sample)
	if err != nil {
		return fmt.Errorf("marshal sensor sample: %w", err)
	}
	line = append(line, '\n')
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		delete(s.files, id)
		return fmt.Errorf("write sensor history: %w", err)
	}
	return nil
}

func (s *Store) openFileLocked(id string) (*os.File, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("create sensor history directory: %w", err)
	}
	path := filepath.Join(s.dir, id+".ndjson")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open sensor history file: %w", err)
	}
	return file, nil
}

// Recent returns samples for id within the window, oldest first.
func (s *Store) Recent(id string, now time.Time) []sensors.Sample {
	cutoff := now.Add(-s.window)
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []sensors.Sample
	for _, sample := range s.ring[id] {
		if sample.At.After(cutoff) || sample.At.Equal(cutoff) {
			result = append(result, sample)
		}
	}
	return result
}

// LoadTail reads persisted history for id from disk, returning samples within
// the window relative to now. It is used to seed the ring on startup.
func (s *Store) LoadTail(id string, now time.Time) ([]sensors.Sample, error) {
	path := filepath.Join(s.dir, id+".ndjson")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open sensor history for load: %w", err)
	}
	defer file.Close()

	cutoff := now.Add(-s.window)
	var samples []sensors.Sample
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var sample sensors.Sample
		if err := json.Unmarshal(line, &sample); err != nil {
			s.logger.Printf("sensor history: skipping unparsable line in %s: %v", path, err)
			continue
		}
		if sample.At.Before(cutoff) {
			continue
		}
		samples = append(samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read sensor history: %w", err)
	}
	return samples, nil
}

// Compact rewrites every history file keeping only samples within the
// retention window.
func (s *Store) Compact() error {
	cutoff := s.now().Add(-s.retention)
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, file := range s.files {
		if err := file.Close(); err != nil {
			s.logger.Printf("sensor history: close %s: %v", id, err)
		}
		delete(s.files, id)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read sensor history directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ndjson" {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		samples, err := s.readAllLocked(path)
		if err != nil {
			s.logger.Printf("sensor history: compact %s: %v", path, err)
			continue
		}
		kept := samples[:0]
		for _, sample := range samples {
			if sample.At.After(cutoff) || sample.At.Equal(cutoff) {
				kept = append(kept, sample)
			}
		}
		if err := rewrite(path, kept); err != nil {
			s.logger.Printf("sensor history: rewrite %s: %v", path, err)
		}
	}
	return nil
}

func (s *Store) readAllLocked(path string) ([]sensors.Sample, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var samples []sensors.Sample
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var sample sensors.Sample
		if err := json.Unmarshal(line, &sample); err != nil {
			continue
		}
		samples = append(samples, sample)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].At.Before(samples[j].At) })
	return samples, nil
}

func rewrite(path string, samples []sensors.Sample) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	for _, sample := range samples {
		line, err := json.Marshal(sample)
		if err != nil {
			return err
		}
		line = append(line, '\n')
		if _, err := file.Write(line); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmp, path)
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
