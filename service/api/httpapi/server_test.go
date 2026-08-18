package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/adapters/btle"
	"empirebus-tests/service/buildinfo"
	"empirebus-tests/service/config"
	domainheating "empirebus-tests/service/domains/heating"
	domainlights "empirebus-tests/service/domains/lights"
	domainlocation "empirebus-tests/service/domains/location"
	"empirebus-tests/service/domains/overview"
	"empirebus-tests/service/domains/sensors"
	domainwater "empirebus-tests/service/domains/water"
	"empirebus-tests/service/host"
	"empirebus-tests/service/recording"
	"empirebus-tests/service/runtime"
	"empirebus-tests/service/tracking"
)

type fakeApp struct {
	broker             *events.Broker
	schedule           config.HeatingScheduleDocument
	mode               config.HeatingRuntimeState
	cancelBoostCalled  *bool
	scheduledWater     domainwater.State
	cancelWaterCalled  *bool
	lights             domainlights.State
	water              domainwater.State
	location           domainlocation.State
	flashLightsErr     error
	waterErr           error
	setTargetErr       error
	recording          recording.State
	startRecordingErr  error
	stopRecordingCalls *int
	trackingSettings   tracking.Settings
	trackingDir        string
	updateTrackingErr  error
	trackingState      tracking.State
	startTrackingErr   error
	stopTrackingErr    error
	piStatus           host.Metrics
	trackFiles         []tracking.FileInfo
	trackListErr       error
	trackReadData      []byte
	trackReadErr       error
	trackReadNames     *[]string
	trackDeleteErr     error
	trackDeleteNames   *[]string
	overview           overview.Document
	overviewSettings   overview.Settings
	updateOverviewErr  error
	sensorSettings     sensors.Settings
	updateSensorErr    error
	discoverDevices    []btle.SeenDevice
	discoverErr        error
	history            []sensors.Sample
	historyErr         error
}

func (f fakeApp) Health() runtime.ServiceHealthView {
	return runtime.ServiceHealthView{Status: "ok"}
}

func (f fakeApp) HeatingState() runtime.HeatingStateView {
	return runtime.HeatingStateView{PowerState: domainheating.PowerStateOff}
}

func (f fakeApp) EnsurePower(context.Context, string) error {
	return nil
}

func (f fakeApp) SetTargetTemperature(context.Context, float64) error {
	return f.setTargetErr
}

func (f fakeApp) HeatingPrograms(time.Time) []runtime.ProgramStatus {
	return nil
}

func (f fakeApp) HeatingMode() config.HeatingRuntimeState {
	return f.mode
}

func (f fakeApp) SetHeatingModeSchedule(context.Context) (config.HeatingRuntimeState, error) {
	return f.mode, nil
}

func (f fakeApp) SetHeatingModeOff(context.Context) (config.HeatingRuntimeState, error) {
	return f.mode, nil
}

func (f fakeApp) SetHeatingModeManual(context.Context, float64) (config.HeatingRuntimeState, error) {
	return f.mode, nil
}

func (f fakeApp) SetHeatingModeBoost(context.Context, float64, time.Duration) (config.HeatingRuntimeState, error) {
	return f.mode, nil
}

func (f fakeApp) CancelHeatingModeBoost(context.Context) (config.HeatingRuntimeState, error) {
	if f.cancelBoostCalled != nil {
		*f.cancelBoostCalled = true
	}
	return f.mode, nil
}

func (f fakeApp) HeatingSchedule() config.HeatingScheduleDocument {
	return f.schedule
}

func (f fakeApp) UpdateHeatingSchedule(context.Context, config.HeatingScheduleDocument) (config.HeatingScheduleDocument, error) {
	return f.schedule, nil
}

func (f fakeApp) LightsState() domainlights.State {
	return f.lights
}

func (f fakeApp) FlashExteriorLights(context.Context, int) error {
	return f.flashLightsErr
}

func (f fakeApp) WaterState() domainwater.State {
	return f.water
}

func (f fakeApp) OpenGreyWaterValve(context.Context) error {
	return f.waterErr
}

func (f fakeApp) CloseGreyWaterValve(context.Context) error {
	return f.waterErr
}

