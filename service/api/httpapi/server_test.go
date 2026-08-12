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
	"empirebus-tests/service/buildinfo"
	"empirebus-tests/service/config"
	domainheating "empirebus-tests/service/domains/heating"
	domainlights "empirebus-tests/service/domains/lights"
	domainlocation "empirebus-tests/service/domains/location"
	domainwater "empirebus-tests/service/domains/water"
	"empirebus-tests/service/recording"
	"empirebus-tests/service/runtime"
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

func (f fakeApp) Broker() *events.Broker {
	return f.broker
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
	if body := rr.Body.String(); !strings.Contains(body, `id="buildInfo"`) {
		t.Fatalf("index body did not contain build footer: %s", body)
	}
	for _, want := range []string{`id="lightingTab" class="tab is-active" type="button" aria-controls="lightingPanel">Light</button>`, `id="waterTab" class="tab" type="button" aria-controls="waterPanel">Water</button>`, `id="heatingTab" class="tab" type="button" aria-controls="heatingPanel">Heat</button>`, `id="settingsTab" class="tab" type="button" aria-controls="settingsPanel">Settings</button>`} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("index body did not contain compact tab %q: %s", want, rr.Body.String())
		}
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
	for _, want := range []string{"class XturaApi", "setHeatingModeSchedule", "setHeatingModeOff", "getBuildInfo", "renderBuild"} {
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
	for _, want := range []string{"@media (max-width: 759px) and (orientation: portrait)", `url("/static/xtura-background-mobile.avif?v=1")`, `url("/static/xtura-background.avif?v=1")`} {
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
