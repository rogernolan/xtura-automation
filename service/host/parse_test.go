package host

import "testing"

func TestParseLoadAvg(t *testing.T) {
	got := parseLoadAvg("0.50 0.35 0.20 2/123 4567\n")
	want := [3]float64{0.50, 0.35, 0.20}
	if got != want {
		t.Fatalf("parseLoadAvg = %v, want %v", got, want)
	}
}

func TestParseLoadAvgUnavailable(t *testing.T) {
	if got := parseLoadAvg(""); got != [3]float64{} {
		t.Fatalf("empty parseLoadAvg = %v, want zero", got)
	}
	if got := parseLoadAvg("not a number"); got != [3]float64{} {
		t.Fatalf("junk parseLoadAvg = %v, want zero", got)
	}
}

func TestParseUptime(t *testing.T) {
	if got := parseUptime("123456.78 654321.00\n"); got != 123456 {
		t.Fatalf("parseUptime = %d, want 123456", got)
	}
	if got := parseUptime(""); got != 0 {
		t.Fatalf("empty parseUptime = %d, want 0", got)
	}
	if got := parseUptime("abc"); got != 0 {
		t.Fatalf("junk parseUptime = %d, want 0", got)
	}
	if got := parseUptime("-5.0"); got != 0 {
		t.Fatalf("negative parseUptime = %d, want 0", got)
	}
}

func TestParseMeminfo(t *testing.T) {
	info := parseMeminfo("MemTotal:        518000 kB\nMemFree:          100000 kB\nMemAvailable:     300000 kB\n")
	if info.totalKB != 518000 || info.availableKB != 300000 {
		t.Fatalf("parseMeminfo = %#v", info)
	}
}

func TestParseMeminfoUnavailable(t *testing.T) {
	info := parseMeminfo("")
	if info.totalKB != 0 || info.availableKB != 0 {
		t.Fatalf("empty parseMeminfo = %#v", info)
	}
}

func TestMemoryFromMeminfo(t *testing.T) {
	memory := memoryFromMeminfo(memoryInfo{totalKB: 1000, availableKB: 400})
	if memory.TotalBytes != 1024000 || memory.AvailableBytes != 409600 {
		t.Fatalf("memory = %#v", memory)
	}
	if memory.UsedPercent < 59.9 || memory.UsedPercent > 60.1 {
		t.Fatalf("used percent = %v, want ~60", memory.UsedPercent)
	}
	if zero := memoryFromMeminfo(memoryInfo{}); zero.UsedPercent != 0 {
		t.Fatalf("zero memory used percent = %v", zero.UsedPercent)
	}
}

func TestParseCpuinfo(t *testing.T) {
	data := "processor\t: 0\nHardware\t: BCM2835\nModel\t: Raspberry Pi Zero 2 W Rev 1.0\nprocessor\t: 1\n"
	model, cores := parseCpuinfo(data)
	if model != "Raspberry Pi Zero 2 W Rev 1.0" || cores != 2 {
		t.Fatalf("parseCpuinfo = %q, %d", model, cores)
	}
}

func TestParseCpuinfoHardwareFallback(t *testing.T) {
	model, cores := parseCpuinfo("Hardware : BCM2835\nprocessor : 0\n")
	if model != "BCM2835" || cores != 1 {
		t.Fatalf("parseCpuinfo = %q, %d", model, cores)
	}
}

func TestParseCpuinfoUnavailable(t *testing.T) {
	model, cores := parseCpuinfo("")
	if model != "" || cores != 0 {
		t.Fatalf("empty parseCpuinfo = %q, %d", model, cores)
	}
}

func TestDecodeThrottled(t *testing.T) {
	current, latched, ok := decodeThrottled("0x10000")
	if !ok {
		t.Fatal("decodeThrottled reported not ok for 0x10000")
	}
	if current.underVoltage || current.frequencyCapped || current.throttled || current.softTempLimit {
		t.Fatalf("unexpected current flags: %#v", current)
	}
	if !latched.underVoltage || latched.frequencyCapped || latched.throttled || latched.softTempLimit {
		t.Fatalf("unexpected latched flags: %#v", latched)
	}
}

func TestDecodeThrottledCurrentBits(t *testing.T) {
	current, latched, ok := decodeThrottled("0x7")
	if !ok {
		t.Fatal("decodeThrottled reported not ok for 0x7")
	}
	if !current.underVoltage || !current.frequencyCapped || !current.throttled || current.softTempLimit {
		t.Fatalf("unexpected current flags: %#v", current)
	}
	if latched.underVoltage || latched.frequencyCapped || latched.throttled || latched.softTempLimit {
		t.Fatalf("unexpected latched flags: %#v", latched)
	}
}

func TestDecodeThrottledSoftTempLimit(t *testing.T) {
	_, latched, ok := decodeThrottled("0x80000")
	if !ok {
		t.Fatal("decodeThrottled reported not ok for 0x80000")
	}
	if !latched.softTempLimit || latched.underVoltage || latched.throttled || latched.frequencyCapped {
		t.Fatalf("unexpected latched flags: %#v", latched)
	}
}

func TestDecodeThrottledZero(t *testing.T) {
	current, latched, ok := decodeThrottled("0x0")
	if !ok {
		t.Fatal("decodeThrottled reported not ok for 0x0")
	}
	if current != (throttledFlags{}) || latched != (throttledFlags{}) {
		t.Fatalf("zero decode = %#v, %#v", current, latched)
	}
}

func TestDecodeThrottledMalformed(t *testing.T) {
	for _, raw := range []string{"", "0xzz", "throttled=", "   "} {
		if _, _, ok := decodeThrottled(raw); ok {
			t.Fatalf("decodeThrottled(%q) reported ok", raw)
		}
	}
}

func TestBuildPowerStatus(t *testing.T) {
	ok := buildPowerStatus(throttledFlags{}, throttledFlags{}, "0x0")
	if ok.Status != "ok" {
		t.Fatalf("ok status = %q", ok.Status)
	}

	current := throttledFlags{underVoltage: true}
	latched := throttledFlags{throttled: true, softTempLimit: true}
	warn := buildPowerStatus(current, latched, "0x0")
	if warn.Status != "warning" {
		t.Fatalf("warn status = %q", warn.Status)
	}
	if !warn.UnderVoltage || warn.Throttled || warn.FrequencyCapped || warn.SoftTempLimit {
		t.Fatalf("warn current flags = %#v", warn)
	}
	if len(warn.OccurredSinceBoot) != 2 || warn.OccurredSinceBoot[0] != "throttled" || warn.OccurredSinceBoot[1] != "soft_temp_limit" {
		t.Fatalf("occurred since boot = %#v", warn.OccurredSinceBoot)
	}
}
