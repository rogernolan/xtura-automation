package heating

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	DefaultWSURL            = "ws://192.168.1.1:8888/ws"
	DefaultHeartbeatMessage = "{\"messagetype\":128,\"messagecmd\":0,\"size\":1,\"data\":[0]}"
)

var DefaultBootstrapMessages = []string{
	"{\"messagetype\":96,\"messagecmd\":0,\"size\":306,\"data\":[152,0,1,0,4,0,5,0,6,0,7,0,9,0,10,0,11,0,20,0,21,0,22,0,23,0,27,0,30,0,31,0,32,0,33,0,34,0,35,0,36,0,37,0,38,0,39,0,40,0,41,0,51,0,52,0,53,0,54,0,55,0,56,0,57,0,58,0,59,0,60,0,68,0,69,0,70,0,71,0,72,0,73,0,78,0,79,0,83,0,76,0,49,0,50,0,225,0,226,0,227,0,228,0,229,0,230,0,231,0,232,0,233,0,45,0,46,0,47,0,48,0,77,0,84,0,85,0,177,0,178,0,172,0,179,0,2,0,38,1,3,0,237,0,238,0,239,0,12,0,13,0,61,0,62,0,63,0,14,0,66,0,25,0,24,0,74,0,75,0,101,0,102,0,103,0,105,0,106,0,107,0,108,0,110,0,113,0,114,0,115,0,119,0,97,0,87,0,88,0,89,0,90,0,91,0,92,0,96,0,99,0,98,0,248,0,153,0,15,0,16,0,17,0,18,0,19,0,111,0,93,0,95,0,240,0,26,0,200,0,201,0,202,0,203,0,204,0,205,0,206,0,208,0,209,0,211,0,212,0,213,0,214,0,215,0,216,0,217,0,218,0,199,0,221,0,222,0,223,0,181,0,182,0,185,0,183,0,197,0,196,0,28,0,220,0,195,0,180,0,189,0,191,0,190,0]}",
	"{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}",
}

type SessionConfig struct {
	WSURL             string
	Origin            string
	Headers           http.Header
	HeartbeatInterval time.Duration
	HeartbeatMessage  string
	BootstrapMessages []string
	Verbose           bool
	TraceWindow       time.Duration
	Logger            *log.Logger
}

type Session struct {
	cfg       SessionConfig
	conn      *websocket.Conn
	mu        sync.Mutex
	cond      *sync.Cond
	state     HeaterState
	signals   map[int]int
	signalAt  map[int]time.Time
	closed    bool
	readErr   error
	traceTill time.Time
}

func NewSession(cfg SessionConfig) *Session {
	if cfg.WSURL == "" {
		cfg.WSURL = DefaultWSURL
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 4 * time.Second
	}
	if cfg.HeartbeatMessage == "" {
		cfg.HeartbeatMessage = DefaultHeartbeatMessage
	}
	if len(cfg.BootstrapMessages) == 0 {
		cfg.BootstrapMessages = append([]string(nil), DefaultBootstrapMessages...)
	}
	if cfg.TraceWindow <= 0 {
		cfg.TraceWindow = 3 * time.Second
	}
	s := &Session{cfg: cfg}
	s.signals = make(map[int]int)
	s.signalAt = make(map[int]time.Time)
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *Session) Connect(ctx context.Context) error {
	dialer := websocket.Dialer{}
	headers := make(http.Header)
	for key, values := range s.cfg.Headers {
		copied := append([]string(nil), values...)
		headers[key] = copied
	}
	if s.cfg.Origin != "" {
		headers.Set("Origin", s.cfg.Origin)
	}
	conn, _, err := dialer.DialContext(ctx, s.cfg.WSURL, headers)
	if err != nil {
		return err
	}
	s.conn = conn
	for _, raw := range s.cfg.BootstrapMessages {
		if err := s.sendRaw(raw); err != nil {
			_ = conn.Close()
			return err
		}
	}
	go s.readLoop()
	go s.heartbeatLoop()
	return nil
}

func (s *Session) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cond.Broadcast()
	s.mu.Unlock()
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *Session) State() HeaterState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Clone()
}

