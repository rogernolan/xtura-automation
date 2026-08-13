package location

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestFixMarshalOmitsNilAltitude(t *testing.T) {
	data, err := json.Marshal(Fix{Latitude: 51.5, Longitude: -0.1246})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("altitude")) {
		t.Fatalf("altitude should be omitted when nil: %s", data)
	}
}

func TestFixAltitudeJSONRoundTrip(t *testing.T) {
	alt := 7.5
	fix := Fix{Latitude: 51.5, Longitude: -0.1246, Altitude: &alt}
	data, err := json.Marshal(fix)
	if err != nil {
		t.Fatal(err)
	}
	var got Fix
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Altitude == nil {
		t.Fatal("expected altitude")
	}
	if *got.Altitude != 7.5 {
		t.Fatalf("got altitude %f", *got.Altitude)
	}
}

func TestStateMarshalOmitsNilAltitude(t *testing.T) {
	data, err := json.Marshal(State{Known: true, Latitude: 51.5, Longitude: -0.1246})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("altitude")) {
		t.Fatalf("altitude should be omitted when nil: %s", data)
	}
}

func TestStateAltitudeJSONRoundTrip(t *testing.T) {
	alt := 7.0
	state := State{Known: true, Latitude: 51.5, Longitude: -0.1246, Altitude: &alt}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var got State
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Altitude == nil {
		t.Fatal("expected altitude")
	}
	if *got.Altitude != 7.0 {
		t.Fatalf("got altitude %f", *got.Altitude)
	}
}
