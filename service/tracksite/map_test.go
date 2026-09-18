package tracksite

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

const sampleLineTrack = `{
  "type": "Feature",
  "properties": {
    "name": "track-2026-08-13-0940-1015.geojson",
    "start_time": "2026-08-13T09:40:05Z",
    "end_time": "2026-08-13T10:15:20Z",
    "point_count": 4,
    "sample_interval_seconds": 5,
    "times": ["2026-08-13T09:40:05Z", "2026-08-13T09:40:10Z", "2026-08-13T09:40:15Z", "2026-08-13T09:40:20Z"]
  },
  "geometry": {
    "type": "LineString",
    "coordinates": [[0.854362, 51.065375], [0.8544, 51.0655], [0.85445, 51.0656], [0.8545, 51.0657]]
  }
}`

const sampleEventTrack = `{
  "type": "FeatureCollection",
  "features": [
    {"type": "Feature", "properties": {"name": "track-x.geojson", "start_time": "2026-08-13T09:40:05Z", "end_time": "2026-08-13T09:40:05Z", "point_count": 1, "sample_interval_seconds": 5, "times": ["2026-08-13T09:40:05Z"]}, "geometry": {"type": "Point", "coordinates": [0.854362, 51.065375]}},
    {"type": "Feature", "properties": {"event": "engine_on", "time": "2026-08-13T09:40:05Z"}, "geometry": {"type": "Point", "coordinates": [0.854362, 51.065375]}}
  ]
}`

func decodePNGRect(t *testing.T, data []byte, wantSize int) (image.Image, int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	rect := img.Bounds()
	if rect.Dx() != wantSize || rect.Dy() != wantSize {
		t.Fatalf("map size %dx%d, want %dx%d", rect.Dx(), rect.Dy(), wantSize, wantSize)
	}
	return img, 0
}

func countNonBackground(img image.Image, rect image.Rectangle) int {
	background := color.RGBA{0xF3, 0xF1, 0xEA, 0xFF}
	count := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if uint8(r>>8) != background.R || uint8(g>>8) != background.G || uint8(b>>8) != background.B {
				count++
			}
		}
	}
	return count
}

func TestRenderTrackProducesThumbnailSize(t *testing.T) {
	data, err := RenderTrack([]byte(sampleLineTrack), 1000)
	if err != nil {
		t.Fatalf("render 1000: %v", err)
	}
	img, _ := decodePNGRect(t, data, 1000)
	if count := countNonBackground(img, img.Bounds()); count == 0 {
		t.Fatal("map is blank; expected route pixels")
	}
}

func TestRenderTrackProducesLargeSize(t *testing.T) {
	data, err := RenderTrack([]byte(sampleLineTrack), 2000)
	if err != nil {
		t.Fatalf("render 2000: %v", err)
	}
	_, _ = decodePNGRect(t, data, 2000)
}

func TestRenderTrackDeterministic(t *testing.T) {
	first, err := RenderTrack([]byte(sampleLineTrack), 1000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderTrack([]byte(sampleLineTrack), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("map output is not deterministic")
	}
}

func TestRenderTrackSinglePointWithEvent(t *testing.T) {
	data, err := RenderTrack([]byte(sampleEventTrack), 1000)
	if err != nil {
		t.Fatalf("render single point track: %v", err)
	}
	img, _ := decodePNGRect(t, data, 1000)
	if count := countNonBackground(img, img.Bounds()); count == 0 {
		t.Fatal("map is blank; expected markers")
	}
}

func TestRenderTrackDegenerateSinglePoint(t *testing.T) {
	track := `{"type":"Feature","properties":{"name":"p.geojson"},"geometry":{"type":"Point","coordinates":[0.854362,51.065375]}}`
	data, err := RenderTrack([]byte(track), 1000)
	if err != nil {
		t.Fatalf("render degenerate track: %v", err)
	}
	_, _ = decodePNGRect(t, data, 1000)
}

func TestRenderTrackRejectsBadInput(t *testing.T) {
	if _, err := RenderTrack([]byte(`{not json`), 1000); err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	if _, err := RenderTrack([]byte(sampleLineTrack), 0); err == nil {
		t.Fatal("expected error for size 0")
	}
	if _, err := RenderTrack([]byte(`{"type":"FeatureCollection","features":[]}`), 1000); err == nil {
		t.Fatal("expected error for empty feature collection")
	}
}