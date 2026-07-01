package app

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBackupRoutesCreateRestoreDelete(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

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

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/backups", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/backups empty body status = %d", resp.StatusCode)
	}
	var defaultBackup backupRecord
	if err := json.NewDecoder(resp.Body).Decode(&defaultBackup); err != nil {
		t.Fatalf("decode default backup: %v", err)
	}
	resp.Body.Close()
	if defaultBackup.Type != "manual" || defaultBackup.ID == 0 || !fileExists(defaultBackup.Path) {
		t.Fatalf("invalid default backup response: %#v", defaultBackup)
	}

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/backups", backupRequest{Type: " ../ ", Note: "unsafe type route test"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/backups unsafe type status = %d", resp.StatusCode)
	}
	var normalizedBackup backupRecord
	if err := json.NewDecoder(resp.Body).Decode(&normalizedBackup); err != nil {
		t.Fatalf("decode normalized backup: %v", err)
	}
	resp.Body.Close()
	if normalizedBackup.Type != "manual" || !strings.Contains(normalizedBackup.Filename, "palpanel_manual_") {
		t.Fatalf("backup type was not normalized: %#v", normalizedBackup)
	}
	tasks, err := panel.listTasks(10)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	var sawNormalizedTask bool
	for _, task := range tasks {
		if task.Type == "backup_manual" && strings.Contains(task.Log, "Starting manual backup") && strings.Contains(task.Log, normalizedBackup.Filename) {
			sawNormalizedTask = true
			if strings.Contains(task.Log, "../") {
				t.Fatalf("task log contains raw unsafe backup type: %q", task.Log)
			}
			break
		}
	}
	if !sawNormalizedTask {
		t.Fatalf("normalized backup task not found in %#v", tasks)
	}

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/backups", backupRequest{Type: "manual", Note: "route test"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/backups status = %d", resp.StatusCode)
	}
	var backup backupRecord
	if err := json.NewDecoder(resp.Body).Decode(&backup); err != nil {
		t.Fatalf("decode backup: %v", err)
	}
	resp.Body.Close()
	if backup.ID == 0 || backup.Size == 0 || !fileExists(backup.Path) {
		t.Fatalf("invalid backup response: %#v", backup)
	}

	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("modify save: %v", err)
	}
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/backups/"+strconvID(backup.ID)+"/restore", nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed restore status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/backups/"+strconvID(backup.ID)+"/restore", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST restore status = %d", resp.StatusCode)
	}
	var restored restoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	resp.Body.Close()
	if restored.ProtectiveBackup == nil || !fileExists(restored.ProtectiveBackup.Path) {
		t.Fatalf("protective backup missing: %#v", restored)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read restored save: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("restored content = %q", content)
	}

	resp = doJSON(t, client, http.MethodDelete, server.URL+"/api/backups/"+strconvID(backup.ID), nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodDelete, server.URL+"/api/backups/"+strconvID(backup.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE backup status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if fileExists(backup.Path) {
		t.Fatalf("backup file still exists after delete: %s", backup.Path)
	}
	for _, taskType := range []string{"backup_manual", "backup_restore", "backup_delete"} {
		task := requireTaskWithStatus(t, panel, taskType, "success")
		if !strings.Contains(task.Log, "completed") && !strings.Contains(task.Log, "created") && !strings.Contains(task.Log, "Deleted") {
			t.Fatalf("%s task log did not include operation detail: %q", taskType, task.Log)
		}
	}
}

func TestBackupReadAPIsOmitUnsafePersistedPath(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	outsidePath := filepath.Join(t.TempDir(), "outside-backup.zip")
	_, err = panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		"outside-backup.zip",
		outsidePath,
		123,
		"manual",
		"dirty path",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert dirty backup row: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/backups", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/backups status = %d", resp.StatusCode)
	}
	var backups []backupRecord
	if err := json.NewDecoder(resp.Body).Decode(&backups); err != nil {
		t.Fatalf("decode backups: %v", err)
	}
	resp.Body.Close()
	if len(backups) != 1 {
		t.Fatalf("backup count = %d, want 1", len(backups))
	}
	if backups[0].Path != "" {
		t.Fatalf("unsafe backup path was serialized: %#v", backups[0])
	}

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/dashboard", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/dashboard status = %d", resp.StatusCode)
	}
	var dashboard palDashboardPayload
	if err := json.NewDecoder(resp.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	resp.Body.Close()
	if len(dashboard.RecentBackups) != 1 {
		t.Fatalf("recent backup count = %d, want 1", len(dashboard.RecentBackups))
	}
	if dashboard.RecentBackups[0].Path != "" {
		t.Fatalf("unsafe recent backup path was serialized: %#v", dashboard.RecentBackups[0])
	}
}

