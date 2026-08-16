package switchbot

import (
	"testing"
	"time"

	"empirebus-tests/service/domains/sensors"
)

func sensorsSettingsWith(name, mac string, primary bool) sensors.Settings {
	return sensors.Settings{
		Enabled:   true,
		HCIDevice: "hci0",
		Sensors: []sensors.SensorConfig{
			{Name: name, MAC: mac, Primary: primary},
		},
	}
}

func TestHandleEventRecordsSeenDeviceAndCallback(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	var readings []Reading
	adapter := New(Config{
		Settings: sensorsSettingsWith("Main", "c5:65:68:81:84:32", true),
		Now:      func() time.Time { return now },
		OnReading: func(reading Reading) {
			readings = append(readings, reading)
		},
	})

	ad := adBytes(
		serviceDataElement(0x77, 0x00, 0x41),
		manufacturerElement(0x69, 0x09, 0xc5, 0x65, 0x68, 0x81, 0x84, 0x32, 0x9d, 0x0f, 0x05, 0x06, 0x23, 0x00),
	)
	event := advReportEvent([]byte{0x32, 0x84, 0x81, 0x68, 0x65, 0xc5}, ad, 0xd8)

	adapter.handleEvent(event)

	devices := adapter.SeenDevices()
	if len(devices) != 1 {
		t.Fatalf("expected 1 seen device, got %d", len(devices))
	}
	if devices[0].MAC != "c5:65:68:81:84:32" || devices[0].DevType != 0x77 {
		t.Fatalf("unexpected seen device: %#v", devices[0])
	}
	if len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(readings))
	}
	if readings[0].ID != "c56568818432" {
		t.Fatalf("reading id: got %q", readings[0].ID)
	}
	if readings[0].Temp != -6.5 {
		t.Fatalf("reading temp: got %v", readings[0].Temp)
	}
}

func TestHandleEventIgnoresUnconfiguredDevices(t *testing.T) {
	var readings []Reading
	adapter := New(Config{
		Settings: sensorsSettingsWith("Main", "c5:65:68:81:84:32", true),
		OnReading: func(reading Reading) {
			readings = append(readings, reading)
		},
	})
	ad := adBytes(serviceDataElement(0x77, 0x00, 0x41))
	event := advReportEvent([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, ad, 0x00)
	adapter.handleEvent(event)
	if len(readings) != 0 {
		t.Fatalf("expected no readings for unconfigured device, got %d", len(readings))
	}
}

func TestFeedReadingDeliversDecodedReading(t *testing.T) {
	var readings []Reading
	adapter := New(Config{
		Settings: sensorsSettingsWith("Main", "c5:65:68:81:84:32", true),
		OnReading: func(reading Reading) {
			readings = append(readings, reading)
		},
	})
	hum := 55.0
	battery := 87
	adapter.FeedReading("c5:65:68:81:84:32", Payload{DevType: 0x77, Temp: 21.4, HasTemp: true, Humidity: &hum, Battery: &battery}, -50)
	if len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(readings))
	}
	if readings[0].Temp != 21.4 || *readings[0].Humidity != 55 || *readings[0].Battery != 87 {
		t.Fatalf("unexpected reading: %#v", readings[0])
	}
	if len(adapter.SeenDevices()) != 1 {
		t.Fatalf("expected seen device to be recorded")
	}
}

func TestBatteryOnlyPacketDoesNotEmitReading(t *testing.T) {
	var readings []Reading
	adapter := New(Config{
		Settings: sensorsSettingsWith("Main", "c5:65:68:81:84:32", true),
		OnReading: func(reading Reading) {
			readings = append(readings, reading)
		},
	})
	// WoSensorTHO battery-only service data: device identified but no
	// temperature yet (no manufacturer data).
	ad := adBytes(serviceDataElement(0x77, 0x00, 0x41))
	event := advReportEvent([]byte{0x32, 0x84, 0x81, 0x68, 0x65, 0xc5}, ad, 0x00)
	adapter.handleEvent(event)
	if len(readings) != 0 {
		t.Fatalf("expected no reading for battery-only packet, got %#v", readings)
	}
	if len(adapter.SeenDevices()) != 1 {
		t.Fatalf("battery-only packet should still record the seen device")
	}
}

func TestSettingsUpdateTakesEffectImmediately(t *testing.T) {
	var readings []Reading
	adapter := New(Config{
		OnReading: func(reading Reading) {
			readings = append(readings, reading)
		},
	})
	adapter.Configure(sensorsSettingsWith("Outside", "c5:65:68:81:84:32", false))
	ad := adBytes(
		serviceDataElement(0x77, 0x00, 0x41),
		manufacturerElement(0x69, 0x09, 0xc5, 0x65, 0x68, 0x81, 0x84, 0x32, 0x9d, 0x0f, 0x05, 0x06, 0x23, 0x00),
	)
	event := advReportEvent([]byte{0x32, 0x84, 0x81, 0x68, 0x65, 0xc5}, ad, 0x00)
	adapter.handleEvent(event)
	if len(readings) != 1 || readings[0].ID != "c56568818432" {
		t.Fatalf("expected reading after Configure, got %#v", readings)
	}
}
