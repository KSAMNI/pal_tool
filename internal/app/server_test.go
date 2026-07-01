package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestServerControlRoutes(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}

	setup := map[string]string{"username": "admin", "password": "password123"}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", setup)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	for _, tc := range []struct {
		method  string
		path    string
		status  int
		confirm bool
	}{
		{method: http.MethodGet, path: "/api/server/status", status: http.StatusOK},
		{method: http.MethodGet, path: "/api/server/logs", status: http.StatusOK},
		{method: http.MethodGet, path: "/api/tasks", status: http.StatusOK},
		{method: http.MethodPost, path: "/api/server/install", status: http.StatusBadRequest},
		{method: http.MethodPost, path: "/api/server/update", status: http.StatusPreconditionRequired},
		{method: http.MethodPost, path: "/api/server/update", status: http.StatusBadRequest, confirm: true},
		{method: http.MethodPost, path: "/api/server/start", status: http.StatusBadRequest},
		{method: http.MethodPost, path: "/api/server/restart", status: http.StatusPreconditionRequired},
		{method: http.MethodPost, path: "/api/server/restart", status: http.StatusBadRequest, confirm: true},
		{method: http.MethodPost, path: "/api/server/stop", status: http.StatusPreconditionRequired},
		{method: http.MethodPost, path: "/api/server/stop", status: http.StatusConflict, confirm: true},
	} {
		resp := doJSONMaybeConfirmed(t, client, tc.method, server.URL+tc.path, nil, tc.confirm)
		if resp.StatusCode != tc.status {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.status)
		}
		resp.Body.Close()
	}
}

func TestUnsafeRESTRequestsRejectCrossOriginOrigin(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	server, client := newAuthenticatedTestServer(t, panel)

	resp := doJSONWithOrigin(t, client, http.MethodPost, server.URL+"/api/server/install", nil, "https://evil.example")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	resp.Body.Close()

	resp = doJSONWithOrigin(t, client, http.MethodPost, server.URL+"/api/server/install", nil, server.URL)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("same-origin POST status = %d, want route status %d", resp.StatusCode, http.StatusBadRequest)
	}
	resp.Body.Close()

	resp = doJSONWithOrigin(t, client, http.MethodPut, server.URL+"/api/settings", settingsPayload{}, "https://evil.example")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin PUT status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	resp.Body.Close()

	resp = doJSONWithOrigin(t, client, http.MethodGet, server.URL+"/api/server/status", nil, "https://evil.example")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cross-origin GET status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()
}

func TestSplitCommandLine(t *testing.T) {
	got, err := splitCommandLine(`-useperfthreads -workshopdir="C:\Pal Mods" '-custom value'`)
	if err != nil {
		t.Fatalf("splitCommandLine() error = %v", err)
	}
	want := []string{"-useperfthreads", `-workshopdir=C:\Pal Mods`, "-custom value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCommandLine() = %#v, want %#v", got, want)
	}
}

func TestServerStatusReportsExternalRunningProcess(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	settings := settingsPayload{PalServerPath: serverPath}
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		if settings.PalServerPath != serverPath {
			t.Fatalf("detector received PalServerPath %q, want %q", settings.PalServerPath, serverPath)
		}
		return true, nil
	}

	status := panel.currentServerStatus(settings)
	if !status.Running || status.ManagedRunning || !status.ExternalRunning {
		t.Fatalf("unexpected status for external PalServer: %#v", status)
	}
}

func TestServerStatusReportsOperationRunningSlot(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	release, err := panel.reserveTaskSlot()
	if err != nil {
		t.Fatalf("reserveTaskSlot() error = %v", err)
	}
	status := panel.currentServerStatus(settingsPayload{})
	if !status.OperationRunning {
		t.Fatalf("OperationRunning = false, want true while task slot is reserved")
	}

	release()
	status = panel.currentServerStatus(settingsPayload{})
	if status.OperationRunning {
		t.Fatalf("OperationRunning = true, want false after task slot release")
	}
}

