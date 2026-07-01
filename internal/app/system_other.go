//go:build !windows && !linux

package app

import "errors"

func platformCPUPercent() (float64, error) {
	return 0, errors.New("cpu usage is not implemented on this platform")
}

func platformMemory() (systemMemory, error) {
	return systemMemory{}, errors.New("memory usage is not implemented on this platform")
}

func platformDisk(path string) (systemDisk, error) {
	return systemDisk{Path: path}, errors.New("disk usage is not implemented on this platform")
}
