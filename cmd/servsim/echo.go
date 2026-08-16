package main

import (
	"encoding/json"
	"math"
	"sync"

	"empirebus-tests/heating"
)

const (
	simSignalValveOpen  = 4
	simSignalValveClose = 5
	simSignalPower      = 101
	simSignalBusy       = 102
	simSignalTargetTemp = 105
	simSignalTempUp     = 107
	simSignalTempDown   = 108
	simSignalLightsOn   = 47
	simSignalLightsOff  = 48
)

type echoModel struct {
	mu          sync.Mutex
	heatingOn   bool
	targetKnown bool
	targetC     float64
}

func newEchoModel() *echoModel {
	return &echoModel{}
}

func (m *echoModel) observe(wire heating.WireFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(wire.Data) < 3 {
		return
	}
	signal := wire.Data[0] | (wire.Data[1] << 8)
	switch signal {
	case simSignalPower:
		m.heatingOn = wire.Data[2] == 1
	case simSignalTargetTemp:
		if temp, ok := heating.DecodeTargetTemperature(wire.Data); ok {
			m.targetKnown = true
			m.targetC = temp
		}
	}
}

func (m *echoModel) onCommand(wire heating.WireFrame) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wire.MessageType != 17 || len(wire.Data) < 3 {
		return nil
	}
	signal := wire.Data[0] | (wire.Data[1] << 8)
	value := wire.Data[2]
	switch wire.MessageCmd {
	case 0:
		switch {
		case signal == simSignalPower && value == 3:
			m.heatingOn = true
			out := []string{stateFrame(simSignalPower, 1), stateFrame(simSignalBusy, 0)}
			if !m.targetKnown {
				m.seedTargetLocked()
				out = append(out, targetFrame(m.targetC))
			}
			return out
		case signal == simSignalPower && value == 5:
			m.heatingOn = false
			return []string{stateFrame(simSignalPower, 0)}
		case signal == simSignalLightsOn && value == 3:
			return []string{stateFrame(simSignalLightsOn, 1)}
		case signal == simSignalLightsOff && value == 3:
			return []string{stateFrame(simSignalLightsOff, 1)}
		}
	case 1:
		switch {
		case signal == simSignalTempUp && value == 0:
			if !m.targetKnown {
				m.seedTargetLocked()
			}
			m.targetC += 0.5
			return []string{targetFrame(m.targetC)}
		case signal == simSignalTempDown && value == 0:
			if !m.targetKnown {
				m.seedTargetLocked()
			}
			m.targetC -= 0.5
			return []string{targetFrame(m.targetC)}
		case signal == simSignalValveOpen || signal == simSignalValveClose:
			return []string{stateFrame(signal, value)}
		}
	}
	return nil
}

func (m *echoModel) seedTargetLocked() {
	m.targetKnown = true
	m.targetC = 20.0
}

func stateFrame(signal, value int) string {
	raw := heating.WireFrame{
		MessageType: 16,
		MessageCmd:  5,
		Size:        8,
		Data:        []int{signal & 0xff, (signal >> 8) & 0xff, value, 0, 0, 0, 0, 0},
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(payload)
}

func targetFrame(temp float64) string {
	raw := int32(math.Round((temp + 273.15) * 1000))
	rawData := []int{
		105, 0, 0, 22,
		int(byte(raw)), int(byte(raw >> 8)), int(byte(raw >> 16)), int(byte(raw >> 24)),
	}
	payload, err := json.Marshal(heating.WireFrame{MessageType: 16, MessageCmd: 5, Size: 8, Data: rawData})
	if err != nil {
		return ""
	}
	return string(payload)
}
