package switchbot

import (
	"testing"
)

func adBytes(elements ...AD) []byte {
	var out []byte
	for _, element := range elements {
		out = append(out, byte(len(element.Data)+1), element.Type)
		out = append(out, element.Data...)
	}
	return out
}

func serviceDataElement(payload ...byte) AD {
	data := []byte{0x3d, 0xfd}
	data = append(data, payload...)
	return AD{Type: adTypeServiceData16, Data: data}
}

func manufacturerElement(payload ...byte) AD {
	return AD{Type: adTypeManufacturer, Data: payload}
}

// advReportEvent builds a LE meta event packet with one advertising report.
func advReportEvent(address []byte, ad []byte, rssi byte) []byte {
	var params []byte
	params = append(params, 0x02, 0x01, 0x00, 0x00)
	params = append(params, address...)
	params = append(params, byte(len(ad)))
	params = append(params, ad...)
	params = append(params, rssi)
	var event []byte
	event = append(event, 0x3e, byte(len(params)))
	event = append(event, params...)
	return event
}

func TestParseADElements(t *testing.T) {
	payload := adBytes(
		AD{Type: 0x01, Data: []byte{0x06}},
		AD{Type: 0xff, Data: []byte{0x69, 0x09, 0x01}},
	)
	elements, err := ParseADElements(payload)
	if err != nil {
		t.Fatalf("ParseADElements: %v", err)
	}
	if len(elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elements))
	}
	if elements[0].Type != 0x01 || len(elements[0].Data) != 1 {
		t.Fatalf("unexpected first element: %#v", elements[0])
	}
	if elements[1].Type != 0xff || len(elements[1].Data) != 3 {
		t.Fatalf("unexpected second element: %#v", elements[1])
	}
}

func TestParseADElementsOverrun(t *testing.T) {
	_, err := ParseADElements([]byte{0x05, 0xff, 0x01})
	if err == nil {
		t.Fatal("expected overrun error")
	}
}

func TestDecodeMeter(t *testing.T) {
	// Layout per OpenWonderLabs meter.md: 0x54 type, battery 100, temp 26.4,
	// humidity 52 (sign bit set = positive, scale bit set = celsius).
	payload, ok := Decode([]AD{serviceDataElement(0x54, 0x00, 100, 0x04, 0x9A, 0xB4)})
	if !ok {
		t.Fatal("expected decode ok")
	}
	if payload.DevType != 0x54 {
		t.Fatalf("dev type: got %#x", payload.DevType)
	}
	if payload.Temp != 26.4 {
		t.Fatalf("temp: got %v", payload.Temp)
	}
	if payload.Humidity == nil || *payload.Humidity != 52 {
		t.Fatalf("humidity: got %v", payload.Humidity)
	}
	if payload.Battery == nil || *payload.Battery != 100 {
		t.Fatalf("battery: got %v", payload.Battery)
	}
}

func TestDecodeMeterNegativeTemp(t *testing.T) {
	// sign bit (0x80) clear = negative; integer byte 0x05, decimal 0x07 -> -5.7
	payload, ok := Decode([]AD{serviceDataElement(0x54, 0x00, 60, 0x07, 0x05, 0x30)})
	if !ok {
		t.Fatal("expected decode ok")
	}
	if payload.Temp != -5.7 {
		t.Fatalf("temp: got %v", payload.Temp)
	}
}

func TestDecodeOutdoorFromManufacturerData(t *testing.T) {
	// Real capture (temp -6.5C, humidity 35%, battery 65%):
	// service data 3dfd770041, manufacturer data 6909c565688184329d0f05062300.
	elements := []AD{
		serviceDataElement(0x77, 0x00, 0x41),
		manufacturerElement(0x69, 0x09, 0xc5, 0x65, 0x68, 0x81, 0x84, 0x32, 0x9d, 0x0f, 0x05, 0x06, 0x23, 0x00),
	}
	payload, ok := Decode(elements)
	if !ok {
		t.Fatal("expected decode ok")
	}
	if payload.DevType != 0x77 {
		t.Fatalf("dev type: got %#x", payload.DevType)
	}
	if payload.Temp != -6.5 {
		t.Fatalf("temp: got %v", payload.Temp)
	}
	if payload.Humidity == nil || *payload.Humidity != 35 {
		t.Fatalf("humidity: got %v", payload.Humidity)
	}
	if payload.Battery == nil || *payload.Battery != 65 {
		t.Fatalf("battery: got %v", payload.Battery)
	}
}

