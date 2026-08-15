package heating

import (
	"encoding/json"
	"fmt"
	"time"
)

type Direction string

const (
	DirectionSend    Direction = "send"
	DirectionReceive Direction = "receive"
)

const (
	SignalFreshWaterPercent           = 12
	SignalGreyWaterPercent            = 13
	SignalHeatingPower                = 101
	SignalHeatingBusy                 = 102
	SignalHeatingError                = 103
	SignalHeatingTargetTemp           = 105
	SignalHeatingActualTemp           = 106
	SignalHeatingTempUp               = 107
	SignalHeatingTempDown             = 108
	SignalHeatingGas                  = 110
	SignalHeatingGasText              = 111
	SignalHeatingElec1                = 113
	SignalHeatingElec2                = 114
	SignalHeatingElec3                = 115
	SignalHeatingPump                 = 119
	SignalBatteryCurrentA             = 212
	SignalBatteryStateOfChargePercent = 213
)

type WireFrame struct {
	MessageType int   `json:"messagetype"`
	MessageCmd  int   `json:"messagecmd"`
	Size        int   `json:"size"`
	Data        []int `json:"data"`
}

type Frame struct {
	At        time.Time
	Direction Direction
	Wire      WireFrame
}

func ParseWireFrame(raw string) (WireFrame, error) {
	var frame WireFrame
	if err := json.Unmarshal([]byte(raw), &frame); err != nil {
		return WireFrame{}, err
	}
	return frame, nil
}

func (f Frame) SignalID() int {
	if len(f.Wire.Data) == 0 {
		return -1
	}
	if len(f.Wire.Data) == 1 {
		return f.Wire.Data[0]
	}
	return f.Wire.Data[0] | (f.Wire.Data[1] << 8)
}

func (f Frame) RelevantToHeating() bool {
	switch f.SignalID() {
	case 14, 15, 26, 87, 88, 89, 90, 91, 92, 93, 95, 97, 98, 99,
		SignalHeatingPower, SignalHeatingBusy, SignalHeatingError, SignalHeatingTargetTemp,
		SignalHeatingActualTemp, SignalHeatingTempUp, SignalHeatingTempDown,
		SignalHeatingGas, SignalHeatingGasText, SignalHeatingElec1, SignalHeatingElec2,
		SignalHeatingElec3, SignalHeatingPump:
		return true
	default:
		return false
	}
}

var signalLabels = map[int]string{
	14:  "Heating Restart Button",
	15:  "30 Minutes Boost Timer",
	26:  "Alde Boost Off Text",
	87:  "Heating Elec. 2kW",
	88:  "Heating Elec 1kW",
	89:  "Heating Elec Off",
	90:  "Hot Water Boost",
	91:  "Hot Water Normal",
	92:  "Hot Water Off",
	93:  "Heating Indication",
	95:  "Hot Water Auto-Boost",
	97:  "Heating Elec. 3kW",
	98:  "Priority: Electricity",
	99:  "Priority: GAS",
	101: "HeatingTurnON/OFF ALDE",
	102: "HeatingBusy",
	103: "HeatingError",
	105: "HeatingTargetTemp",
	106: "Actual Temp ALDE",
	107: "HeatingTempUP ALDE",
	108: "HeatingTempDWN ALDE",
	110: "HeatingSettingGaz ALDE",
	111: "Gas On Text Indication",
	113: "HeatingElec1KW",
	114: "HeatingElec2KW",
	115: "HeatingElec3KW",
	119: "HeatingPumpRunning",
}

func (f Frame) String() string {
	compactPart := ""
	if compact := f.CompactInterpretation(); compact != "" {
		compactPart = fmt.Sprintf(" %s", compact)
	}
	return fmt.Sprintf(
		"%s %s type=%d cmd=%d signal=%d%s data=%v",
		f.At.Format("15:04:05.000"),
		f.Direction,
		f.Wire.MessageType,
		f.Wire.MessageCmd,
		f.SignalID(),
		compactPart,
		f.Wire.Data,
	)
}

func (f Frame) CompactInterpretation() string {
	label := signalLabels[f.SignalID()]
	value := f.InterpretationValue()
	if label == "" || value == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", label, value)
}

func (f Frame) InterpretationValue() string {
	data := f.Wire.Data
	if len(data) < 3 {
		return ""
	}
	switch f.SignalID() {
	case SignalHeatingPower:
		switch data[2] {
		case 0:
			return "off"
		case 1:
			return "on"
		case 3:
			return "command_on"
		case 5:
			return "command_off"
		case 129:
			return "transition"
		}
	case SignalHeatingTargetTemp:
		if _, tempC, ok := decodeTargetTemperature(data); ok {
			return fmt.Sprintf("%.1fC", tempC)
		}
	case SignalHeatingBusy:
		if data[2] == 0 {
			return "false"
		}
		if data[2] == 1 {
			return "true"
		}
	case SignalHeatingPump:
		if data[2] == 0 {
			return "false"
		}
		if data[2] == 1 {
			return "true"
		}
	}
	return ""
}
