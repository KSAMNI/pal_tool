package app

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	errExternalServerRunning       = errors.New("PalServer appears to be running outside this panel; stop it before using this operation")
	errServerRunningForModMutation = errors.New("PalServer is running; stop it before changing MOD files")
	errSettingsRetargetRunning     = errors.New("cannot change pal_server_path while the configured PalServer is running")
)

func (a *App) externalServerRunning(settings settingsPayload) (bool, string) {
	if a.isServerRunning() {
		return false, ""
	}
	if a.serverProcessDetector == nil || strings.TrimSpace(settings.PalServerPath) == "" {
		return false, ""
	}
	running, err := a.serverProcessDetector(settings)
	if err != nil {
		return false, "external PalServer detection failed: " + err.Error()
	}
	return running, ""
}

func (a *App) ensureNoExternalServerRunning(settings settingsPayload) error {
	running, _ := a.externalServerRunning(settings)
	if running {
		return errExternalServerRunning
	}
	return nil
}

func (a *App) ensureServerStoppedForModMutation(settings settingsPayload) error {
	if a.isServerRunning() {
		return errServerRunningForModMutation
	}
	return a.ensureNoExternalServerRunning(settings)
}

func (a *App) settingsRetargetsRunningServer(previous settingsPayload, nextPalServerPath string) bool {
	previousPath := strings.TrimSpace(previous.PalServerPath)
	nextPath := strings.TrimSpace(nextPalServerPath)
	if previousPath == "" || sameFilesystemPath(previousPath, nextPath) {
		return false
	}
	if a.isManagedServerRunning() {
		return true
	}
	running, _ := a.externalServerRunning(previous)
	return running
}

func sameFilesystemPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr == nil {
		a = aAbs
	}
	if bErr == nil {
		b = bAbs
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
