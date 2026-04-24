package runtime

import (
	"testing"
	"time"

	domainlights "empirebus-tests/service/domains/lights"
)

func TestDefaultLightsStateIsUnknownAndIdle(t *testing.T) {
	app := App{}
	state := app.LightsState()
	if state.ExternalKnown {
		t.Fatalf("expected external state to start unknown")
	}
	if state.ExternalOn {
		t.Fatalf("expected external_on zero value to be false")
	}
	if state.FlashInProgress {
		t.Fatalf("expected flash to start idle")
	}
	if state.LastCommandError != "" {
		t.Fatalf("expected no last command error, got %q", state.LastCommandError)
	}
	if state.LastUpdatedAt != nil {
		t.Fatalf("expected last updated time to start unset")
	}
}

func TestMemoryLightsStateTracksExteriorOnOff(t *testing.T) {
	at := time.Unix(1710000000, 0).UTC()
	state := recordExteriorSignal(domainlights.State{}, true, at)
	if !state.ExternalKnown {
		t.Fatalf("expected exterior state to become known")
	}
	if !state.ExternalOn {
		t.Fatalf("expected exterior lights to be on")
	}
	if state.LastUpdatedAt == nil || !state.LastUpdatedAt.Equal(at) {
		t.Fatalf("expected last update timestamp to be recorded")
	}

	state = recordExteriorSignal(state, false, at.Add(time.Second))
	if !state.ExternalKnown {
		t.Fatalf("expected exterior state to remain known")
	}
	if state.ExternalOn {
		t.Fatalf("expected exterior lights to be off after off signal")
	}
}