func TestDecodeManufacturerOnlySwitchBotReadings(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		mac  string
		temp float64
		hum  float64
	}{
		{name: "main", data: []byte{0xe6, 0x55, 0x83, 0xc6, 0x64, 0x24, 0x4a, 0x0f, 0x03, 0x9a, 0x31}, mac: "e6:55:83:c6:64:24", temp: 26.3, hum: 49},
		{name: "outside", data: []byte{0xeb, 0x6b, 0x00, 0xc6, 0x06, 0x69, 0x30, 0x02, 0x03, 0x96, 0x42}, mac: "eb:6b:00:c6:06:69", temp: 22.3, hum: 66},
		{name: "hold", data: []byte{0xeb, 0x6b, 0x04, 0x06, 0x14, 0x2a, 0x21, 0x0e, 0x04, 0x9a, 0x36}, mac: "eb:6b:04:06:14:2a", temp: 26.4, hum: 54},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload, ok := DecodeOutdoorMFR([]AD{manufacturerElement(append([]byte{0x69, 0x09}, test.data...)...)}, test.mac)
			if !ok || !payload.HasTemp {
				t.Fatalf("expected temperature payload, got %#v, ok=%t", payload, ok)
			}
			if payload.Temp != test.temp {
				t.Fatalf("temp: got %v, want %v", payload.Temp, test.temp)
			}
			if payload.Humidity == nil || *payload.Humidity != test.hum {
				t.Fatalf("humidity: got %v, want %v", payload.Humidity, test.hum)
			}
			if payload.Battery != nil {
				t.Fatalf("battery should be unknown for MFR-only, got %v", *payload.Battery)
			}
		})
	}
}

func TestDecodeOutdoorBatteryOnly(t *testing.T) {
	// WoSensorTHO alternates advertisements: battery may arrive without a
	// manufacturer data payload. Decode must still identify the device.
	payload, ok := Decode([]AD{serviceDataElement(0x77, 0x00, 0xe4)})
	if !ok {
		t.Fatal("expected decode ok")
	}
	if payload.Battery == nil || *payload.Battery != 100 {
		t.Fatalf("battery: got %v", payload.Battery)
	}
	if payload.Humidity != nil {
		t.Fatalf("humidity should be absent, got %v", payload.Humidity)
	}
}

func TestDecodeOutdoorFromManufacturerDataOnly(t *testing.T) {
	// Outdoor sensor advertising only manufacturer data (no 0xFD3D service
	// data). This happens when the device alternates advertisements.
	// Temp 30.4C, humidity 44%: mfr payload (company id stripped) =
	// e65583c66424300f 04 9e 2c
	elements := []AD{
		manufacturerElement(0x69, 0x09, 0xe6, 0x55, 0x83, 0xc6, 0x64, 0x24, 0x30, 0x0f, 0x04, 0x9e, 0x2c),
	}
	payload, ok := DecodeOutdoorMFR(elements, "e6:55:83:c6:64:24")
	if !ok {
		t.Fatal("expected decode ok for MFR-only outdoor sensor")
	}
	if payload.DevType != 0x77 {
		t.Fatalf("dev type: got %#x", payload.DevType)
	}
	if payload.Temp != 30.4 {
		t.Fatalf("temp: got %v", payload.Temp)
	}
	if payload.Humidity == nil || *payload.Humidity != 44 {
		t.Fatalf("humidity: got %v", payload.Humidity)
	}
	if payload.Battery != nil {
		t.Fatalf("battery should be nil for MFR-only, got %v", payload.Battery)
	}
}

func TestDecodeOutdoorMFRRejectsShortPayload(t *testing.T) {
	elements := []AD{
		manufacturerElement(0x69, 0x09, 0xe6, 0x55),
	}
	_, ok := DecodeOutdoorMFR(elements, "e6:55:83:c6:64:24")
	if ok {
		t.Fatal("expected short MFR payload to be rejected")
	}
}

func TestDecodeOutdoorMFRRejectsMismatchedEmbeddedMAC(t *testing.T) {
	elements := []AD{
		manufacturerElement(0x69, 0x09, 0xe6, 0x55, 0x83, 0xc6, 0x64, 0x24, 0x30, 0x0f, 0x04, 0x9e, 0x2c),
	}
	_, ok := DecodeOutdoorMFR(elements, "eb:6b:00:c6:06:69")
	if ok {
		t.Fatal("expected MFR payload with mismatched embedded MAC to be rejected")
	}
}

func TestDecodeIgnoresUnknownDevice(t *testing.T) {
	_, ok := Decode([]AD{serviceDataElement(0x01, 0x00, 0x00)})
	if ok {
		t.Fatal("expected unknown device to be rejected")
	}
}

func TestParseAdvertisingReports(t *testing.T) {
	// LE meta event (0x3e), subevent 0x02, one report with a SwitchBot AD.
	address := []byte{0x32, 0x84, 0x81, 0x68, 0x65, 0xc5}
	ad := adBytes(serviceDataElement(0x77, 0x00, 0x41))
	event := advReportEvent(address, ad, 0xd8)

	reports, err := ParseAdvertisingReports(event)
	if err != nil {
		t.Fatalf("ParseAdvertisingReports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	report := reports[0]
	if report.MAC != "c5:65:68:81:84:32" {
		t.Fatalf("MAC: got %q", report.MAC)
	}
	if report.RSSI != -40 {
		t.Fatalf("RSSI: got %d", report.RSSI)
	}
}

func TestParseAdvertisingReportsMultiple(t *testing.T) {
	var event []byte
	event = append(event, 0x3e, 0)
	params := []byte{0x02, 0x02}
	for i := 0; i < 2; i++ {
		params = append(params, 0x01, 0x00)
		params = append(params, byte(i), 0, 0, 0, 0, 0)
		params = append(params, 0x00)
		params = append(params, 0xd8)
	}
	event[1] = byte(len(params))
	event = append(event, params...)
	reports, err := ParseAdvertisingReports(event)
	if err != nil {
		t.Fatalf("ParseAdvertisingReports: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
}
