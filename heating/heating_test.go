package heating

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDecodeTargetTemperatureSamples(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data []int
		want float64
	}{
		{"8.0", []int{105, 0, 0, 22, 12, 74, 4, 0}, 8.0},
		{"10.0", []int{105, 0, 0, 22, 230, 81, 4, 0}, 10.0},
		{"13.0", []int{105, 0, 0, 22, 158, 93, 4, 0}, 13.0},
		{"20.0", []int{105, 0, 0, 22, 0, 121, 4, 0}, 20.0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, got, ok := decodeTargetTemperature(tc.data)
			if !ok {
				t.Fatalf("decode failed for %v", tc.data)
			}
			if got != tc.want {
				t.Fatalf("got %.1f want %.1f", got, tc.want)
			}
		})
	}
}

func TestDecodeTargetTemperatureExported(t *testing.T) {
	t.Parallel()
	temp, ok := DecodeTargetTemperature([]int{105, 0, 0, 22, 12, 74, 4, 0})
	if !ok {
		t.Fatal("decode failed")
	}
	if temp != 8.0 {
		t.Fatalf("got %.1f want 8.0", temp)
	}
	if _, ok := DecodeTargetTemperature([]int{105, 0, 0, 21, 12, 74, 4, 0}); ok {
		t.Fatal("expected ok=false when data[3] is not 22")
	}
	if _, ok := DecodeTargetTemperature([]int{107, 0, 0, 22, 12, 74, 4, 0}); ok {
		t.Fatal("expected ok=false when the signal id is not 105")
	}
}

func TestDecodeTargetTemperatureUsesFullMillikelvinScalar(t *testing.T) {
	t.Parallel()
	raw, got, ok := decodeTargetTemperature([]int{105, 0, 0, 22, 12, 74, 4, 0})
	if !ok {
		t.Fatal("decode failed")
	}
	if raw != 281100 {
		t.Fatalf("got raw %d want 281100", raw)
	}
	if got != 8.0 {
		t.Fatalf("got %.1f want displayed setpoint 8.0", got)
	}
}

func TestReplayHeatingHAR(t *testing.T) {
	t.Parallel()
	frames, err := LoadHARFrames(filepath.Join("..", "Heating.har"))
	if err != nil {
		t.Fatal(err)
	}
	state := ReplayFrames(frames)
	if !state.TargetTempKnown {
		t.Fatal("expected target temperature to be known")
	}
	if state.TargetTempC != 8.0 {
		t.Fatalf("got %.1f want 8.0", state.TargetTempC)
	}
	if state.PowerState != PowerOff {
		t.Fatalf("got power %s want off", state.PowerState)
	}
}

func TestReplayHeating20CHAR(t *testing.T) {
	t.Parallel()
	frames, err := LoadHARFrames(filepath.Join("..", "Load with Heating on at 20C.har"))
	if err != nil {
		t.Fatal(err)
	}
	state := ReplayFrames(frames)
	if !state.TargetTempKnown || state.TargetTempC != 20.0 {
		t.Fatalf("got target %.1f known=%t want 20.0", state.TargetTempC, state.TargetTempKnown)
	}
	if state.PowerState != PowerOn {
		t.Fatalf("got power %s want on", state.PowerState)
	}
	if !state.Ready() {
		t.Fatalf("expected ready state, got %s", state.String())
	}
}

func TestReplayHeatingSweepHAR(t *testing.T) {
	t.Parallel()
	frames, err := LoadHARFrames(filepath.Join("..", "Heating 13C-20C.har"))
	if err != nil {
		t.Fatal(err)
	}
	state := ReplayFrames(frames)
	if !state.TargetTempKnown || state.TargetTempC != 20.0 {
		t.Fatalf("got target %.1f known=%t want 20.0", state.TargetTempC, state.TargetTempKnown)
	}
	if state.PowerState != PowerOff {
		t.Fatalf("got power %s want off", state.PowerState)
	}
}

