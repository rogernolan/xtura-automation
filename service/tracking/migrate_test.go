package tracking_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"empirebus-tests/service/tracking"
)

func writeLegacyTrack(t *testing.T, dir, name string, times []string, coordinates [][]float64) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type": "Feature",
		"properties": map[string]any{
			"name": name, "start_time": times[0], "end_time": times[len(times)-1],
			"point_count": len(coordinates), "sample_interval_seconds": 5, "times": times,
		},
		"geometry": map[string]any{"type": "LineString", "coordinates": coordinates},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyTracksCombinesDayAndArchivesSources(t *testing.T) {
	dir := t.TempDir()
	writeLegacyTrack(t, dir, "track-20260813T090000Z.geojson",
		[]string{"2026-08-13T09:00:05Z", "2026-08-13T09:00:10Z"}, [][]float64{{1, 2}, {3, 4}})
	writeLegacyTrack(t, dir, "track-20260813T110000Z.geojson",
		[]string{"2026-08-13T11:00:05Z", "2026-08-13T11:00:10Z"}, [][]float64{{5, 6}, {7, 8}})

	report, err := tracking.MigrateLegacyTracks(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Days != 1 || report.Files != 2 || report.Points != 4 {
		t.Fatalf("report = %+v", report)
	}
	target := filepath.Join(dir, "track-2026-08-13-0900-1100.geojson")
	var feature struct {
		Properties struct {
			Name  string   `json:"name"`
			Times []string `json:"times"`
		} `json:"properties"`
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &feature); err != nil {
		t.Fatal(err)
	}
	if feature.Properties.Name != filepath.Base(target) || len(feature.Properties.Times) != 4 || len(feature.Geometry.Coordinates) != 4 {
		t.Fatalf("migrated feature = %+v", feature)
	}
	for _, name := range []string{"track-20260813T090000Z.geojson", "track-20260813T110000Z.geojson"} {
		if _, err := os.Stat(filepath.Join(dir, name+".legacy")); err != nil {
			t.Fatalf("archived %s: %v", name, err)
		}
	}
}

func TestMigrateLegacyTracksDryRunDoesNotChangeFiles(t *testing.T) {
	dir := t.TempDir()
	name := "track-20260813T090000Z.geojson"
	writeLegacyTrack(t, dir, name, []string{"2026-08-13T09:00:05Z", "2026-08-13T09:00:10Z"}, [][]float64{{1, 2}, {3, 4}})
	if _, err := tracking.MigrateLegacyTracks(dir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "track-2026-08-13-0900-0900.geojson")); !os.IsNotExist(err) {
		t.Fatalf("dry run created target, err=%v", err)
	}
}
