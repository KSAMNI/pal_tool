package app

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type systemSummary struct {
	CollectedAt string       `json:"collected_at"`
	CPU         systemCPU    `json:"cpu"`
	Memory      systemMemory `json:"memory"`
	Disk        systemDisk   `json:"disk"`
	Errors      []string     `json:"errors,omitempty"`
}

type systemCPU struct {
	Cores        int      `json:"cores"`
	UsagePercent *float64 `json:"usage_percent,omitempty"`
}

type systemMemory struct {
	TotalBytes  uint64   `json:"total_bytes"`
	UsedBytes   uint64   `json:"used_bytes"`
	FreeBytes   uint64   `json:"free_bytes"`
	UsedPercent *float64 `json:"used_percent,omitempty"`
}

type systemDisk struct {
	Path        string   `json:"path"`
	TotalBytes  uint64   `json:"total_bytes"`
	UsedBytes   uint64   `json:"used_bytes"`
	FreeBytes   uint64   `json:"free_bytes"`
	UsedPercent *float64 `json:"used_percent,omitempty"`
}

func (a *App) currentSystemSummary(settings settingsPayload) systemSummary {
	diskPath := systemDiskPath(settings.PalServerPath, a.dataDir)
	summary := systemSummary{
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		CPU: systemCPU{
			Cores: runtime.NumCPU(),
		},
		Disk: systemDisk{
			Path: diskPath,
		},
	}

	if percent, err := platformCPUPercent(); err == nil {
		summary.CPU.UsagePercent = percentPtr(percent)
	} else {
		summary.Errors = append(summary.Errors, "cpu: "+err.Error())
	}
	if memory, err := platformMemory(); err == nil {
		summary.Memory = memory
	} else {
		summary.Errors = append(summary.Errors, "memory: "+err.Error())
	}
	if disk, err := platformDisk(diskPath); err == nil {
		summary.Disk = disk
	} else {
		summary.Errors = append(summary.Errors, "disk: "+err.Error())
	}
	return summary
}

func systemDiskPath(serverPath, dataDir string) string {
	for _, candidate := range []string{strings.TrimSpace(serverPath), strings.TrimSpace(dataDir), "."} {
		if candidate == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil {
			if !info.IsDir() {
				abs = filepath.Dir(abs)
			}
			return abs
		}
	}
	return "."
}

func percentPtr(value float64) *float64 {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	rounded := math.Round(value*10) / 10
	return &rounded
}

func fillMemoryPercent(memory systemMemory) systemMemory {
	if memory.TotalBytes > 0 {
		used := memory.TotalBytes - memory.FreeBytes
		memory.UsedBytes = used
		memory.UsedPercent = percentPtr(float64(used) * 100 / float64(memory.TotalBytes))
	}
	return memory
}

func fillDiskPercent(disk systemDisk) systemDisk {
	if disk.TotalBytes > 0 {
		used := disk.TotalBytes - disk.FreeBytes
		disk.UsedBytes = used
		disk.UsedPercent = percentPtr(float64(used) * 100 / float64(disk.TotalBytes))
	}
	return disk
}
