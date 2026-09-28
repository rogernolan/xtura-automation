package events

import (
	"testing"
	"time"
)

func TestSubscribeReceivesLatestRetainedEvent(t *testing.T) {
	broker := NewBroker(4)
	first := Event{Type: "pi.state_changed", Timestamp: time.Unix(1, 0)}
	latest := Event{Type: "pi.state_changed", Timestamp: time.Unix(2, 0)}
	broker.PublishRetained(first)
	broker.PublishRetained(latest)

	stream, unsubscribe := broker.Subscribe()
	defer unsubscribe()

	select {
	case got := <-stream:
		if !got.Timestamp.Equal(latest.Timestamp) {
			t.Fatalf("replayed event timestamp = %v, want latest %v", got.Timestamp, latest.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("late subscriber did not receive retained event")
	}
	select {
	case got := <-stream:
		t.Fatalf("late subscriber received stale event: %+v", got)
	default:
	}
}
