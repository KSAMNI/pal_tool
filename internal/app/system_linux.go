//go:build linux

package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func platformCPUPercent() (float64, error) {
	first, err := readLinuxCPUTimes()
	if err != nil {
		return 0, err
	}
	time.Sleep(100 * time.Millisecond)
	second, err := readLinuxCPUTimes()
	if err != nil {
		return 0, err
	}
	total := second.total - first.total
	idle := second.idle - first.idle
	if total <= 0 {
		return 0, nil
	}
	return float64(total-idle) * 100 / float64(total), nil
}

type linuxCPUTimes struct {
	idle  uint64
	total uint64
}

func readLinuxCPUTimes() (linuxCPUTimes, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return linuxCPUTimes{}, err
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return linuxCPUTimes{}, fmt.Errorf("unexpected /proc/stat cpu line")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return linuxCPUTimes{}, err
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return linuxCPUTimes{idle: idle, total: total}, nil
}

func platformMemory() (systemMemory, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return systemMemory{}, err
	}
	unit := uint64(info.Unit)
	if unit == 0 {
		unit = 1
	}
	return fillMemoryPercent(systemMemory{
		TotalBytes: info.Totalram * unit,
		FreeBytes:  info.Freeram * unit,
	}), nil
}

func platformDisk(path string) (systemDisk, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return systemDisk{}, err
	}
	blockSize := uint64(stat.Bsize)
	return fillDiskPercent(systemDisk{
		Path:       path,
		TotalBytes: stat.Blocks * blockSize,
		FreeBytes:  stat.Bavail * blockSize,
	}), nil
}
