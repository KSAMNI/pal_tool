//go:build !windows

package app

import (
	"os"
	"syscall"
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