func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readErr
}

func (s *Session) SignalIsOn(signal int) (bool, bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.signals[signal]
	if !ok {
		return false, false, time.Time{}
	}
	return value == 1, true, s.signalAt[signal]
}

func (s *Session) WithTraceWindow(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(s.traceTill) {
		s.traceTill = until
	}
}

func (s *Session) waitFor(ctx context.Context, predicate func(HeaterState) bool) (HeaterState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if predicate(s.state) {
			return s.state.Clone(), nil
		}
		if s.readErr != nil {
			return HeaterState{}, s.readErr
		}
		if s.closed {
			return HeaterState{}, context.Canceled
		}
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.cond.Broadcast()
				s.mu.Unlock()
			case <-done:
			}
		}()
		s.cond.Wait()
		close(done)
		if err := ctx.Err(); err != nil {
			return HeaterState{}, err
		}
	}
}

func (s *Session) WaitForSignalIsOn(ctx context.Context, signal int, wantOn bool) (time.Time, error) {
	return s.WaitForSignalIsOnAfter(ctx, signal, wantOn, time.Time{})
}

func (s *Session) WaitForSignalIsOnAfter(ctx context.Context, signal int, wantOn bool, after time.Time) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if value, ok := s.signals[signal]; ok && (value == 1) == wantOn {
			at := s.signalAt[signal]
			if after.IsZero() || at.After(after) {
				return at, nil
			}
		}
		if s.readErr != nil {
			return time.Time{}, s.readErr
		}
		if s.closed {
			return time.Time{}, context.Canceled
		}
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.cond.Broadcast()
				s.mu.Unlock()
			case <-done:
			}
		}()
		s.cond.Wait()
		close(done)
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
	}
}

func (s *Session) sendCommand(frame WireFrame) error {
	_, err := s.sendCommandAt(frame)
	return err
}

func (s *Session) sendCommandAt(frame WireFrame) (time.Time, error) {
	raw, err := marshalWireFrame(frame)
	if err != nil {
		return time.Time{}, err
	}
	return s.sendRawAt(raw)
}

func (s *Session) sendRaw(raw string) error {
	_, err := s.sendRawAt(raw)
	return err
}

func (s *Session) sendRawAt(raw string) (time.Time, error) {
	if s.conn == nil {
		return time.Time{}, fmt.Errorf("session not connected")
	}
	sentAt := time.Now()
	if err := s.conn.WriteMessage(websocket.TextMessage, []byte(raw)); err != nil {
		return time.Time{}, err
	}
	wire, err := ParseWireFrame(raw)
	if err == nil {
		s.ingest(Frame{At: sentAt, Direction: DirectionSend, Wire: wire})
	}
	return sentAt, nil
}

func (s *Session) heartbeatLoop() {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		_ = s.sendRaw(s.cfg.HeartbeatMessage)
	}
}

func (s *Session) readLoop() {
	for {
		_, payload, err := s.conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			s.readErr = err
			s.cond.Broadcast()
			s.mu.Unlock()
			return
		}
		wire, err := ParseWireFrame(string(payload))
		if err != nil {
			continue
		}
		s.ingest(Frame{At: time.Now(), Direction: DirectionReceive, Wire: wire})
	}
}

func (s *Session) ingest(frame Frame) {
	s.mu.Lock()
	changed := updateState(&s.state, frame)
	if frame.Direction == DirectionReceive && len(frame.Wire.Data) >= 3 {
		s.signals[frame.SignalID()] = frame.Wire.Data[2]
		s.signalAt[frame.SignalID()] = frame.At
	}
	trace := s.cfg.Verbose && frame.RelevantToHeating() && (frame.Direction == DirectionSend || time.Now().Before(s.traceTill))
	s.cond.Broadcast()
	s.mu.Unlock()
	if changed || trace {
		s.logFrame(frame)
	}
}

func (s *Session) logFrame(frame Frame) {
	if s.cfg.Logger == nil {
		return
	}
	s.cfg.Logger.Print(frame.String())
}

func marshalWireFrame(frame WireFrame) (string, error) {
	payload, err := jsonMarshal(frame)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
