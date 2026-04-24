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

	"github.com/gorilla/websocket"
)

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
	time.Sleep(20 * time.Millisecond)
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
	time.Sleep(20 * time.Millisecond)
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
	time.Sleep(20 * time.Millisecond)

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
