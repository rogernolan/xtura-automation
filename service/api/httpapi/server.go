package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"empirebus-tests/service/adapters/btle"
	"empirebus-tests/service/api/events"
	"empirebus-tests/service/buildinfo"
	"empirebus-tests/service/config"
	domainlights "empirebus-tests/service/domains/lights"
	domainlocation "empirebus-tests/service/domains/location"
	"empirebus-tests/service/domains/overview"
	"empirebus-tests/service/domains/sensors"
	domainwater "empirebus-tests/service/domains/water"
	"empirebus-tests/service/host"
	"empirebus-tests/service/notifications"
	"empirebus-tests/service/recording"
	"empirebus-tests/service/runtime"
	"empirebus-tests/service/tracking"
	"empirebus-tests/service/waterhistory"
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
	WaterState() domainwater.State
	WaterHistory() waterhistory.Document
	OpenGreyWaterValve(context.Context) error
	CloseGreyWaterValve(context.Context) error
	ScheduleGreyWaterOpening(context.Context, string, time.Duration) (domainwater.State, error)
	CancelGreyWaterOpening(context.Context) (domainwater.State, error)
	LocationState() domainlocation.State
	HostStatus() host.Metrics
	RecordingState() recording.State
	StartRecording(context.Context, recording.StartRequest) (recording.State, error)
	StopRecording(context.Context) recording.State
	TrackingSettings() tracking.Settings
	TrackingDirectory() string
	Overview() overview.Document
	OverviewSettings() overview.Settings
	UpdateOverviewSettings(context.Context, overview.Settings) (overview.Settings, error)
	SensorSettings() sensors.Settings
	UpdateSensorSettings(context.Context, sensors.Settings) (sensors.Settings, error)
	SensorDiscover(context.Context) ([]btle.SeenDevice, error)
	SensorHistory(string, int) ([]sensors.Sample, error)
	NotificationSettings() notifications.Settings
	UpdateNotificationSettings(context.Context, notifications.Settings) (notifications.Settings, error)
	NotificationCapabilities() map[string]string
	RegisterPushSubscription(notifications.Subscription) error
	RemovePushSubscription(string) error
	UpdateTrackingSettings(context.Context, tracking.Settings) (tracking.Settings, error)
	TrackingState() tracking.State
	StartTracking(context.Context) (tracking.State, error)
	StopTracking(context.Context) (tracking.State, error)
	TrackList() ([]tracking.FileInfo, error)
	TrackRead(string) ([]byte, error)
	TrackDelete(string) error
	Broker() *events.Broker
}

func New(app Application) *Server {
	return &Server{app: app, broker: app.Broker()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/build", s.handleBuild)
	mux.HandleFunc("/v1/overview", s.handleOverview)
	mux.HandleFunc("/v1/overview/settings", s.handleOverviewSettings)
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
	mux.HandleFunc("/v1/water/state", s.handleWaterState)
	mux.HandleFunc("/v1/water/history", s.handleWaterHistory)
	mux.HandleFunc("/v1/water/grey-valve/open", s.handleGreyWaterValveOpen)
	mux.HandleFunc("/v1/water/grey-valve/close", s.handleGreyWaterValveClose)
	mux.HandleFunc("/v1/water/grey-valve/schedule", s.handleGreyWaterSchedule)
	mux.HandleFunc("/v1/water/grey-valve/schedule/cancel", s.handleGreyWaterScheduleCancel)
	mux.HandleFunc("/v1/location/state", s.handleLocationState)
	mux.HandleFunc("/v1/pi/state", s.handlePiStatus)
	mux.HandleFunc("/v1/recording/state", s.handleRecordingState)
	mux.HandleFunc("/v1/recording/start", s.handleRecordingStart)
	mux.HandleFunc("/v1/recording/stop", s.handleRecordingStop)
	mux.HandleFunc("/v1/tracking/settings", s.handleTrackingSettings)
	mux.HandleFunc("/v1/tracking/state", s.handleTrackingState)
	mux.HandleFunc("/v1/tracking/start", s.handleTrackingStart)
	mux.HandleFunc("/v1/tracking/stop", s.handleTrackingStop)
	mux.HandleFunc("/v1/sensors/settings", s.handleSensorSettings)
	mux.HandleFunc("/v1/sensors/discover", s.handleSensorDiscover)
	mux.HandleFunc("/v1/sensors/history/{id}", s.handleSensorHistory)
	mux.HandleFunc("/v1/notifications/settings", s.handleNotificationSettings)
	mux.HandleFunc("/v1/notifications/capabilities", s.handleNotificationCapabilities)
	mux.HandleFunc("/v1/notifications/subscription", s.handleNotificationSubscription)
	mux.HandleFunc("/v1/tracks", s.handleTracks)
	mux.HandleFunc("/v1/tracks/{name}", s.handleTrack)
	mux.HandleFunc("/v1/events", s.handleEvents)
	registerStaticRoutes(mux)
	return mux
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Overview())
}

func (s *Server) handleOverviewSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.app.OverviewSettings())
	case http.MethodPut:
		var settings overview.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
			return
		}
		updated, err := s.app.UpdateOverviewSettings(r.Context(), settings)
		if err != nil {
			if isValidationError(err) || strings.HasPrefix(err.Error(), "overview.") {
				writeValidationError(w, err)
			} else {
				writeError(w, http.StatusBadGateway, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Health())
}

func (s *Server) handleNotificationSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.app.NotificationSettings())
	case http.MethodPut:
		var settings notifications.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.app.UpdateNotificationSettings(r.Context(), settings)
		if err != nil {
			writeValidationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		methodNotAllowed(w)
	}
}
func (s *Server) handleNotificationCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.NotificationCapabilities())
}
func (s *Server) handleNotificationSubscription(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var sub notifications.Subscription
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.app.RegisterPushSubscription(sub); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	case http.MethodDelete:
		endpoint := r.URL.Query().Get("endpoint")
		if endpoint == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("endpoint is required"))
			return
		}
		if err := s.app.RemovePushSubscription(endpoint); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, buildinfo.Current())
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
		if isValidationError(err) {
			writeValidationError(w, err)
			return
		}
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
		if isValidationError(err) {
			writeValidationError(w, err)
			return
		}
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
		if isValidationError(err) {
			writeValidationError(w, err)
			return
		}
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

