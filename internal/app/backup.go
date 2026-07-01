package app

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errNoBackupSources = errors.New("no backup sources exist under pal_server_path")

var (
	maxBackupRestoreEntryBytes int64 = 8 << 30
	maxBackupRestoreTotalBytes int64 = 64 << 30
	maxBackupRestoreFileCount        = 200000
)

var renameBackupArchive = os.Rename
var removeBackupPath = os.Remove
var commitBackupDeleteTx = func(tx *sql.Tx) error {
	return tx.Commit()
}

type backupRecord struct {
	ID        int64  `json:"id"`
	Filename  string `json:"filename"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Type      string `json:"type"`
	Note      string `json:"note,omitempty"`
	CreatedAt string `json:"created_at"`
}

type backupRequest struct {
	Type string `json:"type"`
	Note string `json:"note"`
}

type restoreResponse struct {
	RestoredBackup   backupRecord  `json:"restored_backup"`
	ProtectiveBackup *backupRecord `json:"protective_backup,omitempty"`
}

func (a *App) handleListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := a.listBackups()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, backups)
}

func (a *App) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	var payload backupRequest
	if !decodeOptionalJSON(w, r, &payload) {
		return
	}
	backupType := normalizeBackupType(payload.Type)
	payload.Type = backupType
	taskType := "backup_" + backupType
	startMessage := fmt.Sprintf("Starting %s backup", backupType)
	var backup backupRecord
	err := a.runOperationTask(taskType, startMessage, "", func(taskID int64) error {
		var err error
		backup, err = a.createBackup(backupType, payload.Note)
		if err != nil {
			return err
		}
		a.logTaskf(taskID, "Backup created: %s (%d bytes)", backup.Filename, backup.Size)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, backup)
}

func (a *App) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	id, err := parseIDPathValue(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var result restoreResponse
	err = a.runOperationTask("backup_restore", fmt.Sprintf("Restoring backup #%d", id), "Backup restore completed", func(taskID int64) error {
		var err error
		result, err = a.restoreBackup(id)
		if err != nil {
			return err
		}
		a.logTaskf(taskID, "Restored backup: %s", result.RestoredBackup.Filename)
		if result.ProtectiveBackup != nil {
			a.logTaskf(taskID, "Protective backup created: %s", result.ProtectiveBackup.Filename)
		}
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	id, err := parseIDPathValue(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	err = a.runOperationTask("backup_delete", fmt.Sprintf("Deleting backup #%d", id), "Backup delete completed", func(taskID int64) error {
		if err := a.deleteBackup(id); err != nil {
			return err
		}
		a.logTaskf(taskID, "Deleted backup #%d", id)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) createBackup(backupType, note string) (backupRecord, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return backupRecord{}, err
	}
	return a.createBackupWithSettings(settings, backupType, note)
}

func (a *App) createBackupWithSettings(settings settingsPayload, backupType, note string) (backupRecord, error) {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()

	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return backupRecord{}, errors.New("pal_server_path is required before creating backups")
	}
	base, err := filepath.Abs(serverPath)
	if err != nil {
		return backupRecord{}, err
	}
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return backupRecord{}, fmt.Errorf("pal_server_path is not a directory: %s", serverPath)
	}

	sources, err := backupSources(base)
	if err != nil {
		return backupRecord{}, err
	}
	if len(sources) == 0 {
		return backupRecord{}, errNoBackupSources
	}
	note = appendBackupNote(note, a.trySaveWorldBeforeBackup(settings))

	backupType = sanitizeBackupType(backupType)
	if backupType == "" {
		backupType = "manual"
	}
	dir := a.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return backupRecord{}, err
	}
	filename := fmt.Sprintf("palpanel_%s_%s.zip", backupType, time.Now().UTC().Format("20060102T150405.000000000Z"))
	targetPath := filepath.Join(dir, filename)
	tmp, err := os.CreateTemp(dir, filename+".*.tmp")
	if err != nil {
		return backupRecord{}, err
	}
	tmpPath := tmp.Name()
	if err := writeZipBackup(tmp, base, sources); err != nil {
		tmp.Close()
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)
	tx, err := a.db.Begin()
	if err != nil {
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	defer tx.Rollback()
	result, err := a.execBackupRecordInsertTx(
		tx,
		`INSERT INTO backups(filename, path, size, type, note, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		filename,
		targetPath,
		info.Size(),
		backupType,
		strings.TrimSpace(note),
		createdAt,
	)
	if err != nil {
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	if err := renameBackupArchive(tmpPath, targetPath); err != nil {
		_ = removeBackupPath(tmpPath)
		return backupRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		if removeErr := removeBackupPath(targetPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return backupRecord{}, fmt.Errorf("%w; failed to remove untracked backup archive: %v", err, removeErr)
		}
		return backupRecord{}, err
	}
	record := backupRecord{
		ID:        id,
		Filename:  filename,
		Path:      targetPath,
		Size:      info.Size(),
		Type:      backupType,
		Note:      strings.TrimSpace(note),
		CreatedAt: createdAt,
	}
	if backupType != "manual" {
		_ = a.pruneAutomaticBackups(settings.AutoBackupRetention)
	}
	return record, nil
}

func (a *App) execBackupRecordInsert(query string, args ...any) (sql.Result, error) {
	return execBackupRecordInsertWithRetry(a.db.Exec, query, args...)
}

func (a *App) execBackupRecordInsertTx(tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return execBackupRecordInsertWithRetry(tx.Exec, query, args...)
}

func execBackupRecordInsertWithRetry(exec func(string, ...any) (sql.Result, error), query string, args ...any) (sql.Result, error) {
	var result sql.Result
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		result, err = exec(query, args...)
		if err == nil {
			return result, nil
		}
		if !isTemporaryDatabaseBusy(err) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *App) trySaveWorldBeforeBackup(settings settingsPayload) string {
	if strings.TrimSpace(settings.RestAPIURL) == "" || strings.TrimSpace(settings.RestAPIPassword) == "" {
		return ""
	}
	client, err := a.newPalAPIClientFromSettings(settings)
	if err != nil {
		return "REST save before backup failed: " + err.Error()
	}
	if err := client.post("/save", nil, nil); err != nil {
		return "REST save before backup failed: " + err.Error()
	}
	return "REST save requested before backup"
}

func appendBackupNote(note, extra string) string {
	note = strings.TrimSpace(note)
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return note
	}
	if note == "" {
		return extra
	}
	return note + " | " + extra
}

