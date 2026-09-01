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

func writeLegacyCollection(t *testing.T, dir, name string, times []string, coordinates [][]float64, eventTime string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type": "FeatureCollection",
		"features": []any{
			map[string]any{
				"type":       "Feature",
				"properties": map[string]any{"name": name, "times": times, "sample_interval_seconds": 5},
				"geometry":   map[string]any{"type": "LineString", "coordinates": coordinates},
			},
			map[string]any{
				"type":       "Feature",
				"properties": map[string]any{"event": "engine_off", "time": eventTime},
				"geometry":   map[string]any{"type": "Point", "coordinates": coordinates[len(coordinates)-1]},
			},
		},
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

func TestMigrateLegacyTracksReadsCollectionsAndSplitsSamplesByUTCDate(t *testing.T) {
	dir := t.TempDir()
	name := "track-20260813T235500Z.geojson"
	writeLegacyCollection(t, dir, name,
		[]string{"2026-08-13T23:59:55Z", "2026-08-14T00:00:05Z"},
		[][]float64{{1, 2}, {3, 4}}, "2026-08-14T00:00:05Z")

	report, err := tracking.MigrateLegacyTracks(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Days != 2 || report.Files != 1 || report.Points != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, name := range []string{
		"track-2026-08-13-2359-2359.geojson",
		"track-2026-08-14-0000-0000.geojson",
	} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Type     string            `json:"type"`
			Features []json.RawMessage `json:"features"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if name == "track-2026-08-14-0000-0000.geojson" {
			if document.Type != "FeatureCollection" || len(document.Features) != 2 {
				t.Fatalf("migrated %s = %s", name, data)
			}
		} else if document.Type != "Feature" {
			t.Fatalf("migrated %s = %s", name, data)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, name+".legacy")); err != nil {
		t.Fatalf("source was not archived: %v", err)
	}
}