func TestDeleteBackupKeepsArchiveWhenDatabaseDeleteFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

	backup, err := panel.createBackup("manual", "delete failure test")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if !fileExists(backup.Path) {
		t.Fatalf("backup file missing before delete test: %s", backup.Path)
	}
	if _, err := panel.db.Exec(`CREATE TRIGGER block_backup_delete BEFORE DELETE ON backups BEGIN SELECT RAISE(FAIL, 'delete blocked'); END`); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}

	err = panel.deleteBackup(backup.ID)
	if err == nil || !strings.Contains(err.Error(), "delete blocked") {
		t.Fatalf("deleteBackup() error = %v, want delete blocked", err)
	}
	if !fileExists(backup.Path) {
		t.Fatalf("backup file was removed after failed database delete: %s", backup.Path)
	}
	if _, err := panel.getBackup(backup.ID); err != nil {
		t.Fatalf("backup row missing after failed database delete: %v", err)
	}
}

func TestDeleteBackupKeepsArchiveAndRowWhenDatabaseCommitFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	backup, err := panel.createBackup("manual", "commit failure")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	previousCommit := commitBackupDeleteTx
	commitBackupDeleteTx = func(tx *sql.Tx) error {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return errors.New("commit blocked")
	}
	defer func() {
		commitBackupDeleteTx = previousCommit
	}()

	err = panel.deleteBackup(backup.ID)
	if err == nil || !strings.Contains(err.Error(), "commit blocked") {
		t.Fatalf("deleteBackup() error = %v, want commit blocked", err)
	}
	if !fileExists(backup.Path) {
		t.Fatalf("backup file was removed after failed database commit: %s", backup.Path)
	}
	remaining, err := panel.getBackup(backup.ID)
	if err != nil {
		t.Fatalf("backup row missing after failed database commit: %v", err)
	}
	if remaining.Path != backup.Path || remaining.Type != backup.Type || remaining.Note != backup.Note {
		t.Fatalf("backup row changed after failed database commit: got %#v want %#v", remaining, backup)
	}
}

func TestDeleteBackupRestoresRowWhenArchiveRemovalFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	backup, err := panel.createBackup("manual", "remove failure")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	previousRemove := removeBackupPath
	removeBackupPath = func(path string) error {
		if path != backup.Path {
			t.Fatalf("removeBackupPath called for unexpected path: %s", path)
		}
		return errors.New("remove blocked")
	}
	defer func() {
		removeBackupPath = previousRemove
	}()

	err = panel.deleteBackup(backup.ID)
	if err == nil || !strings.Contains(err.Error(), "remove blocked") {
		t.Fatalf("deleteBackup() error = %v, want remove blocked", err)
	}
	if !fileExists(backup.Path) {
		t.Fatalf("backup file missing after failed archive removal: %s", backup.Path)
	}
	restored, err := panel.getBackup(backup.ID)
	if err != nil {
		t.Fatalf("backup row was not restored after failed archive removal: %v", err)
	}
	if restored.Path != backup.Path || restored.Type != backup.Type || restored.Note != backup.Note {
		t.Fatalf("restored backup row = %#v, want %#v", restored, backup)
	}
}

func TestCreateBackupDoesNotCreateFinalArchiveWhenDatabaseInsertFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	if _, err := panel.db.Exec(`CREATE TRIGGER block_backup_insert BEFORE INSERT ON backups BEGIN SELECT RAISE(FAIL, 'insert blocked'); END`); err != nil {
		t.Fatalf("create insert trigger: %v", err)
	}
	previousRemove := removeBackupPath
	removeBackupPath = func(path string) error {
		return errors.New("remove blocked")
	}
	defer func() {
		removeBackupPath = previousRemove
	}()

	_, err = panel.createBackup("manual", "insert failure")
	if err == nil || !strings.Contains(err.Error(), "insert blocked") {
		t.Fatalf("createBackup() error = %v, want insert blocked", err)
	}
	assertNoFinalBackupArchives(t, panel.backupDir())
	var count int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&count); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if count != 0 {
		t.Fatalf("backup rows = %d, want 0", count)
	}
}

