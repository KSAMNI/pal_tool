//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func serverSysProcAttr() *syscall.SysProcAttr {
	return nil
}

// Windows has no cross-console interrupt for detached children; graceful
// stops rely on the Palworld REST API instead.
func signalServerStop(*os.Process) bool {
	return false
}

// The PalServer.exe launcher waits for its shipping child, so once the
// tracked process exits the tree is gone; the force path uses taskkill /T.
func waitServerProcessTreeGone(*os.Process, time.Time) bool {
	return true
}

// PalServer.exe is only a launcher for PalServer-Win64-Shipping-Cmd.exe;
// killing the launcher alone leaves the actual game server running.
func terminateServerProcessTree(process *os.Process) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(process.Pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return fmt.Errorf("taskkill: %v: %s", err, detail)
		}
		return fmt.Errorf("taskkill: %w", err)
	}
	return nil
}
