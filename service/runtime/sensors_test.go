package runtime

import (
	"testing"
	"time"

	"empirebus-tests/service/domains/overview"
	"empirebus-tests/service/domains/sensors"
	"empirebus-tests/service/history"
)

const testMACMain = "c5:65:68:81:84:32"
const testMACOutside = "d6:66:69:92:95:43"

func newSensorApp(t *testing.T, settings sensors.Settings) (*App, *history.Store) {
	t.Helper()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := history.New(t.TempDir(), history.DefaultWindow, history.DefaultRetention, func() time.Time { return now }, nil)
	app := &App{
		now:            func() time.Time { return now },
		sensorSettings: settings,
		sensorStates:   make(map[string]*sensorState),
		lastHistory:    make(map[string]sensorStamp),
		sensorsStore:   store,
	}
	return app, store
}

func seedSamples(t *testing.T, store *history.Store, id string, recent, baseline float64) {
	t.Helper()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		at := now.Add(-time.Duration(i+15) * time.Minute)
		if err := store.Append(id, sensors.Sample{At: at, Temp: baseline}); err != nil {
			t.Fatalf("seed baseline: %v", err)
		}
	}
	for i := 0; i < 6; i++ {
		at := now.Add(-time.Duration(i) * time.Minute)
		if err := store.Append(id, sensors.Sample{At: at, Temp: recent}); err != nil {
			t.Fatalf("seed recent: %v", err)
		}
	}
}

func setState(app *App, id, name, source string, temp float64) {
	app.mu.Lock()
	defer app.mu.Unlock()
	app.sensorStates[id] = &sensorState{
		id:     id,
		name:   name,
		source: source,
		temp:   &temp,
	}
}

func TestRecordSensorReadingPreservesBatteryWhenReadingOmitsIt(t *testing.T) {
	app, _ := newSensorApp(t, sensors.Settings{Enabled: true})
	id := sensors.NormalizeMAC(testMACMain)
	battery := 65
	at := app.now()

	app.recordSensorReading(id, "Main", "switchbot", 20.0, nil, &battery, at)
	app.recordSensorReading(id, "Main", "switchbot", 20.5, nil, nil, at.Add(time.Minute))

	app.mu.RLock()
	state := app.sensorStates[id]
	app.mu.RUnlock()
	if state == nil || state.battery == nil || *state.battery != 65 {
		t.Fatalf("battery should remain 65 after a reading without battery, got %#v", state)
	}
}

func TestTemperatureDisabledShowsOnlyAlde(t *testing.T) {
	settings := sensors.Settings{Enabled: false}
	app, store := newSensorApp(t, settings)
	seedSamples(t, store, sensors.AldeID, 21.0, 20.0)
	aldeTemp := 21.0
	updated := time.Date(2026, 8, 16, 9, 59, 55, 0, time.UTC)
	telemetry := overview.Telemetry{AldeTemperatureC: &aldeTemp, UpdatedAt: &updated}

	doc := app.temperatureDocument(telemetry)
	if doc.PrimaryID != sensors.AldeID {
		t.Fatalf("primary: got %q, want alde", doc.PrimaryID)
	}
	if len(doc.Sensors) != 1 || doc.Sensors[0].ID != sensors.AldeID {
		t.Fatalf("expected only alde sensor, got %#v", doc.Sensors)
	}
	if doc.Primary == nil || doc.Primary.Temp == nil || *doc.Primary.Temp != 21.0 {
		t.Fatalf("primary temp: got %#v", doc.Primary)
	}
}

func TestTemperaturePromotesConfiguredPrimary(t *testing.T) {
	settings := sensors.Settings{
		Enabled:   true,
		HCIDevice: "hci0",
		Sensors: []sensors.SensorConfig{
			{Name: "Main", MAC: testMACMain, Primary: true},
			{Name: "Outside", MAC: testMACOutside},
		},
	}
	app, store := newSensorApp(t, settings)
	seedSamples(t, store, sensors.NormalizeMAC(testMACMain), 22.0, 20.0)
	setState(app, sensors.NormalizeMAC(testMACMain), "Main", "switchbot", 22.0)

	doc := app.temperatureDocument(overview.Telemetry{})
	if doc.PrimaryID != sensors.NormalizeMAC(testMACMain) {
		t.Fatalf("primary: got %q", doc.PrimaryID)
	}
	if len(doc.Sensors) != 3 {
		t.Fatalf("expected 3 sensors, got %d", len(doc.Sensors))
	}
	if doc.Sensors[0].ID != sensors.NormalizeMAC(testMACMain) {
		t.Fatalf("primary should be first, got %#v", doc.Sensors[0])
	}
	if doc.Primary == nil || len(doc.Primary.History) == 0 {
		t.Fatalf("expected primary history chart data, got %#v", doc.Primary)
	}
}

func TestTemperaturePrimaryFallsBackToAlde(t *testing.T) {
	settings := sensors.Settings{
		Enabled:   true,
		HCIDevice: "hci0",
		Sensors: []sensors.SensorConfig{
			{Name: "Main", MAC: testMACMain, Primary: true},
			{Name: "Outside", MAC: testMACOutside},
		},
	}
	app, store := newSensorApp(t, settings)
	// No switchbot readings. Seed alde so its trend computes.
	seedSamples(t, store, sensors.AldeID, 21.5, 20.0)
	aldeTemp := 21.5
	telemetry := overview.Telemetry{AldeTemperatureC: &aldeTemp}

	doc := app.temperatureDocument(telemetry)
	if doc.PrimaryID != sensors.AldeID {
		t.Fatalf("primary should fall back to alde, got %q", doc.PrimaryID)
	}
	// Alde must be first, with both switchbots listed after.
	if doc.Sensors[0].ID != sensors.AldeID {
		t.Fatalf("alde should be first, got %#v", doc.Sensors[0])
	}
	if len(doc.Sensors) != 3 {
		t.Fatalf("expected 3 sensors, got %d", len(doc.Sensors))
	}
}

func TestSensorTrendRising(t *testing.T) {
	settings := sensors.Settings{Enabled: false}
	app, store := newSensorApp(t, settings)
	seedSamples(t, store, sensors.AldeID, 22.0, 20.0)
	aldeTemp := 22.0
	updated := time.Date(2026, 8, 16, 9, 59, 55, 0, time.UTC)
	doc := app.temperatureDocument(overview.Telemetry{AldeTemperatureC: &aldeTemp, UpdatedAt: &updated})
	if doc.Primary == nil || doc.Primary.Trend != string(sensors.TrendRising) {
		t.Fatalf("expected rising trend, got %q", doc.Primary.Trend)
	}
}

func TestTemperatureAldeExpiresWhenStale(t *testing.T) {
	settings := sensors.Settings{Enabled: false}
	app, _ := newSensorApp(t, settings)
	aldeTemp := 21.0
	// UpdatedAt is 1 minute old, beyond the 30s staleness window.
	updated := time.Date(2026, 8, 16, 9, 59, 0, 0, time.UTC)
	doc := app.temperatureDocument(overview.Telemetry{AldeTemperatureC: &aldeTemp, UpdatedAt: &updated})
	if doc.PrimaryID != sensors.AldeID {
		t.Fatalf("primary: got %q", doc.PrimaryID)
	}
	if doc.Primary == nil || doc.Primary.Temp != nil {
		t.Fatalf("expected stale Alde temp to be suppressed, got %#v", doc.Primary)
	}
	if doc.Sensors[0].Temp != nil {
		t.Fatalf("expected stale Alde sensor entry to omit temp, got %#v", doc.Sensors[0])
	}
}
