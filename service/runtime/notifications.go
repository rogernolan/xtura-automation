package runtime

import (
	"context"
	"fmt"
	"time"

	"empirebus-tests/service/config"
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
	return notifications.Settings{Alerts: append([]notifications.Alert(nil), a.rawConfig.Notifications.Alerts...)}
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
	if path == "" {
		return notifications.Settings{}, fmt.Errorf("config path is not configured")
	}
	next.Notifications.Alerts = append([]notifications.Alert(nil), settings.Alerts...)
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
	for _, n := range a.notificationEvaluator.Evaluate(id, name, temp, at) {
		for _, sub := range a.notificationSubs.List() {
			if err := a.notificationSender.Send(context.Background(), sub, n); err != nil {
				a.logger.Printf("notification send %s: %v", n.AlertID, err)
			}
		}
	}
}
