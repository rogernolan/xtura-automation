package heating

import (
	"fmt"
	"math"
	"time"
)

type PowerState string

const (
	PowerUnknown    PowerState = "unknown"
	PowerOff        PowerState = "off"
	PowerOn         PowerState = "on"
	PowerTransition PowerState = "transition"
)

type Evidence string

const (
	EvidenceUnknown       Evidence = "unknown"
	EvidenceSignal101     Evidence = "signal101"
	EvidenceSignal105     Evidence = "signal105"
	EvidenceSignal102     Evidence = "signal102"
	EvidenceSignal119     Evidence = "signal119"
	EvidenceCorrelatedAck Evidence = "correlated-ack"
)

type HeaterState struct {
	PowerState         PowerState
	PowerEvidence      Evidence
	BusyKnown          bool
	Busy               bool
	BusyEvidence       Evidence
	PumpKnown          bool
	PumpRunning        bool
	PumpEvidence       Evidence
	TargetTempKnown    bool
	TargetTempC        float64
	TargetRaw          int
	TargetEvidence     Evidence
	TargetPayload      []int
	LastUpdated        time.Time
	LastHeatingFrameAt time.Time
}

func (s HeaterState) Ready() bool {
	if s.PowerState != PowerOn {
		return false
	}
	return s.BusyKnown && !s.Busy
}

func (s HeaterState) Clone() HeaterState {
	dup := s
	if s.TargetPayload != nil {
		dup.TargetPayload = append([]int(nil), s.TargetPayload...)
	}
	return dup
}

func (s HeaterState) String() string {
	target := "unknown"
	if s.TargetTempKnown {
		target = fmt.Sprintf("%.1fC", s.TargetTempC)
	}
	return fmt.Sprintf(
		"power=%s busy=%t pump=%t target=%s raw=%d",
		s.PowerState,
		s.Busy,
		s.PumpRunning,
		target,
		s.TargetRaw,
	)
}

func updateState(state *HeaterState, frame Frame) bool {
	changed := false
	state.LastUpdated = frame.At
	if frame.RelevantToHeating() {
		state.LastHeatingFrameAt = frame.At
	}
	data := frame.Wire.Data
	if len(data) < 3 {
		return false
	}
	switch frame.SignalID() {
	case SignalHeatingPower:
		next := PowerUnknown
		switch data[2] {
		case 0:
			next = PowerOff
		case 1:
			next = PowerOn
		case 129:
			next = PowerTransition
		}
		if next != PowerUnknown && state.PowerState != next {
			state.PowerState = next
			state.PowerEvidence = EvidenceSignal101
			changed = true
		}
	case SignalHeatingBusy:
		value := data[2] == 1
		if !state.BusyKnown || state.Busy != value {
			state.BusyKnown = true
			state.Busy = value
			state.BusyEvidence = EvidenceSignal102
			changed = true
		}
	case SignalHeatingPump:
		value := data[2] == 1
		if !state.PumpKnown || state.PumpRunning != value {
			state.PumpKnown = true
			state.PumpRunning = value
			state.PumpEvidence = EvidenceSignal119
			changed = true
		}
	case SignalHeatingTargetTemp:
		raw, tempC, ok := decodeTargetTemperature(data)
		if ok && (!state.TargetTempKnown || state.TargetRaw != raw || math.Abs(state.TargetTempC-tempC) > 0.001) {
			state.TargetTempKnown = true
			state.TargetTempC = tempC
			state.TargetRaw = raw
			state.TargetEvidence = EvidenceSignal105
			state.TargetPayload = append([]int(nil), data...)
			changed = true
		}
	}
	return changed
}

func decodeTargetTemperature(data []int) (int, float64, bool) {
	if len(data) < 8 || (data[0]|(data[1]<<8)) != SignalHeatingTargetTemp || data[3] != 22 {
		return 0, 0, false
	}
	raw := int32(data[4] | (data[5] << 8) | (data[6] << 16) | (data[7] << 24))
	celsius := float64(raw)/1000 - 273.15
	return int(raw), math.Round(celsius*2) / 2, true
}

// DecodeTargetTemperature extracts the displayed setpoint from a signal 105
// payload, rounding to the observed 0.5 C grid. ok is false when the payload
// is not a valid signal 105 frame.
func DecodeTargetTemperature(data []int) (float64, bool) {
	_, tempC, ok := decodeTargetTemperature(data)
	return tempC, ok
}