func (f fakeApp) ScheduleGreyWaterOpening(context.Context, string, time.Duration) (domainwater.State, error) {
	return f.scheduledWater, f.waterErr
}

func (f fakeApp) CancelGreyWaterOpening(context.Context) (domainwater.State, error) {
	if f.cancelWaterCalled != nil {
		*f.cancelWaterCalled = true
	}
	return f.scheduledWater, f.waterErr
}

func (f fakeApp) LocationState() domainlocation.State {
	return f.location
}

func (f fakeApp) RecordingState() recording.State {
	return f.recording
}

func (f fakeApp) StartRecording(_ context.Context, request recording.StartRequest) (recording.State, error) {
	if request.DurationMinutes < 0 {
		return f.recording, errors.New("recording duration must not be negative")
	}
	return f.recording, f.startRecordingErr
}

func (f fakeApp) StopRecording(context.Context) recording.State {
	if f.stopRecordingCalls != nil {
		*f.stopRecordingCalls++
	}
	return f.recording
}

func (f fakeApp) TrackingSettings() tracking.Settings {
	return f.trackingSettings
}

func (f fakeApp) Overview() overview.Document         { return f.overview }
func (f fakeApp) OverviewSettings() overview.Settings { return f.overviewSettings }
func (f fakeApp) UpdateOverviewSettings(_ context.Context, settings overview.Settings) (overview.Settings, error) {
	return settings, f.updateOverviewErr
}

func (f fakeApp) SensorSettings() sensors.Settings {
	return f.sensorSettings
}

func (f fakeApp) UpdateSensorSettings(_ context.Context, settings sensors.Settings) (sensors.Settings, error) {
	if f.updateSensorErr != nil {
		return sensors.Settings{}, f.updateSensorErr
	}
	return settings, nil
}

func (f fakeApp) SensorDiscover(_ context.Context) ([]btle.SeenDevice, error) {
	return f.discoverDevices, f.discoverErr
}

func (f fakeApp) SensorHistory(string, int) ([]sensors.Sample, error) {
	return f.history, f.historyErr
}

func (f fakeApp) TrackingDirectory() string {
	return f.trackingDir
}

func (f fakeApp) UpdateTrackingSettings(_ context.Context, settings tracking.Settings) (tracking.Settings, error) {
	if f.updateTrackingErr != nil {
		return tracking.Settings{}, f.updateTrackingErr
	}
	return settings, nil
}

func (f fakeApp) TrackingState() tracking.State {
	return f.trackingState
}

func (f fakeApp) StartTracking(context.Context) (tracking.State, error) {
	return f.trackingState, f.startTrackingErr
}

func (f fakeApp) StopTracking(context.Context) (tracking.State, error) {
	return f.trackingState, f.stopTrackingErr
}

func (f fakeApp) TrackList() ([]tracking.FileInfo, error) {
	return f.trackFiles, f.trackListErr
}

func (f fakeApp) TrackRead(name string) ([]byte, error) {
	if f.trackReadNames != nil {
		*f.trackReadNames = append(*f.trackReadNames, name)
	}
	return f.trackReadData, f.trackReadErr
}

func (f fakeApp) TrackDelete(name string) error {
	if f.trackDeleteNames != nil {
		*f.trackDeleteNames = append(*f.trackDeleteNames, name)
	}
	return f.trackDeleteErr
}

func (f fakeApp) Broker() *events.Broker {
	return f.broker
}

func (f fakeApp) HostStatus() host.Metrics {
	return f.piStatus
}

func TestHandlerRoutesHealth(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d", rr.Code)
	}
}

func TestHandlerRoutesBuild(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/v1/build", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	var view buildinfo.View
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.GitSHA != "dev" {
		t.Fatalf("got git_sha %q", view.GitSHA)
	}
}

func TestHandlerRoutesOverviewAndSettings(t *testing.T) {
	settings := overview.Settings{Comfort: []float64{10, 18, 24, 30}, UsableBatteryCapacityAh: 100}
	server := New(fakeApp{broker: events.NewBroker(1), overviewSettings: settings})
	for _, path := range []string{"/v1/overview", "/v1/overview/settings"} {
		rr := httptest.NewRecorder()
		server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d", path, rr.Code)
		}
	}
	body, _ := json.Marshal(settings)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/overview/settings", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT settings: got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/overview", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST overview: got %d", rr.Code)
	}
}

