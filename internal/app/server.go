package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	errTaskRunning          = errors.New("another task is already running")
	errServerAlreadyRunning = errors.New("server is already running")
	errServerNotRunning     = errors.New("server is not running from this panel")
)

var maxTaskLogBytes = 1 << 20

var maxServerLogMessageBytes = 8 << 10

var maxFinishedTaskRecords = 200

const taskLogTruncatedMarker = "[... earlier task log truncated ...]\n"
const serverLogTruncatedSuffix = " [... truncated]"

func (a *App) handleServerInstall(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	a.handleSteamCMDTask(w, "server_install")
}

func (a *App) handleServerUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	a.handleSteamCMDTask(w, "server_update")
}

func (a *App) handleSteamCMDTask(w http.ResponseWriter, taskType string) {
	task, err := a.startSteamCMDTask(taskType)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]taskRecord{"task": task})
}

func (a *App) handleServerStart(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.startServerProcess(settings); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, a.currentServerStatus(settings))
}

func (a *App) handleServerStop(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	if err := a.stopServerProcess(15 * time.Second); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, a.currentServerStatus(settings))
}

func (a *App) handleServerRestart(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, _, _, err := a.validateServerStartPreflight(settings); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()
	if a.isManagedServerRunning() {
		if err := a.stopManagedServerProcess(15 * time.Second); err != nil {
			writeError(w, actionErrorStatus(err), err)
			return
		}
	}
	if err := a.startManagedServerProcessAdmitted(settings); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, a.currentServerStatus(settings))
}

func (a *App) handleServerLogs(w http.ResponseWriter, r *http.Request) {
	settings, err := a.loadSettings()
	running := a.isServerRunning()
	if err == nil {
		status := a.currentServerStatus(settings)
		running = status.Running
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"running": running,
		"logs":    a.recentServerLogs(0),
	})
}

func (a *App) handleTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := a.listTasks(10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (a *App) startSteamCMDTask(taskType string) (taskRecord, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return taskRecord{}, err
	}
	installDir := strings.TrimSpace(settings.PalServerPath)
	if installDir == "" {
		return taskRecord{}, errors.New("pal_server_path is required before running SteamCMD")
	}
	steamPath := resolveSteamCMD(settings.SteamCMDPath)
	if steamPath == "" {
		return taskRecord{}, errors.New("steamcmd was not found; set steamcmd_path or add it to PATH")
	}
	if err := a.ensureNoExternalServerRunning(settings); err != nil {
		return taskRecord{}, err
	}

	taskID, releaseTask, err := a.beginTask(taskType)
	if err != nil {
		return taskRecord{}, err
	}
	restartAfterUpdate := taskType == "server_update" && a.isServerRunningForUpdate()

	go a.runSteamCMDTask(taskID, taskType, steamPath, installDir, settings, restartAfterUpdate, releaseTask)

	return a.getTask(taskID)
}

