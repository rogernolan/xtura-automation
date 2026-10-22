package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"empirebus-tests/service/config"
	"empirebus-tests/service/domains/heating"
	"empirebus-tests/service/notifications"
)

type stubElectricSupply struct{ state heating.ElectricSupplyState }

func (s *stubElectricSupply) ElectricSupplyState(time.Time) heating.ElectricSupplyState {
	return s.state
}

func TestAldePowerRuntimeUsesSupplyAndSettings(t *testing.T) {
	now := time.Unix(1000, 0)
	provider := &stubElectricSupply{state: heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 3000, Session: 1}}
	a := &App{now: func() time.Time { return now }, electricSupply: provider, aldePowerEvaluator: &notifications.AldePowerEvaluator{}}
	a.notificationSender, a.notificationSubs = testAldePush(t)
	if got := a.aldePowerNotifications(); len(got) != 0 {
		t.Fatal(got)
	}
	now = now.Add(time.Minute)
	if got := a.aldePowerNotifications(); len(got) != 1 || got[0].Kind != "alde_power" {
		t.Fatalf("runtime warning: %+v", got)
	}
	disabled := false
	a.rawConfig.Notifications.AldePowerAlertEnabled = &disabled
	if got := a.aldePowerNotifications(); len(got) != 0 {
		t.Fatalf("disabled: %+v", got)
	}
	a.rawConfig.Notifications.AldePowerAlertEnabled = nil
	if got := a.aldePowerNotifications(); len(got) != 0 {
		t.Fatalf("reenable skipped delay: %+v", got)
	}
}

func testAldePush(t *testing.T) (*notifications.Sender, *notifications.SubscriptionStore) {
	t.Helper()
	store, err := notifications.LoadSubscriptionStore(filepath.Join(t.TempDir(), "subscriptions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(notifications.Subscription{Endpoint: "https://push.example.test/subscription", FormatVersion: 2}); err != nil {
		t.Fatal(err)
	}
	return notifications.NewSender(notifications.PushConfig{PublicKey: "test-public", PrivateKey: "test-private", Subject: "mailto:test@example.test"}, nil), store
}

func TestAldePowerRecoversWhileUnsubscribed(t *testing.T) {
	now := time.Unix(1000, 0)
	provider := &stubElectricSupply{state: heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 3000}}
	a := &App{now: func() time.Time { return now }, electricSupply: provider, aldePowerEvaluator: &notifications.AldePowerEvaluator{}}
	a.notificationSender, a.notificationSubs = testAldePush(t)
	a.aldePowerNotifications()
	now = now.Add(time.Minute)
	if len(a.aldePowerNotifications()) != 1 {
		t.Fatal("initial warning missing")
	}
	if err := a.notificationSubs.Remove("https://push.example.test/subscription"); err != nil {
		t.Fatal(err)
	}
	provider.state.HeaterOn = false
	a.aldePowerNotifications()
	provider.state.HeaterOn = true
	a.aldePowerNotifications()
	if err := a.notificationSubs.Upsert(notifications.Subscription{Endpoint: "https://push.example.test/subscription", FormatVersion: 2}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	a.aldePowerNotifications()
	now = now.Add(time.Minute)
	if len(a.aldePowerNotifications()) != 1 {
		t.Fatal("known safe interval did not rearm warning")
	}
}

func TestAldePowerWaitsForCompatiblePhoneWorker(t *testing.T) {
	now := time.Unix(1000, 0)
	a := &App{now: func() time.Time { return now }, electricSupply: &stubElectricSupply{state: heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 3000}}, aldePowerEvaluator: &notifications.AldePowerEvaluator{}}
	a.notificationSender, a.notificationSubs = testAldePush(t)
	if err := a.notificationSubs.Upsert(notifications.Subscription{Endpoint: "https://push.example.test/subscription"}); err != nil {
		t.Fatal(err)
	}
	a.aldePowerNotifications()
	now = now.Add(time.Minute)
	if got := a.aldePowerNotifications(); len(got) != 0 {
		t.Fatalf("old worker consumed warning: %+v", got)
	}
	if err := a.notificationSubs.Upsert(notifications.Subscription{Endpoint: "https://push.example.test/subscription", FormatVersion: 2}); err != nil {
		t.Fatal(err)
	}
	a.aldePowerNotifications()
	now = now.Add(time.Minute)
	if got := a.aldePowerNotifications(); len(got) != 1 {
		t.Fatalf("updated worker missing warning: %+v", got)
	}
}

func TestAldePowerDoesNotConsumeWarningBeforePhoneSubscribes(t *testing.T) {
	now := time.Unix(1000, 0)
	a := &App{now: func() time.Time { return now }, electricSupply: &stubElectricSupply{state: heating.ElectricSupplyState{Known: true, HeaterOn: true, ElectricWatts: 3000}}, aldePowerEvaluator: &notifications.AldePowerEvaluator{}}
	a.aldePowerNotifications()
	now = now.Add(time.Minute)
	if got := a.aldePowerNotifications(); len(got) != 0 {
		t.Fatalf("consumed without a phone: %+v", got)
	}
	a.notificationSender, a.notificationSubs = testAldePush(t)
	now = now.Add(time.Second)
	if got := a.aldePowerNotifications(); len(got) != 0 {
		t.Fatalf("no fresh delay: %+v", got)
	}
	now = now.Add(time.Minute)
	if got := a.aldePowerNotifications(); len(got) != 1 {
		t.Fatalf("subscribed warning: %+v", got)
	}
}

func TestAldePowerSettingPersistsAndLegacyTemperatureEditsPreserveIt(t *testing.T) {
	raw := config.Config{Garmin: config.GarminConfig{WSURL: "ws://localhost:8090/ws", HeartbeatInterval: time.Second}, API: config.APIConfig{Listen: ":8091"}, Automation: config.AutomationConfig{Timezone: "UTC", HeatingPrograms: []config.HeatingProgramConfig{{ID: "test", Days: []string{"mon"}, Periods: []config.HeatingPeriodConfig{{Start: "00:00", Mode: "off"}}}}}}
	path := filepath.Join(t.TempDir(), "config.yaml")
	a := &App{rawConfig: raw, configPath: path, notificationEvaluator: notifications.NewEvaluator(notifications.Settings{}), aldePowerEvaluator: &notifications.AldePowerEvaluator{}}
	disabled := false
	if _, err := a.UpdateNotificationSettings(context.Background(), notifications.Settings{AldePowerAlertEnabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	limit := 5.0
	alert := notifications.Alert{ID: "cold", Name: "Cold", SensorID: "alde", LowCelsius: &limit}
	got, err := a.UpdateNotificationSettings(context.Background(), notifications.Settings{Alerts: []notifications.Alert{alert}})
	if err != nil {
		t.Fatal(err)
	}
	if got.PowerAlertEnabled() || len(got.Alerts) != 1 {
		t.Fatalf("legacy edit reset power toggle: %+v", got)
	}
	saved, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Notifications.AldePowerAlertEnabled == nil || *saved.Notifications.AldePowerAlertEnabled {
		t.Fatalf("not persisted: %+v", saved.Notifications)
	}
	if len(saved.Notifications.Alerts) != 1 {
		t.Fatal("temperature alert lost")
	}
}