func TestRecordingRoutes(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), recording: recording.State{Status: "armed"}}
	server := New(app).Handler()

	state := httptest.NewRecorder()
	server.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/v1/recording/state", nil))
	if state.Code != http.StatusOK {
		t.Fatalf("state status = %d body=%s", state.Code, state.Body.String())
	}

	start := httptest.NewRecorder()
	server.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"victron_on","duration_minutes":0}`)))
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d body=%s", start.Code, start.Body.String())
	}
	var started recording.State
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started != app.recording {
		t.Fatalf("start state = %#v, want %#v", started, app.recording)
	}
}

func TestRecordingStartRejectsBadDurationAndConflict(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), startRecordingErr: recording.ErrActive}
	server := New(app).Handler()

	bad := httptest.NewRecorder()
	server.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"immediate","duration_minutes":-1}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d", bad.Code)
	}

	conflict := httptest.NewRecorder()
	server.ServeHTTP(conflict, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"immediate","duration_minutes":1}`)))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d", conflict.Code)
	}
}

func TestRecordingStartRejectsMalformedJSONAndUnexpectedFailure(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), startRecordingErr: errors.New("storage unavailable")}
	server := New(app).Handler()

	malformed := httptest.NewRecorder()
	server.ServeHTTP(malformed, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":`)))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d", malformed.Code)
	}

	concatenated := httptest.NewRecorder()
	server.ServeHTTP(concatenated, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"immediate","duration_minutes":1}{"wait_for":"immediate","duration_minutes":1}`)))
	if concatenated.Code != http.StatusBadRequest {
		t.Fatalf("concatenated status = %d", concatenated.Code)
	}

	failure := httptest.NewRecorder()
	server.ServeHTTP(failure, httptest.NewRequest(http.MethodPost, "/v1/recording/start", strings.NewReader(`{"wait_for":"immediate","duration_minutes":1}`)))
	if failure.Code != http.StatusInternalServerError {
		t.Fatalf("failure status = %d", failure.Code)
	}
}

func TestRecordingStopIsIdempotent(t *testing.T) {
	stops := 0
	app := fakeApp{broker: events.NewBroker(1), stopRecordingCalls: &stops}
	server := New(app).Handler()
	for range 2 {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/recording/stop", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d", rr.Code)
		}
	}
	if stops != 2 {
		t.Fatalf("stops = %d", stops)
	}
}

func TestTrackingSettingsRoutes(t *testing.T) {
	app := fakeApp{
		broker:           events.NewBroker(1),
		trackingSettings: tracking.Settings{WhenEngineOn: true, SampleInterval: 2 * time.Second},
		trackingDir:      "/var/lib/xtura/tracks",
	}
	server := New(app).Handler()

	get := httptest.NewRecorder()
	server.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/tracking/settings", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", get.Code, get.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got["when_engine_on"] != true {
		t.Fatalf("get when_engine_on = %v", got["when_engine_on"])
	}
	if got["sample_interval_seconds"] != 2.0 {
		t.Fatalf("get sample_interval_seconds = %v", got["sample_interval_seconds"])
	}
	if got["directory"] != "/var/lib/xtura/tracks" {
		t.Fatalf("get directory = %v", got["directory"])
	}
	if _, ok := got["SampleInterval"]; ok {
		t.Fatalf("get response leaked Go field names: %v", got)
	}

	put := httptest.NewRecorder()
	server.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/v1/tracking/settings", strings.NewReader(`{"when_engine_on":false,"sample_interval_seconds":30,"directory":"/ignored"}`)))
	if put.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", put.Code, put.Body.String())
	}
	var updated map[string]interface{}
	if err := json.Unmarshal(put.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode put response: %v", err)
	}
	if updated["when_engine_on"] != false {
		t.Fatalf("put when_engine_on = %v", updated["when_engine_on"])
	}
	if updated["sample_interval_seconds"] != 30.0 {
		t.Fatalf("put sample_interval_seconds = %v", updated["sample_interval_seconds"])
	}
	if updated["directory"] != "/var/lib/xtura/tracks" {
		t.Fatalf("put directory = %v, want runtime directory", updated["directory"])
	}

	malformed := httptest.NewRecorder()
	server.ServeHTTP(malformed, httptest.NewRequest(http.MethodPut, "/v1/tracking/settings", strings.NewReader(`{"when_engine_on":`)))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d", malformed.Code)
	}

	badMethod := httptest.NewRecorder()
	server.ServeHTTP(badMethod, httptest.NewRequest(http.MethodPost, "/v1/tracking/settings", nil))
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status = %d", badMethod.Code)
	}
}

func TestTrackingSettingsDTOConversion(t *testing.T) {
	settings := trackingSettingsFromDTO(trackingSettingsDTO{
		WhenEngineOn:          true,
		SampleIntervalSeconds: 30,
	})
	if !settings.WhenEngineOn || settings.SampleInterval != 30*time.Second {
		t.Fatalf("from DTO = %#v", settings)
	}
	zero := trackingSettingsFromDTO(trackingSettingsDTO{SampleIntervalSeconds: 0})
	if zero.SampleInterval != 0 {
		t.Fatalf("zero sample interval = %s, want 0", zero.SampleInterval)
	}
	dto := trackingSettingsToDTO(tracking.Settings{
		WhenEngineOn:   true,
		SampleInterval: 5 * time.Second,
	}, "/var/lib/xtura/tracks")
	if dto.WhenEngineOn != true || dto.SampleIntervalSeconds != 5.0 || dto.Directory != "/var/lib/xtura/tracks" {
		t.Fatalf("to DTO = %#v", dto)
	}
}

func TestTrackingSettingsUpdateRejectsValidationError(t *testing.T) {
	app := fakeApp{broker: events.NewBroker(1), updateTrackingErr: errors.New("tracking requires location.enabled")}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/tracking/settings", bytes.NewBufferString(`{"when_engine_on":true}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestTrackingStateRoute(t *testing.T) {
	state := tracking.State{WhenEngineOn: false, Tracking: true, PointCount: 3}
	server := New(fakeApp{broker: events.NewBroker(1), trackingState: state}).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracking/state", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got tracking.State
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WhenEngineOn || !got.Tracking || got.PointCount != 3 {
		t.Fatalf("state = %#v, want %#v", got, state)
	}
}

func TestTrackingStartStopRoutes(t *testing.T) {
	state := tracking.State{WhenEngineOn: false, Tracking: true, PointCount: 2}
	server := New(fakeApp{broker: events.NewBroker(1), trackingState: state}).Handler()

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/start", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("start code = %d, body %s", rr.Code, rr.Body.String())
	}
	var started tracking.State
	if err := json.Unmarshal(rr.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if !started.Tracking || started.PointCount != 2 {
		t.Fatalf("start body = %+v", started)
	}

	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/stop", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("stop code = %d, body %s", rr.Code, rr.Body.String())
	}

	server = New(fakeApp{broker: events.NewBroker(1), startTrackingErr: tracking.ErrEngineMode}).Handler()
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/start", nil))
	if rr.Code != http.StatusConflict {
		t.Fatalf("engine-mode start code = %d, body %s", rr.Code, rr.Body.String())
	}

	server = New(fakeApp{broker: events.NewBroker(1), stopTrackingErr: tracking.ErrEngineMode}).Handler()
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tracking/stop", nil))
	if rr.Code != http.StatusConflict {
		t.Fatalf("engine-mode stop code = %d, body %s", rr.Code, rr.Body.String())
	}

	badMethod := httptest.NewRecorder()
	server.ServeHTTP(badMethod, httptest.NewRequest(http.MethodGet, "/v1/tracking/start", nil))
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get start code = %d", badMethod.Code)
	}
}

