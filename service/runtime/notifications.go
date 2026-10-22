package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/heating"
	"empirebus-tests/service/domains/sensors"
	"empirebus-tests/service/notifications"
)

func (a *App) notificationSensorIDs() map[string]struct{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ids := map[string]struct{}{sensors.AldeID: {}}
	for _, sensor := range a.sensorSettings.Sensors {
		ids[sensor.ID()] = struct{}{}
	}
	return ids
}
func (a *App) NotificationSettings() notifications.Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return notifications.Settings{Alerts: append([]notifications.Alert(nil), a.rawConfig.Notifications.Alerts...), AldePowerAlertEnabled: a.rawConfig.Notifications.AldePowerAlertEnabled}
}
func (a *App) UpdateNotificationSettings(_ context.Context, settings notifications.Settings) (notifications.Settings, error) {
	if err := settings.Validate(a.notificationSensorIDs()); err != nil {
		return notifications.Settings{}, err
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.mu.RLock()
	next := a.rawConfig
	path := a.configPath
	a.mu.RUnlock()
	powerWasEnabled := (notifications.Settings{AldePowerAlertEnabled: next.Notifications.AldePowerAlertEnabled}).PowerAlertEnabled()
	if path == "" {
		return notifications.Settings{}, fmt.Errorf("config path is not configured")
	}
	next.Notifications.Alerts = append([]notifications.Alert(nil), settings.Alerts...)
	// Older clients only send temperature alerts. Omission preserves the
	// persisted toggle rather than silently re-enabling a disabled warning.
	if settings.AldePowerAlertEnabled != nil {
		next.Notifications.AldePowerAlertEnabled = settings.AldePowerAlertEnabled
	}
	normalized, err := next.Normalize()
	if err != nil {
		return notifications.Settings{}, err
	}
	if err := config.SaveFile(path, next); err != nil {
		return notifications.Settings{}, err
	}
	a.mu.Lock()
	a.rawConfig = next
	a.cfg = normalized
	a.notificationEvaluator.Configure(settings)
	powerIsEnabled := (notifications.Settings{AldePowerAlertEnabled: next.Notifications.AldePowerAlertEnabled}).PowerAlertEnabled()
	if a.aldePowerEvaluator != nil && powerWasEnabled != powerIsEnabled {
		a.aldePowerEvaluator.Evaluate(heating.ElectricSupplyState{}, false, time.Now())
	}
	a.revision = readConfigRevision(path)
	a.mu.Unlock()
	return a.NotificationSettings(), nil
}
func (a *App) NotificationCapabilities() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return map[string]string{"public_key": a.cfg.Notifications.VAPIDPublicKey}
}
func (a *App) RegisterPushSubscription(sub notifications.Subscription) error {
	if a.notificationSubs == nil {
		return fmt.Errorf("notification subscriptions are unavailable")
	}
	return a.notificationSubs.Upsert(sub)
}
func (a *App) RemovePushSubscription(endpoint string) error {
	if a.notificationSubs == nil {
		return fmt.Errorf("notification subscriptions are unavailable")
	}
	return a.notificationSubs.Remove(endpoint)
}
func (a *App) evaluateNotifications(id, name string, temp float64, at time.Time) {
	if a.notificationEvaluator == nil || a.notificationSender == nil || a.notificationSubs == nil {
		return
	}
	a.dispatchNotifications(a.notificationEvaluator.Evaluate(id, name, temp, at))
}

func (a *App) evaluateOfflineNotifications() {
	if a.notificationEvaluator == nil || a.notificationSender == nil || a.notificationSubs == nil {
		return
	}
	names := map[string]string{sensors.AldeID: "Alde"}
	a.mu.RLock()
	for _, sensor := range a.sensorSettings.Sensors {
		names[sensor.ID()] = sensor.Name
	}
	a.mu.RUnlock()
	a.dispatchNotifications(a.notificationEvaluator.CheckOffline(a.now().UTC(), names))
}

func (a *App) dispatchNotifications(items []notifications.Notification) {
	if a.notificationSubs == nil || a.notificationSender == nil {
		return
	}
	for _, n := range items {
		for _, sub := range a.notificationSubs.List() {
			if n.Kind == "alde_power" && sub.FormatVersion < 2 {
				continue
			}
			goSafe(a.logger, "notification_send", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := a.notificationSender.Send(ctx, sub, n); err != nil {
					var pushErr *notifications.PushError
					if errors.As(err, &pushErr) && pushErr.Terminal {
						_ = a.notificationSubs.Remove(sub.Endpoint)
					}
					a.logger.Printf("notification send %s: %v", n.AlertID, err)
				}
			})
		}
	}
}

func (a *App) aldePowerNotifications() []notifications.Notification {
	if a.electricSupply == nil || a.aldePowerEvaluator == nil {
		return nil
	}
	now := a.now().UTC()
	state := a.electricSupply.ElectricSupplyState(now)
	canDeliver := false
	if a.notificationSender.Configured() && a.notificationSubs != nil {
		for _, sub := range a.notificationSubs.List() {
			if sub.FormatVersion >= 2 {
				canDeliver = true
				break
			}
		}
	}
	if !canDeliver && state.HeaterOn && state.ElectricWatts > state.ShoreWatts {
		// Delay emission until a compatible phone can receive it, while still
		// processing known safe states so they can rearm a previous warning.
		state.Known = false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	settings := notifications.Settings{AldePowerAlertEnabled: a.rawConfig.Notifications.AldePowerAlertEnabled}
	return a.aldePowerEvaluator.Evaluate(state, settings.PowerAlertEnabled(), now)
}
