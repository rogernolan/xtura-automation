package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"empirebus-tests/heating"

	"github.com/gorilla/websocket"
)

type Server struct {
	addr    string
	capture []replayItem
	loop    bool
	speed   float64
	verbose bool
	logger  *log.Logger
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleConn)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "servsim: fake Garmin SERV")
	})
	return mux
}

func (s *Server) serve(ctx context.Context) error {
	httpServer := &http.Server{Addr: s.addr, Handler: s.handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	return httpServer.ListenAndServe()
}

func (s *Server) handleConn(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Printf("servsim upgrade: %v", err)
		return
	}
	defer conn.Close()
	model := newEchoModel()
	writeMu := &sync.Mutex{}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.replayLoop(ctx, conn, writeMu, model)
	s.readLoop(ctx, conn, writeMu, model)
}

// writeFrame serializes all writes to a connection. Gorilla WebSocket allows
// only one concurrent writer per connection, and the replay and read loops
// write from different goroutines (capture frames vs command echoes).
func (s *Server) writeFrame(conn *websocket.Conn, writeMu *sync.Mutex, message string) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, []byte(message))
}

func (s *Server) replayLoop(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, model *echoModel) {
	for {
		for _, item := range s.capture {
			delay := time.Duration(float64(item.delay) / s.speed)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if err := s.writeFrame(conn, writeMu, item.message); err != nil {
				return
			}
			if wire, err := heating.ParseWireFrame(item.message); err == nil {
				model.observe(wire)
			}
			if s.verbose {
				s.logger.Printf("servsim replay: %s", item.message)
			}
		}
		if !s.loop {
			return
		}
	}
}

func (s *Server) readLoop(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, model *echoModel) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		raw := string(payload)
		if s.verbose {
			s.logger.Printf("servsim recv: %s", raw)
		}
		wire, err := heating.ParseWireFrame(raw)
		if err != nil {
			continue
		}
		for _, out := range model.onCommand(wire) {
			if err := s.writeFrame(conn, writeMu, out); err != nil {
				return
			}
			if s.verbose {
				s.logger.Printf("servsim echo: %s", out)
			}
		}
	}
}
