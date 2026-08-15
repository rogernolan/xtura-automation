package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeVcgencmd(t *testing.T, output string) {
	t.Helper()
	tmp := t.TempDir()
	script := filepath.Join(tmp, "vcgencmd")
	content := "#!/bin/sh\nprintf '%s\n' '" + output + "'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestReadPowerStatusViaVcgencmd(t *testing.T) {
	t.Run("throttled warning", func(t *testing.T) {
		fakeVcgencmd(t, "throttled=0x50000")
		status := readPowerStatus()
		if status.Status != "warning" {
			t.Fatalf("status = %q, want warning", status.Status)
		}
		if status.UnderVoltage {
			t.Fatalf("under voltage = true, want false for current bits clear in 0x50000")
		}
		joined := strings.Join(status.OccurredSinceBoot, ",")
		for _, want := range []string{"under_voltage", "throttled"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("occurred since boot = %#v, missing %q", status.OccurredSinceBoot, want)
			}
		}
		if status.RawThrottle != "0x50000" {
			t.Fatalf("raw throttle = %q, want 0x50000", status.RawThrottle)
		}
	})

	t.Run("no throttle", func(t *testing.T) {
		fakeVcgencmd(t, "throttled=0x0")
		status := readPowerStatus()
		if status.Status != "ok" {
			t.Fatalf("status = %q, want ok", status.Status)
		}
		if status.UnderVoltage || status.Throttled || status.FrequencyCapped || status.SoftTempLimit {
			t.Fatalf("unexpected current flags: %#v", status)
		}
		if len(status.OccurredSinceBoot) != 0 {
			t.Fatalf("occurred since boot = %#v, want empty", status.OccurredSinceBoot)
		}
	})
}

func TestReadDiskUsage(t *testing.T) {
	entries := readDiskUsage()
	mounts := map[string]bool{}
	for i, entry := range entries {
		if entry.TotalBytes <= 0 {
			t.Fatalf("entry %d (%s) total = %d, want > 0", i, entry.Mount, entry.TotalBytes)
		}
		if entry.UsedPercent < 0 || entry.UsedPercent > 100 {
			t.Fatalf("entry %d (%s) used percent = %v, want within 0..100", i, entry.Mount, entry.UsedPercent)
		}
		if mounts[entry.Mount] {
			t.Fatalf("duplicate mount %q", entry.Mount)
		}
		mounts[entry.Mount] = true
	}
	if len(entries) > 0 && entries[0].Mount != "/" {
		t.Fatalf("first mount = %q, want /", entries[0].Mount)
	}
}
