//go:build windows

package app

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	errGetSystemTimes        = errors.New("GetSystemTimes is unavailable")
	errGlobalMemoryStatusEx  = errors.New("GlobalMemoryStatusEx is unavailable")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func platformCPUPercent() (float64, error) {
	first, err := getWindowsCPUTimes()
	if err != nil {
		return 0, err
	}
	time.Sleep(100 * time.Millisecond)
	second, err := getWindowsCPUTimes()
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

type windowsCPUTimes struct {
	idle  int64
	total int64
}

func getWindowsCPUTimes() (windowsCPUTimes, error) {
	var idle, kernel, user windows.Filetime
	r1, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		if err != syscall.Errno(0) {
			return windowsCPUTimes{}, err
		}
		return windowsCPUTimes{}, errGetSystemTimes
	}
	idleNS := idle.Nanoseconds()
	kernelNS := kernel.Nanoseconds()
	userNS := user.Nanoseconds()
	return windowsCPUTimes{idle: idleNS, total: kernelNS + userNS}, nil
}

func platformMemory() (systemMemory, error) {
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	r1, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if r1 == 0 {
		if err != syscall.Errno(0) {
			return systemMemory{}, err
		}
		return systemMemory{}, errGlobalMemoryStatusEx
	}
	return fillMemoryPercent(systemMemory{
		TotalBytes: status.TotalPhys,
		FreeBytes:  status.AvailPhys,
	}), nil
}

func platformDisk(path string) (systemDisk, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return systemDisk{}, err
	}
	var freeAvailable, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvailable, &total, &totalFree); err != nil {
		return systemDisk{}, err
	}
	return fillDiskPercent(systemDisk{
		Path:       path,
		TotalBytes: total,
		FreeBytes:  totalFree,
	}), nil
}
