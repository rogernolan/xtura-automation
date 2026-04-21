package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	domainheating "empirebus-tests/service/domains/heating"
	"empirebus-tests/service/runtime"
)

type fakeApp struct {
	broker *events.Broker
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
