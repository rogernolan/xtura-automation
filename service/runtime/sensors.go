package runtime

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"empirebus-tests/service/adapters/switchbot"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/overview"
	"empirebus-tests/service/domains/sensors"
	"empirebus-tests/service/history"
)

const (
	// sensorHistoryDirectory holds the per-sensor NDJSON files.
	sensorHistoryDirectory = "/var/lib/xtura/sensors"
	// sensorDedupeWindow is how long identical readings are suppressed.
	sensorDedupeWindow = 2 * time.Minute
	// discoverWindow is how long a manual discovery scan runs.
	discoverWindow = 12 * time.Second
)

// sensorState is the latest live state for one sensor.
type sensorState struct {
	id       string
	name     string
	source   string
	temp     *float64
	humidity *float64
	battery  *int
	lastSeen *time.Time
}

// sensorStamp is the last appended history sample used for deduplication.
type sensorStamp struct {
	at   time.Time
	temp float64
	hum  *float64
}

func (s sensorStamp) matches(temp float64, hum *float64) bool {
	return s.temp == temp && sameHumidity(s.hum, hum)
}

func sameHumidity(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// recordSensorReading updates the live state for a sensor and appends a
// history sample unless it duplicates the previous one within the dedupe
// window. Disk I/O happens without holding the app mutex.
func (a *App) recordSensorReading(id, name, source string, temp float64, hum *float64, battery *int, at time.Time) {
	appendIt := false
	a.mu.Lock()
	if a.sensorStates == nil {
		a.sensorStates = make(map[string]*sensorState)
	}
	if a.lastHistory == nil {
		a.lastHistory = make(map[string]sensorStamp)
	}
	state := a.sensorStates[id]
	if state == nil {
		state = &sensorState{id: id}
		a.sensorStates[id] = state
	}
	state.name = name
	state.source = source
	state.temp = &temp
	state.humidity = hum
	state.battery = battery
	seen := at
	state.lastSeen = &seen
	last, ok := a.lastHistory[id]
	if !ok || !last.matches(temp, hum) || at.Sub(last.at) >= sensorDedupeWindow {
		a.lastHistory[id] = sensorStamp{at: at, temp: temp, hum: hum}
		appendIt = true
	}
	a.mu.Unlock()
	if appendIt && a.sensorsStore != nil {
		if err := a.sensorsStore.Append(id, sensors.Sample{At: at, Temp: temp, Hum: hum}); err != nil {
			a.logger.Printf("sensor history append: %v", err)
		}
	}
}

// handleSensorReading is the switchbot adapter callback.
func (a *App) handleSensorReading(reading switchbot.Reading) {
	a.mu.RLock()
	settings := a.sensorSettings
	a.mu.RUnlock()
	if !settings.Enabled {
		return
	}
	name := reading.ID
	for _, sensor := range settings.Sensors {
		if sensor.ID() == reading.ID {
			name = sensor.Name
			break
		}
	}
	a.recordSensorReading(reading.ID, name, "switchbot", reading.Temp, reading.Humidity, reading.Battery, a.now().UTC())
}

// observeAldeTelemetry appends Alde temperature samples from the latest Garmin
// telemetry. It runs from publishStateLoop so history/trend track the same
// cadence as the overview document.
func (a *App) observeAldeTelemetry() {
	if a.overviewTelemetry == nil {
		return
	}
	telemetry := a.overviewTelemetry()
	if telemetry.AldeTemperatureC == nil {
		return
	}
	temp := *telemetry.AldeTemperatureC
	at := a.now().UTC()
	if telemetry.UpdatedAt != nil && telemetry.UpdatedAt.After(at.Add(-sensorDedupeWindow)) {
		at = telemetry.UpdatedAt.UTC()
	}
	a.recordSensorReading(sensors.AldeID, "Alde", "garmin", temp, nil, nil, at)
}

// temperatureDocument builds the temperature panel. The first entry in
// Sensors is always the promoted primary: the configured primary with a
// reading, else the first configured switchbot sensor with a reading, else
// Alde. When scanning is disabled only Alde is shown.
func (a *App) temperatureDocument(telemetry overview.Telemetry) overview.Temperature {
	now := a.nowUTC()
	a.mu.RLock()
	settings := a.sensorSettings
	states := make(map[string]*sensorState, len(a.sensorStates))
	for id, state := range a.sensorStates {
		states[id] = state
	}
	a.mu.RUnlock()

	if !settings.Enabled {
		out := overview.Temperature{Sensors: []overview.TemperatureSensor{}}
		out.Sensors = append(out.Sensors, a.temperatureSensorEntry(sensors.AldeID, "Alde", "garmin", states[sensors.AldeID], telemetry, now))
		out.PrimaryID = sensors.AldeID
		out.Primary = a.temperaturePrimary(sensors.AldeID, states[sensors.AldeID], telemetry.AldeTemperatureC, now)
		return out
	}

	entries := make([]overview.TemperatureSensor, 0, len(settings.Sensors)+1)
	for _, sensor := range settings.Sensors {
		entries = append(entries, a.temperatureSensorEntry(sensor.ID(), sensor.Name, "switchbot", states[sensor.ID()], overview.Telemetry{}, now))
	}
	entries = append(entries, a.temperatureSensorEntry(sensors.AldeID, "Alde", "garmin", states[sensors.AldeID], telemetry, now))

	primaryID := ""
	for _, sensor := range settings.Sensors {
		if sensor.Primary && states[sensor.ID()] != nil && states[sensor.ID()].temp != nil {
			primaryID = sensor.ID()
			break
		}
	}
	if primaryID == "" {
		for _, sensor := range settings.Sensors {
			if states[sensor.ID()] != nil && states[sensor.ID()].temp != nil {
				primaryID = sensor.ID()
				break
			}
		}
	}
	if primaryID == "" {
		primaryID = sensors.AldeID
	}

	reordered := make([]overview.TemperatureSensor, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == primaryID {
			reordered = append(reordered, entry)
			break
		}
	}
	for _, entry := range entries {
		if entry.ID != primaryID {
			reordered = append(reordered, entry)
		}
	}

	out := overview.Temperature{Sensors: reordered, PrimaryID: primaryID}
	var primaryTemp *float64
	if primaryID == sensors.AldeID {
		primaryTemp = telemetry.AldeTemperatureC
	}
	out.Primary = a.temperaturePrimary(primaryID, states[primaryID], primaryTemp, now)
	return out
}

func (a *App) temperatureSensorEntry(id, name, source string, state *sensorState, telemetry overview.Telemetry, now time.Time) overview.TemperatureSensor {
	entry := overview.TemperatureSensor{
		ID:     id,
		Name:   name,
		Source: source,
		Trend:  string(sensors.TrendUnavailable),
	}
	if state != nil {
		if state.temp != nil {
			temp := *state.temp
			entry.Temp = &temp
		}
		if state.humidity != nil {
			humidity := *state.humidity
			entry.Humidity = &humidity
		}
		if state.battery != nil {
			battery := *state.battery
			entry.Battery = &battery
		}
		if state.lastSeen != nil {
			seen := *state.lastSeen
			entry.LastSeen = &seen
		}
	}
	if source == "garmin" && telemetry.AldeTemperatureC != nil {
		temp := *telemetry.AldeTemperatureC
		entry.Temp = &temp
		if telemetry.UpdatedAt != nil {
			seen := *telemetry.UpdatedAt
			entry.LastSeen = &seen
		}
	}
	if entry.Temp != nil {
		entry.Trend = string(sensors.TrendOf(a.sensorRecent(id, now), now))
	}
	return entry
}

func (a *App) temperaturePrimary(id string, state *sensorState, fallbackTemp *float64, now time.Time) *overview.TemperaturePrimary {
	primary := &overview.TemperaturePrimary{
		ID:      id,
		Trend:   string(sensors.TrendUnavailable),
		History: []overview.TemperaturePoint{},
	}
	if state != nil {
		if state.temp != nil {
			temp := *state.temp
			primary.Temp = &temp
		}
		if state.humidity != nil {
			humidity := *state.humidity
			primary.Humidity = &humidity
		}
	}
	if primary.Temp == nil && fallbackTemp != nil {
		temp := *fallbackTemp
		primary.Temp = &temp
	}
	if primary.Temp != nil {
		primary.Trend = string(sensors.TrendOf(a.sensorRecent(id, now), now))
	}
	for _, sample := range a.sensorRecent(id, now) {
		primary.History = append(primary.History, overview.TemperaturePoint{At: sample.At, Temp: sample.Temp})
	}
	return primary
}

// sensorRecent returns the recent history for a sensor, tolerating an
// unconfigured store for tests.
func (a *App) sensorRecent(id string, now time.Time) []sensors.Sample {
	if a.sensorsStore == nil {
		return nil
	}
	return a.sensorsStore.Recent(id, now)
}

// nowUTC returns the current time, defaulting to the system clock for tests
// that build a bare App.
func (a *App) nowUTC() time.Time {
	if a.now == nil {
		return time.Now().UTC()
	}
	return a.now().UTC()
}

// SensorSettings returns the current switchbot settings snapshot.
func (a *App) SensorSettings() sensors.Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneSensorSettings(a.sensorSettings)
}

