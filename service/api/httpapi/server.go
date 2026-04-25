package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"empirebus-tests/service/api/events"
	"empirebus-tests/service/config"
	domainlights "empirebus-tests/service/domains/lights"
	"empirebus-tests/service/runtime"
)

type Server struct {
	app    Application
	broker *events.Broker
}

type Application interface {
	Health() runtime.ServiceHealthView
	HeatingState() runtime.HeatingStateView
	EnsurePower(context.Context, string) error
	SetTargetTemperature(context.Context, float64) error
	HeatingPrograms(time.Time) []runtime.ProgramStatus
	HeatingMode() config.HeatingRuntimeState
	SetHeatingModeSchedule(context.Context) (config.HeatingRuntimeState, error)
	SetHeatingModeOff(context.Context) (config.HeatingRuntimeState, error)
	SetHeatingModeManual(context.Context, float64) (config.HeatingRuntimeState, error)
	SetHeatingModeBoost(context.Context, float64, time.Duration) (config.HeatingRuntimeState, error)
	CancelHeatingModeBoost(context.Context) (config.HeatingRuntimeState, error)
	HeatingSchedule() config.HeatingScheduleDocument
	UpdateHeatingSchedule(context.Context, config.HeatingScheduleDocument) (config.HeatingScheduleDocument, error)
	LightsState() domainlights.State
	FlashExteriorLights(context.Context, int) error
	Broker() *events.Broker
}

func New(app Application) *Server {
	return &Server{app: app, broker: app.Broker()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/heating/state", s.handleHeatingState)
	mux.HandleFunc("/v1/heating/power", s.handleHeatingPower)
	mux.HandleFunc("/v1/heating/target-temperature", s.handleHeatingTargetTemperature)
	mux.HandleFunc("/v1/heating/mode", s.handleHeatingMode)
	mux.HandleFunc("/v1/heating/mode/schedule", s.handleHeatingModeSchedule)
	mux.HandleFunc("/v1/heating/mode/off", s.handleHeatingModeOff)
	mux.HandleFunc("/v1/heating/mode/manual", s.handleHeatingModeManual)
	mux.HandleFunc("/v1/heating/mode/boost", s.handleHeatingModeBoost)
	mux.HandleFunc("/v1/heating/mode/boost/cancel", s.handleHeatingModeBoostCancel)
	mux.HandleFunc("/v1/automation/heating-programs", s.handleHeatingPrograms)
	mux.HandleFunc("/v1/automation/heating-schedule", s.handleHeatingSchedule)
	mux.HandleFunc("/v1/lights/state", s.handleLightsState)
	mux.HandleFunc("/v1/lights/external/flash", s.handleExteriorFlash)
	mux.HandleFunc("/v1/events", s.handleEvents)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Health())
}

func (s *Server) handleHeatingState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.HeatingState())
}

func (s *Server) handleHeatingPower(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.app.EnsurePower(ctx, body.State); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.HeatingState())
}

func (s *Server) handleHeatingTargetTemperature(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		Celsius float64 `json:"celsius"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.app.SetTargetTemperature(ctx, body.Celsius); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.HeatingState())
}

func (s *Server) handleHeatingPrograms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.HeatingPrograms(time.Now()))
}

func (s *Server) handleHeatingSchedule(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.app.HeatingSchedule())
	case http.MethodPut:
		var body config.HeatingScheduleDocument
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		doc, err := s.app.UpdateHeatingSchedule(ctx, body)
		if err != nil {
			switch {
			case errors.Is(err, runtime.ErrScheduleRevisionConflict):
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			case isValidationError(err):
				writeValidationError(w, err)
			default:
				writeError(w, http.StatusBadGateway, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, doc)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleHeatingMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.HeatingMode())
}

func (s *Server) handleHeatingModeSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.SetHeatingModeSchedule(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleHeatingModeOff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.SetHeatingModeOff(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleHeatingModeManual(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		TargetCelsius float64 `json:"target_celsius"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.SetHeatingModeManual(ctx, body.TargetCelsius)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleHeatingModeBoost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		TargetCelsius   float64 `json:"target_celsius"`
		DurationMinutes int     `json:"duration_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.SetHeatingModeBoost(ctx, body.TargetCelsius, time.Duration(body.DurationMinutes)*time.Minute)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleHeatingModeBoostCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.CancelHeatingModeBoost(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleLightsState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.LightsState())
}

func (s *Server) handleExteriorFlash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.app.FlashExteriorLights(ctx, body.Count); err != nil {
		switch {
		case errors.Is(err, runtime.ErrInvalidFlashCount):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, runtime.ErrLightsFlashInProgress):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "flash_in_progress"})
		default:
			writeError(w, http.StatusBadGateway, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, s.app.LightsState())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
		return
	}
	ch, cancel := s.broker.Subscribe()
	defer cancel()
	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeValidationError(w http.ResponseWriter, err error) {
	details := make([]map[string]string, 0)
	for _, part := range strings.Split(err.Error(), "; ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		details = append(details, map[string]string{"message": part})
	}
	writeJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":   "validation_failed",
		"details": details,
	})
}

func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "automation.") ||
		strings.Contains(msg, "target_celsius") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "redundant") ||
		strings.Contains(msg, "overlaps") ||
		strings.Contains(msg, "must contain at least one")
}
