package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	domainheating "empirebus-tests/service/domains/heating"
	"empirebus-tests/service/runtime"
)

type fakeApp struct {
	broker   *events.Broker
	schedule config.HeatingScheduleDocument
	mode     config.HeatingRuntimeState
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
	return nil
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

func (f fakeApp) HeatingSchedule() config.HeatingScheduleDocument {
	return f.schedule
}

func (f fakeApp) UpdateHeatingSchedule(context.Context, config.HeatingScheduleDocument) (config.HeatingScheduleDocument, error) {
	return f.schedule, nil
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