func TestCreateBackupRollsBackRowWhenFinalRenameFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	previousRename := renameBackupArchive
	renameBackupArchive = func(src, dst string) error {
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("temporary archive missing before injected rename failure: %v", err)
		}
		return errors.New("rename blocked")
	}
	defer func() {
		renameBackupArchive = previousRename
	}()

	_, err = panel.createBackup("manual", "rename failure")
	if err == nil || !strings.Contains(err.Error(), "rename blocked") {
		t.Fatalf("createBackup() error = %v, want rename blocked", err)
	}
	assertNoFinalBackupArchives(t, panel.backupDir())
	var count int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&count); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if count != 0 {
		t.Fatalf("backup rows = %d, want 0", count)
	}
}

func TestCreateBackupRejectsSourceResourceLimitsWithoutBackupRecord(t *testing.T) {
	tests := []struct {
		name       string
		entryBytes int64
		totalBytes int64
		fileCount  int
		files      map[string]string
		want       string
	}{
		{
			name:       "file count",
			entryBytes: 1024,
			totalBytes: 4096,
			fileCount:  1,
			files: map[string]string{
				"a.txt": "a",
				"b.txt": "b",
			},
			want: "more than 1 files",
		},
		{
			name:       "single file",
			entryBytes: 5,
			totalBytes: 100,
			fileCount:  10,
			files: map[string]string{
				"large.txt": "123456",
			},
			want: "exceeds 5 bytes",
		},
		{
			name:       "total bytes",
			entryBytes: 10,
			totalBytes: 10,
			fileCount:  10,
			files: map[string]string{
				"a.txt": "123456",
				"b.txt": "abcdef",
			},
			want: "source data exceeds 10 bytes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setBackupRestoreLimits(t, tc.entryBytes, tc.totalBytes, tc.fileCount)
			panel, err := New(t.TempDir())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer panel.Close()

			serverPath := t.TempDir()
			saveDir := filepath.Join(serverPath, "Pal", "Saved", "SaveGames")
			if err := os.MkdirAll(saveDir, 0o755); err != nil {
				t.Fatalf("mkdir save dir: %v", err)
			}
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(saveDir, name), []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			setTestAppSetting(t, panel, "pal_server_path", serverPath)

			_, err = panel.createBackup("manual", "resource limit")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("createBackup() error = %v, want %q", err, tc.want)
			}
			assertNoFinalBackupArchives(t, panel.backupDir())
			assertBackupRowCount(t, panel, 0)
		})
	}
}

func TestBackupCreationLimitReaderCountsActualBytes(t *testing.T) {
	setBackupRestoreLimits(t, 4, 100, 10)
	var copied int64
	reader := &backupCreationLimitReader{
		reader:     strings.NewReader("12345"),
		entryName:  "Pal/Saved/SaveGames/world.txt",
		totalBytes: &copied,
	}
	_, err := io.Copy(io.Discard, reader)
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("entry limit copy error = %v, want exceeds 4 bytes", err)
	}

	setBackupRestoreLimits(t, 100, 4, 10)
	copied = 0
	reader = &backupCreationLimitReader{
		reader:     strings.NewReader("12345"),
		entryName:  "Pal/Saved/SaveGames/world.txt",
		totalBytes: &copied,
	}
	_, err = io.Copy(io.Discard, reader)
	if err == nil || !strings.Contains(err.Error(), "source data exceeds 4 bytes") {
		t.Fatalf("total limit copy error = %v, want source data exceeds 4 bytes", err)
	}
}

func TestCreateBackupSkipsSymlinkEntries(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	saveDir := filepath.Join(serverPath, "Pal", "Saved", "SaveGames")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatalf("mkdir save dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "world.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside save: %v", err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside-secret.txt")
	if err := os.WriteFile(outsidePath, []byte("outside secret"), 0o644); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}
	linkPath := filepath.Join(saveDir, "linked-secret.txt")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink creation unavailable in this environment: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	backup, err := panel.createBackup("manual", "symlink skip")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	reader, err := zip.OpenReader(backup.Path)
	if err != nil {
		t.Fatalf("open backup zip: %v", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name == "Pal/Saved/SaveGames/linked-secret.txt" {
			t.Fatalf("backup included symlink entry: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		src, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", file.Name, err)
		}
		data, readErr := io.ReadAll(src)
		closeErr := src.Close()
		if readErr != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, readErr)
		}
		if closeErr != nil {
			t.Fatalf("close zip entry %s: %v", file.Name, closeErr)
		}
		if strings.Contains(string(data), "outside secret") {
			t.Fatalf("backup included symlink target content in %s", file.Name)
		}
	}
}