func TestExternalRunningProcessBlocksStartAndUpdate(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return true, nil
	}

	if err := panel.startServerProcess(settingsPayload{PalServerPath: serverPath}); !errors.Is(err, errExternalServerRunning) {
		t.Fatalf("startServerProcess() error = %v, want errExternalServerRunning", err)
	}
	if _, err := panel.startSteamCMDTask("server_update"); !errors.Is(err, errExternalServerRunning) {
		t.Fatalf("startSteamCMDTask() error = %v, want errExternalServerRunning", err)
	}
}

func TestServerStartRejectsManagedRunningBeforeStartupBackup(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	writeBackupSource(t, serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	panel.serverMu.Lock()
	panel.serverCmd = exec.Command("PalServer-test")
	panel.serverMu.Unlock()
	t.Cleanup(func() {
		panel.serverMu.Lock()
		panel.serverCmd = nil
		panel.serverMu.Unlock()
	})

	if err := panel.startServerProcess(settingsPayload{PalServerPath: serverPath}); !errors.Is(err, errServerAlreadyRunning) {
		t.Fatalf("startServerProcess() error = %v, want errServerAlreadyRunning", err)
	}
	assertNoBackups(t, panel)
}

func TestServerStartRejectsMalformedArgsBeforeStartupBackup(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	writeBackupSource(t, serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	err = panel.startServerProcess(settingsPayload{
		PalServerPath:    serverPath,
		ServerLaunchArgs: `-workshopdir="C:\Pal Mods`,
	})
	if err == nil || !strings.Contains(err.Error(), "unterminated quote") {
		t.Fatalf("startServerProcess() error = %v, want unterminated quote", err)
	}
	assertNoBackups(t, panel)
}

func TestServerStartRejectsRunningTaskBeforeStartupBackup(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	writeBackupSource(t, serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	if err := panel.startServerProcess(settingsPayload{PalServerPath: serverPath}); !errors.Is(err, errTaskRunning) {
		t.Fatalf("startServerProcess() error = %v, want errTaskRunning", err)
	}
	assertNoBackups(t, panel)
	assertNoTasks(t, panel)
}

func TestServerStartCreatesStartupBackupAfterAdmission(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	if runtime.GOOS == "windows" {
		writeFakePalServerBinary(t, serverPath)
	} else {
		writeLongRunningFakePalServerBinary(t, serverPath)
	}
	writeBackupSource(t, serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	err = panel.startServerProcess(settingsPayload{PalServerPath: serverPath})
	if runtime.GOOS != "windows" && err != nil {
		t.Fatalf("startServerProcess() error = %v", err)
	}
	if err == nil {
		t.Cleanup(func() {
			if panel.isServerRunning() {
				_ = panel.stopServerProcess(2 * time.Second)
			}
		})
	}

	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	startupCount := 0
	for _, backup := range backups {
		if backup.Type == "startup" {
			startupCount++
			if !fileExists(backup.Path) {
				t.Fatalf("startup backup file missing: %#v", backup)
			}
		}
	}
	if startupCount != 1 {
		t.Fatalf("startup backup count = %d, backups=%#v", startupCount, backups)
	}
}

func TestAppCloseStopsManagedServerViaLifecycleHook(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	stopCalls := 0
	panel.serverMu.Lock()
	panel.serverCmd = exec.Command("PalServer")
	panel.serverDone = make(chan error, 1)
	panel.serverMu.Unlock()
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		stopCalls++
		panel.serverMu.Lock()
		panel.serverCmd = nil
		panel.serverDone = nil
		panel.serverMu.Unlock()
		return nil
	}

	if err := panel.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls = %d, want 1", stopCalls)
	}
	if panel.isServerRunning() {
		t.Fatal("server still marked running after Close")
	}
	if err := panel.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls after second Close = %d, want 1", stopCalls)
	}
}

func TestAppCloseStopsManagedServerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell PalServer fixture")
	}
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	serverPath := t.TempDir()
	writeInterruptibleFakePalServerBinary(t, serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}
	if err := panel.startServerProcess(settingsPayload{PalServerPath: serverPath}); err != nil {
		_ = panel.Close()
		t.Fatalf("startServerProcess() error = %v", err)
	}
	if !panel.isServerRunning() {
		_ = panel.Close()
		t.Fatal("server was not running after start")
	}
	if err := panel.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if panel.isServerRunning() {
		t.Fatal("managed server still running after Close")
	}
	if err := panel.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestServerRestartRejectsMalformedArgsBeforeStop(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "server_launch_args", `-workshopdir="C:\Pal Mods`)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	var stopCalls int32
	var startCalls int32
	panel.isServerRunningFunc = func() bool { return true }
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		atomic.AddInt32(&stopCalls, 1)
		return nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		atomic.AddInt32(&startCalls, 1)
		return nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/restart", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("restart status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if stopCalls != 0 || startCalls != 0 {
		t.Fatalf("calls stop/start = %d/%d, want 0/0", stopCalls, startCalls)
	}
}

