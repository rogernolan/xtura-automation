package tracking

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var legacyTrackPattern = regexp.MustCompile(`^track-(\d{8})T\d{6}Z\.geojson$`)

// MigrationReport describes legacy files found or migrated by
// MigrateLegacyTracks.
type MigrationReport struct {
	Days   int
	Files  int
	Points int
}

// MigrateLegacyTracks combines legacy timestamp-named tracks into one track
// per UTC day. Source files are renamed with a .legacy suffix after the
// combined files are written. With dryRun set, it only reports what would be
// changed.
func MigrateLegacyTracks(dir string, dryRun bool) (MigrationReport, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MigrationReport{}, nil
		}
		return MigrationReport{}, fmt.Errorf("list tracks: %w", err)
	}
	legacy := make([]legacyTrack, 0)
	for _, entry := range entries {
		if legacyTrackPattern.FindStringSubmatch(entry.Name()) == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return MigrationReport{}, fmt.Errorf("read legacy track %s: %w", entry.Name(), err)
		}
		track, ok := parseLegacyTrack(data)
		if !ok || (len(track.times) == 0 && len(track.events) == 0) {
			return MigrationReport{}, fmt.Errorf("invalid legacy track %s", entry.Name())
		}
		track.name = entry.Name()
		legacy = append(legacy, track)
	}

	groups := make(map[string]*activeTrack)
	for _, source := range legacy {
		for i, at := range source.times {
			day := at.UTC().Format("20060102")
			group := groups[day]
			if group == nil {
				group = &activeTrack{}
				groups[day] = group
			}
			group.times = append(group.times, at)
			group.points = append(group.points, source.points[i])
		}
		for _, event := range source.events {
			day := event.Time.UTC().Format("20060102")
			group := groups[day]
			if group == nil {
				group = &activeTrack{}
				groups[day] = group
			}
			group.events = append(group.events, event)
		}
	}

	report := MigrationReport{Files: len(legacy)}
	for _, source := range legacy {
		report.Points += len(source.points)
	}
	days := make([]string, 0, len(groups))
	for day := range groups {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		combined := groups[day]
		sortTrack(combined)
		start, end := trackActivityRange(combined)
		targetName := humanTrackName(start, end, "")
		target := filepath.Join(dir, targetName)
		if fileExists(target) {
			return report, fmt.Errorf("migration target already exists: %s", targetName)
		}
		report.Days++
		if dryRun {
			continue
		}
		combined.name = targetName
		data, err := json.MarshalIndent(buildMigratedGeoJSON(combined, migrationSettings(legacy)), "", "  ")
		if err != nil {
			return report, fmt.Errorf("encode migrated track %s: %w", day, err)
		}
		data = append(data, '\n')
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return report, fmt.Errorf("write migrated track %s: %w", day, err)
		}
		if err := os.Rename(tmp, target); err != nil {
			return report, fmt.Errorf("rename migrated track %s: %w", day, err)
		}
	}
	if dryRun {
		return report, nil
	}
	for _, source := range legacy {
		if err := os.Rename(filepath.Join(dir, source.name), filepath.Join(dir, source.name+".legacy")); err != nil {
			return report, fmt.Errorf("archive legacy track %s: %w", source.name, err)
		}
	}
	return report, nil
}

func buildMigratedGeoJSON(track *activeTrack, settings Settings) any {
	if len(track.events) == 0 && len(track.points) == 1 {
		feature := buildFeature(track, settings)
		return singlePointFeature{
			Type: "Feature", Properties: feature.Properties,
			Geometry: pointGeometry{Type: "Point", Coordinates: track.points[0]},
		}
	}
	return buildGeoJSON(track, settings)
}

type legacyTrack struct {
	name   string
	times  []time.Time
	points [][]float64
	events []trackEvent
}

func parseLegacyTrack(data []byte) (legacyTrack, bool) {
	if times, points, ok := parseTrackFile(data); ok {
		return legacyTrack{times: times, points: points}, true
	}
	var collection trackCollection
	if json.Unmarshal(data, &collection) != nil || collection.Type != "FeatureCollection" {
		return legacyTrack{}, false
	}
	track := legacyTrack{}
	for _, raw := range collection.Features {
		if times, points, ok := parseTrackFile(raw); ok {
			track.times, track.points = times, points
			continue
		}
		var event eventFeature
		if json.Unmarshal(raw, &event) == nil && event.Geometry.Type == "Point" && event.Properties.Event != "" {
			at, err := time.Parse(time.RFC3339, event.Properties.Time)
			if err == nil && (len(event.Geometry.Coordinates) == 2 || len(event.Geometry.Coordinates) == 3) {
				track.events = append(track.events, trackEvent{Type: event.Properties.Event, Time: at, Position: event.Geometry.Coordinates})
			}
			continue
		}
		var point singlePointFeature
		if json.Unmarshal(raw, &point) == nil && point.Geometry.Type == "Point" && len(point.Properties.Times) == 1 {
			at, err := time.Parse(time.RFC3339, point.Properties.Times[0])
			if err == nil && (len(point.Geometry.Coordinates) == 2 || len(point.Geometry.Coordinates) == 3) {
				track.times = []time.Time{at}
				track.points = [][]float64{point.Geometry.Coordinates}
			}
		}
	}
	return track, len(track.times) > 0 || len(track.events) > 0
}

func sortTrack(track *activeTrack) {
	type sample struct {
		at time.Time
		pt []float64
	}
	samples := make([]sample, len(track.times))
	for i := range track.times {
		samples[i] = sample{track.times[i], track.points[i]}
	}
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].at.Before(samples[j].at) })
	track.times = track.times[:0]
	track.points = track.points[:0]
	for _, item := range samples {
		track.times = append(track.times, item.at)
		track.points = append(track.points, item.pt)
	}
	sort.SliceStable(track.events, func(i, j int) bool { return track.events[i].Time.Before(track.events[j].Time) })
}

func trackActivityRange(track *activeTrack) (time.Time, time.Time) {
	start, end := time.Time{}, time.Time{}
	for _, at := range track.times {
		if start.IsZero() || at.Before(start) {
			start = at
		}
		if end.IsZero() || at.After(end) {
			end = at
		}
	}
	for _, event := range track.events {
		if start.IsZero() || event.Time.Before(start) {
			start = event.Time
		}
		if end.IsZero() || event.Time.After(end) {
			end = event.Time
		}
	}
	return start, end
}

func migrationSettings(tracks []legacyTrack) Settings {
	for _, track := range tracks {
		if len(track.times) >= 2 {
			return Settings{SampleInterval: track.times[1].Sub(track.times[0])}
		}
	}
	return Settings{SampleInterval: 5 * time.Second}
}
