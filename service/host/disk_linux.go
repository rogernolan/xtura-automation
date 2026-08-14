//go:build linux

package host

import "syscall"

// diskUsage returns total bytes, used percent, and a device ID for a path.
func diskUsage(path string) (totalBytes uint64, usedPercent float64, deviceID uint64, ok bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, false
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	if total == 0 {
		return 0, 0, 0, false
	}
	return total, float64(total-free) / float64(total) * 100, uint64(uint32(stat.Fsid.X__val[0]))<<32 | uint64(uint32(stat.Fsid.X__val[1])), true
}
