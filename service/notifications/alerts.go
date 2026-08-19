package notifications

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const RepeatInterval = 5 * time.Minute

type DeliveryMode string

const (
	DeliveryCrossing DeliveryMode = "crossing"
	DeliveryRepeat   DeliveryMode = "repeat"
)

type Alert struct {
	ID          string       `json:"id" yaml:"id"`
	Name        string       `json:"name" yaml:"name"`
	SensorID    string       `json:"sensor_id" yaml:"sensor_id"`
	HighCelsius *float64     `json:"high_celsius,omitempty" yaml:"high_celsius,omitempty"`
	LowCelsius  *float64     `json:"low_celsius,omitempty" yaml:"low_celsius,omitempty"`
	Mode        DeliveryMode `json:"mode" yaml:"mode"`
}

type Settings struct {
	Alerts []Alert `json:"alerts,omitempty" yaml:"alerts,omitempty"`
}

func (s Settings) Validate(knownSensors map[string]struct{}) error {
	seen := make(map[string]struct{}, len(s.Alerts))
	for i, alert := range s.Alerts {
		if strings.TrimSpace(alert.ID) == "" {
			return fmt.Errorf("notifications.alerts[%d].id is required", i)
		}
		if _, ok := seen[alert.ID]; ok {
			return fmt.Errorf("notifications.alerts[%d].id duplicates %q", i, alert.ID)
		}
		seen[alert.ID] = struct{}{}
		if strings.TrimSpace(alert.Name) == "" {
			return fmt.Errorf("notifications.alerts[%d].name is required", i)
		}
		if _, ok := knownSensors[alert.SensorID]; !ok {
			return fmt.Errorf("notifications.alerts[%d].sensor_id %q is unknown", i, alert.SensorID)
		}
		if alert.HighCelsius == nil && alert.LowCelsius == nil {
			return fmt.Errorf("notifications.alerts[%d] needs a high or low limit", i)
		}
		if alert.HighCelsius != nil && (!finite(*alert.HighCelsius)) {
			return fmt.Errorf("notifications.alerts[%d].high_celsius must be finite", i)
		}
		if alert.LowCelsius != nil && (!finite(*alert.LowCelsius)) {
			return fmt.Errorf("notifications.alerts[%d].low_celsius must be finite", i)
		}
		if alert.HighCelsius != nil && alert.LowCelsius != nil && *alert.LowCelsius >= *alert.HighCelsius {
			return fmt.Errorf("notifications.alerts[%d] low_celsius must be less than high_celsius", i)
		}
		if alert.Mode == "" {
			continue
		}
		if alert.Mode != DeliveryCrossing && alert.Mode != DeliveryRepeat {
			return fmt.Errorf("notifications.alerts[%d].mode %q is unsupported", i, alert.Mode)
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

type Notification struct {
	AlertID      string    `json:"alert_id"`
	AlertName    string    `json:"alert_name"`
	SensorID     string    `json:"sensor_id"`
	SensorName   string    `json:"sensor_name"`
	Side         string    `json:"side"`
	TemperatureC float64   `json:"temperature_c"`
	LimitCelsius float64   `json:"limit_celsius"`
	At           time.Time `json:"at"`
}

type sideState struct {
	violated bool
	lastSent time.Time
}
type Evaluator struct {
	settings Settings
	state    map[string]map[string]sideState
}

func NewEvaluator(settings Settings) *Evaluator {
	return &Evaluator{settings: settings, state: make(map[string]map[string]sideState)}
}

func (e *Evaluator) Configure(settings Settings) {
	e.settings = settings
	e.state = make(map[string]map[string]sideState)
}

func (e *Evaluator) Evaluate(sensorID, sensorName string, temp float64, at time.Time) []Notification {
	if !finite(temp) {
		return nil
	}
	var out []Notification
	for _, alert := range e.settings.Alerts {
		if alert.SensorID != sensorID {
			continue
		}
		if e.state[alert.ID] == nil {
			e.state[alert.ID] = make(map[string]sideState)
		}
		out = append(out, e.evaluateSide(alert, sensorName, temp, at, "high", alert.HighCelsius)...)
		out = append(out, e.evaluateSide(alert, sensorName, temp, at, "low", alert.LowCelsius)...)
	}
	return out
}

func (e *Evaluator) evaluateSide(alert Alert, sensorName string, temp float64, at time.Time, side string, limit *float64) []Notification {
	if limit == nil {
		return nil
	}
	violated := (side == "high" && temp > *limit) || (side == "low" && temp < *limit)
	state := e.state[alert.ID][side]
	if !violated {
		state.violated = false
		state.lastSent = time.Time{}
		e.state[alert.ID][side] = state
		return nil
	}
	shouldSend := !state.violated || (alert.Mode == DeliveryRepeat && !state.lastSent.IsZero() && !at.Before(state.lastSent.Add(RepeatInterval)))
	state.violated = true
	if !shouldSend {
		e.state[alert.ID][side] = state
		return nil
	}
	state.lastSent = at
	e.state[alert.ID][side] = state
	return []Notification{{AlertID: alert.ID, AlertName: alert.Name, SensorID: alert.SensorID, SensorName: sensorName, Side: side, TemperatureC: temp, LimitCelsius: *limit, At: at}}
}
