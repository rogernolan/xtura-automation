package btle

import (
	"testing"
)

func TestDecodeMopeka(t *testing.T) {
	// Build a valid Mopeka Pro Check manufacturer data packet (12 bytes).
	// Layout after 2-byte company ID (0x0059):
	//   [2] Sensor type = 0x03 (standard bottom-up)
	//   [3] Battery raw = 0x60 (96) → 96/32 = 3.0V
	//   [4] Temperature raw = 0x37 (55) → 55-40 = 15°C
	//   [5] Level low byte = 0x8C (140)
	//   [6] Level high (bits 0-5) + Quality (bits 6-7)
	//       Quality = 1 → bits 6-7 = 01 → byte = 0x40 | (level_high & 0x3F)
	//       Level high = 3 → 0x03 | 0x40 = 0x43
	data := []byte{
		0x59, 0x00, // company ID
		0x03,       // sensor type: standard bottom-up
		0x60,       // battery raw = 96
		0x37,       // temperature raw = 55 → 15°C
		0x8C,       // level low = 140
		0x43,       // level high=3 | quality=1 (0x40 | 0x03)
		0x00, 0x00, 0x00, 0x00, 0x00, // accel/padding
	}
	reading, err := DecodeMopeka(data, -55, "AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading == nil {
		t.Fatal("expected non-nil reading")
	}

	// Battery: 96/32 = 3.0V
	if reading.BatteryV != 3.0 {
		t.Errorf("BatteryV = %v, want 3.0", reading.BatteryV)
	}

	// Battery %: (3.0 - 2.2) / 0.65 * 100 = 123.07 → clamped to 100
	if reading.BatteryPct != 100.0 {
		t.Errorf("BatteryPct = %v, want 100", reading.BatteryPct)
	}

	// Temperature: 55 - 40 = 15
	if reading.TempC != 15.0 {
		t.Errorf("TempC = %v, want 15", reading.TempC)
	}

	// Sensor type
	if reading.SensorType != 0x03 {
		t.Errorf("SensorType = %v, want 3", reading.SensorType)
	}

	// Quality
	if reading.Quality != 1 {
		t.Errorf("Quality = %v, want 1", reading.Quality)
	}

	// Distance: raw_level = ((0x43 << 8 | 0x8C) & 0x3FFF) = (17292 & 0x3FFF) = 908
	// raw_t = 0x37 = 55
	// coef = 0.573045 + (-0.002822)*55 + (-0.00000535)*55*55 = 0.40164
	// dist = 908 * 0.40164 ≈ 364.7
	expectedDist := 908.0 * (0.573045 + (-0.002822)*55.0 + (-0.00000535)*55.0*55.0)
	if reading.DistanceMm < expectedDist-1 || reading.DistanceMm > expectedDist+1 {
		t.Errorf("DistanceMm = %v, want ~%v", reading.DistanceMm, expectedDist)
	}

	if reading.RSSI != -55 {
		t.Errorf("RSSI = %v, want -55", reading.RSSI)
	}

	if reading.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("MAC = %v, want AA:BB:CC:DD:EE:FF", reading.MAC)
	}
}

func TestDecodeMopekaTooShort(t *testing.T) {
	reading, err := DecodeMopeka([]byte{0x59, 0x00, 0x03}, -50, "AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading != nil {
		t.Error("expected nil reading for too-short data")
	}
}

func TestDecodeMopekaWrongCompanyID(t *testing.T) {
	data := make([]byte, 12)
	data[0] = 0x69 // wrong
	data[1] = 0x09
	reading, err := DecodeMopeka(data, -50, "AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading != nil {
		t.Error("expected nil reading for wrong company ID")
	}
}

func TestDecodeMopekaBatteryClampLow(t *testing.T) {
	data := make([]byte, 12)
	data[0] = 0x59
	data[1] = 0x00
	data[3] = 0x00 // battery raw = 0 → 0V → clamped to 0%
	reading, err := DecodeMopeka(data, -50, "AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading.BatteryPct != 0 {
		t.Errorf("BatteryPct = %v, want 0", reading.BatteryPct)
	}
}

func TestDecodeMopekaQualityValues(t *testing.T) {
	data := make([]byte, 12)
	data[0] = 0x59
	data[1] = 0x00
	data[6] = 0xC0 // quality bits = 3
	reading, err := DecodeMopeka(data, -50, "AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading.Quality != 3 {
		t.Errorf("Quality = %v, want 3", reading.Quality)
	}
}