func TestTracksRoutes(t *testing.T) {
	data := []byte(`{"type":"FeatureCollection"}`)
	readNames := []string{}
	deleteNames := []string{}
	app := fakeApp{
		broker:           events.NewBroker(1),
		trackFiles:       []tracking.FileInfo{{Name: "track-20260813.geojson", Bytes: 42, PointCount: 3}},
		trackReadData:    data,
		trackReadNames:   &readNames,
		trackDeleteNames: &deleteNames,
	}
	server := New(app).Handler()

	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/tracks", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var files []tracking.FileInfo
	if err := json.Unmarshal(list.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "track-20260813.geojson" {
		t.Fatalf("list = %#v", files)
	}

	download := httptest.NewRecorder()
	server.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/v1/tracks/track-20260813.geojson", nil))
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d body=%s", download.Code, download.Body.String())
	}
	if ct := download.Header().Get("Content-Type"); ct != "application/geo+json" {
		t.Fatalf("content type = %q", ct)
	}
	if cd := download.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment; filename=") || !strings.Contains(cd, "track-20260813.geojson") {
		t.Fatalf("content disposition = %q", cd)
	}
	if body := download.Body.String(); body != string(data) {
		t.Fatalf("body = %q, want %q", body, data)
	}
	if len(readNames) != 1 || readNames[0] != "track-20260813.geojson" {
		t.Fatalf("read names = %#v", readNames)
	}

	del := httptest.NewRecorder()
	server.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/v1/tracks/track-20260813.geojson", nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", del.Code, del.Body.String())
	}
	if len(deleteNames) != 1 || deleteNames[0] != "track-20260813.geojson" {
		t.Fatalf("delete names = %#v", deleteNames)
	}
}