func (a *App) runSteamCMDTask(taskID int64, taskType, steamPath, installDir string, settings settingsPayload, restartAfterUpdate bool, releaseTask func()) {
	defer releaseTask()

	args := []string{
		"+force_install_dir", installDir,
		"+login", "anonymous",
		"+app_update", "2394010", "validate",
		"+quit",
	}
	a.logTaskf(taskID, "Starting %s", taskType)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		a.logTaskf(taskID, "%s failed: prepare install directory: %v", taskType, err)
		_ = a.finishTask(taskID, "failed")
		return
	}
	backup, err := a.createBackupIfPossibleWithSettings(settings, "pre_update", fmt.Sprintf("Before %s", taskType))
	if err != nil {
		a.logTaskf(taskID, "%s failed: create pre-operation backup: %v", taskType, err)
		_ = a.finishTask(taskID, "failed")
		return
	}
	if backup != nil {
		a.logTaskf(taskID, "Pre-operation backup created: %s (%d bytes)", backup.Filename, backup.Size)
	} else {
		a.logTaskf(taskID, "No pre-operation backup sources found; continuing")
	}
	if restartAfterUpdate {
		a.logTaskf(taskID, "PalServer is running; stopping before %s", taskType)
		if err := a.stopServerForUpdate(30 * time.Second); err != nil {
			a.logTaskf(taskID, "%s failed: stop PalServer before update: %v", taskType, err)
			_ = a.finishTask(taskID, "failed")
			return
		}
	}
	a.logTaskf(taskID, "Running %s %s", steamPath, strings.Join(args, " "))

	cmd := exec.Command(steamPath, args...)
	cmd.Dir = filepath.Dir(steamPath)
	writer := &taskLogWriter{app: a, taskID: taskID}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err = a.runExternalCommand(cmd)
	writer.Flush()
	if err != nil {
		a.logTaskf(taskID, "%s failed: %v", taskType, err)
		_ = a.finishTask(taskID, "failed")
		return
	}
	if restartAfterUpdate {
		a.logTaskf(taskID, "Restarting PalServer after successful %s", taskType)
		if err := a.startServerAfterUpdate(settings); err != nil {
			a.logTaskf(taskID, "%s failed: restart PalServer after update: %v", taskType, err)
			_ = a.finishTask(taskID, "failed")
			return
		}
	}
	a.logTaskf(taskID, "%s completed", taskType)
	_ = a.finishTask(taskID, "success")
}

func (a *App) runExternalCommand(cmd *exec.Cmd) error {
	if a.commandRunner != nil {
		return a.commandRunner(cmd)
	}
	return cmd.Run()
}

func (a *App) isServerRunningForUpdate() bool {
	return a.isManagedServerRunning()
}

func (a *App) isManagedServerRunning() bool {
	if a.isServerRunningFunc != nil {
		return a.isServerRunningFunc()
	}
	return a.isServerRunning()
}

func (a *App) stopServerForUpdate(timeout time.Duration) error {
	return a.stopManagedServerProcess(timeout)
}

func (a *App) stopManagedServerProcess(timeout time.Duration) error {
	if a.stopServerProcessFunc != nil {
		return a.stopServerProcessFunc(timeout)
	}
	return a.stopServerProcess(timeout)
}

func (a *App) startServerAfterUpdate(settings settingsPayload) error {
	return a.startManagedServerProcessAdmitted(settings)
}

func (a *App) startManagedServerProcess(settings settingsPayload) error {
	if a.startServerProcessFunc != nil {
		return a.startServerProcessFunc(settings)
	}
	return a.startServerProcess(settings)
}

func (a *App) startManagedServerProcessAdmitted(settings settingsPayload) error {
	if a.startServerProcessFunc != nil {
		return a.startServerProcessFunc(settings)
	}
	return a.startServerProcessAdmitted(settings)
}

func (a *App) startServerProcess(settings settingsPayload) error {
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		return err
	}
	defer releaseTask()
	return a.startServerProcessAdmitted(settings)
}

func (a *App) startServerProcessAdmitted(settings settingsPayload) error {
	serverPath, binary, args, err := a.validateServerStartPreflight(settings)
	if err != nil {
		return err
	}

	a.serverMu.Lock()
	if a.serverCmd != nil || a.serverStarting {
		a.serverMu.Unlock()
		return errServerAlreadyRunning
	}
	a.serverStarting = true
	a.serverMu.Unlock()

	startReserved := true
	defer func() {
		if startReserved {
			a.serverMu.Lock()
			a.serverStarting = false
			a.serverMu.Unlock()
		}
	}()

	if _, err := a.createBackupIfPossibleWithSettings(settings, "startup", "Before starting PalServer"); err != nil {
		return err
	}

	cmd := buildServerCommand(binary, args)
	cmd.Dir = serverPath
	cmd.Env = os.Environ()
	writer := &serverLogWriter{app: a}
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	a.serverMu.Lock()
	a.serverCmd = cmd
	a.serverDone = done
	a.serverStarting = false
	startReserved = false
	a.serverMu.Unlock()

	a.appendServerLog(fmt.Sprintf("Started %s with pid %d", filepath.Base(binary), cmd.Process.Pid))

	go func() {
		err := cmd.Wait()
		writer.Flush()

		a.serverMu.Lock()
		if a.serverCmd == cmd {
			a.serverCmd = nil
			a.serverDone = nil
		}
		a.serverMu.Unlock()
		done <- err

		if err != nil {
			a.appendServerLog(fmt.Sprintf("PalServer exited: %v", err))
			return
		}
		a.appendServerLog("PalServer exited")
	}()

	return nil
}

