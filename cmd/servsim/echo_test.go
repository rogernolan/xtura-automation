package main

import (
	"encoding/json"
	"testing"

	"empirebus-tests/heating"
)

type echoFrame struct {
	MessageType int   `json:"messagetype"`
	MessageCmd  int   `json:"messagecmd"`
	Data        []int `json:"data"`
}

func parseEchoFrames(t *testing.T, raws []string) []echoFrame {
	t.Helper()
	frames := make([]echoFrame, 0, len(raws))
	for _, raw := range raws {
		var frame echoFrame
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			t.Fatalf("unmarshal echo frame %q: %v", raw, err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func tempOf(t *testing.T, frames []echoFrame) []float64 {
	t.Helper()
	var temps []float64
	for _, frame := range frames {
		if len(frame.Data) >= 8 && frame.Data[0] == 105 {
			temp, ok := heating.DecodeTargetTemperature(frame.Data)
			if !ok {
				t.Fatalf("frame %v is not a decodable 105 frame", frame.Data)
			}
			temps = append(temps, temp)
		}
	}
	return temps
}

func cmd(typ, cmd int, data []int) heating.WireFrame {
	return heating.WireFrame{MessageType: typ, MessageCmd: cmd, Size: len(data), Data: data}
}

func TestEchoModelHeatingPowerOnSeedsTarget(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{101, 0, 3})))
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if frames[0].Data[0] != 101 || frames[0].Data[2] != 1 {
		t.Fatalf("frame 0 = %v, want 101 value 1", frames[0].Data)
	}
	if frames[1].Data[0] != 102 || frames[1].Data[2] != 0 {
		t.Fatalf("frame 1 = %v, want 102 value 0", frames[1].Data)
	}
	temps := tempOf(t, frames)
	if len(temps) != 1 || temps[0] != 20.0 {
		t.Fatalf("got temps %v, want [20]", temps)
	}
}

func TestEchoModelHeatingPowerOff(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{101, 0, 5})))
	if len(frames) != 1 || frames[0].Data[0] != 101 || frames[0].Data[2] != 0 {
		t.Fatalf("got %v, want single 101 value 0 frame", frames)
	}
}

func TestEchoModelTempAdjustsFromObservedBaseline(t *testing.T) {
	model := newEchoModel()
	model.observe(cmd(16, 5, []int{105, 0, 0, 22, 12, 74, 4, 0})) // 8.0 C
	up := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{107, 0, 0})))
	temps := tempOf(t, up)
	if len(temps) != 1 || temps[0] != 8.5 {
		t.Fatalf("got temps %v, want [8.5]", temps)
	}
	down := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{108, 0, 0})))
	temps = tempOf(t, down)
	if len(temps) != 1 || temps[0] != 8.0 {
		t.Fatalf("got temps %v, want [8.0]", temps)
	}
}

func TestEchoModelTempSeedsWhenUnknown(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{108, 0, 0})))
	temps := tempOf(t, frames)
	if len(temps) != 1 || temps[0] != 19.5 {
		t.Fatalf("got temps %v, want [19.5] (seeded 20.0 then -0.5)", temps)
	}
}

func TestEchoModelValveAndLights(t *testing.T) {
	model := newEchoModel()
	frames := parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{4, 0, 1})))
	if len(frames) != 1 || frames[0].Data[0] != 4 || frames[0].Data[2] != 1 {
		t.Fatalf("valve press: got %v, want 4 value 1", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{4, 0, 0})))
	if len(frames) != 1 || frames[0].Data[0] != 4 || frames[0].Data[2] != 0 {
		t.Fatalf("valve release: got %v, want 4 value 0", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 1, []int{5, 0, 1})))
	if len(frames) != 1 || frames[0].Data[0] != 5 || frames[0].Data[2] != 1 {
		t.Fatalf("valve close press: got %v, want 5 value 1", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{47, 0, 3})))
	if len(frames) != 1 || frames[0].Data[0] != 47 || frames[0].Data[2] != 1 {
		t.Fatalf("lights on: got %v, want 47 value 1", frames)
	}
	frames = parseEchoFrames(t, model.onCommand(cmd(17, 0, []int{48, 0, 3})))
	if len(frames) != 1 || frames[0].Data[0] != 48 || frames[0].Data[2] != 1 {
		t.Fatalf("lights off: got %v, want 48 value 1", frames)
	}
}

func TestEchoModelIgnoresBootstrapHeartbeatAndUnknown(t *testing.T) {
	model := newEchoModel()
	if got := model.onCommand(cmd(96, 0, []int{0, 0})); len(got) != 0 {
		t.Fatalf("bootstrap produced frames: %v", got)
	}
	if got := model.onCommand(cmd(128, 0, []int{0})); len(got) != 0 {
		t.Fatalf("heartbeat produced frames: %v", got)
	}
	if got := model.onCommand(cmd(17, 0, []int{200, 0, 3})); len(got) != 0 {
		t.Fatalf("unknown signal produced frames: %v", got)
	}
}