func TestServerRestartRejectsExternalRuntimeBeforeStart(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return true, nil
	}

	var stopCalls int32
	var startCalls int32
	panel.isServerRunningFunc = func() bool { return false }
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		atomic.AddInt32(&stopCalls, 1)
		return nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		atomic.AddInt32(&startCalls, 1)
		return nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/restart", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("restart status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	if stopCalls != 0 || startCalls != 0 {
		t.Fatalf("calls stop/start = %d/%d, want 0/0", stopCalls, startCalls)
	}
}

func TestServerRestartRejectsRunningTaskBeforeStopOrStartupBackup(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	writeBackupSource(t, serverPath)
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	var stopCalls int32
	var startCalls int32
	panel.isServerRunningFunc = func() bool { return true }
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		atomic.AddInt32(&stopCalls, 1)
		return nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		atomic.AddInt32(&startCalls, 1)
		return nil
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/restart", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("restart status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	if stopCalls != 0 || startCalls != 0 {
		t.Fatalf("calls stop/start = %d/%d, want 0/0", stopCalls, startCalls)
	}
	assertNoBackups(t, panel)
	assertNoTasks(t, panel)
}

func TestServerRestartStopsThenStartsAfterPreflight(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	var stopCalls int32
	var startCalls int32
	panel.isServerRunningFunc = func() bool { return true }
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		atomic.AddInt32(&stopCalls, 1)
		return nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		if atomic.LoadInt32(&stopCalls) == 0 {
			return errors.New("PalServer restarted before stop completed")
		}
		if settings.PalServerPath != serverPath {
			return errors.New("restart received wrong PalServer path")
		}
		atomic.AddInt32(&startCalls, 1)
		return nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/restart", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restart status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if stopCalls != 1 || startCalls != 1 {
		t.Fatalf("calls stop/start = %d/%d, want 1/1", stopCalls, startCalls)
	}
}

func TestSteamCMDTaskRejectsRunningTaskBeforeBackupSideEffects(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	savePath := filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "world.txt")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		t.Fatalf("mkdir save dir: %v", err)
	}
	if err := os.WriteFile(savePath, []byte("save"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	if _, err := panel.startSteamCMDTask("server_update"); !errors.Is(err, errTaskRunning) {
		t.Fatalf("startSteamCMDTask() error = %v, want errTaskRunning", err)
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("backups created despite rejected task: %#v", backups)
	}
}

func TestSteamCMDTaskCreatesAndLogsPreUpdateBackup(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	savePath := filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "world.txt")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		t.Fatalf("mkdir save dir: %v", err)
	}
	if err := os.WriteFile(savePath, []byte("save"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}
	panel.commandRunner = func(cmd *exec.Cmd) error {
		return nil
	}

	task, err := panel.startSteamCMDTask("server_install")
	if err != nil {
		t.Fatalf("startSteamCMDTask() error = %v", err)
	}
	task = waitForTaskStatus(t, panel, task.ID, "success")
	if !strings.Contains(task.Log, "Pre-operation backup created:") {
		t.Fatalf("task log missing pre-operation backup: %s", task.Log)
	}
	if !strings.Contains(task.Log, "server_install completed") {
		t.Fatalf("task log missing completion: %s", task.Log)
	}

	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	preUpdateCount := 0
	for _, backup := range backups {
		if backup.Type == "pre_update" {
			preUpdateCount++
			if !fileExists(backup.Path) {
				t.Fatalf("pre_update backup file missing: %#v", backup)
			}
		}
	}
	if preUpdateCount != 1 {
		t.Fatalf("pre_update backup count = %d, backups=%#v", preUpdateCount, backups)
	}
}

