package events

import (
	"sync"
	"time"
)

type Event struct {
	Type          string      `json:"type"`
	Timestamp     time.Time   `json:"timestamp"`
	CorrelationID string      `json:"correlation_id,omitempty"`
	Payload       interface{} `json:"payload,omitempty"`
}

type Broker struct {
	mu     sync.RWMutex
	subs   map[chan Event]struct{}
	buffer int
}

func NewBroker(buffer int) *Broker {
	if buffer <= 0 {
		buffer = 16
	}
	return &Broker{
		subs:   make(map[chan Event]struct{}),
		buffer: buffer,
	}
}

func (b *Broker) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, b.buffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}
