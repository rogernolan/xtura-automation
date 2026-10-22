package heating

import (
	"testing"
	"time"
)

func TestSessionLastReceivedAtIgnoresOutgoingCommands(t *testing.T) {
	s := NewSession(SessionConfig{})
	received := time.Unix(1000, 0)
	s.ingest(Frame{At: received, Direction: DirectionReceive, Wire: WireFrame{MessageType: 16, MessageCmd: 0, Size: 3, Data: []int{28, 0, 1}}})
	s.ingest(Frame{At: received.Add(time.Minute), Direction: DirectionSend, Wire: WireFrame{MessageType: 17, MessageCmd: 0, Size: 3, Data: []int{101, 0, 3}}})
	if got := s.LastReceivedAt(); !got.Equal(received) {
		t.Fatalf("outbound refreshed receive time: %s", got)
	}
}
