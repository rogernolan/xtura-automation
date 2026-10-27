package garmin

import (
	"time"

	rootheating "empirebus-tests/heating"
	"empirebus-tests/service/domains/heating"
)

// ElectricSupplyState uses session-local received state. Settings are sent on
// change, so their age alone does not make them stale while the stream is live.
func (a *Adapter) ElectricSupplyState(now time.Time) heating.ElectricSupplyState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	session, generation := a.session, a.sessionGeneration
	state := heating.ElectricSupplyState{Session: generation}
	if session == nil || session.Err() != nil {
		return state
	}
	last := session.LastReceivedAt()
	if last.IsZero() || now.Sub(last) > 15*time.Second {
		return state
	}
	power, ok := electricStatus(session, 101, 0)
	if !ok {
		return state
	}
	if power == 0 {
		state.Known = true
		return state
	}
	// 129 is the observed on/transition indication during setting changes.
	if power != 1 && power != 129 {
		return state
	}
	state.HeaterOn = true
	selected := 0
	for _, setting := range []struct {
		signal int
		watts  float64
	}{{89, 0}, {88, 1000}, {87, 2000}, {97, 3000}} {
		flags, valid := electricStatus(session, setting.signal, 1)
		if !valid || flags > 1 {
			return state
		}
		if flags == 1 {
			selected++
			state.ElectricWatts = setting.watts
		}
	}
	if selected != 1 {
		return state
	}
	if state.ElectricWatts == 0 {
		state.Known = true
		return state
	}
	shore, ok := electricStatus(session, 28, 0)
	if !ok {
		return state
	}
	voltage, voltageFlags, ok := electricValue(session, 205, 27)
	if !ok {
		return state
	}
	// The live unplug capture paired an off/unavailable shore indication with
	// unavailable zero input voltage while the rest of the session stayed live.
	if (shore == 0 || shore == 128) && voltage == 0 && (voltageFlags == 0 || voltageFlags == 128) {
		state.Known = true
		return state
	}
	if shore != 1 || voltageFlags != 0 || voltage <= 0 || voltage > 300 {
		return state
	}
	limit, flags, ok := electricValue(session, 204, 6)
	if !ok || flags != 0 || limit < 0 {
		return state
	}
	state.Known, state.ShoreConnected = true, true
	state.InputLimitA, state.InputVoltageV = limit, voltage
	state.ShoreWatts = limit * voltage
	return state
}

func electricStatus(session *rootheating.Session, signal, command int) (int, bool) {
	frame, _, ok := session.LatestReceivedSignal(signal)
	if !ok || frame.MessageType != 16 || frame.MessageCmd != command || frame.Size != 3 || len(frame.Data) != 3 {
		return 0, false
	}
	flags := frame.Data[2]
	return flags, flags >= 0 && flags <= 255
}

func electricValue(session *rootheating.Session, signal, kind int) (float64, int, bool) {
	frame, _, ok := session.LatestReceivedSignal(signal)
	if !ok || frame.MessageType != 16 || frame.MessageCmd != 5 || frame.Size != 8 || len(frame.Data) != 8 || frame.Data[3] != kind {
		return 0, 0, false
	}
	for _, value := range frame.Data {
		if value < 0 || value > 255 {
			return 0, 0, false
		}
	}
	raw, ok := signedInt32LittleEndian(frame.Data[4:8])
	return float64(raw) / 1000, frame.Data[2], ok
}
