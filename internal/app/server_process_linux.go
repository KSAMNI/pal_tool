//go:build linux

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func detectProcessByExecutablePath(binary string) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		procDir := filepath.Join("/proc", entry.Name())
		if executable, err := os.Readlink(filepath.Join(procDir, "exe")); err == nil && sameExecutablePath(executable, binary) {
			return true, nil
		}
		matches, err := linuxProcessCommandLineMatches(procDir, binary)
		if err != nil {
			continue
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}

func linuxProcessCommandLineMatches(procDir, binary string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(procDir, "cmdline"))
	if err != nil || len(data) == 0 {
		return false, err
	}
	cwd, _ := os.Readlink(filepath.Join(procDir, "cwd"))
	for _, raw := range strings.Split(string(data), "\x00") {
		arg := strings.TrimSpace(raw)
		if arg == "" {
			continue
		}
		if !filepath.IsAbs(arg) && cwd != "" {
			arg = filepath.Join(cwd, arg)
		}
		if sameExecutablePath(arg, binary) {
			return true, nil
		}
	}
	return false, nil
}
