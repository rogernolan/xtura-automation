package switchbot

// Payload is a decoded SwitchBot thermometer advertisement.
type Payload struct {
	DevType  byte
	Temp     float64
	HasTemp  bool
	Humidity *float64
	Battery  *int
}

const (
	devTypeMeter    = 0x54
	devTypeMeterAdd = 0x74
	devTypeOutdoor  = 0x77
)

func devTypeOf(serviceData []byte) byte {
	if len(serviceData) == 0 {
		return 0
	}
	return serviceData[0] & 0x7f
}

// Decode decodes a SwitchBot thermometer reading from advertising elements.
// It returns ok=false for devices that do not look like a SwitchBot
// thermometer. Payloads may be partial (battery only, or temperature only)
// because WoSensorTHO alternates service-data and manufacturer-data
// advertisements.
func Decode(elements []AD) (Payload, bool) {
	serviceData, ok := serviceData16(elements, switchBotServiceUUID)
	if !ok {
		return Payload{}, false
	}
	switch devTypeOf(serviceData) {
	case devTypeMeter, devTypeMeterAdd:
		return decodeMeter(serviceData)
	case devTypeOutdoor:
		return decodeOutdoor(elements, serviceData)
	default:
		return Payload{}, false
	}
}

// decodeMeter decodes WoSensorTH (Meter). Layout of the 0xFD3D service data
// payload, confirmed against OpenWonderLabs meter.md:
//
//	[0] device type (0x54/0x74)
//	[1] group/status flags
//	[2] battery percent
//	[3] high nibble alert flags, low nibble temperature decimals
//	[4] bit 7 temperature sign, low 7 bits temperature integer
//	[5] bit 7 temperature scale flag, low 7 bits humidity percent
func decodeMeter(serviceData []byte) (Payload, bool) {
	if len(serviceData) < 6 {
		return Payload{}, false
	}
	battery := int(serviceData[2] & 0x7f)
	tempInt := int(serviceData[4] & 0x7f)
	tempDec := int(serviceData[3] & 0x0f)
	temp := float64(tempInt*10+tempDec) / 10
	if serviceData[4]&0x80 == 0 {
		temp = -temp
	}
	humidity := float64(serviceData[5] & 0x7f)
	return Payload{
		DevType:  serviceData[0],
		Temp:     temp,
		HasTemp:  true,
		Humidity: &humidity,
		Battery:  &battery,
	}, true
}

// decodeOutdoor decodes WoSensorTH2/WoSensorTHO (Outdoor). The 0xFD3D service
// data carries the device type and battery; temperature and humidity ride in
// the 0x0969 manufacturer data. manufacturerData strips the 2-byte company id,
// so the offsets below are relative to that stripped payload:
//
//	[0..5]  MAC
//	[6..7]  status/reserved
//	[8]     high nibble temperature decimals
//	[9]     bit 7 temperature sign, low 7 bits temperature integer
//	[10]    humidity percent (bit 7 = sign for sub-zero humidity)
//
// Battery is the last byte of the service data payload masked to 7 bits.
// Matches real captures (e.g. 3dfd770041 -> 65%, 3dfd7700e4 -> 100%) and the
// offsets used by Home Assistant Community ble_monitor (issue #1204).
func decodeOutdoor(elements []AD, serviceData []byte) (Payload, bool) {
	payload := Payload{DevType: devTypeOutdoor}
	if len(serviceData) >= 3 {
		battery := int(serviceData[len(serviceData)-1] & 0x7f)
		payload.Battery = &battery
	}
	mfr, ok := manufacturerData(elements, switchBotCompanyID)
	// manufacturerData strips the 2-byte company id, so the documented
	// offsets [10],[11],[12] (on the full payload) become [8],[9],[10].
	if !ok || len(mfr) < 11 {
		return payload, true
	}
	temp := float64(mfr[8]&0x0f)*0.1 + float64(mfr[9]&0x7f)
	if mfr[9]&0x80 == 0 {
		temp = -temp
	}
	humidity := float64(mfr[10] & 0x7f)
	payload.Temp = temp
	payload.HasTemp = true
	payload.Humidity = &humidity
	return payload, true
}
