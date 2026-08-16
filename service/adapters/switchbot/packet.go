package switchbot

import (
	"fmt"
	"strings"
)

// AD is a single advertising-data element.
type AD struct {
	Type byte
	Data []byte
}

const (
	adTypeServiceData16 = 0x16
	adTypeManufacturer  = 0xff

	switchBotServiceUUID = 0xfd3d
	switchBotCompanyID   = 0x0969
)

// ParseADElements parses AD structures from an advertising payload per the
// Core v5.x "Generic Access Profile" length/type format.
func ParseADElements(data []byte) ([]AD, error) {
	var elements []AD
	for i := 0; i < len(data); {
		length := int(data[i])
		if length == 0 {
			i++
			continue
		}
		end := i + 1 + length
		if end > len(data) {
			return elements, fmt.Errorf("AD element overruns payload")
		}
		elements = append(elements, AD{Type: data[i+1], Data: data[i+2 : end]})
		i = end
	}
	return elements, nil
}

// serviceData16 returns the payload of the first 0x16 AD element whose
// 16-bit UUID matches (payload excludes the UUID bytes).
func serviceData16(elements []AD, uuid uint16) ([]byte, bool) {
	for _, element := range elements {
		if element.Type != adTypeServiceData16 || len(element.Data) < 3 {
			continue
		}
		if uint16(element.Data[0])|uint16(element.Data[1])<<8 != uuid {
			continue
		}
		return element.Data[2:], true
	}
	return nil, false
}

// manufacturerData returns the payload of the first 0xff AD element whose
// company id matches (payload excludes the company id bytes).
func manufacturerData(elements []AD, companyID uint16) ([]byte, bool) {
	for _, element := range elements {
		if element.Type != adTypeManufacturer || len(element.Data) < 2 {
			continue
		}
		if uint16(element.Data[0])|uint16(element.Data[1])<<8 != companyID {
			continue
		}
		return element.Data[2:], true
	}
	return nil, false
}

// AdvertisingReport is one parsed LE advertising report.
type AdvertisingReport struct {
	MAC  string
	RSSI int
	Data []byte
}

// ParseAdvertisingReports parses LE advertising reports from an HCI LE meta
// event packet (subevent 0x02). The event must start at the event code byte.
func ParseAdvertisingReports(event []byte) ([]AdvertisingReport, error) {
	if len(event) < 3 {
		return nil, fmt.Errorf("truncated HCI event")
	}
	if event[0] != 0x3e {
		return nil, fmt.Errorf("not an LE meta event")
	}
	params := event[2 : 2+int(event[1])]
	if len(params) < 1 || params[0] != 0x02 {
		return nil, fmt.Errorf("not an advertising report subevent")
	}
	body := params[1:]
	if len(body) < 1 {
		return nil, fmt.Errorf("missing report count")
	}
	num := int(body[0])
	body = body[1:]
	reports := make([]AdvertisingReport, 0, num)
	for i := 0; i < num; i++ {
		if len(body) < 10 {
			return nil, fmt.Errorf("truncated report")
		}
		dataLen := int(body[8])
		if len(body) < 10+dataLen {
			return nil, fmt.Errorf("truncated report data")
		}
		address := body[2:8]
		rssi := int(int8(body[9+dataLen]))
		reports = append(reports, AdvertisingReport{
			MAC:  macString(address),
			RSSI: rssi,
			Data: body[9 : 9+dataLen],
		})
		body = body[10+dataLen:]
	}
	return reports, nil
}

// macString renders a big-endian MAC address string from the little-endian
// on-air address bytes.
func macString(address []byte) string {
	parts := make([]string, 0, 6)
	for i := len(address) - 1; i >= 0; i-- {
		parts = append(parts, fmt.Sprintf("%02x", address[i]))
	}
	return strings.Join(parts, ":")
}
