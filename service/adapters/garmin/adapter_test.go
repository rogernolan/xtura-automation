package garmin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rootheating "empirebus-tests/heating"
	domainlights "empirebus-tests/service/domains/lights"
	domainoverview "empirebus-tests/service/domains/overview"

	"github.com/gorilla/websocket"
)

func newGreyWaterDischargeTestAdapter(t *testing.T) (*Adapter, *rootheating.Session, *websocket.Conn) {
	t.Helper()
	conns := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		select {
		case conns <- conn:
		default:
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             "ws" + strings.TrimPrefix(server.URL, "http"),
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	var conn *websocket.Conn
	select {
	case conn = <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket connection")
	}

	adapter := &Adapter{
		session: session,
		client:  rootheating.NewClient(session),
	}
	return adapter, session, conn
}

func waitForCondition(t *testing.T, description string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForSessionSignalState(t *testing.T, session *rootheating.Session, signal int, wantOn bool) {
	t.Helper()
	waitForCondition(t, "session signal state", func() bool {
		gotOn, known, _ := session.SignalIsOn(signal)
		return known && gotOn == wantOn
	})
}

func waitForSessionErr(t *testing.T, session *rootheating.Session) error {
	t.Helper()
	var err error
	waitForCondition(t, "session error", func() bool {
		err = session.Err()
		return err != nil
	})
	return err
}

func waitForLatestReceivedSignal(t *testing.T, session *rootheating.Session, signal int) {
	t.Helper()
	waitForCondition(t, "latest received signal", func() bool {
		_, _, ok := session.LatestReceivedSignal(signal)
		return ok
	})
}

func waitForAdapterSession(t *testing.T, adapter *Adapter) *rootheating.Session {
	t.Helper()
	var session *rootheating.Session
	waitForCondition(t, "adapter session", func() bool {
		adapter.mu.RLock()
		defer adapter.mu.RUnlock()
		session = adapter.session
		return session != nil
	})
	return session
}

func sendGreyWaterSignalFrame(t *testing.T, conn *websocket.Conn, session *rootheating.Session, signal int, on bool) {
	t.Helper()
	value := 0
	if on {
		value = 1
	}
	if err := conn.WriteJSON(rootheating.WireFrame{
		MessageType: 16,
		MessageCmd:  0,
		Size:        3,
		Data:        []int{signal, 0, value},
	}); err != nil {
		t.Fatal(err)
	}
	waitForSessionSignalState(t, session, signal, on)
}

func TestDrainGreyWaterDischargeEventsEmitsOpenEventOnSignal4OnEdge(t *testing.T) {
	t.Parallel()
	adapter, session, conn := newGreyWaterDischargeTestAdapter(t)

	sendGreyWaterSignalFrame(t, conn, session, 4, false)
	adapter.pollState()
	if got := adapter.DrainGreyWaterDischargeEvents(); len(got) != 0 {
		t.Fatalf("expected no events from baseline off frame, got %v", got)
	}

	sendGreyWaterSignalFrame(t, conn, session, 4, true)
	adapter.pollState()
	got := adapter.DrainGreyWaterDischargeEvents()
	if len(got) != 1 {
		t.Fatalf("got %d events want 1: %v", len(got), got)
	}
	if got[0].Kind != KindOpen {
		t.Fatalf("got kind %q want %q", got[0].Kind, KindOpen)
	}
	if got[0].At.IsZero() {
		t.Fatal("expected open event to carry a timestamp")
	}
}

func TestDrainGreyWaterDischargeEventsEmitsCloseEventOnSignal5OnEdge(t *testing.T) {
	t.Parallel()
	adapter, session, conn := newGreyWaterDischargeTestAdapter(t)

	sendGreyWaterSignalFrame(t, conn, session, 5, false)
	adapter.pollState()
	if got := adapter.DrainGreyWaterDischargeEvents(); len(got) != 0 {
		t.Fatalf("expected no events from baseline off frame, got %v", got)
	}

	sendGreyWaterSignalFrame(t, conn, session, 5, true)
	adapter.pollState()
	got := adapter.DrainGreyWaterDischargeEvents()
	if len(got) != 1 {
		t.Fatalf("got %d events want 1: %v", len(got), got)
	}
	if got[0].Kind != KindClose {
		t.Fatalf("got kind %q want %q", got[0].Kind, KindClose)
	}
	if got[0].At.IsZero() {
		t.Fatal("expected close event to carry a timestamp")
	}
}

func TestDrainGreyWaterDischargeEventsIgnoresOffTransitionsAndDuplicates(t *testing.T) {
	t.Parallel()
	adapter, session, conn := newGreyWaterDischargeTestAdapter(t)

	sendGreyWaterSignalFrame(t, conn, session, 4, false)
	adapter.pollState()
	sendGreyWaterSignalFrame(t, conn, session, 4, true)
	adapter.pollState()
	sendGreyWaterSignalFrame(t, conn, session, 4, true)
	adapter.pollState()
	sendGreyWaterSignalFrame(t, conn, session, 4, false)
	adapter.pollState()

	got := adapter.DrainGreyWaterDischargeEvents()
	if len(got) != 1 {
		t.Fatalf("got %d events want 1: %v", len(got), got)
	}
	if got[0].Kind != KindOpen {
		t.Fatalf("got kind %q want %q", got[0].Kind, KindOpen)
	}
	if again := adapter.DrainGreyWaterDischargeEvents(); len(again) != 0 {
		t.Fatalf("expected drain to empty queue, got %v", again)
	}
}

func TestDrainGreyWaterDischargeEventsDrainsQueuedEdgesBeforeTerminalSessionError(t *testing.T) {
	t.Parallel()
	adapter, session, conn := newGreyWaterDischargeTestAdapter(t)

	sendGreyWaterSignalFrame(t, conn, session, 4, false)
	sendGreyWaterSignalFrame(t, conn, session, 5, false)
	adapter.pollState()
	if got := adapter.DrainGreyWaterDischargeEvents(); len(got) != 0 {
		t.Fatalf("expected no events from baseline frames, got %v", got)
	}

	sendGreyWaterSignalFrame(t, conn, session, 4, true)
	sendGreyWaterSignalFrame(t, conn, session, 5, true)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitForSessionErr(t, session); err == nil {
		t.Fatal("expected terminal session error after disconnect")
	}

	adapter.pollState()
	got := adapter.DrainGreyWaterDischargeEvents()
	if len(got) != 2 {
		t.Fatalf("got %d events want 2: %v", len(got), got)
	}
	if got[0].Kind != KindOpen || got[1].Kind != KindClose {
		t.Fatalf("unexpected drained events: %v", got)
	}
}

func TestAdapterLoopPreservesQueuedGreyWaterEdgesAcrossReconnect(t *testing.T) {
	t.Parallel()
	conns := make(chan *websocket.Conn, 2)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		select {
		case conns <- conn:
		default:
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	adapter := New(Config{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adapter.Start(ctx)

	var firstConn *websocket.Conn
	select {
	case firstConn = <-conns:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first websocket connection")
	}
	firstSession := waitForAdapterSession(t, adapter)

	sendGreyWaterSignalFrame(t, firstConn, firstSession, 4, false)
	sendGreyWaterSignalFrame(t, firstConn, firstSession, 5, false)
	waitForCondition(t, "baseline poll", func() bool {
		adapter.pollState()
		return true
	})
	if got := adapter.DrainGreyWaterDischargeEvents(); len(got) != 0 {
		t.Fatalf("expected no events from baseline frames, got %v", got)
	}

	sendGreyWaterSignalFrame(t, firstConn, firstSession, 4, true)
	sendGreyWaterSignalFrame(t, firstConn, firstSession, 5, true)
	if err := firstConn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitForSessionErr(t, firstSession); err == nil {
		t.Fatal("expected first session to fail after disconnect")
	}

	var secondConn *websocket.Conn
	select {
	case secondConn = <-conns:
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for reconnect")
	}
	t.Cleanup(func() { _ = secondConn.Close() })
	waitForCondition(t, "adapter reconnect", func() bool {
		adapter.mu.RLock()
		defer adapter.mu.RUnlock()
		return adapter.session != nil && adapter.session != firstSession
	})
	waitForCondition(t, "queued discharge events", func() bool {
		adapter.mu.RLock()
		defer adapter.mu.RUnlock()
		return len(adapter.greyEvents) == 2
	})

	got := adapter.DrainGreyWaterDischargeEvents()
	if len(got) != 2 {
		t.Fatalf("got %d events want 2: %v", len(got), got)
	}
	if got[0].Kind != KindOpen || got[1].Kind != KindClose {
		t.Fatalf("unexpected drained events after reconnect: %v", got)
	}
}

func TestEnsureExteriorOnWaitsForReceivedConfirmation(t *testing.T) {
	t.Parallel()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	adapter := &Adapter{
		session: session,
		client:  rootheating.NewClient(session),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := adapter.EnsureExteriorOn(ctx)
	if err == nil {
		t.Fatal("expected missing received confirmation to fail")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("got err %v want %v", err, context.DeadlineExceeded)
	}
}

func TestLightsSnapshotFromSessionTracksLatestExteriorSignal(t *testing.T) {
	t.Parallel()
	conns := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		select {
		case conns <- conn:
		default:
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	var conn *websocket.Conn
	select {
	case conn = <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket connection")
	}

	if state := lightsSnapshotFromSession(session, domainlights.State{}); state.ExternalKnown {
		t.Fatalf("expected lights state to start unknown")
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":16,"messagecmd":0,"size":3,"data":[47,0,1]}`)); err != nil {
		t.Fatal(err)
	}
	waitForSessionSignalState(t, session, 47, true)
	onState := lightsSnapshotFromSession(session, domainlights.State{})
	if !onState.ExternalKnown || !onState.ExternalOn {
		t.Fatalf("expected exterior on state, got known=%t on=%t", onState.ExternalKnown, onState.ExternalOn)
	}
	if onState.LastUpdatedAt == nil {
		t.Fatal("expected on state to record update time")
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}`)); err != nil {
		t.Fatal(err)
	}
	waitForSessionSignalState(t, session, 48, true)
	offState := lightsSnapshotFromSession(session, onState)
	if !offState.ExternalKnown || offState.ExternalOn {
		t.Fatalf("expected exterior off state, got known=%t on=%t", offState.ExternalKnown, offState.ExternalOn)
	}
	if offState.LastUpdatedAt == nil {
		t.Fatal("expected off state to record update time")
	}
	if !offState.LastUpdatedAt.After(*onState.LastUpdatedAt) && !offState.LastUpdatedAt.Equal(*onState.LastUpdatedAt) {
		t.Fatalf("expected off update time %v to be at or after on update time %v", offState.LastUpdatedAt, onState.LastUpdatedAt)
	}
}

func TestEnsureExteriorOffSucceedsOnReceivedOffConfirmation(t *testing.T) {
	t.Parallel()
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
			wire, err := rootheating.ParseWireFrame(string(payload))
			if err != nil {
				continue
			}
			if len(wire.Data) >= 3 && wire.Data[0] == 48 && wire.Data[2] == 3 {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}`)); err != nil {
					t.Error(err)
				}
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	adapter := &Adapter{
		session: session,
		client:  rootheating.NewClient(session),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := adapter.EnsureExteriorOff(ctx); err != nil {
		t.Fatalf("expected exterior off confirmation to succeed, got %v", err)
	}

	state := adapter.LightsState()
	if !state.ExternalKnown || state.ExternalOn {
		t.Fatalf("expected exterior state known off, got known=%t on=%t", state.ExternalKnown, state.ExternalOn)
	}
	if state.LastUpdatedAt == nil {
		t.Fatal("expected exterior off to record update time")
	}
}

func TestEnsureExteriorOnSucceedsOnReceivedOnConfirmation(t *testing.T) {
	t.Parallel()
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
			wire, err := rootheating.ParseWireFrame(string(payload))
			if err != nil {
				continue
			}
			if len(wire.Data) >= 3 && wire.Data[0] == 47 && wire.Data[2] == 3 {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":16,"messagecmd":0,"size":3,"data":[47,0,1]}`)); err != nil {
					t.Error(err)
				}
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	adapter := &Adapter{
		session: session,
		client:  rootheating.NewClient(session),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := adapter.EnsureExteriorOn(ctx); err != nil {
		t.Fatalf("expected exterior on confirmation to succeed, got %v", err)
	}

	state := adapter.LightsState()
	if !state.ExternalKnown || !state.ExternalOn {
		t.Fatalf("expected exterior state known on, got known=%t on=%t", state.ExternalKnown, state.ExternalOn)
	}
	if state.LastUpdatedAt == nil {
		t.Fatal("expected exterior on to record update time")
	}
}

func TestEnsureExteriorOffIgnoresStalePreCommandConfirmation(t *testing.T) {
	t.Parallel()
	conns := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		select {
		case conns <- conn:
		default:
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             wsURL,
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{
			`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`,
		},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	var conn *websocket.Conn
	select {
	case conn = <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket connection")
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}`)); err != nil {
		t.Fatal(err)
	}
	waitForSessionSignalState(t, session, 48, true)

	adapter := &Adapter{
		session: session,
		client:  rootheating.NewClient(session),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := adapter.EnsureExteriorOff(ctx)
	if err == nil {
		t.Fatal("expected stale pre-command confirmation to be ignored")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("got err %v want %v", err, context.DeadlineExceeded)
	}
}

func TestOverviewTelemetryDecodesScalarStatusFrames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		signal      int
		messageType int
		messageCmd  int
		valueType   int
		raw         int32
		shortFrame  bool
		want        *float64
		field       func(domainoverview.Telemetry) *float64
	}{
		{
			name:        "Alde temperature",
			signal:      106,
			messageType: 16,
			messageCmd:  5,
			valueType:   22,
			raw:         293150,
			want:        float64Pointer(20),
			field:       func(value domainoverview.Telemetry) *float64 { return value.AldeTemperatureC },
		},
		{
			name:        "fresh water percentage",
			signal:      12,
			messageType: 16,
			messageCmd:  5,
			valueType:   14,
			raw:         76500,
			want:        float64Pointer(76.5),
			field:       func(value domainoverview.Telemetry) *float64 { return value.FreshWaterPercent },
		},
		{
			name:        "grey water percentage",
			signal:      13,
			messageType: 16,
			messageCmd:  5,
			valueType:   14,
			raw:         12345,
			want:        float64Pointer(12.345),
			field:       func(value domainoverview.Telemetry) *float64 { return value.GreyWaterPercent },
		},
		{
			name:        "battery current",
			signal:      212,
			messageType: 16,
			messageCmd:  5,
			valueType:   6,
			raw:         -1250,
			want:        float64Pointer(-1.25),
			field:       func(value domainoverview.Telemetry) *float64 { return value.BatteryCurrentA },
		},
		{
			name:        "battery state of charge percentage",
			signal:      213,
			messageType: 16,
			messageCmd:  5,
			valueType:   14,
			raw:         80000,
			want:        float64Pointer(80),
			field:       func(value domainoverview.Telemetry) *float64 { return value.BatteryStateOfChargePercent },
		},
		{
			name:        "invalid value type is rejected",
			signal:      106,
			messageType: 16,
			messageCmd:  5,
			valueType:   14,
			raw:         293150,
			field:       func(value domainoverview.Telemetry) *float64 { return value.AldeTemperatureC },
		},
		{
			name:        "short scalar frame is rejected",
			signal:      12,
			messageType: 16,
			messageCmd:  5,
			valueType:   14,
			raw:         76500,
			shortFrame:  true,
			field:       func(value domainoverview.Telemetry) *float64 { return value.FreshWaterPercent },
		},
		{
			name:        "non-scalar status frame is rejected",
			signal:      13,
			messageType: 16,
			messageCmd:  0,
			valueType:   14,
			raw:         12345,
			field:       func(value domainoverview.Telemetry) *float64 { return value.GreyWaterPercent },
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session, conn := newOverviewTestSession(t)
			data := scalarStatusData(tt.signal, tt.valueType, tt.raw)
			if tt.shortFrame {
				data = data[:7]
			}
			if err := conn.WriteJSON(rootheating.WireFrame{MessageType: tt.messageType, MessageCmd: tt.messageCmd, Size: len(data), Data: data}); err != nil {
				t.Fatal(err)
			}
			waitForLatestReceivedSignal(t, session, tt.signal)

			adapter := &Adapter{session: session}
			adapter.pollState()
			telemetry := adapter.OverviewTelemetry()
			got := tt.field(telemetry)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("decoded value = %v, want nil", got)
				}
				if telemetry.UpdatedAt != nil {
					t.Fatalf("UpdatedAt = %v, want nil", telemetry.UpdatedAt)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("decoded value = %v, want %v", got, *tt.want)
			}
			if telemetry.UpdatedAt == nil {
				t.Fatal("expected valid scalar status to record update time")
			}
		})
	}
}

func newOverviewTestSession(t *testing.T) (*rootheating.Session, *websocket.Conn) {
	t.Helper()
	conns := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		select {
		case conns <- conn:
		default:
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	session := rootheating.NewSession(rootheating.SessionConfig{
		WSURL:             "ws" + strings.TrimPrefix(server.URL, "http"),
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		BootstrapMessages: []string{`{"messagetype":96,"messagecmd":0,"size":0,"data":[]}`},
	})
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	select {
	case conn := <-conns:
		return session, conn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket connection")
		return nil, nil
	}
}

func scalarStatusData(signal, valueType int, raw int32) []int {
	return []int{
		signal & 0xff,
		signal >> 8,
		0,
		valueType,
		int(byte(raw)),
		int(byte(raw >> 8)),
		int(byte(raw >> 16)),
		int(byte(raw >> 24)),
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}
