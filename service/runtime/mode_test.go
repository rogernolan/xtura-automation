package runtime

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	domainheating "empirebus-tests/service/domains/heating"
)

type fakeHeatingController struct {
	ensureOnCalls  int
	ensureOffCalls int
	setTargetCalls []float64
}

func (f *fakeHeatingController) EnsureOn(context.Context) error {
	f.ensureOnCalls++
	return nil
}

func (f *fakeHeatingController) EnsureOff(context.Context) error {
	f.ensureOffCalls++
	return nil
}

func (f *fakeHeatingController) SetTargetTemperature(_ context.Context, celsius float64) error {
	f.setTargetCalls = append(f.setTargetCalls, celsius)
	return nil
}

func (f *fakeHeatingController) CurrentState() HeatingStateView {
	return domainheating.State{}
}

func (f *fakeHeatingController) Health() domainheating.AdapterHealth {
	return domainheating.AdapterHealth{}
}

func TestSetHeatingModeManualPersistsAndApplies(t *testing.T) {
	t.Parallel()
	adapter := &fakeHeatingController{}
	app := &App{
		adapter:          adapter,
		broker:           events.NewBroker(1),
		logger:           log.New(io.Discard, "", 0),
		runtimeStatePath: filepath.Join(t.TempDir(), "runtime.yaml"),
		schedulerWake:    make(chan struct{}, 1),
	}
	state, err := app.SetHeatingModeManual(context.Background(), 19.0)
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != config.HeatingModeManual {
		t.Fatalf("got mode %q", state.Mode)
	}
	if adapter.ensureOnCalls != 1 {
		t.Fatalf("got ensureOnCalls=%d", adapter.ensureOnCalls)
	}
	if len(adapter.setTargetCalls) != 1 || adapter.setTargetCalls[0] != 19.0 {
		t.Fatalf("unexpected set target calls %#v", adapter.setTargetCalls)
	}
}

func TestCollapseExpiredBoostRestoresResumeMode(t *testing.T) {
	t.Parallel()
	manual := 18.5
	state := config.HeatingRuntimeState{
		Mode: config.HeatingModeBoost,
		Boost: &config.HeatingBoostState{
			TargetCelsius:             22.0,
			ExpiresAt:                 time.Now().UTC().Add(-time.Minute),
			ResumeMode:                config.HeatingModeManual,
			ResumeManualTargetCelsius: &manual,
		},
	}
	expired, collapsed := collapseExpiredBoost(state, time.Now().UTC())
	if !expired {
		t.Fatal("expected boost to be expired")
	}
	if collapsed.Mode != config.HeatingModeManual {
		t.Fatalf("got mode %q", collapsed.Mode)
	}
	if collapsed.ManualTargetCelsius == nil || *collapsed.ManualTargetCelsius != manual {
		t.Fatalf("unexpected manual target %#v", collapsed.ManualTargetCelsius)
	}
}
