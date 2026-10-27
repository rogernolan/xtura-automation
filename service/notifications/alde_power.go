package notifications

import (
	"fmt"
	"sync"
	"time"

	"empirebus-tests/service/domains/heating"
)

const AldePowerDelay = time.Minute

// AldePowerEvaluator latches one warning until a known safe state or explicit
// disable. Unknown data interrupts the timer without rearming a sent warning.
type AldePowerEvaluator struct {
	mu      sync.Mutex
	since   time.Time
	sent    bool
	session uint64
}

func (e *AldePowerEvaluator) Evaluate(state heating.ElectricSupplyState, enabled bool, now time.Time) []Notification {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !enabled {
		e.since, e.sent = time.Time{}, false
		return nil
	}
	if state.Session != e.session {
		e.since, e.session = time.Time{}, state.Session
	}
	if !state.Known || !finite(state.ElectricWatts) || !finite(state.ShoreWatts) || state.ShoreWatts < 0 {
		e.since = time.Time{}
		return nil
	}
	if !state.HeaterOn || state.ElectricWatts <= state.ShoreWatts {
		e.since, e.sent = time.Time{}, false
		return nil
	}
	if e.sent {
		return nil
	}
	if e.since.IsZero() || now.Before(e.since) {
		e.since = now
	}
	if now.Sub(e.since) < AldePowerDelay {
		return nil
	}
	e.sent = true
	body := fmt.Sprintf("Alde is set to %.0f kW, but shore power has been unavailable for 60 seconds.", state.ElectricWatts/1000)
	if state.ShoreConnected {
		body = fmt.Sprintf("Alde is set to %.0f kW; the configured shore limit is %.1f A at %.0f V (about %.0f W). The selected setting has exceeded this allowance for 60 seconds.", state.ElectricWatts/1000, state.InputLimitA, state.InputVoltageV, state.ShoreWatts)
	}
	return []Notification{{Kind: "alde_power", AlertID: "alde-power", AlertName: "Alde shore power", Title: "Alde electric heating exceeds shore allowance", Body: body, At: now}}
}