func TestAutomaticBackupRetentionKeepsManualBackups(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("base"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	for key, value := range map[string]string{
		"pal_server_path":       serverPath,
		"auto_backup_retention": "2",
	} {
		if _, err := panel.db.Exec(
			`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key,
			value,
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	manual, err := panel.createBackup("manual", "keep")
	if err != nil {
		t.Fatalf("manual createBackup() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(savePath, []byte{byte('0' + i)}, 0o644); err != nil {
			t.Fatalf("modify save: %v", err)
		}
		if _, err := panel.createBackup("pre_update", "auto"); err != nil {
			t.Fatalf("auto createBackup() error = %v", err)
		}
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	manualCount := 0
	autoCount := 0
	for _, backup := range backups {
		switch backup.Type {
		case "manual":
			manualCount++
			if backup.ID != manual.ID {
				t.Fatalf("unexpected manual backup: %#v", backup)
			}
		case "pre_update":
			autoCount++
		}
	}
	if manualCount != 1 || autoCount != 2 {
		t.Fatalf("manualCount=%d autoCount=%d backups=%#v", manualCount, autoCount, backups)
	}
}

func TestPruneAutomaticBackupsKeepsArchiveWhenDatabaseDeleteFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("base"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	for key, value := range map[string]string{
		"pal_server_path":       serverPath,
		"auto_backup_retention": "20",
	} {
		if _, err := panel.db.Exec(
			`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key,
			value,
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	oldAuto, err := panel.createBackup("auto", "old")
	if err != nil {
		t.Fatalf("old auto createBackup() error = %v", err)
	}
	if err := os.WriteFile(savePath, []byte("new"), 0o644); err != nil {
		t.Fatalf("update save: %v", err)
	}
	if _, err := panel.createBackup("auto", "new"); err != nil {
		t.Fatalf("new auto createBackup() error = %v", err)
	}
	if _, err := panel.db.Exec(`CREATE TRIGGER block_backup_prune BEFORE DELETE ON backups BEGIN SELECT RAISE(FAIL, 'delete blocked'); END`); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}

	err = panel.pruneAutomaticBackups(1)
	if err == nil || !strings.Contains(err.Error(), "delete blocked") {
		t.Fatalf("pruneAutomaticBackups() error = %v, want delete blocked", err)
	}
	if !fileExists(oldAuto.Path) {
		t.Fatalf("old auto backup file was removed after failed database delete: %s", oldAuto.Path)
	}
	if _, err := panel.getBackup(oldAuto.ID); err != nil {
		t.Fatalf("old auto row missing after failed database delete: %v", err)
	}
}

func TestPruneAutomaticBackupsRestoresRowWhenArchiveRemovalFails(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	setTestAppSetting(t, panel, "auto_backup_retention", "20")

	oldAuto, err := panel.createBackup("auto", "old")
	if err != nil {
		t.Fatalf("old auto createBackup() error = %v", err)
	}
	if err := os.WriteFile(savePath, []byte("new"), 0o644); err != nil {
		t.Fatalf("update save: %v", err)
	}
	if _, err := panel.createBackup("auto", "new"); err != nil {
		t.Fatalf("new auto createBackup() error = %v", err)
	}
	previousRemove := removeBackupPath
	removeBackupPath = func(path string) error {
		if path != oldAuto.Path {
			t.Fatalf("removeBackupPath called for unexpected path: %s", path)
		}
		return errors.New("remove blocked")
	}
	defer func() {
		removeBackupPath = previousRemove
	}()

	err = panel.pruneAutomaticBackups(1)
	if err == nil || !strings.Contains(err.Error(), "remove blocked") {
		t.Fatalf("pruneAutomaticBackups() error = %v, want remove blocked", err)
	}
	if !fileExists(oldAuto.Path) {
		t.Fatalf("old auto backup file missing after failed archive removal: %s", oldAuto.Path)
	}
	restored, err := panel.getBackup(oldAuto.ID)
	if err != nil {
		t.Fatalf("old auto row was not restored after failed archive removal: %v", err)
	}
	if restored.Path != oldAuto.Path || restored.Type != oldAuto.Type || restored.Note != oldAuto.Note {
		t.Fatalf("restored old auto row = %#v, want %#v", restored, oldAuto)
	}
}

func TestRestoreBackupRejectsExternalRunningServer(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	savePath := filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "world.txt")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		t.Fatalf("mkdir save dir: %v", err)
	}
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	backup, err := panel.createBackup("manual", "before external restore")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("modify save: %v", err)
	}
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return true, nil
	}

	_, err = panel.restoreBackup(backup.ID)
	if !errors.Is(err, errExternalServerRunning) {
		t.Fatalf("restoreBackup() error = %v, want errExternalServerRunning", err)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save after rejected restore: %v", err)
	}
	if string(content) != "changed" {
		t.Fatalf("restore changed save content despite external server guard: %q", content)
	}
}

