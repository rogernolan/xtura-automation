package host

import "strconv"
import "strings"

// parseLoadAvg extracts the 1/5/15-minute load averages from /proc/loadavg.
// Unparseable fields are left at their zero value.
func parseLoadAvg(data string) [3]float64 {
	var load [3]float64
	fields := strings.Fields(data)
	for i := 0; i < 3 && i < len(fields); i++ {
		if value, err := strconv.ParseFloat(fields[i], 64); err == nil {
			load[i] = value
		}
	}
	return load
}

// parseUptime returns the whole-second uptime from /proc/uptime.
func parseUptime(data string) uint64 {
	fields := strings.Fields(data)
	if len(fields) == 0 {
		return 0
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return uint64(seconds)
}

// memoryInfo holds the kilobytes read from /proc/meminfo.
type memoryInfo struct {
	totalKB     uint64
	availableKB uint64
}

// parseMeminfo extracts MemTotal and MemAvailable from /proc/meminfo (KiB).
func parseMeminfo(data string) memoryInfo {
	values := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value
	}
	return memoryInfo{totalKB: values["MemTotal"], availableKB: values["MemAvailable"]}
}

// memoryFromMeminfo converts KiB values to bytes and derives used percent.
func memoryFromMeminfo(info memoryInfo) Memory {
	total := info.totalKB * 1024
	available := info.availableKB * 1024
	out := Memory{TotalBytes: total, AvailableBytes: available}
	if total > 0 {
		out.UsedPercent = float64(total-available) / float64(total) * 100
	}
	return out
}

// parseCpuinfo extracts the SoC model (preferring the Model line, falling back
// to Hardware) and the number of processor lines from /proc/cpuinfo.
func parseCpuinfo(data string) (model string, cores int) {
	var hardware string
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "processor":
			cores++
		case "Model", "model name":
			if model == "" {
				model = value
			}
		case "Hardware":
			hardware = value
		}
	}
	if model == "" {
		model = hardware
	}
	return model, cores
}

// throttledFlags are the decoded get_throttled conditions. Current flags come
// from bits 0-3; latched (since boot) flags from bits 16-19.
type throttledFlags struct {
	underVoltage    bool
	frequencyCapped bool
	throttled       bool
	softTempLimit   bool
}

// decodeThrottled parses a vcgencmd get_throttled hex bitmask such as "0x50000".
func decodeThrottled(raw string) (current, latched throttledFlags, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return throttledFlags{}, throttledFlags{}, false
	}
	value, err := strconv.ParseUint(trimmed, 0, 32)
	if err != nil {
		return throttledFlags{}, throttledFlags{}, false
	}
	current = throttledFlags{
		underVoltage:    value&(1<<0) != 0,
		frequencyCapped: value&(1<<1) != 0,
		throttled:       value&(1<<2) != 0,
		softTempLimit:   value&(1<<3) != 0,
	}
	latched = throttledFlags{
		underVoltage:    value&(1<<16) != 0,
		frequencyCapped: value&(1<<17) != 0,
		throttled:       value&(1<<18) != 0,
		softTempLimit:   value&(1<<19) != 0,
	}
	return current, latched, true
}

// buildPowerStatus derives the wire PowerStatus from decoded flags.
func buildPowerStatus(current, latched throttledFlags, raw string) PowerStatus {
	out := PowerStatus{RawThrottle: raw}
	if current == (throttledFlags{}) && latched == (throttledFlags{}) {
		out.Status = "ok"
		return out
	}
	out.Status = "warning"
	out.UnderVoltage = current.underVoltage
	out.Throttled = current.throttled
	out.FrequencyCapped = current.frequencyCapped
	out.SoftTempLimit = current.softTempLimit
	for _, flag := range []struct {
		set   bool
		label string
	}{
		{latched.underVoltage, "under_voltage"},
		{latched.frequencyCapped, "frequency_capped"},
		{latched.throttled, "throttled"},
		{latched.softTempLimit, "soft_temp_limit"},
	} {
		if flag.set {
			out.OccurredSinceBoot = append(out.OccurredSinceBoot, flag.label)
		}
	}
	return out
}