func (s *Server) handleWaterState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.WaterState())
}

func (s *Server) handleWaterHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.WaterHistory())
}

func (s *Server) handleGreyWaterValveOpen(w http.ResponseWriter, r *http.Request) {
	s.handleGreyWaterValveCommand(w, r, s.app.OpenGreyWaterValve)
}

func (s *Server) handleGreyWaterValveClose(w http.ResponseWriter, r *http.Request) {
	s.handleGreyWaterValveCommand(w, r, s.app.CloseGreyWaterValve)
}

func (s *Server) handleGreyWaterValveCommand(w http.ResponseWriter, r *http.Request, command func(context.Context) error) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := command(ctx); err != nil {
		if errors.Is(err, runtime.ErrWaterCommandInProgress) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "water_command_in_progress"})
			return
		}
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.WaterState())
}

func (s *Server) handleGreyWaterSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		TargetTime      string `json:"target_time"`
		DurationMinutes int    `json:"duration_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	state, err := s.app.ScheduleGreyWaterOpening(ctx, body.TargetTime, time.Duration(body.DurationMinutes)*time.Minute)
	if err != nil {
		if isValidationError(err) {
			writeValidationError(w, err)
			return
		}
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleGreyWaterScheduleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	state, err := s.app.CancelGreyWaterOpening(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleLocationState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.LocationState())
}

func (s *Server) handlePiStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.HostStatus())
}

func (s *Server) handleRecordingState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.RecordingState())
}

func (s *Server) handleRecordingStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		WaitFor         recording.WaitFor `json:"wait_for"`
		DurationMinutes int               `json:"duration_minutes"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: expected a single JSON object"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.StartRecording(ctx, recording.StartRequest{
		WaitFor:         body.WaitFor,
		DurationMinutes: body.DurationMinutes,
	})
	if err != nil {
		switch {
		case errors.Is(err, recording.ErrActive):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case isValidationError(err):
			writeValidationError(w, err)
		default:
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleRecordingStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.app.StopRecording(ctx))
}

// trackingSettingsDTO is the wire shape for GET/PUT /v1/tracking/settings.
// Directory is fixed at construction and ignored on PUT.
type trackingSettingsDTO struct {
	WhenEngineOn          bool    `json:"when_engine_on"`
	SampleIntervalSeconds float64 `json:"sample_interval_seconds"`
	Directory             string  `json:"directory"`
}

func trackingSettingsFromDTO(body trackingSettingsDTO) tracking.Settings {
	return tracking.Settings{
		WhenEngineOn:   body.WhenEngineOn,
		SampleInterval: time.Duration(body.SampleIntervalSeconds * float64(time.Second)),
	}
}

func trackingSettingsToDTO(settings tracking.Settings, directory string) trackingSettingsDTO {
	return trackingSettingsDTO{
		WhenEngineOn:          settings.WhenEngineOn,
		SampleIntervalSeconds: settings.SampleInterval.Seconds(),
		Directory:             directory,
	}
}

func (s *Server) handleTrackingSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, trackingSettingsToDTO(s.app.TrackingSettings(), s.app.TrackingDirectory()))
	case http.MethodPut:
		var body trackingSettingsDTO
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		settings, err := s.app.UpdateTrackingSettings(ctx, trackingSettingsFromDTO(body))
		if err != nil {
			if isValidationError(err) {
				writeValidationError(w, err)
				return
			}
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, trackingSettingsToDTO(settings, s.app.TrackingDirectory()))
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleTrackingState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.app.TrackingState())
}

func (s *Server) handleTrackingStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.StartTracking(ctx)
	if err != nil {
		switch {
		case errors.Is(err, tracking.ErrEngineMode):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleTrackingStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := s.app.StopTracking(ctx)
	if err != nil {
		switch {
		case errors.Is(err, tracking.ErrEngineMode):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleTracks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	files, err := s.app.TrackList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleSensorSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.app.SensorSettings())
	case http.MethodPut:
		var settings sensors.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		updated, err := s.app.UpdateSensorSettings(ctx, settings)
		if err != nil {
			if isValidationError(err) || strings.HasPrefix(err.Error(), "switchbot.") {
				writeValidationError(w, err)
			} else {
				writeError(w, http.StatusBadGateway, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleSensorDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	devices, err := s.app.SensorDiscover(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) handleSensorHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	samples, err := s.app.SensorHistory(r.PathValue("id"), 720)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, samples)
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch r.Method {
	case http.MethodGet:
		data, err := s.app.TrackRead(name)
		if err != nil {
			writeTrackError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		_, _ = w.Write(data)
	case http.MethodDelete:
		if err := s.app.TrackDelete(name); err != nil {
			writeTrackError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}

func writeTrackError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, err)
	case strings.Contains(err.Error(), "invalid track name"):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
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
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
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
		strings.Contains(msg, "tracking.") ||
		strings.Contains(msg, "tracking requires") ||
		strings.Contains(msg, "target_celsius") ||
		strings.Contains(msg, "duration") ||
		strings.Contains(msg, "HH:MM") ||
		strings.Contains(msg, "24-hour time") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "redundant") ||
		strings.Contains(msg, "overlaps") ||
		strings.Contains(msg, "must contain at least one")
}