func TestRestoreBackupStopsAndRestartsManagedServer(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	backup, err := panel.createBackup("manual", "managed restore")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("modify save: %v", err)
	}

	managedRunning := true
	var stopCalls int
	var startCalls int
	panel.isServerRunningFunc = func() bool {
		return managedRunning
	}
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		stopCalls++
		managedRunning = false
		return nil
	}
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return false, nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		startCalls++
		content, err := os.ReadFile(savePath)
		if err != nil {
			return err
		}
		if string(content) != "original" {
			return errors.New("PalServer restarted before backup content was restored")
		}
		managedRunning = true
		return nil
	}

	result, err := panel.restoreBackup(backup.ID)
	if err != nil {
		t.Fatalf("restoreBackup() error = %v", err)
	}
	if result.ProtectiveBackup == nil {
		t.Fatalf("restore did not create protective backup: %#v", result)
	}
	if stopCalls != 1 || startCalls != 1 || !managedRunning {
		t.Fatalf("stop/start/running = %d/%d/%v, want 1/1/true", stopCalls, startCalls, managedRunning)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read restored save: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("restored content = %q", content)
	}
}

func TestRestoreBackupRejectsExternalServerAfterStoppingManagedServer(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	backup, err := panel.createBackup("manual", "mixed restore")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("modify save: %v", err)
	}

	managedRunning := true
	var stopCalls int
	var startCalls int
	panel.isServerRunningFunc = func() bool {
		return managedRunning
	}
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		stopCalls++
		managedRunning = false
		return nil
	}
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return true, nil
	}
	panel.startServerProcessFunc = func(settings settingsPayload) error {
		startCalls++
		return nil
	}

	_, err = panel.restoreBackup(backup.ID)
	if !errors.Is(err, errExternalServerRunning) {
		t.Fatalf("restoreBackup() error = %v, want errExternalServerRunning", err)
	}
	if stopCalls != 1 || startCalls != 0 {
		t.Fatalf("stop/start calls = %d/%d, want 1/0", stopCalls, startCalls)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save after rejected restore: %v", err)
	}
	if string(content) != "changed" {
		t.Fatalf("restore changed save content despite mixed runtime guard: %q", content)
	}
}

func TestRestoreBackupRejectsExternalServerBeforeCreatingServerDirectory(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	backupPath := filepath.Join(panel.backupDir(), "external-before-mkdir.zip")
	writeTestBackupZip(t, backupPath, map[string]string{
		"Pal/Saved/SaveGames/world.txt": "restored",
	})
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		filepath.Base(backupPath),
		backupPath,
		int64(1),
		"manual",
		"external preflight",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert backup record: %v", err)
	}
	backupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	serverRoot := filepath.Join(t.TempDir(), "missing-palserver")
	setTestAppSetting(t, panel, "pal_server_path", serverRoot)
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return true, nil
	}

	_, err = panel.restoreBackup(backupID)
	if !errors.Is(err, errExternalServerRunning) {
		t.Fatalf("restoreBackup() error = %v, want errExternalServerRunning", err)
	}
	if _, err := os.Stat(serverRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("server directory stat error = %v, want not exist", err)
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	for _, backup := range backups {
		if backup.Type == "pre_restore" {
			t.Fatalf("protective backup created despite rejected restore: %#v", backups)
		}
	}
}

