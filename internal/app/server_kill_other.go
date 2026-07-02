//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// PalServer runs in its own process group so stop signals reach the whole
// tree (PalServer.sh wraps the actual server binary).
func serverSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func signalServerStop(process *os.Process) bool {
	if syscall.Kill(-process.Pid, syscall.SIGINT) == nil {
		return true
	}
	return process.Signal(os.Interrupt) == nil
}

func terminateServerProcessTree(process *os.Process) error {
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return process.Kill()
}

// The sh wrapper often dies before the shipping binary finishes its graceful
// shutdown, so waiting on the wrapper alone lets a restart race the old
// server for the game port.
func waitServerProcessTreeGone(process *os.Process, deadline time.Time) bool {
	for {
		if !serverProcessGroupAlive(process.Pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func serverProcessGroupAlive(pgid int) bool {
	if alive, ok := procProcessGroupAlive(pgid); ok {
		return alive
	}
	return syscall.Kill(-pgid, 0) == nil
}

// Scanning /proc lets zombies be ignored: when the panel runs as container
// PID 1, orphaned server processes reparent to it and are never reaped, yet
// kill(-pgid, 0) still counts them as alive.
func procProcessGroupAlive(pgid int) (alive bool, ok bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		state, group, parsed := parseProcStat(string(data))
		if !parsed || group != pgid {
			continue
		}
		if state != "Z" && state != "X" {
			return true, true
		}
	}
	return false, true
}

func parseProcStat(data string) (state string, pgrp int, ok bool) {
	end := strings.LastIndex(data, ")")
	if end < 0 {
		return "", 0, false
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) < 3 {
		return "", 0, false
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, false
	}
	return fields[0], group, true
}