func (a *App) createBackupIfPossible(backupType, note string) (*backupRecord, error) {
	record, err := a.createBackup(backupType, note)
	if err != nil {
		if errors.Is(err, errNoBackupSources) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (a *App) createBackupIfPossibleWithSettings(settings settingsPayload, backupType, note string) (*backupRecord, error) {
	record, err := a.createBackupWithSettings(settings, backupType, note)
	if err != nil {
		if errors.Is(err, errNoBackupSources) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (a *App) restoreBackup(id int64) (restoreResponse, error) {
	record, err := a.getBackup(id)
	if err != nil {
		return restoreResponse{}, err
	}
	if err := a.ensureBackupPathAllowed(record.Path); err != nil {
		return restoreResponse{}, err
	}
	if !fileExists(record.Path) {
		return restoreResponse{}, fmt.Errorf("backup file is missing: %s", record.Path)
	}
	settings, err := a.loadSettings()
	if err != nil {
		return restoreResponse{}, err
	}
	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return restoreResponse{}, errors.New("pal_server_path is required before restoring backups")
	}
	base, err := filepath.Abs(serverPath)
	if err != nil {
		return restoreResponse{}, err
	}
	if err := validateZipBackup(record.Path, base); err != nil {
		return restoreResponse{}, err
	}

	wasRunning := a.isManagedServerRunning()
	if wasRunning {
		if err := a.stopManagedServerProcess(30 * time.Second); err != nil {
			return restoreResponse{}, err
		}
	}
	if err := a.ensureNoExternalServerRunning(settings); err != nil {
		return restoreResponse{}, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return restoreResponse{}, err
	}
	protective, err := a.createBackupIfPossible("pre_restore", fmt.Sprintf("Before restoring backup #%d", id))
	if err != nil {
		return restoreResponse{}, err
	}
	if err := extractZipBackup(record.Path, base); err != nil {
		return restoreResponse{}, err
	}
	if wasRunning {
		if err := a.startManagedServerProcessAdmitted(settings); err != nil {
			return restoreResponse{}, err
		}
	}
	return restoreResponse{RestoredBackup: record, ProtectiveBackup: protective}, nil
}

func (a *App) deleteBackup(id int64) error {
	record, err := a.getBackup(id)
	if err != nil {
		return err
	}
	if err := a.ensureBackupPathAllowed(record.Path); err != nil {
		return err
	}
	return a.deleteBackupRecord(record)
}

func (a *App) deleteBackupRecord(record backupRecord) error {
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
	if _, err := tx.Exec(`DELETE FROM backups WHERE id = ?`, record.ID); err != nil {
		return err
	}
	if err := commitBackupDeleteTx(tx); err != nil {
		return err
	}
	committed = true
	if err := removeBackupPath(record.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		if restoreErr := a.restoreDeletedBackupRecord(record); restoreErr != nil {
			return fmt.Errorf("%w; failed to restore backup database state: %v", err, restoreErr)
		}
		return err
	}
	return nil
}

func (a *App) restoreDeletedBackupRecord(record backupRecord) error {
	_, err := a.db.Exec(
		`INSERT INTO backups(id, filename, path, size, type, note, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			filename = excluded.filename,
			path = excluded.path,
			size = excluded.size,
			type = excluded.type,
			note = excluded.note,
			created_at = excluded.created_at`,
		record.ID,
		record.Filename,
		record.Path,
		record.Size,
		record.Type,
		record.Note,
		record.CreatedAt,
	)
	return err
}

func (a *App) getBackup(id int64) (backupRecord, error) {
	var record backupRecord
	var note sql.NullString
	err := a.db.QueryRow(
		`SELECT id, filename, path, size, type, note, CAST(created_at AS TEXT)
		 FROM backups
		 WHERE id = ?`,
		id,
	).Scan(&record.ID, &record.Filename, &record.Path, &record.Size, &record.Type, &note, &record.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return backupRecord{}, fmt.Errorf("backup not found: %d", id)
		}
		return backupRecord{}, err
	}
	if note.Valid {
		record.Note = note.String
	}
	return record, nil
}

func (a *App) listBackups() ([]backupRecord, error) {
	return a.listBackupsLimit(0)
}

func (a *App) listBackupsLimit(limit int) ([]backupRecord, error) {
	query := `SELECT id, filename, path, size, type, note, CAST(created_at AS TEXT)
		 FROM backups
		 ORDER BY id DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := a.db.Query(
		query,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []backupRecord
	for rows.Next() {
		var record backupRecord
		var note sql.NullString
		if err := rows.Scan(&record.ID, &record.Filename, &record.Path, &record.Size, &record.Type, &note, &record.CreatedAt); err != nil {
			return nil, err
		}
		if note.Valid {
			record.Note = note.String
		}
		record = a.sanitizeBackupRecordForRead(record)
		backups = append(backups, record)
	}
	return backups, rows.Err()
}

func (a *App) sanitizeBackupRecordForRead(record backupRecord) backupRecord {
	if err := a.ensureBackupPathAllowed(record.Path); err != nil {
		record.Path = ""
	}
	return record
}

func (a *App) pruneAutomaticBackups(retention int) error {
	if retention <= 0 {
		retention = 20
	}
	rows, err := a.db.Query(
		`SELECT id, filename, path, size, type, note, CAST(created_at AS TEXT)
		 FROM backups
		 WHERE type <> 'manual'
		 ORDER BY id DESC`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var remove []backupRecord
	kept := 0
	for rows.Next() {
		var record backupRecord
		var note sql.NullString
		if err := rows.Scan(&record.ID, &record.Filename, &record.Path, &record.Size, &record.Type, &note, &record.CreatedAt); err != nil {
			return err
		}
		if note.Valid {
			record.Note = note.String
		}
		if kept < retention {
			kept++
			continue
		}
		remove = append(remove, record)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, record := range remove {
		if err := a.ensureBackupPathAllowed(record.Path); err != nil {
			return err
		}
		if err := a.deleteBackupRecord(record); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) backupDir() string {
	return filepath.Join(a.dataDir, "backups")
}

func (a *App) ensureBackupPathAllowed(path string) error {
	base, err := filepath.Abs(a.backupDir())
	if err != nil {
		return err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return ensureWithin(base, target)
}

func backupSources(base string) ([]string, error) {
	candidates := []string{
		filepath.Join(base, "Pal", "Saved"),
		filepath.Join(base, "Mods"),
		filepath.Join(base, "DefaultPalWorldSettings.ini"),
	}
	configPath, _, _, err := palConfigPaths(base)
	if err == nil {
		candidates = append(candidates, configPath)
	}
	var sources []string
	covered := make(map[string]bool)
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return nil, err
		}
		if err := ensureWithin(base, abs); err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if isCoveredBy(abs, covered) {
			continue
		}
		sources = append(sources, abs)
		info, err := os.Stat(abs)
		if err == nil && info.IsDir() {
			covered[abs] = true
		}
	}
	return sources, nil
}

func isCoveredBy(path string, dirs map[string]bool) bool {
	for dir := range dirs {
		rel, err := filepath.Rel(dir, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func writeZipBackup(file *os.File, base string, sources []string) error {
	writer := zip.NewWriter(file)
	defer writer.Close()
	var limits backupCreationScanState

	manifest := map[string]any{
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"tool":       "palpanel-lite",
		"version":    "v0.3",
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	header := &zip.FileHeader{
		Name:     ".palpanel-backup.json",
		Method:   zip.Deflate,
		Modified: time.Now(),
	}
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := entry.Write(manifestBytes); err != nil {
		return err
	}

	for _, source := range sources {
		if err := filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
				return fmt.Errorf("backup source escapes server directory: %s", path)
			}
			rel = filepath.ToSlash(rel)
			if err := limits.checkFile(info, rel); err != nil {
				return err
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = rel
			header.Method = zip.Deflate
			entry, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			limited := &backupCreationLimitReader{
				reader:     src,
				entryName:  rel,
				totalBytes: &limits.copiedBytes,
			}
			_, copyErr := io.Copy(entry, limited)
			closeErr := src.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}); err != nil {
			return err
		}
	}
	return nil
}

type backupCreationScanState struct {
	fileCount     int
	declaredBytes int64
	copiedBytes   int64
}

func (s *backupCreationScanState) checkFile(info os.FileInfo, name string) error {
	s.fileCount++
	if s.fileCount > maxBackupRestoreFileCount {
		return fmt.Errorf("backup source contains more than %d files", maxBackupRestoreFileCount)
	}
	size := info.Size()
	if size < 0 {
		return fmt.Errorf("backup source file %s has invalid size", name)
	}
	if size > maxBackupRestoreEntryBytes {
		return fmt.Errorf("backup source file %s exceeds %d bytes", name, maxBackupRestoreEntryBytes)
	}
	remaining := maxBackupRestoreTotalBytes - s.declaredBytes
	if remaining < 0 || size > remaining {
		return fmt.Errorf("backup source data exceeds %d bytes", maxBackupRestoreTotalBytes)
	}
	s.declaredBytes += size
	return nil
}

type backupCreationLimitReader struct {
	reader     io.Reader
	entryName  string
	entryBytes int64
	totalBytes *int64
}

func (r *backupCreationLimitReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	entryRemaining := maxBackupRestoreEntryBytes - r.entryBytes
	if entryRemaining <= 0 {
		return r.readLimitProbe(fmt.Errorf("backup source file %s exceeds %d bytes", r.entryName, maxBackupRestoreEntryBytes))
	}
	totalRemaining := maxBackupRestoreTotalBytes - *r.totalBytes
	if totalRemaining <= 0 {
		return r.readLimitProbe(fmt.Errorf("backup source data exceeds %d bytes", maxBackupRestoreTotalBytes))
	}
	allowed := entryRemaining
	if totalRemaining < allowed {
		allowed = totalRemaining
	}
	if int64(len(p)) > allowed {
		p = p[:allowed]
	}
	n, err := r.reader.Read(p)
	r.entryBytes += int64(n)
	*r.totalBytes += int64(n)
	return n, err
}

func (r *backupCreationLimitReader) readLimitProbe(limitErr error) (int, error) {
	var one [1]byte
	n, err := r.reader.Read(one[:])
	if n > 0 {
		return 0, limitErr
	}
	if err == nil {
		return 0, io.ErrNoProgress
	}
	return 0, err
}

func extractZipBackup(zipPath, base string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var limits backupRestoreScanState
	for _, file := range reader.File {
		name := strings.TrimPrefix(filepath.Clean(filepath.FromSlash(file.Name)), string(filepath.Separator))
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup archive contains symlink entry: %s", file.Name)
		}
		if err := limits.checkFileHeader(file); err != nil {
			return err
		}
		if name == "." || name == ".palpanel-backup.json" {
			continue
		}
		target := filepath.Join(base, name)
		if err := ensureWithin(base, target); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		perm := file.Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		limited := &backupRestoreLimitReader{
			reader:     src,
			entryName:  file.Name,
			totalBytes: &limits.readBytes,
		}
		writeErr := atomicWriteFileFromReader(target, limited, perm)
		closeErr := src.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func validateZipBackup(zipPath, base string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var limits backupRestoreScanState
	for _, file := range reader.File {
		name := strings.TrimPrefix(filepath.Clean(filepath.FromSlash(file.Name)), string(filepath.Separator))
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup archive contains symlink entry: %s", file.Name)
		}
		if err := limits.checkFileHeader(file); err != nil {
			return err
		}
		if name != "." {
			target := filepath.Join(base, name)
			if err := ensureWithin(base, target); err != nil {
				return err
			}
		}
		if file.FileInfo().IsDir() {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		limited := &backupRestoreLimitReader{
			reader:     src,
			entryName:  file.Name,
			totalBytes: &limits.readBytes,
		}
		_, copyErr := io.Copy(io.Discard, limited)
		closeErr := src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type backupRestoreScanState struct {
	fileCount     int
	declaredBytes int64
	readBytes     int64
}

func (s *backupRestoreScanState) checkFileHeader(file *zip.File) error {
	if file.FileInfo().IsDir() {
		return nil
	}
	s.fileCount++
	if s.fileCount > maxBackupRestoreFileCount {
		return fmt.Errorf("backup archive contains more than %d files", maxBackupRestoreFileCount)
	}
	declared := file.UncompressedSize64
	if declared > uint64(maxBackupRestoreEntryBytes) {
		return fmt.Errorf("backup archive entry %s exceeds %d bytes", file.Name, maxBackupRestoreEntryBytes)
	}
	remaining := maxBackupRestoreTotalBytes - s.declaredBytes
	if remaining < 0 || declared > uint64(remaining) {
		return fmt.Errorf("backup archive expands beyond %d bytes", maxBackupRestoreTotalBytes)
	}
	s.declaredBytes += int64(declared)
	return nil
}

type backupRestoreLimitReader struct {
	reader     io.Reader
	entryName  string
	entryBytes int64
	totalBytes *int64
}

func (r *backupRestoreLimitReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	entryRemaining := maxBackupRestoreEntryBytes - r.entryBytes
	if entryRemaining <= 0 {
		return r.readLimitProbe(fmt.Errorf("backup archive entry %s exceeds %d bytes", r.entryName, maxBackupRestoreEntryBytes))
	}
	totalRemaining := maxBackupRestoreTotalBytes - *r.totalBytes
	if totalRemaining <= 0 {
		return r.readLimitProbe(fmt.Errorf("backup archive expands beyond %d bytes", maxBackupRestoreTotalBytes))
	}
	allowed := entryRemaining
	if totalRemaining < allowed {
		allowed = totalRemaining
	}
	if int64(len(p)) > allowed {
		p = p[:allowed]
	}
	n, err := r.reader.Read(p)
	r.entryBytes += int64(n)
	*r.totalBytes += int64(n)
	return n, err
}

func (r *backupRestoreLimitReader) readLimitProbe(limitErr error) (int, error) {
	var one [1]byte
	n, err := r.reader.Read(one[:])
	if n > 0 {
		return 0, limitErr
	}
	if err == nil {
		return 0, io.ErrNoProgress
	}
	return 0, err
}

func sanitizeBackupType(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func normalizeBackupType(input string) string {
	backupType := sanitizeBackupType(input)
	if backupType == "" {
		return "manual"
	}
	return backupType
}

func parseIDPathValue(r *http.Request, name string) (int64, error) {
	value := r.PathValue(name)
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s: %s", name, value)
	}
	return id, nil
}