func TestRestoreBackupRejectsCorruptArchiveBeforeFileSideEffects(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	backupPath := filepath.Join(panel.backupDir(), "corrupt-restore.zip")
	writeCorruptTestBackupZip(t, backupPath)
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat corrupt backup: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		filepath.Base(backupPath),
		backupPath,
		info.Size(),
		"manual",
		"corrupt restore",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert backup record: %v", err)
	}
	backupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	var stopCalls int
	panel.isServerRunningFunc = func() bool {
		return true
	}
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		stopCalls++
		return nil
	}
	_, err = panel.restoreBackup(backupID)
	if err == nil {
		t.Fatalf("restoreBackup() succeeded for corrupt archive")
	}
	if stopCalls != 0 {
		t.Fatalf("managed server stop calls = %d, want 0 before corrupt archive rejection", stopCalls)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save after corrupt restore rejection: %v", err)
	}
	if string(content) != "changed" {
		t.Fatalf("corrupt restore changed save content: %q", content)
	}
	if fileExists(filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "bad.txt")) {
		t.Fatalf("corrupt restore created later archive entry")
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	for _, backup := range backups {
		if backup.Type == "pre_restore" {
			t.Fatalf("protective backup created despite corrupt archive preflight failure: %#v", backups)
		}
	}
}

func TestRestoreBackupRejectsSymlinkEntryBeforeFileSideEffects(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	backupPath := filepath.Join(panel.backupDir(), "symlink-entry.zip")
	writeSymlinkEntryBackupZip(t, backupPath)
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat symlink backup: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		filepath.Base(backupPath),
		backupPath,
		info.Size(),
		"manual",
		"symlink restore",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert backup record: %v", err)
	}
	backupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	var stopCalls int
	panel.isServerRunningFunc = func() bool {
		return true
	}
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		stopCalls++
		return nil
	}
	_, err = panel.restoreBackup(backupID)
	if err == nil || !strings.Contains(err.Error(), "symlink entry") {
		t.Fatalf("restoreBackup() error = %v, want symlink entry rejection", err)
	}
	if stopCalls != 0 {
		t.Fatalf("managed server stop calls = %d, want 0 before symlink archive rejection", stopCalls)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save after symlink restore rejection: %v", err)
	}
	if string(content) != "changed" {
		t.Fatalf("symlink restore changed save content: %q", content)
	}
	if fileExists(filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "linked.txt")) {
		t.Fatalf("symlink restore created linked archive entry")
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	for _, backup := range backups {
		if backup.Type == "pre_restore" {
			t.Fatalf("protective backup created despite symlink archive preflight failure: %#v", backups)
		}
	}
}

func TestRestoreBackupRejectsOversizedArchiveBeforeFileSideEffects(t *testing.T) {
	setBackupRestoreLimits(t, 64, 16, 10)

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
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	backupPath := filepath.Join(panel.backupDir(), "oversized-restore.zip")
	writeTestBackupZip(t, backupPath, map[string]string{
		"Pal/Saved/SaveGames/world.txt": "restored",
		"Pal/Saved/SaveGames/large.bin": strings.Repeat("x", 17),
	})
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat oversized backup: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		filepath.Base(backupPath),
		backupPath,
		info.Size(),
		"manual",
		"oversized restore",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert backup record: %v", err)
	}
	backupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	var stopCalls int
	panel.isServerRunningFunc = func() bool {
		return true
	}
	panel.stopServerProcessFunc = func(timeout time.Duration) error {
		stopCalls++
		return nil
	}
	_, err = panel.restoreBackup(backupID)
	if err == nil || !strings.Contains(err.Error(), "expands beyond") {
		t.Fatalf("restoreBackup() error = %v, want expanded-size rejection", err)
	}
	if stopCalls != 0 {
		t.Fatalf("managed server stop calls = %d, want 0 before oversized archive rejection", stopCalls)
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save after oversized restore rejection: %v", err)
	}
	if string(content) != "changed" {
		t.Fatalf("oversized restore changed save content: %q", content)
	}
	if fileExists(filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "large.bin")) {
		t.Fatalf("oversized restore created archive entry")
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	for _, backup := range backups {
		if backup.Type == "pre_restore" {
			t.Fatalf("protective backup created despite oversized archive preflight failure: %#v", backups)
		}
	}
}

func TestValidateZipBackupRejectsRestoreResourceLimits(t *testing.T) {
	t.Run("single entry size", func(t *testing.T) {
		setBackupRestoreLimits(t, 4, 64, 10)
		backupPath := filepath.Join(t.TempDir(), "large-entry.zip")
		writeTestBackupZip(t, backupPath, map[string]string{
			"Pal/Saved/SaveGames/large.bin": "12345",
		})
		err := validateZipBackup(backupPath, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "entry Pal/Saved/SaveGames/large.bin exceeds") {
			t.Fatalf("validateZipBackup() error = %v, want entry-size rejection", err)
		}
	})

	t.Run("file count", func(t *testing.T) {
		setBackupRestoreLimits(t, 64, 64, 1)
		backupPath := filepath.Join(t.TempDir(), "too-many-files.zip")
		writeTestBackupZip(t, backupPath, map[string]string{
			"Pal/Saved/SaveGames/one.txt": "1",
			"Pal/Saved/SaveGames/two.txt": "2",
		})
		err := validateZipBackup(backupPath, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "more than 1 files") {
			t.Fatalf("validateZipBackup() error = %v, want file-count rejection", err)
		}
	})
}

