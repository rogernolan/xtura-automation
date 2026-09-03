package runtime

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	domainwater "empirebus-tests/service/domains/water"
)

type stubWaterController struct {
	mu       sync.Mutex
	state    domainwater.State
	commands []string
	holds    []time.Duration
	err      error
	block    chan struct{}
}

func (s *stubWaterController) OpenGreyWaterValve(_ context.Context, hold time.Duration) error {
	return s.record("open", hold)
}

func (s *stubWaterController) CloseGreyWaterValve(_ context.Context, hold time.Duration) error {
	return s.record("close", hold)
}

func (s *stubWaterController) WaterState() domainwater.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *stubWaterController) record(command string, hold time.Duration) error {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, command)
	s.holds = append(s.holds, hold)
	if s.err != nil {
		return s.err
	}
	s.state.ValveKnown = true
	s.state.ValveMoving = false
	return nil
}

func TestOpenGreyWaterValveUsesFiveSecondHold(t *testing.T) {
	t.Parallel()

	controller := &stubWaterController{}
	app := &App{water: controller}
	if err := app.OpenGreyWaterValve(context.Background()); err != nil {
		t.Fatalf("OpenGreyWaterValve returned error: %v", err)
	}
	if got, want := controller.commands, []string{"open"}; !equalStrings(got, want) {
		t.Fatalf("unexpected commands: got=%v want=%v", got, want)
	}
	if len(controller.holds) != 1 || controller.holds[0] != 5*time.Second {
		t.Fatalf("expected one 5s hold, got %v", controller.holds)
	}
	if state := app.WaterState(); state.CommandInProgress {
		t.Fatalf("expected command flag cleared")
	}
}

func TestWaterCommandRejectsBusy(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	controller := &stubWaterController{block: block}
	app := &App{water: controller}
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.OpenGreyWaterValve(context.Background())
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if app.WaterState().CommandInProgress {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for water command to begin")
		}
		time.Sleep(10 * time.Millisecond)
	}

	err := app.CloseGreyWaterValve(context.Background())
	if !errors.Is(err, ErrWaterCommandInProgress) {
		t.Fatalf("expected ErrWaterCommandInProgress, got %v", err)
	}
	close(block)
	if err := <-errCh; err != nil {
		t.Fatalf("expected first command to finish cleanly, got %v", err)
	}
}

func TestWaterCommandRecordsError(t *testing.T) {
	t.Parallel()

	controller := &stubWaterController{err: errors.New("valve failed")}
	app := &App{water: controller}
	err := app.CloseGreyWaterValve(context.Background())
	if err == nil || err.Error() != "valve failed" {
		t.Fatalf("expected valve failure, got %v", err)
	}
	if state := app.WaterState(); state.LastCommandError != "valve failed" {
		t.Fatalf("expected command error, got %q", state.LastCommandError)
	}
}