func (a *App) validateServerStartPreflight(settings settingsPayload) (string, string, []string, error) {
	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return "", "", nil, errors.New("pal_server_path is required before starting the server")
	}
	binary := palServerBinary(serverPath)
	if !fileExists(binary) {
		return "", "", nil, fmt.Errorf("PalServer binary not found at %s", binary)
	}
	if err := a.ensureNoExternalServerRunning(settings); err != nil {
		return "", "", nil, err
	}
	args, err := splitCommandLine(settings.ServerLaunchArgs)
	if err != nil {
		return "", "", nil, err
	}
	return serverPath, binary, args, nil
}

func (a *App) stopServerProcess(timeout time.Duration) error {
	a.serverMu.Lock()
	cmd := a.serverCmd
	done := a.serverDone
	a.serverMu.Unlock()

	if cmd == nil || cmd.Process == nil || done == nil {
		return errServerNotRunning
	}

	a.appendServerLog("Stopping PalServer")
	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
	} else if err := cmd.Process.Signal(os.Interrupt); err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil {
			return fmt.Errorf("signal failed: %v; kill failed: %w", err, killErr)
		}
	}

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		a.appendServerLog("PalServer did not stop before timeout; killing process")
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
		<-done
		return nil
	}
}

func buildServerCommand(binary string, args []string) *exec.Cmd {
	if runtime.GOOS != "windows" && strings.HasSuffix(binary, ".sh") {
		shArgs := append([]string{binary}, args...)
		return exec.Command("sh", shArgs...)
	}
	return exec.Command(binary, args...)
}

func (a *App) isServerRunning() bool {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	return a.serverCmd != nil
}

func (a *App) recentServerLogs(limit int) []serverLogEntry {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	start := 0
	if limit > 0 && len(a.serverLogs) > limit {
		start = len(a.serverLogs) - limit
	}
	return append([]serverLogEntry(nil), a.serverLogs[start:]...)
}

func (a *App) appendServerLog(message string) {
	message = redactSensitive(message)
	lines := splitLogLines(message)
	if len(lines) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entries := make([]serverLogEntry, 0, len(lines))
	for _, line := range lines {
		line = truncateServerLogMessage(line)
		if line == "" {
			continue
		}
		entries = append(entries, serverLogEntry{
			Time:    now,
			Message: line,
		})
	}
	if len(entries) == 0 {
		return
	}

	a.serverMu.Lock()
	a.serverLogs = append(a.serverLogs, entries...)
	if len(a.serverLogs) > 400 {
		a.serverLogs = append([]serverLogEntry(nil), a.serverLogs[len(a.serverLogs)-400:]...)
	}
	running := a.serverCmd != nil
	a.serverMu.Unlock()

	a.broadcastRuntimeEvent(runtimeEvent{Type: "server_log", ServerLogs: entries, Running: &running})
}

