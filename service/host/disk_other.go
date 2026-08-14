//go:build !linux && !darwin

package host

// diskUsage is unavailable on unsupported platforms.
func diskUsage(path string) (totalBytes uint64, usedPercent float64, deviceID uint64, ok bool) {
	return 0, 0, 0, false
}