func TestFrameStringIncludesTargetTemperatureInterpretation(t *testing.T) {
	t.Parallel()
	frame := Frame{
		At:        time.Unix(0, 0),
		Direction: DirectionReceive,
		Wire: WireFrame{
			MessageType: 16,
			MessageCmd:  0,
			Size:        8,
			Data:        []int{105, 0, 0, 22, 0, 121, 4, 0},
		},
	}

	got := frame.String()
	if !strings.Contains(got, `HeatingTargetTemp:20.0C`) {
		t.Fatalf("expected target temperature interpretation in %q", got)
	}
	if strings.Contains(got, `label="HeatingTargetTemp"`) {
		t.Fatalf("expected redundant label to be removed from %q", got)
	}
}

func TestSetTargetTempRejectsTargetOutsideSafeRange(t *testing.T) {
	t.Parallel()
	session := NewSession(SessionConfig{})
	client := NewClient(session)
	for _, target := range []float64{4.5, 25.0} {
		target := target
		t.Run("", func(t *testing.T) {
			t.Parallel()
			err := client.SetTargetTemp(context.Background(), target)
			if err == nil {
				t.Fatalf("expected validation error for %.1fC", target)
			}
			if !strings.Contains(err.Error(), "target_celsius") {
				t.Fatalf("expected target validation error, got %v", err)
			}
		})
	}
}

func TestFrameStringIncludesHeatingPowerInterpretation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data []int
		want string
	}{
		{name: "power on state", data: []int{101, 0, 1}, want: `HeatingTurnON/OFF ALDE:on`},
		{name: "power off state", data: []int{101, 0, 0}, want: `HeatingTurnON/OFF ALDE:off`},
		{name: "power on command", data: []int{101, 0, 3}, want: `HeatingTurnON/OFF ALDE:command_on`},
		{name: "power off command", data: []int{101, 0, 5}, want: `HeatingTurnON/OFF ALDE:command_off`},
		{name: "power transition", data: []int{101, 0, 129}, want: `HeatingTurnON/OFF ALDE:transition`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			frame := Frame{
				At:        time.Unix(0, 0),
				Direction: DirectionReceive,
				Wire: WireFrame{
					MessageType: 16,
					MessageCmd:  0,
					Size:        len(tc.data),
					Data:        tc.data,
				},
			}

			got := frame.String()
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected %s in %q", tc.want, got)
			}
		})
	}
}

func TestFrameStringIncludesHeatingBusyCompactInterpretation(t *testing.T) {
	t.Parallel()
	frame := Frame{
		At:        time.Unix(0, 0),
		Direction: DirectionReceive,
		Wire: WireFrame{
			MessageType: 16,
			MessageCmd:  0,
			Size:        3,
			Data:        []int{102, 0, 0},
		},
	}

	got := frame.String()
	if !strings.Contains(got, `HeatingBusy:false`) {
		t.Fatalf("expected busy interpretation in %q", got)
	}
}