// startSwitchbotScan launches the adapter scan loop when enabled.
func (a *App) startSwitchbotScan() {
	a.switchbotMu.Lock()
	defer a.switchbotMu.Unlock()
	if a.switchbot == nil || a.switchbotCancel != nil || !a.switchbot.Settings().Enabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.switchbotCancel = cancel
	go a.switchbot.Run(ctx)
}

// stopSwitchbotScan cancels a running adapter scan loop.
func (a *App) stopSwitchbotScan() {
	a.switchbotMu.Lock()
	defer a.switchbotMu.Unlock()
	if a.switchbotCancel != nil {
		a.switchbotCancel()
		a.switchbotCancel = nil
	}
}

// restartSwitchbotIfNeeded applies live enable/disable toggles after settings
// change: a disabled-to-enabled switch starts the scan, an enabled-to-disabled
// switch cancels it.
func (a *App) restartSwitchbotIfNeeded() {
	enabled := false
	if a.switchbot != nil {
		enabled = a.switchbot.Settings().Enabled
	}
	a.switchbotMu.Lock()
	defer a.switchbotMu.Unlock()
	if enabled && a.switchbotCancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		a.switchbotCancel = cancel
		go a.switchbot.Run(ctx)
	} else if !enabled && a.switchbotCancel != nil {
		a.switchbotCancel()
		a.switchbotCancel = nil
	}
}