func TestOperationTaskRejectsRunningTaskBeforeCreatingTask(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	called := false
	err = panel.runOperationTask("backup_manual", "Starting backup", "Backup completed", func(taskID int64) error {
		called = true
		return nil
	})
	if !errors.Is(err, errTaskRunning) {
		t.Fatalf("runOperationTask() error = %v, want errTaskRunning", err)
	}
	if called {
		t.Fatalf("operation callback ran despite active task")
	}
	assertNoTasks(t, panel)
}

func TestOperationTaskReservesSlotAgainstSteamCMDTask(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}

	err = panel.runOperationTask("backup_manual", "Starting backup", "Backup completed", func(taskID int64) error {
		_, err := panel.startSteamCMDTask("server_install")
		if !errors.Is(err, errTaskRunning) {
			return fmt.Errorf("startSteamCMDTask() error = %v, want errTaskRunning", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runOperationTask() error = %v", err)
	}
	assertNoTasksOfType(t, panel, "server_install")
}

func TestSteamCMDTaskReservesSlotAgainstOperationTask(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}
	errCh := make(chan error, 1)
	panel.commandRunner = func(cmd *exec.Cmd) error {
		err := panel.runOperationTask("backup_manual", "Starting backup", "Backup completed", func(taskID int64) error {
			return nil
		})
		if !errors.Is(err, errTaskRunning) {
			errCh <- fmt.Errorf("runOperationTask() error = %v, want errTaskRunning", err)
		} else {
			errCh <- nil
		}
		return nil
	}

	task, err := panel.startSteamCMDTask("server_install")
	if err != nil {
		t.Fatalf("startSteamCMDTask() error = %v", err)
	}
	task = waitForTaskStatus(t, panel, task.ID, "success")
	if !strings.Contains(task.Log, "server_install completed") {
		t.Fatalf("task log missing completion: %s", task.Log)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	assertNoTasksOfType(t, panel, "backup_manual")
}

func TestServerUpdateStopsRunningServerAndRestartsOnSuccess(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)

	var stopCalls int32
	var runCalls int32
	var startCalls int32
	panel.isServerRunningFunc = func() bool { return true }
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		atomic.AddInt32(&stopCalls, 1)
		return nil
	}
	panel.commandRunner = func(cmd *exec.Cmd) error {
		if atomic.LoadInt32(&stopCalls) == 0 {
			return errors.New("steamcmd ran before PalServer stopped")
		}
		if !strings.Contains(strings.Join(cmd.Args, " "), "+app_update 2394010 validate") {
			return errors.New("steamcmd args missing app_update 2394010 validate")
		}
		atomic.AddInt32(&runCalls, 1)
		return nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		if atomic.LoadInt32(&runCalls) == 0 {
			return errors.New("PalServer restarted before SteamCMD completed")
		}
		if settings.PalServerPath != serverPath {
			return errors.New("restart received wrong PalServer path")
		}
		atomic.AddInt32(&startCalls, 1)
		return nil
	}

	task, err := panel.startSteamCMDTask("server_update")
	if err != nil {
		t.Fatalf("startSteamCMDTask() error = %v", err)
	}
	task = waitForTaskStatus(t, panel, task.ID, "success")
	if stopCalls != 1 || runCalls != 1 || startCalls != 1 {
		t.Fatalf("calls stop/run/start = %d/%d/%d, want 1/1/1", stopCalls, runCalls, startCalls)
	}
	for _, want := range []string{"stopping before server_update", "Restarting PalServer after successful server_update", "server_update completed"} {
		if !strings.Contains(task.Log, want) {
			t.Fatalf("task log missing %q: %s", want, task.Log)
		}
	}
}

