package switchbot

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"empirebus-tests/service/domains/sensors"
)

// ErrUnsupported is returned when BLE scanning is unavailable on the platform.
var ErrUnsupported = errors.New("switchbot BLE scanning is not supported on this platform")

// scanRetryInterval is the backoff between failed scan sessions.
const scanRetryInterval = 5 * time.Second

// discoverWindow is the default duration of a temporary discovery scan.
const discoverWindow = 12 * time.Second

// Reading is a decoded live reading for a configured sensor.
type Reading struct {
	ID       string
	Temp     float64
	Humidity *float64
	Battery  *int
	RSSI     int
}

// SeenDevice is a SwitchBot device observed by the scan.
type SeenDevice struct {
	MAC      string    `json:"mac"`
	DevType  byte      `json:"dev_type"`
	LastSeen time.Time `json:"last_seen"`
	RSSI     int       `json:"rssi"`
}

// Config configures the switchbot adapter.
type Config struct {
	Settings  sensors.Settings
	Logger    *log.Logger
	OnReading func(Reading)
	Now       func() time.Time
}

// Adapter runs the BLE scan and matches advertisements against configured
// sensors. All state is mutex-guarded so the runtime can query seen devices
// while the scan runs.
type Adapter struct {
	cfg    Config
	logger *log.Logger
	now    func() time.Time

	mu       sync.RWMutex
	settings sensors.Settings
	seen     map[string]SeenDevice
	lastErr  string
}

// New creates an adapter. It does not start scanning until Run is called.
func New(cfg Config) *Adapter {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	return &Adapter{
		cfg:      cfg,
		logger:   cfg.Logger,
		now:      cfg.Now,
		settings: cfg.Settings,
		seen:     make(map[string]SeenDevice),
	}
}

// Configure replaces the runtime settings. It is safe to call while scanning;
// the next report is matched against the new settings.
func (a *Adapter) Configure(settings sensors.Settings) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.settings = settings
}

// Settings returns the current settings snapshot.
func (a *Adapter) Settings() sensors.Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings
}

// SeenDevices returns the devices observed so far, most recently seen first.
func (a *Adapter) SeenDevices() []SeenDevice {
	a.mu.RLock()
	defer a.mu.RUnlock()
	devices := make([]SeenDevice, 0, len(a.seen))
	for _, device := range a.seen {
		devices = append(devices, device)
	}
	sortSeenByLastSeenDesc(devices)
	return devices
}

// LastError returns the most recent scan session error, if any.
func (a *Adapter) LastError() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastErr
}

func (a *Adapter) setLastError(err error) {
	a.mu.Lock()
	a.lastErr = err.Error()
	a.mu.Unlock()
}

// Run scans until ctx is cancelled, reconnecting after transient failures. It
// returns immediately when scanning is disabled.
func (a *Adapter) Run(ctx context.Context) {
	if !a.Settings().Enabled {
		a.logger.Printf("switchbot scan disabled; skipping")
		return
	}
	device := a.Settings().HCIDevice
	if device == "" {
		device = "hci0"
	}
	a.logger.Printf("switchbot scan starting: device=%s", device)
	for {
		if err := a.scanLoop(ctx, device); err != nil {
			if ctx.Err() != nil {
				return
			}
			a.setLastError(err)
			a.logger.Printf("switchbot scan error: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(scanRetryInterval):
			}
		}
	}
}

// handleEvent processes one HCI event packet. It is the testable entry point
// for the run loop.
func (a *Adapter) handleEvent(event []byte) {
	reports, err := ParseAdvertisingReports(event)
	if err != nil {
		return
	}
	for _, report := range reports {
		a.handleReport(report)
	}
}

// handleReport decodes and applies one advertising report.
func (a *Adapter) handleReport(report AdvertisingReport) {
	elements, err := ParseADElements(report.Data)
	if err != nil {
		return
	}
	payload, ok := Decode(elements)
	if !ok {
		return
	}
	a.applyReading(report.MAC, report.RSSI, payload)
}

// applyReading records a seen device and fires the reading callback for a
// configured sensor.
func (a *Adapter) applyReading(mac string, rssi int, payload Payload) {
	now := a.now().UTC()
	a.mu.Lock()
	a.seen[mac] = SeenDevice{
		MAC:      mac,
		DevType:  payload.DevType,
		LastSeen: now,
		RSSI:     rssi,
	}
	settings := a.settings
	a.mu.Unlock()

	sensor, matched := settings.SensorByMAC(mac)
	if !matched || !payload.HasTemp {
		return
	}
	if a.cfg.OnReading != nil {
		a.cfg.OnReading(Reading{
			ID:       sensor.ID(),
			Temp:     payload.Temp,
			Humidity: payload.Humidity,
			Battery:  payload.Battery,
			RSSI:     rssi,
		})
	}
}

// FeedReading delivers a decoded reading for a MAC exactly as if it had been
// observed by the scan. It is used by the staging/simulation path where BLE
// scanning is unavailable; it is a no-op for unconfigured MACs.
func (a *Adapter) FeedReading(mac string, payload Payload, rssi int) {
	a.applyReading(mac, rssi, payload)
}

// Discover returns the devices observed by the scan. When scanning is already
// running it returns the rolling table; otherwise it performs a temporary
// passive scan for the window so discovery works before the feature is
// enabled.
func (a *Adapter) Discover(ctx context.Context, window time.Duration) ([]SeenDevice, error) {
	if a.Settings().Enabled {
		return a.SeenDevices(), nil
	}
	if window <= 0 {
		window = discoverWindow
	}
	device := a.Settings().HCIDevice
	if device == "" {
		device = "hci0"
	}
	scanCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	if err := a.scanLoop(scanCtx, device); err != nil && scanCtx.Err() == nil {
		return nil, err
	}
	return a.SeenDevices(), nil
}

func sortSeenByLastSeenDesc(devices []SeenDevice) {
	for i := 1; i < len(devices); i++ {
		for j := i; j > 0 && devices[j].LastSeen.After(devices[j-1].LastSeen); j-- {
			devices[j], devices[j-1] = devices[j-1], devices[j]
		}
	}
}
