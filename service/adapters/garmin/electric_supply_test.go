package garmin

import (
	"reflect"
	"testing"
	"time"

	rootheating "empirebus-tests/heating"
	"github.com/gorilla/websocket"
)

func sendElectricFrames(t *testing.T, session *rootheating.Session, conn *websocket.Conn, frames ...rootheating.WireFrame) {
	t.Helper()
	for _, frame := range frames {
		if err := conn.WriteJSON(frame); err != nil {
			t.Fatal(err)
		}
	}
	last := frames[len(frames)-1]
	signal := last.Data[0] | last.Data[1]<<8
	waitForCondition(t, "electric supply frames", func() bool {
		got, _, ok := session.LatestReceivedSignal(signal)
		return ok && reflect.DeepEqual(got, last)
	})
}

func electricToggle(signal, value int) rootheating.WireFrame {
	cmd := 1
	if signal == 28 || signal == 101 {
		cmd = 0
	}
	return rootheating.WireFrame{MessageType: 16, MessageCmd: cmd, Size: 3, Data: []int{signal, 0, value}}
}
func electricScalar(signal, flags, kind, raw int) rootheating.WireFrame {
	return rootheating.WireFrame{MessageType: 16, MessageCmd: 5, Size: 8, Data: []int{signal, 0, flags, kind, raw & 255, (raw >> 8) & 255, (raw >> 16) & 255, (raw >> 24) & 255}}
}
func seedElectricSupply(t *testing.T, session *rootheating.Session, conn *websocket.Conn) {
	t.Helper()
	sendElectricFrames(t, session, conn, electricToggle(101, 1), electricToggle(89, 0), electricToggle(88, 0), electricToggle(87, 1), electricToggle(97, 0), electricToggle(28, 1), electricScalar(204, 0, 6, 3100), electricScalar(205, 0, 27, 233000))
}

func TestElectricSupplyConnectedAndUnplugged(t *testing.T) {
	a, s, conn := newGreyWaterDischargeTestAdapter(t)
	seedElectricSupply(t, s, conn)
	got := a.ElectricSupplyState(time.Now())
	if !got.Known || !got.HeaterOn || !got.ShoreConnected || got.ElectricWatts != 2000 || got.ShoreWatts != 722.3000000000001 && got.ShoreWatts != 722.3 {
		t.Fatalf("connected: %+v", got)
	}
	sendElectricFrames(t, s, conn, electricToggle(101, 129), electricToggle(87, 0), electricToggle(97, 1))
	got = a.ElectricSupplyState(time.Now())
	if !got.Known || !got.HeaterOn || got.ElectricWatts != 3000 {
		t.Fatalf("transition selected 3kW: %+v", got)
	}
	sendElectricFrames(t, s, conn, electricToggle(28, 128), electricScalar(205, 128, 27, 0))
	got = a.ElectricSupplyState(time.Now())
	if !got.Known || got.ShoreConnected || got.ShoreWatts != 0 {
		t.Fatalf("recorded unplug: %+v", got)
	}
	if got := a.ElectricSupplyState(time.Now().Add(16 * time.Second)); got.Known {
		t.Fatalf("silent session: %+v", got)
	}
	_ = conn.Close()
	waitForSessionErr(t, s)
	if got := a.ElectricSupplyState(time.Now()); got.Known {
		t.Fatalf("disconnected session: %+v", got)
	}
}

func TestElectricSupplyRejectsUnknownAndMalformedFrames(t *testing.T) {
	cases := map[string]rootheating.WireFrame{
		"unavailable positive voltage":    electricScalar(205, 128, 27, 233000),
		"unavailable limit":               electricScalar(204, 128, 6, 3100),
		"wrong voltage type":              electricScalar(205, 0, 6, 233000),
		"negative current limit":          electricScalar(204, 0, 6, -1000),
		"negative voltage":                electricScalar(205, 0, 27, -1000),
		"unavailable shore alone":         electricToggle(28, 128),
		"heater fault":                    electricToggle(101, 3),
		"conflicting electric selections": electricToggle(97, 1),
		"wrong status family":             {MessageType: 17, MessageCmd: 1, Size: 3, Data: []int{87, 0, 1}},
		"short scalar":                    {MessageType: 16, MessageCmd: 5, Size: 4, Data: []int{205, 0, 0, 27}},
		"invalid byte":                    {MessageType: 16, MessageCmd: 5, Size: 8, Data: []int{205, 0, 0, 27, 999, 0, 0, 0}},
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			a, s, conn := newGreyWaterDischargeTestAdapter(t)
			seedElectricSupply(t, s, conn)
			sendElectricFrames(t, s, conn, frame)
			if got := a.ElectricSupplyState(time.Now()); got.Known {
				t.Fatalf("accepted invalid state: %+v", got)
			}
		})
	}
}

func TestElectricSupplyOffOneKWAndReplacementSession(t *testing.T) {
	a, s, conn := newGreyWaterDischargeTestAdapter(t)
	seedElectricSupply(t, s, conn)
	sendElectricFrames(t, s, conn, electricToggle(87, 0), electricToggle(88, 1))
	if got := a.ElectricSupplyState(time.Now()); !got.Known || got.ElectricWatts != 1000 {
		t.Fatalf("catalogue 1kW: %+v", got)
	}
	sendElectricFrames(t, s, conn, electricToggle(88, 0), electricToggle(89, 1))
	if got := a.ElectricSupplyState(time.Now()); !got.Known || got.ElectricWatts != 0 {
		t.Fatalf("electric off: %+v", got)
	}
	sendElectricFrames(t, s, conn, electricToggle(101, 0))
	if got := a.ElectricSupplyState(time.Now()); !got.Known || got.HeaterOn {
		t.Fatalf("heater off: %+v", got)
	}
	before := a.ElectricSupplyState(time.Now())
	_, replacement, _ := newGreyWaterDischargeTestAdapter(t)
	a.installSession(replacement)
	if got := a.ElectricSupplyState(time.Now()); got.Known || got.Session == before.Session {
		t.Fatalf("reused session state: %+v", got)
	}
}

func TestElectricSupplyDoesNotInferSelectionFrom113To115(t *testing.T) {
	a, s, conn := newGreyWaterDischargeTestAdapter(t)
	sendElectricFrames(t, s, conn, electricToggle(101, 1), electricToggle(113, 1), electricToggle(114, 1), electricToggle(115, 1), electricToggle(28, 1), electricScalar(204, 0, 6, 3100), electricScalar(205, 0, 27, 233000))
	if got := a.ElectricSupplyState(time.Now()); got.Known {
		t.Fatalf("inferred selection from text controls: %+v", got)
	}
}