func TestServerUpdateLeavesServerStoppedAfterSteamCMDFailure(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	steamPath := filepath.Join(t.TempDir(), steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)

	var stopCalls int32
	var runCalls int32
	var startCalls int32
	panel.isServerRunningFunc = func() bool { return true }
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		atomic.AddInt32(&stopCalls, 1)
		return nil
	}
	panel.commandRunner = func(cmd *exec.Cmd) error {
		atomic.AddInt32(&runCalls, 1)
		return errors.New("steamcmd failed")
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		atomic.AddInt32(&startCalls, 1)
		return nil
	}

	task, err := panel.startSteamCMDTask("server_update")
	if err != nil {
		t.Fatalf("startSteamCMDTask() error = %v", err)
	}
	task = waitForTaskStatus(t, panel, task.ID, "failed")
	if stopCalls != 1 || runCalls != 1 || startCalls != 0 {
		t.Fatalf("calls stop/run/start = %d/%d/%d, want 1/1/0", stopCalls, runCalls, startCalls)
	}
	if !strings.Contains(task.Log, "steamcmd failed") {
		t.Fatalf("task log missing SteamCMD failure: %s", task.Log)
	}
	if strings.Contains(task.Log, "Restarting PalServer") {
		t.Fatalf("task log unexpectedly restarted PalServer after failure: %s", task.Log)
	}
}

func TestAppendTaskLogTruncatesToSizeLimit(t *testing.T) {
	setTaskLogLimit(t, 80)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	taskID, err := panel.createTask("log_limit")
	if err != nil {
		t.Fatalf("createTask() error = %v", err)
	}
	if err := panel.appendTaskLog(taskID, strings.Repeat("a", 60)+"\n"); err != nil {
		t.Fatalf("append first task log: %v", err)
	}
	if err := panel.appendTaskLog(taskID, strings.Repeat("b", 60)+"\n"); err != nil {
		t.Fatalf("append second task log: %v", err)
	}
	task, err := panel.getTask(taskID)
	if err != nil {
		t.Fatalf("getTask() error = %v", err)
	}
	if len(task.Log) > maxTaskLogBytes {
		t.Fatalf("task log length = %d, want <= %d", len(task.Log), maxTaskLogBytes)
	}
	if !strings.HasPrefix(task.Log, taskLogTruncatedMarker) {
		t.Fatalf("task log missing truncation marker: %q", task.Log)
	}
	if strings.Contains(task.Log, strings.Repeat("a", 20)) {
		t.Fatalf("task log retained old prefix after truncation: %q", task.Log)
	}
	if !strings.Contains(task.Log, strings.Repeat("b", 20)) {
		t.Fatalf("task log did not retain recent suffix: %q", task.Log)
	}
}

func TestAppendTaskLogTruncationPreservesUTF8(t *testing.T) {
	setTaskLogLimit(t, len(taskLogTruncatedMarker)+7)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	taskID, err := panel.createTask("log_utf8_limit")
	if err != nil {
		t.Fatalf("createTask() error = %v", err)
	}
	if err := panel.appendTaskLog(taskID, strings.Repeat("x", 50)+"旧旧旧🙂🙂🙂\n"); err != nil {
		t.Fatalf("append utf8 task log: %v", err)
	}
	task, err := panel.getTask(taskID)
	if err != nil {
		t.Fatalf("getTask() error = %v", err)
	}
	if len(task.Log) > maxTaskLogBytes {
		t.Fatalf("task log length = %d, want <= %d", len(task.Log), maxTaskLogBytes)
	}
	if !utf8.ValidString(task.Log) {
		t.Fatalf("task log is not valid UTF-8: %q", task.Log)
	}
	if !strings.HasPrefix(task.Log, taskLogTruncatedMarker) {
		t.Fatalf("task log missing truncation marker: %q", task.Log)
	}
}

func TestFinishedTaskRetentionKeepsNewestAndRunning(t *testing.T) {
	setFinishedTaskRetentionLimit(t, 2)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	runningID, err := panel.createTask("old_running")
	if err != nil {
		t.Fatalf("create running task: %v", err)
	}
	var finishedIDs []int64
	for i := 0; i < 4; i++ {
		taskID, err := panel.createTask(fmt.Sprintf("finished_%d", i))
		if err != nil {
			t.Fatalf("create finished task %d: %v", i, err)
		}
		if err := panel.finishTask(taskID, "success"); err != nil {
			t.Fatalf("finish task %d: %v", i, err)
		}
		finishedIDs = append(finishedIDs, taskID)
	}

	tasks, err := panel.listTasks(20)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	seen := make(map[int64]taskRecord)
	for _, task := range tasks {
		seen[task.ID] = task
	}
	if _, ok := seen[runningID]; !ok {
		t.Fatalf("running task %d was pruned; tasks = %#v", runningID, tasks)
	}
	for _, taskID := range finishedIDs[:2] {
		if _, ok := seen[taskID]; ok {
			t.Fatalf("old finished task %d was retained; tasks = %#v", taskID, tasks)
		}
	}
	for _, taskID := range finishedIDs[2:] {
		if _, ok := seen[taskID]; !ok {
			t.Fatalf("new finished task %d was pruned; tasks = %#v", taskID, tasks)
		}
	}
	if len(tasks) != 3 {
		t.Fatalf("task count = %d, want 3: %#v", len(tasks), tasks)
	}
}