func TestLoadWaterRuntimeStateRecoveryClosesValveAndResetsState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "water-runtime.yaml")
	if err := os.WriteFile(path, []byte("scheduled_opening: [corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	controller := &stubWaterController{}
	app := &App{
		water:                 controller,
		waterRuntimeStatePath: path,
		logger:                log.New(io.Discard, "", 0),
	}
	if err := app.loadWaterRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if got, want := controller.commands, []string{"close"}; !equalStrings(got, want) {
		t.Fatalf("recovery commands: got=%v want=%v", got, want)
	}
	if app.currentWaterRuntimeState().ScheduledOpening != nil {
		t.Fatal("recovered water state retained a schedule")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("safe replacement state missing: %v", err)
	}
}

func TestScheduleGreyWaterOpeningPersistsNextLocalOccurrenceAsUTC(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 5, 22, 0, 0, 0, loc)
	app := &App{
		rawConfig:             config.Config{Automation: config.AutomationConfig{Timezone: "Europe/Rome"}},
		waterRuntimeStatePath: filepath.Join(t.TempDir(), "water-runtime.yaml"),
		broker:                events.NewBroker(1),
		logger:                log.New(io.Discard, "", 0),
		now:                   func() time.Time { return now },
		waterSchedulerWake:    make(chan struct{}, 1),
	}

	state, err := app.ScheduleGreyWaterOpening(context.Background(), "03:00", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wantOpenAt := time.Date(2026, 5, 6, 3, 0, 0, 0, loc).UTC()
	if state.ScheduledOpening == nil {
		t.Fatal("expected scheduled opening in water state")
	}
	if !state.ScheduledOpening.OpenAt.Equal(wantOpenAt) {
		t.Fatalf("got open_at %s want %s", state.ScheduledOpening.OpenAt, wantOpenAt)
	}
	if state.ScheduledOpening.DurationMinutes != 30 || state.ScheduledOpening.LocalTime != "03:00" {
		t.Fatalf("unexpected schedule %#v", state.ScheduledOpening)
	}
	loaded, err := config.LoadWaterRuntimeState(app.waterRuntimeStatePath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ScheduledOpening == nil || !loaded.ScheduledOpening.OpenAt.Equal(wantOpenAt) {
		t.Fatalf("schedule was not persisted: %#v", loaded.ScheduledOpening)
	}
}

func TestCancelGreyWaterOpeningClearsPersistedSchedule(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "water-runtime.yaml")
	openAt := time.Date(2026, 5, 6, 1, 0, 0, 0, time.UTC)
	if err := config.SaveWaterRuntimeState(path, config.WaterRuntimeState{
		ScheduledOpening: &config.GreyWaterScheduledOpening{
			OpenAt:          openAt,
			LocalTime:       "03:00",
			Timezone:        "Europe/Rome",
			DurationMinutes: 30,
			Status:          config.GreyWaterSchedulePending,
		},
	}); err != nil {
		t.Fatal(err)
	}
	app := &App{
		waterState: domainwater.State{
			ScheduledOpening: &domainwater.ScheduledOpening{
				OpenAt:          openAt,
				LocalTime:       "03:00",
				Timezone:        "Europe/Rome",
				DurationMinutes: 30,
				Status:          "pending",
			},
		},
		waterRuntimeStatePath: path,
		broker:                events.NewBroker(1),
		logger:                log.New(io.Discard, "", 0),
		waterSchedulerWake:    make(chan struct{}, 1),
	}

	state, err := app.CancelGreyWaterOpening(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.ScheduledOpening != nil {
		t.Fatalf("expected schedule cleared, got %#v", state.ScheduledOpening)
	}
	loaded, err := config.LoadWaterRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ScheduledOpening != nil {
		t.Fatalf("expected persisted schedule cleared, got %#v", loaded.ScheduledOpening)
	}
}

func TestExecuteDueGreyWaterOpeningClosesAfterDurationAndLeavesMessage(t *testing.T) {
	t.Parallel()

	controller := &stubWaterController{}
	path := filepath.Join(t.TempDir(), "water-runtime.yaml")
	openAt := time.Date(2026, 5, 6, 1, 0, 0, 0, time.UTC)
	now := openAt
	app := &App{
		water:                 controller,
		waterRuntimeStatePath: path,
		broker:                events.NewBroker(4),
		logger:                log.New(io.Discard, "", 0),
		sleep:                 func(time.Duration) {},
		now:                   func() time.Time { return now },
		waterSchedulerWake:    make(chan struct{}, 1),
		waterRuntimeState: config.WaterRuntimeState{
			ScheduledOpening: &config.GreyWaterScheduledOpening{
				OpenAt:          openAt,
				LocalTime:       "03:00",
				Timezone:        "Europe/Rome",
				DurationMinutes: 30,
				Status:          config.GreyWaterSchedulePending,
			},
		},
	}

	if err := app.executeDueGreyWaterOpening(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := controller.commands, []string{"open", "close"}; !equalStrings(got, want) {
		t.Fatalf("unexpected commands: got=%v want=%v", got, want)
	}
	state := app.WaterState()
	if state.ScheduledOpening != nil {
		t.Fatalf("expected schedule cleared, got %#v", state.ScheduledOpening)
	}
	if state.LastScheduleMessage == "" {
		t.Fatal("expected completion message")
	}
	loaded, err := config.LoadWaterRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ScheduledOpening != nil || loaded.LastScheduleMessage == "" {
		t.Fatalf("unexpected persisted water runtime state %#v", loaded)
	}
}