func TestTrackRouteRejectsTraversalName(t *testing.T) {
	readNames := []string{}
	app := fakeApp{
		broker:         events.NewBroker(1),
		trackReadErr:   errors.New(`invalid track name "../x.geojson"`),
		trackReadNames: &readNames,
	}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracks/..%2Fx.geojson", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(readNames) != 1 || readNames[0] != "../x.geojson" {
		t.Fatalf("read names = %#v", readNames)
	}
}

func TestTrackDeleteRejectsTraversalName(t *testing.T) {
	deleteNames := []string{}
	app := fakeApp{
		broker:           events.NewBroker(1),
		trackDeleteErr:   errors.New(`invalid track name "../x.geojson"`),
		trackDeleteNames: &deleteNames,
	}
	server := New(app).Handler()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/v1/tracks/..%2Fx.geojson", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(deleteNames) != 1 || deleteNames[0] != "../x.geojson" {
		t.Fatalf("delete names = %#v", deleteNames)
	}
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"ok": "yes"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("got status %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("unexpected content type %q", ct)
	}
}

func TestHandleHeatingProgramsMethod(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodPost, "/v1/automation/heating-programs", nil)
	rr := httptest.NewRecorder()
	server.handleHeatingPrograms(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got status %d", rr.Code)
	}
	_ = time.Now()
}