func TestNewPrunesFinishedTaskHistory(t *testing.T) {
	previous := maxFinishedTaskRecords
	maxFinishedTaskRecords = -1
	t.Cleanup(func() {
		maxFinishedTaskRecords = previous
	})
	dataDir := t.TempDir()
	panel, err := New(dataDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for i := 0; i < 4; i++ {
		taskID, err := panel.createTask(fmt.Sprintf("startup_finished_%d", i))
		if err != nil {
			t.Fatalf("create task %d: %v", i, err)
		}
		if err := panel.finishTask(taskID, "success"); err != nil {
			t.Fatalf("finish task %d: %v", i, err)
		}
	}
	if err := panel.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	maxFinishedTaskRecords = 2
	panel, err = New(dataDir)
	if err != nil {
		t.Fatalf("New() after existing task history error = %v", err)
	}
	defer panel.Close()

	tasks, err := panel.listTasks(20)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("startup-pruned task count = %d, want 2: %#v", len(tasks), tasks)
	}
	for _, task := range tasks {
		if task.ID <= 2 {
			t.Fatalf("old task survived startup prune: %#v in %#v", task, tasks)
		}
	}
}

func TestAppendServerLogTruncatesLongEntries(t *testing.T) {
	setServerLogMessageLimit(t, 40)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	panel.appendServerLog(strings.Repeat("a", 80))
	logs := panel.recentServerLogs(0)
	if len(logs) != 1 {
		t.Fatalf("server log count = %d, want 1", len(logs))
	}
	if len(logs[0].Message) > maxServerLogMessageBytes {
		t.Fatalf("server log message length = %d, want <= %d", len(logs[0].Message), maxServerLogMessageBytes)
	}
	if !strings.HasSuffix(logs[0].Message, serverLogTruncatedSuffix) {
		t.Fatalf("server log missing truncation suffix: %q", logs[0].Message)
	}
	if !strings.HasPrefix(logs[0].Message, strings.Repeat("a", 10)) {
		t.Fatalf("server log did not retain prefix: %q", logs[0].Message)
	}
}

func TestAppendServerLogTruncationPreservesUTF8AndSplitting(t *testing.T) {
	setServerLogMessageLimit(t, len(serverLogTruncatedSuffix)+7)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	panel.appendServerLog(strings.Repeat("x", 20) + "旧旧旧🙂🙂🙂\nshort")
	logs := panel.recentServerLogs(0)
	if len(logs) != 2 {
		t.Fatalf("server log count = %d, want 2: %#v", len(logs), logs)
	}
	if len(logs[0].Message) > maxServerLogMessageBytes {
		t.Fatalf("server log message length = %d, want <= %d", len(logs[0].Message), maxServerLogMessageBytes)
	}
	if !utf8.ValidString(logs[0].Message) {
		t.Fatalf("server log message is not valid UTF-8: %q", logs[0].Message)
	}
	if !strings.HasSuffix(logs[0].Message, serverLogTruncatedSuffix) {
		t.Fatalf("server log missing truncation suffix: %q", logs[0].Message)
	}
	if logs[1].Message != "short" {
		t.Fatalf("second server log line = %q, want short", logs[1].Message)
	}
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	return doJSONMaybeConfirmed(t, client, method, url, body, false)
}

func newAuthenticatedTestServer(t *testing.T, panel *App) (*httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewServer(panel.Routes())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}
	setup := map[string]string{"username": "admin", "password": "password123"}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", setup)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	return server, client
}

func doJSONConfirmed(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	return doJSONMaybeConfirmed(t, client, method, url, body, true)
}

