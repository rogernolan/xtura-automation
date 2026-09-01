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

// MigrateLegacyTracks combines legacy timestamp-named tracks from the same UTC
// day into one human-readable track. Source files are renamed with a .legacy
// suffix after the combined file is written. With dryRun set, it only reports
// what would be changed.
func MigrateLegacyTracks(dir string, dryRun bool) (MigrationReport, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MigrationReport{}, nil
		}
		return MigrationReport{}, fmt.Errorf("list tracks: %w", err)
	}
	groups := make(map[string][]legacyTrack)
	for _, entry := range entries {
		match := legacyTrackPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return MigrationReport{}, fmt.Errorf("read legacy track %s: %w", entry.Name(), err)
		}
		var feature trackFeature
		if err := json.Unmarshal(data, &feature); err != nil {
			return MigrationReport{}, fmt.Errorf("decode legacy track %s: %w", entry.Name(), err)
		}
		times, points, ok := parseTrackFile(data)
		if !ok || len(times) == 0 {
			return MigrationReport{}, fmt.Errorf("invalid legacy track %s", entry.Name())
		}
		groups[match[1]] = append(groups[match[1]], legacyTrack{name: entry.Name(), feature: feature, times: times, points: points})
	}

	report := MigrationReport{}
	for day, tracks := range groups {
		sort.Slice(tracks, func(i, j int) bool { return tracks[i].times[0].Before(tracks[j].times[0]) })
		combined := &activeTrack{}
		for _, track := range tracks {
			combined.times = append(combined.times, track.times...)
			combined.points = append(combined.points, track.points...)
		}
		start, end := combined.times[0], combined.times[len(combined.times)-1]
		targetName := humanTrackName(start, end, "")
		target := filepath.Join(dir, targetName)
		for _, track := range tracks {
			if track.name == targetName {
				return report, fmt.Errorf("migration target already contains legacy file %s", targetName)
			}
		}
		if fileExists(target) {
			return report, fmt.Errorf("migration target already exists: %s", targetName)
		}
		report.Days++
		report.Files += len(tracks)
		report.Points += len(combined.points)
		if dryRun {
			continue
		}
		combined.name = targetName
		data, err := json.MarshalIndent(buildFeature(combined, Settings{SampleInterval: time.Duration(tracks[0].feature.Properties.SampleIntervalSeconds * float64(time.Second))}), "", "  ")
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
		for _, track := range tracks {
			if err := os.Rename(filepath.Join(dir, track.name), filepath.Join(dir, track.name+".legacy")); err != nil {
				return report, fmt.Errorf("archive legacy track %s: %w", track.name, err)
			}
		}
	}
	return report, nil
}

type legacyTrack struct {
	name    string
	feature trackFeature
	times   []time.Time
	points  [][]float64
}