func TestEnsureOffIsIdempotentWhenAlreadyOff(t *testing.T) {
	t.Parallel()
	session := NewSession(SessionConfig{TraceWindow: time.Second})
	client := NewClient(session)
	session.ingest(Frame{
		At:        time.Unix(0, 0),
		Direction: DirectionReceive,
		Wire:      WireFrame{Data: []int{SignalHeatingPower, 0, 0}},
	})
	if err := client.EnsureOff(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := session.State().PowerState; got != PowerOff {
		t.Fatalf("got power %s want off", got)
	}
}

func TestSessionSignalIsOnTracksLatestReceivedSignal(t *testing.T) {
	t.Parallel()
	session := NewSession(SessionConfig{TraceWindow: time.Second})

	if on, known, at := session.SignalIsOn(47); known || on || !at.IsZero() {
		t.Fatalf("expected unknown signal state, got on=%t known=%t at=%v", on, known, at)
	}

	receiveAt := time.Unix(1710000000, 0).UTC()
	session.ingest(Frame{
		At:        receiveAt,
		Direction: DirectionReceive,
		Wire:      WireFrame{Data: []int{47, 0, 1}},
	})

	on, known, at := session.SignalIsOn(47)
	if !known || !on {
		t.Fatalf("expected signal 47 to be known on, got on=%t known=%t", on, known)
	}
	if !at.Equal(receiveAt) {
		t.Fatalf("got signal time %v want %v", at, receiveAt)
	}

	sendAt := receiveAt.Add(time.Second)
	session.ingest(Frame{
		At:        sendAt,
		Direction: DirectionSend,
		Wire:      WireFrame{Data: []int{47, 0, 0}},
	})

	on, known, at = session.SignalIsOn(47)
	if !known || !on {
		t.Fatalf("expected send frame to leave received signal state unchanged, got on=%t known=%t", on, known)
	}
	if !at.Equal(receiveAt) {
		t.Fatalf("got signal time %v want %v after send frame", at, receiveAt)
	}

	offAt := receiveAt.Add(2 * time.Second)
	session.ingest(Frame{
		At:        offAt,
		Direction: DirectionReceive,
		Wire:      WireFrame{Data: []int{47, 0, 0}},
	})

	on, known, at = session.SignalIsOn(47)
	if !known || on {
		t.Fatalf("expected signal 47 to be known off, got on=%t known=%t", on, known)
	}
	if !at.Equal(offAt) {
		t.Fatalf("got signal time %v want %v after off frame", at, offAt)
	}
}

func TestSessionSignalIsOnUsesTheStatusOnBit(t *testing.T) {
	t.Parallel()
	session := NewSession(SessionConfig{TraceWindow: time.Second})
	session.ingest(Frame{
		At:        time.Unix(1710000000, 0).UTC(),
		Direction: DirectionReceive,
		Wire:      WireFrame{Data: []int{47, 0, 3}},
	})

	on, known, _ := session.SignalIsOn(47)
	if !known || !on {
		t.Fatalf("expected bit-0 on status to be on, got on=%t known=%t", on, known)
	}
}

func TestFrameSignalIDUsesLittleEndianUint16(t *testing.T) {
	t.Parallel()
	frame := Frame{Wire: WireFrame{Data: []int{38, 1, 0}}}
	if got := frame.SignalID(); got != 294 {
		t.Fatalf("got signal id %d want 294", got)
	}
}

func TestSessionAcknowledgesReceivedWDUHeartbeat(t *testing.T) {
	t.Parallel()
	acknowledgements := make(chan WireFrame, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":48,"messagecmd":5,"size":0,"data":[]}`)); err != nil {
			t.Error(err)
			return
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Error(err)
			return
		}
		frame, err := ParseWireFrame(string(payload))
		if err != nil {
			t.Error(err)
			return
		}
		acknowledgements <- frame
	}))
	t.Cleanup(server.Close)

	session := NewSession(SessionConfig{
		WSURL:             "ws" + strings.TrimPrefix(server.URL, "http"),
		HeartbeatInterval: time.Hour,
		BootstrapMessages: []string{`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	select {
	case frame := <-acknowledgements:
		if frame.MessageType != 128 || frame.MessageCmd != 0 || frame.Size != 1 || len(frame.Data) != 1 || frame.Data[0] != 0 {
			t.Fatalf("got acknowledgement %+v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for WDU heartbeat acknowledgement")
	}
}

func TestSessionReportsSuccessfulSentAndReceivedRawFrames(t *testing.T) {
	t.Parallel()
	command := `{"messagetype":17,"messagecmd":0,"size":3,"data":[101,0,3]}`
	response := `{"messagetype":16,"messagecmd":0,"size":3,"data":[101,0,1]}`
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Error(err)
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(response)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	got := make(chan string, 3)
	session := NewSession(SessionConfig{
		WSURL:             "ws" + strings.TrimPrefix(server.URL, "http"),
		HeartbeatInterval: time.Hour,
		BootstrapMessages: []string{`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`},
		RecordFrame: func(_ time.Time, direction Direction, raw string) {
			got <- string(direction) + ":" + raw
		},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if err := session.sendRaw(command); err != nil {
		t.Fatal(err)
	}

	wantSent := "send:" + command
	wantReceived := "receive:" + response
	sawSent, sawReceived := false, false
	timeout := time.After(time.Second)
	for !sawSent || !sawReceived {
		select {
		case frame := <-got:
			sawSent = sawSent || frame == wantSent
			sawReceived = sawReceived || frame == wantReceived
		case <-timeout:
			t.Fatalf("recorded frames sent=%t received=%t", sawSent, sawReceived)
		}
	}
}

func TestSendSimpleCommandWritesSimpleActionFrame(t *testing.T) {
	t.Parallel()
	received := make(chan WireFrame, 8)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			wire, err := ParseWireFrame(string(payload))
			if err != nil {
				continue
			}
			select {
			case received <- wire:
			default:
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := NewSession(SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	client := NewClient(session)
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sentAt, err := client.SendSimpleCommandAt(ctx, 47, 3)
	if err != nil {
		t.Fatal(err)
	}
	if sentAt.IsZero() {
		t.Fatal("expected simple command to return send timestamp")
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case wire := <-received:
			if len(wire.Data) >= 3 && wire.Data[0] == 47 && wire.Data[2] == 3 {
				if wire.MessageType != 17 || wire.MessageCmd != 0 || wire.Size != 3 {
					t.Fatalf("got frame type=%d cmd=%d size=%d want type=17 cmd=0 size=3", wire.MessageType, wire.MessageCmd, wire.Size)
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for simple command frame")
		}
	}
}

func TestWaitForSignalIsOnWaitsForReceivedUpdate(t *testing.T) {
	t.Parallel()
	session := NewSession(SessionConfig{TraceWindow: time.Second})

	done := make(chan struct{})
	var gotAt time.Time
	var gotErr error
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		gotAt, gotErr = session.WaitForSignalIsOn(ctx, 47, true)
	}()

	time.Sleep(20 * time.Millisecond)
	wantAt := time.Unix(1710000000, 0).UTC()
	session.ingest(Frame{
		At:        wantAt,
		Direction: DirectionReceive,
		Wire:      WireFrame{Data: []int{47, 0, 1}},
	})

	<-done
	if gotErr != nil {
		t.Fatalf("wait returned error: %v", gotErr)
	}
	if !gotAt.Equal(wantAt) {
		t.Fatalf("got time %v want %v", gotAt, wantAt)
	}
}

func TestEnsureOffSendsPowerOffAndWaitsForConfirmation(t *testing.T) {
	t.Parallel()
	received := make(chan WireFrame, 8)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			wire, err := ParseWireFrame(string(payload))
			if err != nil {
				continue
			}
			select {
			case received <- wire:
			default:
			}
			if len(wire.Data) >= 3 && wire.Data[0] == SignalHeatingPower && wire.Data[2] == 5 {
				response, err := marshalWireFrame(WireFrame{
					MessageType: 16,
					MessageCmd:  0,
					Size:        3,
					Data:        []int{SignalHeatingPower, 0, 0},
				})
				if err != nil {
					t.Error(err)
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, []byte(response)); err != nil {
					t.Error(err)
				}
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := NewSession(SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	client := NewClient(session)
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})
	session.ingest(Frame{
		At:        time.Unix(0, 0),
		Direction: DirectionReceive,
		Wire:      WireFrame{Data: []int{SignalHeatingPower, 0, 1}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.EnsureOff(ctx); err != nil {
		t.Fatal(err)
	}
	if got := session.State().PowerState; got != PowerOff {
		t.Fatalf("got power %s want off", got)
	}

	offCount := 0
drain:
	for {
		select {
		case wire := <-received:
			if len(wire.Data) >= 3 && wire.Data[0] == SignalHeatingPower && wire.Data[2] == 5 {
				offCount++
			}
		default:
			break drain
		}
	}
	if offCount != 1 {
		t.Fatalf("got %d off commands want 1", offCount)
	}

	if err := client.EnsureOff(ctx); err != nil {
		t.Fatal(err)
	}
	offCount = 0
drainAgain:
	for {
		select {
		case wire := <-received:
			if len(wire.Data) >= 3 && wire.Data[0] == SignalHeatingPower && wire.Data[2] == 5 {
				offCount++
			}
		default:
			break drainAgain
		}
	}
	if offCount != 0 {
		t.Fatalf("got %d extra off commands after idempotent call", offCount)
	}
}
