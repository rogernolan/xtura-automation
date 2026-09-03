// Package history stores per-sensor temperature history in an NDJSON file per
// sensor plus an in-memory ring buffer for the recent window.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"empirebus-tests/service/domains/sensors"
)

// DefaultWindow is how much recent history the store keeps in memory.
const DefaultWindow = 2 * time.Hour

// DefaultRetention is how long ten-minute persisted history is kept before
// samples are moved to the indefinite hourly archive.
const DefaultRetention = 30 * 24 * time.Hour

const (
	recentBucket = 10 * time.Minute
	hourlyBucket = time.Hour
	hourlyDir    = "hourly"
)

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
	n, err := file.Write(line)
	if err != nil {
		_ = file.Close()
		delete(s.files, id)
		return fmt.Errorf("write sensor history: %w", err)
	}
	if n != len(line) {
		_ = file.Close()
		delete(s.files, id)
		return fmt.Errorf("write sensor history: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		delete(s.files, id)
		return fmt.Errorf("sync sensor history: %w", err)
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
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat sensor history: %w", err)
	}

	cutoff := now.Add(-s.window)
	var samples []sensors.Sample
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var validLines [][]byte
	corrupt := false
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var sample sensors.Sample
		if err := json.Unmarshal(line, &sample); err != nil {
			s.logger.Printf("sensor history: skipping unparsable line in %s: %v", path, err)
			corrupt = true
			continue
		}
		validLines = append(validLines, line)
		if sample.At.Before(cutoff) {
			continue
		}
		samples = append(samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read sensor history: %w", err)
	}
	if info.Size() > 0 {
		var lastByte [1]byte
		if _, err := file.ReadAt(lastByte[:], info.Size()-1); err == nil && lastByte[0] != '\n' {
			corrupt = true
		}
	}
	if corrupt {
		if err := rewriteRawNDJSON(path, validLines, info.Mode().Perm()); err != nil {
			s.logger.Printf("sensor history: unable to clean %s: %v", path, err)
		}
	}
	return samples, nil
}

func rewriteRawNDJSON(path string, lines [][]byte, mode os.FileMode) error {
	var data bytes.Buffer
	for _, line := range lines {
		data.Write(line)
		data.WriteByte('\n')
	}
	if mode == 0 {
		mode = 0o644
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sensor-history-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Compact rewrites every history file keeping only samples within the
// retention window.
func (s *Store) Compact() error {
	cutoff := s.now().UTC().Add(-s.retention)
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
		id := strings.TrimSuffix(entry.Name(), ".ndjson")
		if err := s.compactSensorFileLocked(id, path, samples, cutoff); err != nil {
			s.logger.Printf("sensor history: rewrite %s: %v", path, err)
		}
	}
	return nil
}

func (s *Store) compactSensorFileLocked(id, path string, samples []sensors.Sample, cutoff time.Time) error {
	recent := bucketSensorSamples(samples, cutoff, recentBucket)
	archive := make(map[string][]sensors.Sample)
	archiveDir := filepath.Join(s.dir, hourlyDir)
	if entries, err := os.ReadDir(archiveDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), id+"-") || filepath.Ext(entry.Name()) != ".ndjson" {
				continue
			}
			stored, readErr := s.readAllLocked(filepath.Join(archiveDir, entry.Name()))
			if readErr != nil {
				return readErr
			}
			archive[entry.Name()] = stored
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, sample := range samples {
		if sample.At.Before(cutoff) {
			name := archiveFileName(id, sample.At)
			archive[name] = append(archive[name], sample)
		}
	}
	for name, stored := range archive {
		if err := rewrite(filepath.Join(archiveDir, name), bucketSensorSamples(stored, time.Time{}, hourlyBucket)); err != nil {
			return err
		}
	}
	return rewrite(path, recent)
}

func bucketSensorSamples(samples []sensors.Sample, cutoff time.Time, bucket time.Duration) []sensors.Sample {
	buckets := make(map[int64]sensors.Sample)
	for _, sample := range samples {
		if !cutoff.IsZero() && sample.At.Before(cutoff) {
			continue
		}
		key := sample.At.Truncate(bucket).UnixNano()
		if previous, ok := buckets[key]; !ok || sample.At.After(previous.At) {
			buckets[key] = sample
		}
	}
	out := make([]sensors.Sample, 0, len(buckets))
	for _, sample := range buckets {
		out = append(out, sample)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func archiveFileName(id string, at time.Time) string {
	return id + "-" + at.UTC().Format("2006-01") + ".ndjson"
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
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
