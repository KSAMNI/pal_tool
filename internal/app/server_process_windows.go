//go:build windows

package app

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func detectProcessByExecutablePath(binary string) (bool, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, nil
	}
	defer windows.CloseHandle(snapshot)

	targetName := filepath.Base(binary)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if !strings.EqualFold(name, targetName) {
			continue
		}
		imagePath, ok := windowsProcessImagePath(entry.ProcessID)
		if ok && sameExecutablePath(imagePath, binary) {
			return true, nil
		}
	}
	return false, nil
}

func windowsProcessImagePath(pid uint32) (string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", false
	}
	if size == 0 {
		return "", false
	}
	return windows.UTF16ToString(buffer[:size]), true
}
