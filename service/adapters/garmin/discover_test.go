package garmin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const servFingerprintPage = `<html><head>
<script src="logger.js"></script>
<script id="application-settings" type="application/json">{}</script>
<script id="mfd-channel-subscription" type="application/json">{}</script>
</head><body>serv</body></html>`

func startFingerprintServer(t *testing.T, serv bool) string {
	t.Helper()
	page := "<html><body>not a serv</body></html>"
	if serv {
		page = servFingerprintPage
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func startWSServer(t *testing.T) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
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
	return server.URL
}

func wsURLFor(t *testing.T, httpURL string) string {
	t.Helper()
	_, port, err := hostPortOf(strings.Replace(httpURL, "http://", "ws://", 1) + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	return "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/ws"
}

func TestDiscoverServOnSubnetFindsFingerprintedHost(t *testing.T) {
	serverURL := startFingerprintServer(t, true)
	_, port, err := hostPortOf(strings.Replace(serverURL, "http://", "ws://", 1))
	if err != nil {
		t.Fatal(err)
	}
	found, err := discoverServOnSubnet(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if found != want {
		t.Fatalf("found %q want %q", found, want)
	}
}

func TestDiscoverServOnSubnetIgnoresUnfingerprintedHost(t *testing.T) {
	serverURL := startFingerprintServer(t, false)
	_, port, err := hostPortOf(strings.Replace(serverURL, "http://", "ws://", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discoverServOnSubnet(context.Background(), "127.0.0.1", port); err == nil {
		t.Fatal("expected no serv to be discovered")
	}
}

func TestDiscoverServOnSubnetRejectsNonIPv4Host(t *testing.T) {
	if _, err := discoverServOnSubnet(context.Background(), "serv.local", 8888); err == nil {
		t.Fatal("expected hostname to be rejected")
	}
}

func TestServWSURLPreservesSchemeAndPath(t *testing.T) {
	got, err := servWSURL("ws://172.16.11.7:8888/ws", "172.16.11.9:8888")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://172.16.11.9:8888/ws"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestHostPortOf(t *testing.T) {
	host, port, err := hostPortOf("ws://172.16.11.7:8888/ws")
	if err != nil {
		t.Fatal(err)
	}
	if host != "172.16.11.7" || port != 8888 {
		t.Fatalf("got %q %d", host, port)
	}
	if _, _, err := hostPortOf("ws://172.16.11.7/ws"); err == nil {
		t.Fatal("expected missing port to error")
	}
}

func TestAdapterConnectsToConfiguredURLWithoutDiscovery(t *testing.T) {
	serverURL := startWSServer(t)
	var calls atomic.Int32
	adapter := New(Config{
		WSURL:             wsURLFor(t, serverURL),
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		ConnectTimeout:    2 * time.Second,
		Discover: func(context.Context, string, int) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})
	t.Cleanup(adapter.closeSession)
	adapter.tryConnect(context.Background())
	if !adapter.Health().Connected {
		t.Fatalf("expected connected, health=%+v", adapter.Health())
	}
	if calls.Load() != 0 {
		t.Fatalf("expected no discovery, got %d calls", calls.Load())
	}
}

func TestAdapterFallsBackToDiscoveredAddress(t *testing.T) {
	serverURL := startWSServer(t)
	discovered := wsURLFor(t, serverURL)
	_, port, err := hostPortOf(discovered)
	if err != nil {
		t.Fatal(err)
	}
	adapter := New(Config{
		WSURL:             "ws://127.0.0.1:1/ws",
		HeartbeatInterval: time.Hour,
		TraceWindow:       time.Second,
		ConnectTimeout:    2 * time.Second,
		DiscoverTimeout:   5 * time.Second,
		Discover: func(context.Context, string, int) (string, error) {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), nil
		},
	})
	t.Cleanup(adapter.closeSession)
	adapter.tryConnect(context.Background())
	if !adapter.Health().Connected {
		t.Fatalf("expected connected, health=%+v", adapter.Health())
	}
	adapter.discoveryMu.Lock()
	got := adapter.discoveredWSURL
	adapter.discoveryMu.Unlock()
	if got != discovered {
		t.Fatalf("remembered %q want %q", got, discovered)
	}
}

func TestAdapterPrefersDiscoveredAddressOnReconnect(t *testing.T) {
	adapter := New(Config{WSURL: "ws://127.0.0.1:1/ws"})
	discovered := "ws://127.0.0.1:65500/ws"
	adapter.rememberDiscovered(discovered)
	candidates := adapter.connectCandidates()
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %v", candidates)
	}
	if candidates[0] != discovered {
		t.Fatalf("expected discovered candidate first, got %q", candidates[0])
	}
	if candidates[1] != adapter.cfg.WSURL {
		t.Fatalf("expected configured candidate second, got %q", candidates[1])
	}
}

func TestAdapterDoesNotRememberConfiguredAddress(t *testing.T) {
	adapter := New(Config{WSURL: "ws://127.0.0.1:1/ws"})
	adapter.rememberDiscovered(adapter.cfg.WSURL)
	adapter.discoveryMu.Lock()
	defer adapter.discoveryMu.Unlock()
	if adapter.discoveredWSURL != "" {
		t.Fatalf("expected no discovered address, got %q", adapter.discoveredWSURL)
	}
}

func TestDiscoverWSURLThrottlesRepeatedSweeps(t *testing.T) {
	var calls atomic.Int32
	adapter := New(Config{
		WSURL:            "ws://172.16.11.7:8888/ws",
		DiscoverInterval: time.Minute,
		Discover: func(context.Context, string, int) (string, error) {
			calls.Add(1)
			return "172.16.11.7:8888", nil
		},
	})
	if got := adapter.discoverWSURL(context.Background()); got != "ws://172.16.11.7:8888/ws" {
		t.Fatalf("first sweep got %q", got)
	}
	for i := 0; i < 2; i++ {
		if got := adapter.discoverWSURL(context.Background()); got != "" {
			t.Fatalf("throttled sweep %d got %q want empty", i, got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 sweep, got %d", calls.Load())
	}
}

func TestDiscoverWSURLReturnsEmptyWhenDiscoveryFails(t *testing.T) {
	adapter := New(Config{
		WSURL: "ws://172.16.11.7:8888/ws",
		Discover: func(context.Context, string, int) (string, error) {
			return "", context.DeadlineExceeded
		},
	})
	if got := adapter.discoverWSURL(context.Background()); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
