package host

import (
	"context"
	"log"
	"reflect"
	"sync"
	"time"
)

// Manager samples host metrics on a ticker and publishes snapshots when they
// change. The read function is injectable for tests; the default reads real
// host state.
type Manager struct {
	mu       sync.Mutex
	read     func() (Snapshot, error)
	now      func() time.Time
	interval time.Duration
	logger   *log.Logger
	onChange func(Metrics)

	lastSnapshot  Snapshot
	lastSampledAt time.Time
	lastError     string
	lastErrorAt   *time.Time
	lastPublished *Metrics
}

// New creates a host metrics manager. interval <= 0 falls back to 5s; nil now
// and logger fall back to sensible defaults; a nil read uses the real host.
func New(interval time.Duration, read func() (Snapshot, error), now func() time.Time, logger *log.Logger) *Manager {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = log.Default()
	}
	if read == nil {
		read = defaultRead
	}
	return &Manager{read: read, now: now, interval: interval, logger: logger}
}

// SetOnChange installs a callback invoked with each published snapshot.
func (m *Manager) SetOnChange(onChange func(Metrics)) {
	m.mu.Lock()
	m.onChange = onChange
	m.mu.Unlock()
}

// Start launches the sampling goroutine. It samples once immediately, then on
// the configured interval until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		m.Sample()
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Sample()
			}
		}
	}()
}

// Sample performs one read and updates the managed state.
func (m *Manager) Sample() {
	snapshot, err := m.read()

	m.mu.Lock()
	defer m.mu.Unlock()

	at := m.now().UTC()
	m.lastSampledAt = at
	if err != nil {
		m.lastError = err.Error()
		atCopy := at
		m.lastErrorAt = &atCopy
		m.logger.Printf("host metrics: %v", err)
	} else {
		m.lastSnapshot = snapshot
		m.lastError = ""
		m.lastErrorAt = nil
	}
	m.publishIfChangedLocked(at)
}

// State returns the latest metrics snapshot.
func (m *Manager) State() Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Metrics{
		SampledAt:   m.lastSampledAt,
		Snapshot:    m.lastSnapshot,
		LastError:   m.lastError,
		LastErrorAt: m.lastErrorAt,
	}
}

// publishIfChangedLocked publishes a snapshot only when the metric values or
// error state differ from the last published one.
func (m *Manager) publishIfChangedLocked(at time.Time) {
	next := Metrics{
		SampledAt:   at,
		Snapshot:    m.lastSnapshot,
		LastError:   m.lastError,
		LastErrorAt: m.lastErrorAt,
	}
	if m.lastPublished != nil {
		previous := *m.lastPublished
		if previous.LastError == next.LastError &&
			reflect.DeepEqual(previous.LastErrorAt, next.LastErrorAt) &&
			reflect.DeepEqual(previous.Snapshot, next.Snapshot) {
			return
		}
	}
	copied := next
	m.lastPublished = &copied
	if m.onChange != nil {
		m.onChange(copied)
	}
}