func (a *App) createTask(taskType string) (int64, error) {
	result, err := a.db.Exec(
		`INSERT INTO tasks(type, status, log, created_at) VALUES(?, ?, ?, ?)`,
		taskType,
		"running",
		"",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	taskID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	a.broadcastTask(taskID)
	return taskID, nil
}

func (a *App) getTask(taskID int64) (taskRecord, error) {
	var task taskRecord
	var finished sql.NullString
	err := a.db.QueryRow(
		`SELECT id, type, status, COALESCE(log, ''), CAST(created_at AS TEXT), CAST(finished_at AS TEXT)
		 FROM tasks
		 WHERE id = ?`,
		taskID,
	).Scan(&task.ID, &task.Type, &task.Status, &task.Log, &task.CreatedAt, &finished)
	if err != nil {
		return taskRecord{}, err
	}
	if finished.Valid {
		task.FinishedAt = finished.String
	}
	return task, nil
}

func (a *App) listTasks(limit int) ([]taskRecord, error) {
	rows, err := a.db.Query(
		`SELECT id, type, status, COALESCE(log, ''), CAST(created_at AS TEXT), CAST(finished_at AS TEXT)
		 FROM tasks
		 ORDER BY id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]taskRecord, 0)
	for rows.Next() {
		var task taskRecord
		var finished sql.NullString
		if err := rows.Scan(&task.ID, &task.Type, &task.Status, &task.Log, &task.CreatedAt, &finished); err != nil {
			return nil, err
		}
		if finished.Valid {
			task.FinishedAt = finished.String
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (a *App) appendTaskLog(taskID int64, text string) error {
	text = redactSensitive(text)
	text = normalizeLogText(text)
	if text == "" {
		return nil
	}
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = a.appendTaskLogOnce(taskID, text)
		if err == nil {
			a.broadcastTask(taskID)
			return nil
		}
		if !isTemporaryDatabaseBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *App) appendTaskLogOnce(taskID int64, text string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var existing string
	if err := tx.QueryRow(`SELECT COALESCE(log, '') FROM tasks WHERE id = ?`, taskID).Scan(&existing); err != nil {
		return err
	}
	next := truncateTaskLog(existing + text)
	if _, err := tx.Exec(`UPDATE tasks SET log = ? WHERE id = ?`, next, taskID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (a *App) logTaskf(taskID int64, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	_ = a.appendTaskLog(taskID, fmt.Sprintf("[%s] %s\n", time.Now().UTC().Format(time.RFC3339), line))
}

func (a *App) reserveTaskSlot() (func(), error) {
	a.taskMu.Lock()
	if a.taskRunning {
		a.taskMu.Unlock()
		return nil, errTaskRunning
	}
	a.taskRunning = true
	a.taskMu.Unlock()
	a.broadcastOperationRunning(true)

	release := func() {
		a.taskMu.Lock()
		a.taskRunning = false
		a.taskMu.Unlock()
		a.broadcastOperationRunning(false)
	}
	return release, nil
}

func (a *App) operationRunning() bool {
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	return a.taskRunning
}

func (a *App) beginTask(taskType string) (int64, func(), error) {
	a.taskMu.Lock()
	if a.taskRunning {
		a.taskMu.Unlock()
		return 0, nil, errTaskRunning
	}
	taskID, err := a.createTask(taskType)
	if err != nil {
		a.taskMu.Unlock()
		return 0, nil, err
	}
	a.taskRunning = true
	a.taskMu.Unlock()
	a.broadcastOperationRunning(true)

	release := func() {
		a.taskMu.Lock()
		a.taskRunning = false
		a.taskMu.Unlock()
		a.broadcastOperationRunning(false)
	}
	return taskID, release, nil
}

func (a *App) runOperationTask(taskType, startMessage, successMessage string, run func(taskID int64) error) error {
	taskID, releaseTask, err := a.beginTask(taskType)
	if err != nil {
		return err
	}
	defer releaseTask()
	return a.runOperationTaskWithID(taskID, taskType, startMessage, successMessage, run)
}

func (a *App) runOperationTaskAdmitted(taskType, startMessage, successMessage string, run func(taskID int64) error) error {
	taskID, err := a.createTask(taskType)
	if err != nil {
		return err
	}
	return a.runOperationTaskWithID(taskID, taskType, startMessage, successMessage, run)
}

func (a *App) runOperationTaskWithID(taskID int64, taskType, startMessage, successMessage string, run func(taskID int64) error) error {
	if strings.TrimSpace(startMessage) != "" {
		a.logTaskf(taskID, "%s", startMessage)
	}
	if err := run(taskID); err != nil {
		a.logTaskf(taskID, "%s failed: %v", taskType, err)
		_ = a.finishTask(taskID, "failed")
		return err
	}
	if strings.TrimSpace(successMessage) != "" {
		a.logTaskf(taskID, "%s", successMessage)
	}
	return a.finishTask(taskID, "success")
}

func (a *App) finishTask(taskID int64, status string) error {
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = a.db.Exec(
			`UPDATE tasks SET status = ?, finished_at = ? WHERE id = ?`,
			status,
			time.Now().UTC().Format(time.RFC3339),
			taskID,
		)
		if err == nil {
			a.broadcastTask(taskID)
			_ = a.pruneFinishedTasks()
			return nil
		}
		if !isTemporaryDatabaseBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *App) pruneFinishedTasks() error {
	limit := maxFinishedTaskRecords
	if limit < 0 {
		return nil
	}
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = a.db.Exec(
			`DELETE FROM tasks
			 WHERE status != 'running'
			   AND id NOT IN (
				 SELECT id
				 FROM tasks
				 WHERE status != 'running'
				 ORDER BY id DESC
				 LIMIT ?
			   )`,
			limit,
		)
		if err == nil {
			return nil
		}
		if !isTemporaryDatabaseBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func actionErrorStatus(err error) int {
	switch {
	case errors.Is(err, errTaskRunning), errors.Is(err, errServerAlreadyRunning), errors.Is(err, errServerNotRunning), errors.Is(err, errExternalServerRunning), errors.Is(err, errServerRunningForModMutation), errors.Is(err, errSettingsRetargetRunning):
		return http.StatusConflict
	case errors.Is(err, errPalConfigFileTooLarge), errors.Is(err, errPalModSettingsTooLarge):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusBadRequest
	}
}

func normalizeLogText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func truncateTaskLog(text string) string {
	if maxTaskLogBytes <= 0 {
		return ""
	}
	if len(text) <= maxTaskLogBytes {
		return text
	}
	if len(taskLogTruncatedMarker) >= maxTaskLogBytes {
		return taskLogTruncatedMarker[:maxTaskLogBytes]
	}
	keep := maxTaskLogBytes - len(taskLogTruncatedMarker)
	suffix := text[len(text)-keep:]
	for len(suffix) > 0 && !utf8.ValidString(suffix) {
		suffix = suffix[1:]
	}
	return taskLogTruncatedMarker + suffix
}

func truncateServerLogMessage(message string) string {
	if maxServerLogMessageBytes <= 0 {
		return ""
	}
	if len(message) <= maxServerLogMessageBytes {
		return message
	}
	if len(serverLogTruncatedSuffix) >= maxServerLogMessageBytes {
		return serverLogTruncatedSuffix[:maxServerLogMessageBytes]
	}
	keep := maxServerLogMessageBytes - len(serverLogTruncatedSuffix)
	prefix := message[:keep]
	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + serverLogTruncatedSuffix
}

func isTemporaryDatabaseBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy")
}

func splitLogLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func splitCommandLine(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	var args []string
	var current strings.Builder
	var quote rune

	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for _, r := range input {
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}

		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if quote != 0 {
		return nil, errors.New("unterminated quote in server_launch_args")
	}
	flush()
	return args, nil
}

type taskLogWriter struct {
	mu     sync.Mutex
	app    *App
	taskID int64
	buffer string
}

func (w *taskLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	text := w.buffer + string(p)
	lines := strings.Split(text, "\n")
	for _, line := range lines[:len(lines)-1] {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			_ = w.app.appendTaskLog(w.taskID, line+"\n")
		}
	}
	w.buffer = lines[len(lines)-1]
	return len(p), nil
}

func (w *taskLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.buffer) != "" {
		_ = w.app.appendTaskLog(w.taskID, w.buffer+"\n")
	}
	w.buffer = ""
}

type serverLogWriter struct {
	mu     sync.Mutex
	app    *App
	buffer string
}

func (w *serverLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	text := w.buffer + string(p)
	lines := strings.Split(text, "\n")
	for _, line := range lines[:len(lines)-1] {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			w.app.appendServerLog(line)
		}
	}
	w.buffer = lines[len(lines)-1]
	return len(p), nil
}

func (w *serverLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.buffer) != "" {
		w.app.appendServerLog(w.buffer)
	}
	w.buffer = ""
}