func TestHandleHeatingScheduleGet(t *testing.T) {
	server := New(fakeApp{
		broker: events.NewBroker(1),
		schedule: config.HeatingScheduleDocument{
			Timezone: "Europe/London",
			Programs: []config.HeatingScheduleProgramDocument{{ID: "weekday", Enabled: true}},
			Revision: "rev-1",
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/automation/heating-schedule", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d", rr.Code)
	}
	var doc config.HeatingScheduleDocument
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Revision != "rev-1" {
		t.Fatalf("got revision %q", doc.Revision)
	}
}

func TestHandleHeatingTargetTemperatureMapsValidationErrorToBadRequest(t *testing.T) {
	server := New(fakeApp{
		broker:       events.NewBroker(1),
		setTargetErr: domainheating.ValidateTargetCelsius(25.0),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/heating/target-temperature", bytes.NewBufferString(`{"celsius":25}`))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleHeatingSchedulePutMethodAndBody(t *testing.T) {
	server := New(fakeApp{
		broker: events.NewBroker(1),
		schedule: config.HeatingScheduleDocument{
			Timezone: "Europe/London",
			Programs: []config.HeatingScheduleProgramDocument{{ID: "weekday", Enabled: true}},
			Revision: "rev-2",
		},
	})
	body, err := json.Marshal(config.HeatingScheduleDocument{
		Timezone: "Europe/London",
		Programs: []config.HeatingScheduleProgramDocument{
			{
				ID:      "weekday",
				Enabled: true,
				Days:    []string{"mon"},
				Periods: []config.HeatingSchedulePeriodDocument{{Start: "00:00", Mode: "off"}},
			},
		},
		Revision: "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/v1/automation/heating-schedule", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleHeatingModeGet(t *testing.T) {
	server := New(fakeApp{
		broker: events.NewBroker(1),
		mode:   config.HeatingRuntimeState{Mode: config.HeatingModeManual},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/heating/mode", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d", rr.Code)
	}
	var mode config.HeatingRuntimeState
	if err := json.Unmarshal(rr.Body.Bytes(), &mode); err != nil {
		t.Fatal(err)
	}
	if mode.Mode != config.HeatingModeManual {
		t.Fatalf("got mode %q", mode.Mode)
	}
}

func TestHandleHeatingModeBoostCancel(t *testing.T) {
	called := false
	app := fakeApp{
		broker:            events.NewBroker(1),
		mode:              config.HeatingRuntimeState{Mode: config.HeatingModeSchedule},
		cancelBoostCalled: &called,
	}
	server := New(app)
	req := httptest.NewRequest(http.MethodPost, "/v1/heating/mode/boost/cancel", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatal("expected cancel boost to be called")
	}
	var mode config.HeatingRuntimeState
	if err := json.Unmarshal(rr.Body.Bytes(), &mode); err != nil {
		t.Fatal(err)
	}
	if mode.Mode != config.HeatingModeSchedule {
		t.Fatalf("got mode %q", mode.Mode)
	}
}

func TestHandleLightsStateGet(t *testing.T) {
	server := New(fakeApp{
		broker: events.NewBroker(1),
		lights: domainlights.State{ExternalKnown: true, ExternalOn: true},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/lights/state", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d", rr.Code)
	}
	var state domainlights.State
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.ExternalKnown || !state.ExternalOn {
		t.Fatalf("got known=%t on=%t", state.ExternalKnown, state.ExternalOn)
	}
}

func TestHandleExteriorFlashRejectsBusy(t *testing.T) {
	server := New(fakeApp{
		broker:         events.NewBroker(1),
		flashLightsErr: runtime.ErrLightsFlashInProgress,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/lights/external/flash", bytes.NewBufferString(`{"count":2}`))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleExteriorFlashRejectsInvalidCount(t *testing.T) {
	server := New(fakeApp{
		broker:         events.NewBroker(1),
		flashLightsErr: runtime.ErrInvalidFlashCount,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/lights/external/flash", bytes.NewBufferString(`{"count":0}`))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleWaterStateGet(t *testing.T) {
	server := New(fakeApp{
		broker: events.NewBroker(1),
		water:  domainwater.State{ValveKnown: true, ValveMoving: true, ValveDirection: domainwater.ValveDirectionOpening},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/water/state", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d", rr.Code)
	}
	var state domainwater.State
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.ValveKnown || !state.ValveMoving || state.ValveDirection != domainwater.ValveDirectionOpening {
		t.Fatalf("unexpected water state: %+v", state)
	}
}

func TestHandleGreyWaterValveOpenRejectsBusy(t *testing.T) {
	server := New(fakeApp{
		broker:   events.NewBroker(1),
		waterErr: runtime.ErrWaterCommandInProgress,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/water/grey-valve/open", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleGreyWaterSchedulePost(t *testing.T) {
	openAt := time.Date(2026, 5, 6, 1, 0, 0, 0, time.UTC)
	server := New(fakeApp{
		broker: events.NewBroker(1),
		scheduledWater: domainwater.State{
			ScheduledOpening: &domainwater.ScheduledOpening{
				OpenAt:          openAt,
				LocalTime:       "03:00",
				Timezone:        "Europe/Rome",
				DurationMinutes: 30,
				Status:          "pending",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/water/grey-valve/schedule", bytes.NewBufferString(`{"target_time":"03:00","duration_minutes":30}`))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	var state domainwater.State
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.ScheduledOpening == nil || !state.ScheduledOpening.OpenAt.Equal(openAt) {
		t.Fatalf("unexpected scheduled opening %#v", state.ScheduledOpening)
	}
}

func TestHandleGreyWaterScheduleCancel(t *testing.T) {
	called := false
	server := New(fakeApp{
		broker:            events.NewBroker(1),
		cancelWaterCalled: &called,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/water/grey-valve/schedule/cancel", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatal("expected cancel to be called")
	}
}

func TestHandleLocationStateGet(t *testing.T) {
	server := New(fakeApp{
		broker: events.NewBroker(1),
		location: domainlocation.State{
			Configured: true,
			Known:      true,
			Provider:   "rutx50",
			Latitude:   51.5,
			Longitude:  -0.12,
			Timezone:   "Europe/London",
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/location/state", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d", rr.Code)
	}
	var state domainlocation.State
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.Configured || !state.Known || state.Provider != "rutx50" || state.Timezone != "Europe/London" {
		t.Fatalf("unexpected location state: %+v", state)
	}
}

func TestHandlerServesWebIndex(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("unexpected content type %q", ct)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="app"`) {
		t.Fatalf("index body did not contain app root: %s", body)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="deploymentInfo" class="detail-text"`) {
		t.Fatalf("index body did not contain deployment info line: %s", body)
	}
	if body := rr.Body.String(); strings.Contains(body, `id="buildInfo"`) {
		t.Fatalf("index body must not contain build footer: %s", body)
	}
	for _, want := range []string{`href="/static/styles.css?v=dev"`, `src="/static/navigation.js?v=dev"`, `src="/static/app.js?v=dev"`} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("index body did not contain versioned asset %q: %s", want, rr.Body.String())
		}
	}
	for _, want := range []string{
		`id="menuButton" class="menu-button" type="button" aria-label="Open navigation" aria-controls="navigationDrawer" aria-expanded="false">☰</button>`,
		`id="navigationBackdrop" class="navigation-backdrop" hidden`,
		`id="navigationDrawer" class="navigation-drawer" aria-label="Site navigation" aria-hidden="true" hidden`,
		`id="closeMenuButton" type="button" aria-label="Close navigation"`,
		`id="pageTitle">Overview<`,
		`id="connectionStatus" class="connection-pill">Connecting<`,
		`id="app" class="app-shell" aria-live="polite"`,
		`id="deploymentInfo" class="detail-text"`,
		`id="piStatusPanel" class="panel"`,
		`id="statusMessage" class="status-message">Loading controls.<`,
	} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("index body did not contain %q: %s", want, rr.Body.String())
		}
	}
	for _, page := range []string{"overview", "heating", "water", "lighting", "location", "system", "tools", "settings"} {
		for _, want := range []string{`data-page="` + page + `"`, `href="#/` + page + `"`} {
			if !strings.Contains(rr.Body.String(), want) {
				t.Fatalf("index body did not contain page link %q: %s", want, rr.Body.String())
			}
		}
	}
	for _, panel := range []string{"overviewPanel", "heatingPanel", "waterPanel", "lightingPanel", "locationPanel", "systemPanel", "toolsPanel", "settingsPanel"} {
		if !strings.Contains(rr.Body.String(), `id="`+panel+`" class="page-panel"`) &&
			!strings.Contains(rr.Body.String(), `id="`+panel+`" class="page-panel overview-layout"`) {
			t.Fatalf("index body did not contain page panel %q: %s", panel, rr.Body.String())
		}
	}
	if body := rr.Body.String(); strings.Contains(body, `class="section-switch"`) {
		t.Fatalf("index body must not contain section-switch tabs: %s", body)
	}
	if body := rr.Body.String(); strings.Contains(body, `class="eyebrow"`) {
		t.Fatalf("index body must not contain the brand eyebrow: %s", body)
	}
}

func TestWebIndexUsesNativeTimePickerForGreyWaterSchedule(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="greyScheduleTime" name="grey-schedule-time" type="time" value="03:00" step="60"`) {
		t.Fatalf("grey-water schedule must use a native minute-precision time input: %s", body)
	}
}

func TestHandlerServesStaticJavaScript(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("unexpected content type %q", ct)
	}
	if cacheControl := rr.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("unexpected cache control %q", cacheControl)
	}
	body := rr.Body.String()
	for _, want := range []string{"class XturaApi", "setHeatingModeSchedule", "setHeatingModeOff", "getBuildInfo", "renderBuild", "renderPiStatus", "getPiStatus", "applyRoute", "navigate", "XturaNavigation.parse", "hashchange", "pageTitles", "pageIds", "openNavigation", "closeNavigation", "document.querySelector(`[data-page=\"", `aria-current`, `byId("navigationDrawer")`, `byId("navigationBackdrop")`, `byId("menuButton")`, `byId("closeMenuButton")`, `byId("deploymentInfo")`} {
		if !strings.Contains(body, want) {
			t.Fatalf("javascript body did not contain %q: %s", want, body)
		}
	}
}

func TestHandlerServesPortraitMobileBackgroundStyle(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/static/styles.css", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"@media (max-width: 759px) and (orientation: portrait)", `url("/static/xtura-background-mobile.avif?v=1")`, `url("/static/xtura-background.avif?v=1")`, "body::before", "z-index: -1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stylesheet did not contain %q: %s", want, body)
		}
	}
}

func TestHandlerServesNavigationDrawerStyle(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	req := httptest.NewRequest(http.MethodGet, "/static/styles.css", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{".navigation-drawer", ".navigation-backdrop", ".menu-button", `a[aria-current="page"]`, "position: fixed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stylesheet did not contain %q: %s", want, body)
		}
	}
}

func TestHandleEventsFlushesInitialConnectionComment(t *testing.T) {
	server := New(fakeApp{broker: events.NewBroker(1)})
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Handler().ServeHTTP(rr, req)
	}()

	deadline := time.After(500 * time.Millisecond)
	for {
		if strings.Contains(rr.Body.String(), ": connected") {
			cancel()
			wg.Wait()
			return
		}
		select {
		case <-deadline:
			cancel()
			wg.Wait()
			t.Fatalf("expected initial SSE connection comment, got %q", rr.Body.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestHandlerServesPiStatus(t *testing.T) {
	sampledAt := time.Date(2026, 8, 14, 9, 40, 0, 0, time.UTC)
	temperatureC := 52.3
	server := New(fakeApp{
		broker: events.NewBroker(1),
		piStatus: host.Metrics{
			SampledAt: sampledAt,
			Snapshot: host.Snapshot{
				Model:         "Raspberry Pi Zero 2 W",
				Cores:         4,
				Load:          [3]float64{0.5, 0.3, 0.2},
				Memory:        host.Memory{TotalBytes: 1000, AvailableBytes: 400, UsedPercent: 60},
				Disk:          []host.DiskUsage{{Mount: "/", TotalBytes: 5000, UsedPercent: 50}},
				TemperatureC:  &temperatureC,
				UptimeSeconds: 100,
				Power:         host.PowerStatus{Status: "ok"},
			},
		},
	}).Handler()

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/pi/state", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got host.Metrics
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "Raspberry Pi Zero 2 W" || got.Cores != 4 || got.Load != [3]float64{0.5, 0.3, 0.2} {
		t.Fatalf("metrics = %#v", got)
	}
	if got.Memory.TotalBytes != 1000 || got.Memory.AvailableBytes != 400 || got.Memory.UsedPercent != 60 {
		t.Fatalf("memory = %#v", got.Memory)
	}
	if len(got.Disk) != 1 || got.Disk[0].Mount != "/" || got.Disk[0].UsedPercent != 50 {
		t.Fatalf("disk = %#v", got.Disk)
	}
	if got.TemperatureC == nil || *got.TemperatureC != 52.3 {
		t.Fatalf("temperature = %v", got.TemperatureC)
	}
	if got.UptimeSeconds != 100 {
		t.Fatalf("uptime = %d", got.UptimeSeconds)
	}
	if got.Power.Status != "ok" {
		t.Fatalf("power = %#v", got.Power)
	}

	badMethod := httptest.NewRecorder()
	server.ServeHTTP(badMethod, httptest.NewRequest(http.MethodPost, "/v1/pi/state", nil))
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status = %d", badMethod.Code)
	}
}