// startSwitchbotSim feeds synthetic SwitchBot readings through the real
// adapter when XTURA_SIM_SWITCHBOT is set. It is a staging aid for hosts
// without BLE scanning; it exercises the full decode->history->panel path.
func (a *App) startSwitchbotSim(ctx context.Context) {
	if !simSwitchbotEnabled() || a.switchbot == nil {
		return
	}
	a.logger.Printf("switchbot simulation enabled: feeding synthetic BLE readings")
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.switchbotSimTick()
			}
		}
	}()
}

func (a *App) switchbotSimTick() {
	a.mu.RLock()
	settings := a.sensorSettings
	a.mu.RUnlock()
	if !settings.Enabled {
		return
	}
	now := a.nowUTC()
	phase := float64(now.Unix())
	for i, sensor := range settings.Sensors {
		temp := math.Round((18+6*math.Sin(phase/120+float64(i)))*10) / 10
		humidity := math.Round(40 + 20*math.Sin(phase/180+float64(i)*0.7))
		if humidity < 0 {
			humidity = 0
		} else if humidity > 100 {
			humidity = 100
		}
		battery := int(95 - (now.Unix()/3600)%6)
		a.switchbot.FeedReading(sensor.MAC, switchbot.Payload{
			DevType:  0x77,
			Temp:     temp,
			Humidity: &humidity,
			Battery:  &battery,
		}, -50)
	}
}

func simSwitchbotEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("XTURA_SIM_SWITCHBOT"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// sensorCompactLoop rewrites history files to the retention window.
func (a *App) sensorCompactLoop(ctx context.Context) {
	ticker := time.NewTicker(sensorCompactInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.sensorsStore.Compact(); err != nil {
				a.logger.Printf("sensor history compaction: %v", err)
			}
		}
	}
}

// UpdateSensorSettings persists and applies switchbot settings live.
func (a *App) UpdateSensorSettings(_ context.Context, settings sensors.Settings) (sensors.Settings, error) {
	if err := settings.Validate(); err != nil {
		return sensors.Settings{}, err
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.mu.RLock()
	next := a.rawConfig
	path := a.configPath
	a.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return sensors.Settings{}, fmt.Errorf("config path is not configured")
	}
	next.Switchbot = config.SwitchbotConfig{
		Enabled:   settings.Enabled,
		HCIDevice: strings.TrimSpace(settings.HCIDevice),
	}
	for _, sensor := range settings.Sensors {
		next.Switchbot.Sensors = append(next.Switchbot.Sensors, config.SwitchbotSensorConfig{
			Name:    strings.TrimSpace(sensor.Name),
			MAC:     strings.TrimSpace(sensor.MAC),
			Primary: sensor.Primary,
		})
	}
	normalized, err := next.Normalize()
	if err != nil {
		return sensors.Settings{}, err
	}
	if err := config.SaveFile(path, next); err != nil {
		return sensors.Settings{}, err
	}
	a.mu.Lock()
	a.rawConfig = next
	a.cfg = normalized
	a.sensorSettings = normalized.Switchbot
	a.revision = readConfigRevision(path)
	a.mu.Unlock()
	if a.switchbot != nil {
		a.switchbot.Configure(normalized.Switchbot)
	}
	a.restartSwitchbotIfNeeded()
	out := a.SensorSettings()
	a.logger.Printf("switchbot settings updated: enabled=%t sensors=%d", out.Enabled, len(out.Sensors))
	a.broker.Publish(events.Event{Type: "overview.state_changed", Timestamp: a.now().UTC(), Payload: a.Overview()})
	return out, nil
}

// SensorDiscover returns the SwitchBot devices observed by the scan. When
// scanning is disabled it runs a temporary discovery scan.
func (a *App) SensorDiscover(ctx context.Context) ([]switchbot.SeenDevice, error) {
	if a.switchbot == nil {
		return nil, fmt.Errorf("switchbot is not configured")
	}
	return a.switchbot.Discover(ctx, discoverWindow)
}

// SensorHistory returns recent samples for a sensor id, newest last.
func (a *App) SensorHistory(id string, limit int) ([]sensors.Sample, error) {
	if a.sensorsStore == nil {
		return nil, fmt.Errorf("sensor history is not configured")
	}
	samples := a.sensorsStore.Recent(id, a.now().UTC())
	if limit > 0 && len(samples) > limit {
		samples = append([]sensors.Sample(nil), samples[len(samples)-limit:]...)
	}
	return samples, nil
}

// seedSensorHistory loads persisted history for every sensor on startup.
func seedSensorHistory(store *history.Store, now time.Time) {
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("sensor history seed: %v", err)
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".ndjson") {
			continue
		}
		id := strings.TrimSuffix(name, ".ndjson")
		samples, err := store.LoadTail(id, now)
		if err != nil {
			log.Printf("sensor history seed %s: %v", id, err)
			continue
		}
		store.Seed(id, samples)
	}
}

func cloneSensorSettings(in sensors.Settings) sensors.Settings {
	out := in
	out.Sensors = append([]sensors.SensorConfig(nil), in.Sensors...)
	return out
}