func TestRestoreBackupReplacementFailureKeepsExistingFile(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	backupPath := filepath.Join(panel.backupDir(), "restore-replace-fails.zip")
	writeTestBackupZip(t, backupPath, map[string]string{
		"Pal/Saved/SaveGames/world.txt": "restored",
	})
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		filepath.Base(backupPath),
		backupPath,
		info.Size(),
		"manual",
		"restore replacement failure",
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert backup record: %v", err)
	}
	backupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	previousReplace := atomicReplaceFile
	atomicReplaceFile = func(src, dst string) error {
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("replacement source missing before injected failure: %v", err)
		}
		return os.ErrPermission
	}
	defer func() {
		atomicReplaceFile = previousReplace
	}()

	_, err = panel.restoreBackup(backupID)
	if err == nil {
		t.Fatal("restoreBackup() error = nil, want injected replacement failure")
	}
	content, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save after failed restore replacement: %v", err)
	}
	if string(content) != "changed" {
		t.Fatalf("failed restore replacement changed save content: %q", content)
	}
	entries, err := os.ReadDir(filepath.Dir(savePath))
	if err != nil {
		t.Fatalf("read save dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".world.txt.tmp-") {
			t.Fatalf("temporary restore file was not removed: %s", entry.Name())
		}
	}
}