func doJSONMaybeConfirmed(t *testing.T, client *http.Client, method, url string, body any, confirmed bool) *http.Response {
	t.Helper()
	var reqBody *bytes.Reader
	if body == nil {
		reqBody = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if confirmed {
		req.Header.Set(confirmationHeader, "true")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	return resp
}

func doJSONWithOrigin(t *testing.T, client *http.Client, method, url string, body any, origin string) *http.Response {
	t.Helper()
	var reqBody *bytes.Reader
	if body == nil {
		reqBody = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	return resp
}

func setTestAppSetting(t *testing.T, panel *App, key, value string) {
	t.Helper()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	); err != nil {
		t.Fatalf("set app setting %s: %v", key, err)
	}
}

func waitForTaskStatus(t *testing.T, panel *App, taskID int64, want string) taskRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		task, err := panel.getTask(taskID)
		if err != nil {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if task.Status == want {
			return task
		}
		if task.Status != "running" {
			t.Fatalf("task status = %q, want %q; log:\n%s", task.Status, want, task.Log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := panel.getTask(taskID)
	if err != nil {
		if lastErr != nil {
			t.Fatalf("timed out waiting for task status %q; last getTask error = %v; final getTask error = %v", want, lastErr, err)
		}
		t.Fatalf("getTask(%d) after timeout error = %v", taskID, err)
	}
	t.Fatalf("timed out waiting for task status %q; current status = %q; log:\n%s", want, task.Status, task.Log)
	return taskRecord{}
}

func writeFakePalServerBinary(t *testing.T, serverPath string) {
	t.Helper()
	binary := palServerBinary(serverPath)
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatalf("mkdir PalServer binary dir: %v", err)
	}
	if err := os.WriteFile(binary, []byte("fake PalServer"), 0o755); err != nil {
		t.Fatalf("write fake PalServer binary: %v", err)
	}
}

func writeLongRunningFakePalServerBinary(t *testing.T, serverPath string) {
	t.Helper()
	binary := palServerBinary(serverPath)
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatalf("mkdir PalServer binary dir: %v", err)
	}
	script := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake PalServer script: %v", err)
	}
}

func writeInterruptibleFakePalServerBinary(t *testing.T, serverPath string) {
	t.Helper()
	binary := palServerBinary(serverPath)
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatalf("mkdir PalServer binary dir: %v", err)
	}
	script := "#!/bin/sh\ntrap 'exit 0' INT TERM\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write interruptible PalServer script: %v", err)
	}
}

func writeBackupSource(t *testing.T, serverPath string) {
	t.Helper()
	savePath := filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "world.txt")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		t.Fatalf("mkdir save dir: %v", err)
	}
	if err := os.WriteFile(savePath, []byte("save"), 0o644); err != nil {
		t.Fatalf("write save source: %v", err)
	}
}

func assertNoBackups(t *testing.T, panel *App) {
	t.Helper()
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("backups = %#v, want none", backups)
	}
}

func assertNoTasks(t *testing.T, panel *App) {
	t.Helper()
	tasks, err := panel.listTasks(10)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks = %#v, want none", tasks)
	}
}

func assertNoTasksOfType(t *testing.T, panel *App, taskType string) {
	t.Helper()
	tasks, err := panel.listTasks(50)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	for _, task := range tasks {
		if task.Type == taskType {
			t.Fatalf("task type %q exists unexpectedly: %#v", taskType, tasks)
		}
	}
}

func setTaskLogLimit(t *testing.T, limit int) {
	t.Helper()
	previous := maxTaskLogBytes
	maxTaskLogBytes = limit
	t.Cleanup(func() {
		maxTaskLogBytes = previous
	})
}

func setServerLogMessageLimit(t *testing.T, limit int) {
	t.Helper()
	previous := maxServerLogMessageBytes
	maxServerLogMessageBytes = limit
	t.Cleanup(func() {
		maxServerLogMessageBytes = previous
	})
}

func setFinishedTaskRetentionLimit(t *testing.T, limit int) {
	t.Helper()
	previous := maxFinishedTaskRecords
	maxFinishedTaskRecords = limit
	t.Cleanup(func() {
		maxFinishedTaskRecords = previous
	})
}
