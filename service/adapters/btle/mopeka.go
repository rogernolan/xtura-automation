package btle

import "fmt"

const mopekaCompanyID = 0x0059

// MopekaReading is a decoded reading from a Mopeka Pro Check BLE sensor.
type MopekaReading struct {
	DistanceMm  float64
	BatteryPct  float64
	BatteryV    float64
	TempC       float64
	Quality     int
	SensorType  int
	RSSI        int
	MAC         string
}

// MOPEKA_LPG_COEF are the calibration coefficients provided by Mopeka for
// converting the raw ultrasonic time-of-flight to distance in millimetres.
var MOPEKA_LPG_COEF = [3]float64{0.573045, -0.002822, -0.00000535}

// DecodeMopeka decodes a Mopeka Pro Check manufacturer-specific BLE
// advertisement. The manufacturerData should be the full manufacturer-specific
// data bytes including the 2-byte company ID prefix (total 12 bytes).
// Returns nil, nil if the data does not look like a Mopeka advertisement.
func DecodeMopeka(manufacturerData []byte, rssi int, mac string) (*MopekaReading, error) {
	// 12 bytes: 2 company ID + 10 payload
	if len(manufacturerData) < 12 {
		return nil, nil
	}
	// Company ID must be Nordic Semiconductor (0x0059) in little-endian.
	if manufacturerData[0] != 0x59 || manufacturerData[1] != 0x00 {
		return nil, nil
	}

	sensorType := int(manufacturerData[2])

	// Battery voltage: byte 3, lower 7 bits, /32.0 gives volts.
	batteryV := float64(manufacturerData[3]&0x7F) / 32.0
	batteryPct := (batteryV - 2.2) / 0.65 * 100.0
	if batteryPct < 0 {
		batteryPct = 0
	} else if batteryPct > 100 {
		batteryPct = 100
	}

	// Temperature: byte 4, lower 7 bits, minus 40.
	tempC := float64(manufacturerData[4]&0x7F) - 40.0

	// Distance: 14-bit raw value from bytes 5-6, scaled by LPG coefficients.
	// raw = (byte6 << 8) | byte5; raw_level = raw & 0x3FFF
	rawLevel := float64(((int(manufacturerData[6])<<8 | int(manufacturerData[5])) & 0x3FFF))
	rawT := float64(manufacturerData[4] & 0x7F)
	distMm := rawLevel * (MOPEKA_LPG_COEF[0] + MOPEKA_LPG_COEF[1]*rawT + MOPEKA_LPG_COEF[2]*rawT*rawT)

	// Quality: byte 6, upper 2 bits.
	quality := int((manufacturerData[6] >> 6) & 0x03)

	return &MopekaReading{
		DistanceMm: distMm,
		BatteryPct: batteryPct,
		BatteryV:   batteryV,
		TempC:      tempC,
		Quality:    quality,
		SensorType: sensorType,
		RSSI:       rssi,
		MAC:        mac,
	}, nil
}

// MopekaReadingString returns a human-readable summary for logging.
func (r MopekaReading) String() string {
	return fmt.Sprintf("mopeka %s: dist=%.0fmm batt=%.0f%% temp=%.1f°C q=%d type=0x%02X rssi=%d",
		r.MAC, r.DistanceMm, r.BatteryPct, r.TempC, r.Quality, r.SensorType, r.RSSI)
}
