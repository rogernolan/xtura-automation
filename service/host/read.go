package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// defaultRead samples host metrics. Every source is best-effort: an
// unreadable source contributes its zero value, never a whole-snapshot error.
func defaultRead() (Snapshot, error) {
	snapshot := Snapshot{
		Load:          parseLoadAvg(readFileBestEffort("/proc/loadavg")),
		Memory:        memoryFromMeminfo(parseMeminfo(readFileBestEffort("/proc/meminfo"))),
		UptimeSeconds: parseUptime(readFileBestEffort("/proc/uptime")),
		Disk:          readDiskUsage(),
		Power:         readPowerStatus(),
	}
	snapshot.Model, snapshot.Cores = parseCpuinfo(readFileBestEffort("/proc/cpuinfo"))
	if temperatureC, ok := readTemperatureC(); ok {
		snapshot.TemperatureC = &temperatureC
	}
	return snapshot, nil
}

func readFileBestEffort(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// readTemperatureC reads the SoC temperature in degrees Celsius.
func readTemperatureC() (float64, bool) {
	data := readFileBestEffort("/sys/class/thermal/thermal_zone0/temp")
	if data == "" {
		return 0, false
	}
	milli, err := strconv.ParseFloat(strings.TrimSpace(data), 64)
	if err != nil {
		return 0, false
	}
	return milli / 1000, true
}

// readDiskUsage reports root and /var/lib/xtura usage, deduplicated by device
// ID so a shared filesystem is listed once (root wins).
func readDiskUsage() []DiskUsage {
	var entries []DiskUsage
	seen := map[uint64]bool{}
	for _, path := range []string{"/", "/var/lib/xtura"} {
		total, usedPercent, deviceID, ok := diskUsage(path)
		if !ok || total == 0 {
			continue
		}
		if deviceID != 0 && seen[deviceID] {
			continue
		}
		if deviceID != 0 {
			seen[deviceID] = true
		}
		entries = append(entries, DiskUsage{Mount: path, TotalBytes: total, UsedPercent: usedPercent})
	}
	return entries
}

// readPowerStatus prefers vcgencmd get_throttled, falls back to the rpi_hwmon
// sysfs undervoltage alarms, and reports unavailable when neither works.
func readPowerStatus() PowerStatus {
	if raw, ok := runVcgencmdThrottled(); ok {
		if current, latched, ok := decodeThrottled(raw); ok {
			return buildPowerStatus(current, latched, raw)
		}
	}
	if undervoltage, known := readRpiHwmonUndervoltage(); known {
		current := throttledFlags{}
		if undervoltage {
			current.underVoltage = true
		}
		return buildPowerStatus(current, throttledFlags{}, "")
	}
	return PowerStatus{Status: "unavailable"}
}

// runVcgencmdThrottled runs vcgencmd and returns the get_throttled value.
func runVcgencmdThrottled() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "vcgencmd", "get_throttled").Output()
	if err != nil {
		return "", false
	}
	raw := strings.TrimSpace(string(output))
	if !strings.HasPrefix(raw, "throttled=") {
		return "", false
	}
	return strings.TrimPrefix(raw, "throttled="), true
}

// readRpiHwmonUndervoltage reads the rpi_hwmon undervoltage alarm files.
func readRpiHwmonUndervoltage() (undervoltage bool, known bool) {
	matches, err := filepath.Glob("/sys/devices/platform/soc/*/rpi_hwmon/hwmon/hwmon*/in*_lcrit_alarm")
	if err != nil {
		return false, false
	}
	for _, path := range matches {
		value := strings.TrimSpace(readFileBestEffort(path))
		if value == "1" {
			return true, true
		}
	}
	return false, len(matches) > 0
}
