package app

import (
	"path/filepath"
	"runtime"
	"strings"
)

func detectConfiguredServerProcess(settings settingsPayload) (bool, error) {
	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return false, nil
	}
	binary := palServerBinary(serverPath)
	if binary == "" || !fileExists(binary) {
		return false, nil
	}
	return detectProcessByExecutablePath(binary)
}

func sameExecutablePath(left, right string) bool {
	left = normalizeExecutablePath(left)
	right = normalizeExecutablePath(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func normalizeExecutablePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}