func TestBackupRequestsRestSaveWhenCredentialsAreConfigured(t *testing.T) {
	var sawSave bool
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/api/save" {
			http.NotFound(w, r)
			return
		}
		sawSave = true
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer pal.Close()

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
	if err := os.WriteFile(savePath, []byte("base"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	for key, value := range map[string]string{
		"pal_server_path":   serverPath,
		"rest_api_url":      pal.URL + "/v1/api",
		"rest_api_username": "admin",
		"rest_api_password": "secret",
	} {
		if _, err := panel.db.Exec(
			`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key,
			value,
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	backup, err := panel.createBackup("manual", "save first")
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if !sawSave {
		t.Fatalf("REST /save was not requested before backup")
	}
	if !strings.Contains(backup.Note, "REST save requested before backup") {
		t.Fatalf("backup note did not include save result: %q", backup.Note)
	}
}

func TestAutoBackupDueAndRunDueAutoBackup(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	now := time.Now().UTC().Truncate(time.Second)
	due, err := panel.autoBackupDue(now, 24*time.Hour)
	if err != nil {
		t.Fatalf("autoBackupDue() error = %v", err)
	}
	if !due {
		t.Fatalf("autoBackupDue() = false with no backups")
	}
	if _, err := panel.db.Exec(
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		"recent.zip",
		filepath.Join(panel.backupDir(), "recent.zip"),
		1,
		"auto",
		"",
		now.Add(-time.Hour).Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert recent backup: %v", err)
	}
	due, err = panel.autoBackupDue(now, 24*time.Hour)
	if err != nil {
		t.Fatalf("autoBackupDue() recent error = %v", err)
	}
	if due {
		t.Fatalf("autoBackupDue() = true for recent backup")
	}

	serverPath := t.TempDir()
	savePath := filepath.Join(serverPath, "Pal", "Saved", "SaveGames", "world.txt")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		t.Fatalf("mkdir save dir: %v", err)
	}
	if err := os.WriteFile(savePath, []byte("base"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	for key, value := range map[string]string{
		"pal_server_path":            serverPath,
		"auto_backup_enabled":        "true",
		"auto_backup_interval_hours": "24",
	} {
		if _, err := panel.db.Exec(
			`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key,
			value,
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	if _, err := panel.db.Exec(`DELETE FROM backups WHERE type = 'auto'`); err != nil {
		t.Fatalf("delete auto backups: %v", err)
	}
	if err := panel.runDueAutoBackup(now); err != nil {
		t.Fatalf("runDueAutoBackup() error = %v", err)
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	autoCount := 0
	for _, backup := range backups {
		if backup.Type == "auto" {
			autoCount++
		}
	}
	if autoCount != 1 {
		t.Fatalf("auto backup count = %d, backups=%#v", autoCount, backups)
	}
}

func TestAutoBackupSkipsWhileTaskRunning(t *testing.T) {
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
	if err := os.WriteFile(savePath, []byte("base"), 0o644); err != nil {
		t.Fatalf("write save: %v", err)
	}
	for key, value := range map[string]string{
		"pal_server_path":            serverPath,
		"auto_backup_enabled":        "true",
		"auto_backup_interval_hours": "24",
	} {
		setTestAppSetting(t, panel, key, value)
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	err = panel.runDueAutoBackup(time.Now().UTC())
	panel.taskMu.Lock()
	panel.taskRunning = false
	panel.taskMu.Unlock()
	if !errors.Is(err, errTaskRunning) {
		t.Fatalf("runDueAutoBackup() error = %v, want errTaskRunning", err)
	}

	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	for _, backup := range backups {
		if backup.Type == "auto" {
			t.Fatalf("auto backup created despite active task: %#v", backups)
		}
	}
	tasks, err := panel.listTasks(50)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	for _, task := range tasks {
		if task.Type == "auto_backup" {
			t.Fatalf("auto_backup task created despite active task: %#v", tasks)
		}
	}
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func writeTestBackupZip(t *testing.T, target string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	archive, err := os.Create(target)
	if err != nil {
		t.Fatalf("create backup zip: %v", err)
	}
	writer := zip.NewWriter(archive)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("archive close: %v", err)
	}
}

func writeCorruptTestBackupZip(t *testing.T, target string) {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	files := []struct {
		name    string
		content string
	}{
		{name: "Pal/Saved/SaveGames/world.txt", content: "restored"},
		{name: "Pal/Saved/SaveGames/bad.txt", content: "corrupt-me"},
	}
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Store}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("zip create %s: %v", file.name, err)
		}
		if _, err := entry.Write([]byte(file.content)); err != nil {
			t.Fatalf("zip write %s: %v", file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	data := buf.Bytes()
	index := bytes.Index(data, []byte("corrupt-me"))
	if index < 0 {
		t.Fatalf("corrupt marker not found in stored zip data")
	}
	data[index] = 'C'
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("write corrupt backup zip: %v", err)
	}
}

func writeSymlinkEntryBackupZip(t *testing.T, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	archive, err := os.Create(target)
	if err != nil {
		t.Fatalf("create backup zip: %v", err)
	}
	writer := zip.NewWriter(archive)
	header := &zip.FileHeader{
		Name:   "Pal/Saved/SaveGames/linked.txt",
		Method: zip.Deflate,
	}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatalf("zip create symlink entry: %v", err)
	}
	if _, err := entry.Write([]byte("../../outside.txt")); err != nil {
		t.Fatalf("zip write symlink entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("archive close: %v", err)
	}
}

func setBackupRestoreLimits(t *testing.T, entryBytes, totalBytes int64, fileCount int) {
	t.Helper()
	previousEntryBytes := maxBackupRestoreEntryBytes
	previousTotalBytes := maxBackupRestoreTotalBytes
	previousFileCount := maxBackupRestoreFileCount
	maxBackupRestoreEntryBytes = entryBytes
	maxBackupRestoreTotalBytes = totalBytes
	maxBackupRestoreFileCount = fileCount
	t.Cleanup(func() {
		maxBackupRestoreEntryBytes = previousEntryBytes
		maxBackupRestoreTotalBytes = previousTotalBytes
		maxBackupRestoreFileCount = previousFileCount
	})
}

func assertNoFinalBackupArchives(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatalf("read backup dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".zip") {
			t.Fatalf("unexpected final backup archive after failed create: %s", entry.Name())
		}
	}
}

func assertBackupRowCount(t *testing.T, panel *App, want int) {
	t.Helper()
	var count int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&count); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if count != want {
		t.Fatalf("backup rows = %d, want %d", count, want)
	}
}

func requireTaskWithStatus(t *testing.T, panel *App, taskType, status string) taskRecord {
	t.Helper()
	tasks, err := panel.listTasks(50)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	for _, task := range tasks {
		if task.Type == taskType && task.Status == status {
			if strings.TrimSpace(task.Log) == "" {
				t.Fatalf("task %s/%s has empty log: %#v", taskType, status, task)
			}
			return task
		}
	}
	t.Fatalf("task %s/%s not found in %#v", taskType, status, tasks)
	return taskRecord{}
}
