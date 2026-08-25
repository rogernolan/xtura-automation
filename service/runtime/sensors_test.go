package runtime

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"empirebus-tests/service/adapters/garmin"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/domains/overview"
	"empirebus-tests/service/domains/sensors"
	"empirebus-tests/service/history"
	"empirebus-tests/service/waterhistory"
)

const testMACMain = "c5:65:68:81:84:32"
const testMACOutside = "d6:66:69:92:95:43"

type stubGreyWaterDischargeProvider struct {
	batches [][]garmin.GreyWaterDischargeEvent
}

func (s *stubGreyWaterDischargeProvider) DrainGreyWaterDischargeEvents() []garmin.GreyWaterDischargeEvent {
	if len(s.batches) == 0 {
		return nil
	}
	batch := append([]garmin.GreyWaterDischargeEvent(nil), s.batches[0]...)
	s.batches = s.batches[1:]
	return batch
}

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

func waitRuntimeEvent(t *testing.T, stream <-chan events.Event) events.Event {
	t.Helper()
	select {
	case event := <-stream:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime event")
		return events.Event{}
	}
}

func TestGreyWaterHistoryDrainsProviderEventsBeforeSamplesAndPublishesClose(t *testing.T) {
	base := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	now := base
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	store := waterhistory.New(waterhistory.Options{
		Directory:      t.TempDir(),
		Threshold:      5,
		SettlingPeriod: time.Second,
		GroupingWindow: time.Hour,
		Logf:           logger.Printf,
	}, func() time.Time { return now })
	baselineGrey := 75.0
	if changed, err := store.Observe(waterhistory.Sample{At: base, GreyPercent: &baselineGrey}, base); err != nil || !changed {
		t.Fatalf("seed baseline changed=%t err=%v", changed, err)
	}
	openAt := base.Add(time.Minute)
	closeAt := base.Add(2 * time.Minute)
	greyDuringDischarge := 40.0
	greyAfterClose := 0.0
	telemetry := []overview.Telemetry{
		{GreyWaterPercent: &greyDuringDischarge, UpdatedAt: &openAt},
		{GreyWaterPercent: &greyAfterClose, UpdatedAt: &closeAt},
	}
	telemetryCalls := 0
	app := &App{
		now:          func() time.Time { return now },
		logger:       logger,
		broker:       events.NewBroker(8),
		waterHistory: store,
		overviewTelemetry: func() overview.Telemetry {
			current := telemetry[telemetryCalls]
			telemetryCalls++
			return current
		},
		greyWaterDischarge: &stubGreyWaterDischargeProvider{batches: [][]garmin.GreyWaterDischargeEvent{
			{{Kind: garmin.KindOpen, At: openAt}},
			{{Kind: garmin.KindClose, At: closeAt}},
		}},
	}
	stream, unsubscribe := app.Broker().Subscribe()
	t.Cleanup(unsubscribe)

	app.observeWaterHistory()
	first := waitRuntimeEvent(t, stream)
	if first.Type != "water.history_changed" {
		t.Fatalf("first event type = %q, want water.history_changed", first.Type)
	}
	if got := app.WaterHistory().Events; len(got) != 0 {
		t.Fatalf("expected no empty event after open, got %#v", got)
	}

	now = closeAt
	app.observeWaterHistory()
	second := waitRuntimeEvent(t, stream)
	if second.Type != "water.history_changed" {
		t.Fatalf("second event type = %q, want water.history_changed", second.Type)
	}
	doc := app.WaterHistory()
	if len(doc.Events) != 1 {
		t.Fatalf("expected one grey empty event, got %#v", doc.Events)
	}
	event := doc.Events[0]
	if event.Tank != waterhistory.TankGrey || event.Kind != waterhistory.KindEmpty {
		t.Fatalf("unexpected water history event %#v", event)
	}
	if event.From != greyDuringDischarge || event.To != 0 {
		t.Fatalf("unexpected grey empty event %#v", event)
	}
	if got := strings.TrimSpace(logs.String()); got != "" {
		t.Fatalf("expected no grey-drop log with discharge open, got %q", got)
	}
}

func TestObserveWaterTelemetryLogsGreyDropWithoutDischargeOpen(t *testing.T) {
	base := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	now := base
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	store := waterhistory.New(waterhistory.Options{
		Directory:      t.TempDir(),
		Threshold:      5,
		SettlingPeriod: time.Second,
		GroupingWindow: time.Hour,
		Logf:           logger.Printf,
	}, func() time.Time { return now })
	baselineGrey := 70.0
	if changed, err := store.Observe(waterhistory.Sample{At: base, GreyPercent: &baselineGrey}, base); err != nil || !changed {
		t.Fatalf("seed baseline changed=%t err=%v", changed, err)
	}
	nextAt := base.Add(time.Minute)
	nextGrey := 60.0
	app := &App{
		now:          func() time.Time { return now },
		logger:       logger,
		waterHistory: store,
		overviewTelemetry: func() overview.Telemetry {
			return overview.Telemetry{GreyWaterPercent: &nextGrey, UpdatedAt: &nextAt}
		},
	}

	now = nextAt
	if !app.observeWaterTelemetry() {
		t.Fatal("expected grey drop sample to be stored")
	}
	if got := app.WaterHistory().Events; len(got) != 0 {
		t.Fatalf("expected no water-history events, got %#v", got)
	}
	if got := logs.String(); !strings.Contains(got, "grey level dropped from 70.0 to 60.0 without a pending discharge open") {
		t.Fatalf("expected grey-drop log, got %q", got)
	}
}

func TestObserveWaterTelemetryKeepsFreshFillDetection(t *testing.T) {
	base := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	now := base
	store := waterhistory.New(waterhistory.Options{
		Directory:      t.TempDir(),
		Threshold:      5,
		SettlingPeriod: time.Second,
		GroupingWindow: time.Hour,
	}, func() time.Time { return now })
	baselineFresh := 45.0
	if changed, err := store.Observe(waterhistory.Sample{At: base, FreshPercent: &baselineFresh}, base); err != nil || !changed {
		t.Fatalf("seed baseline changed=%t err=%v", changed, err)
	}
	fillAt := base.Add(time.Minute)
	steadyAt := fillAt.Add(2 * time.Second)
	filledFresh := 55.0
	telemetry := []overview.Telemetry{
		{FreshWaterPercent: &filledFresh, UpdatedAt: &fillAt},
		{FreshWaterPercent: &filledFresh, UpdatedAt: &steadyAt},
	}
	telemetryCalls := 0
	app := &App{
		now:          func() time.Time { return now },
		logger:       log.New(&bytes.Buffer{}, "", 0),
		waterHistory: store,
		overviewTelemetry: func() overview.Telemetry {
			current := telemetry[telemetryCalls]
			telemetryCalls++
			return current
		},
	}

	now = fillAt
	if !app.observeWaterTelemetry() {
		t.Fatal("expected first fresh-water fill sample to be stored")
	}
	now = steadyAt
	if !app.observeWaterTelemetry() {
		t.Fatal("expected settling fresh-water fill sample to change history")
	}
	doc := app.WaterHistory()
	if len(doc.Events) != 1 {
		t.Fatalf("expected one fresh-water event, got %#v", doc.Events)
	}
	event := doc.Events[0]
	if event.Tank != waterhistory.TankFresh || event.Kind != waterhistory.KindFill {
		t.Fatalf("unexpected water history event %#v", event)
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
