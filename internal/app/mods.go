package app

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type modRecord struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	PackageName     string `json:"package_name"`
	Version         string `json:"version"`
	Author          string `json:"author"`
	FolderName      string `json:"folder_name"`
	Enabled         bool   `json:"enabled"`
	ServerSupported bool   `json:"server_supported"`
	InfoJSON        string `json:"info_json"`
	InstallPath     string `json:"install_path"`
	InstalledAt     string `json:"installed_at"`
	UpdatedAt       string `json:"updated_at"`
}

type modInfo struct {
	Name            string
	PackageName     string
	Version         string
	Author          string
	ServerSupported bool
	InfoJSON        string
	SourceRoot      string
}

type openDirectoryResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type workshopModDownloadRequest struct {
	WorkshopID  string `json:"workshop_id"`
	WorkshopURL string `json:"workshop_url"`
}

const palworldWorkshopAppID = "1623730"
const maxModArchiveUploadBytes int64 = 512 << 20
const maxModInfoJSONBytes int64 = 1 << 20
const modExtractorOutputTruncatedSuffix = " [... output truncated]"

var modArchiveMultipartMemoryBytes int64 = 32 << 20
var maxPalModSettingsBytes int64 = 1 << 20
var errPalModSettingsTooLarge = errors.New("PalModSettings.ini is too large")

var (
	maxModExtractedEntryBytes  int64 = 2 << 30
	maxModExtractedTotalBytes  int64 = 8 << 30
	maxModExtractedFileCount         = 100000
	modExtractorTimeout              = 5 * time.Minute
	maxModExtractorOutputBytes int64 = 64 << 10
)

var commitModInstallTx = func(tx *sql.Tx) error {
	return tx.Commit()
}

var commitModEnabledTx = func(tx *sql.Tx) error {
	return tx.Commit()
}

var renameModPath = os.Rename
var removeModPath = os.RemoveAll

func (a *App) handleListMods(w http.ResponseWriter, r *http.Request) {
	mods, err := a.listMods()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, mods)
}

func (a *App) handleUploadMod(w http.ResponseWriter, r *http.Request) {
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()

	file, header, cleanup, ok := parseModArchiveUpload(w, r, maxModArchiveUploadBytes)
	if !ok {
		return
	}
	defer cleanup()

	var mod modRecord
	err = a.runOperationTaskAdmitted("mod_upload", fmt.Sprintf("Installing MOD archive %s", header.Filename), "", func(taskID int64) error {
		var err error
		mod, err = a.installUploadedMod(file, header)
		if err != nil {
			return err
		}
		a.logTaskf(taskID, "MOD installed: %s version %s", mod.PackageName, mod.Version)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, mod)
}

func (a *App) handleDownloadWorkshopMod(w http.ResponseWriter, r *http.Request) {
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()

	var req workshopModDownloadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	workshopID, err := normalizeSteamWorkshopID(firstNonEmpty(req.WorkshopID, req.WorkshopURL))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var mod modRecord
	err = a.runOperationTaskAdmitted("mod_workshop_download", fmt.Sprintf("Downloading Steam Workshop MOD %s", workshopID), "", func(taskID int64) error {
		var err error
		mod, err = a.installWorkshopMod(workshopID, taskID)
		if err != nil {
			return err
		}
		a.logTaskf(taskID, "Steam Workshop MOD installed: %s version %s", mod.PackageName, mod.Version)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, mod)
}

func (a *App) handleUpdateMod(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	id, err := parseIDPathValue(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	existing, err := a.getMod(id)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()

	file, header, cleanup, ok := parseModArchiveUpload(w, r, maxModArchiveUploadBytes)
	if !ok {
		return
	}
	defer cleanup()

	var mod modRecord
	err = a.runOperationTaskAdmitted("mod_update", fmt.Sprintf("Updating MOD %s from %s", existing.PackageName, header.Filename), "", func(taskID int64) error {
		var err error
		mod, err = a.installUploadedModForPackage(file, header, existing.PackageName)
		if err != nil {
			return err
		}
		a.logTaskf(taskID, "MOD updated: %s version %s", mod.PackageName, mod.Version)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, mod)
}

func parseModArchiveUpload(w http.ResponseWriter, r *http.Request, maxBytes int64) (multipart.File, *multipart.FileHeader, func(), bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	multipartMemory := modArchiveMultipartMemoryBytes
	if multipartMemory > maxBytes {
		multipartMemory = maxBytes
	}
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("mod archive upload exceeds %d bytes", maxBytes))
			return nil, nil, nil, false
		}
		writeError(w, http.StatusBadRequest, err)
		return nil, nil, nil, false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("multipart file field is required"))
		return nil, nil, nil, false
	}
	cleanup := func() {
		_ = file.Close()
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}
	return file, header, cleanup, true
}

func (a *App) handleEnableMod(w http.ResponseWriter, r *http.Request) {
	a.handleSetModEnabled(w, r, true)
}

func (a *App) handleDisableMod(w http.ResponseWriter, r *http.Request) {
	a.handleSetModEnabled(w, r, false)
}

func (a *App) handleSetModEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if !requireNoRequestBody(w, r) {
		return
	}
	id, err := parseIDPathValue(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	taskType := "mod_disable"
	action := "Disabling"
	success := "MOD disabled"
	if enabled {
		taskType = "mod_enable"
		action = "Enabling"
		success = "MOD enabled"
	}
	var mod modRecord
	err = a.runOperationTask(taskType, fmt.Sprintf("%s MOD #%d", action, id), success, func(taskID int64) error {
		var err error
		mod, err = a.setModEnabled(id, enabled)
		if err != nil {
			return err
		}
		a.logTaskf(taskID, "%s MOD: %s", action, mod.PackageName)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, mod)
}

func (a *App) handleOpenModDirectory(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	id, err := parseIDPathValue(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path, err := a.openModDirectoryByID(id)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, openDirectoryResponse{Status: "ok", Path: path})
}

func (a *App) handleDeleteMod(w http.ResponseWriter, r *http.Request) {
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
	err = a.runOperationTask("mod_delete", fmt.Sprintf("Deleting MOD #%d", id), "MOD delete completed", func(taskID int64) error {
		mod, err := a.getMod(id)
		if err != nil {
			return err
		}
		if err := a.deleteMod(id); err != nil {
			return err
		}
		a.logTaskf(taskID, "Deleted MOD: %s", mod.PackageName)
		return nil
	})
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleModInfo(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDPathValue(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	mod, err := a.getMod(id)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"info_json": mod.InfoJSON})
}

func (a *App) installUploadedMod(file multipart.File, header *multipart.FileHeader) (modRecord, error) {
	return a.installUploadedModForPackage(file, header, "")
}

func (a *App) installUploadedModForPackage(file multipart.File, header *multipart.FileHeader, expectedPackageName string) (modRecord, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return modRecord{}, err
	}
	if err := a.ensureServerStoppedForModMutation(settings); err != nil {
		return modRecord{}, err
	}
	base, err := configuredServerBase(settings)
	if err != nil {
		return modRecord{}, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return modRecord{}, err
	}

	uploadDir := filepath.Join(a.dataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return modRecord{}, err
	}
	workspaceDir, err := os.MkdirTemp(uploadDir, "mod-work-*")
	if err != nil {
		return modRecord{}, err
	}
	defer os.RemoveAll(workspaceDir)

	tempArchive, err := os.CreateTemp(workspaceDir, "archive-*"+filepath.Ext(header.Filename))
	if err != nil {
		return modRecord{}, err
	}
	archivePath := tempArchive.Name()
	if _, err := io.Copy(tempArchive, file); err != nil {
		tempArchive.Close()
		return modRecord{}, err
	}
	if err := tempArchive.Close(); err != nil {
		return modRecord{}, err
	}

	extractDir := filepath.Join(workspaceDir, "content")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return modRecord{}, err
	}

	if err := extractModArchive(archivePath, header.Filename, extractDir); err != nil {
		return modRecord{}, err
	}
	if err := validateExtractionWorkspace(workspaceDir, extractDir, archivePath); err != nil {
		return modRecord{}, err
	}
	if err := validateExtractedModTree(extractDir); err != nil {
		return modRecord{}, err
	}
	info, err := inspectExtractedMod(extractDir)
	if err != nil {
		return modRecord{}, err
	}
	if expectedPackageName != "" && info.PackageName != expectedPackageName {
		return modRecord{}, fmt.Errorf("uploaded mod PackageName %q does not match selected mod %q", info.PackageName, expectedPackageName)
	}
	folderName := sanitizeFolderName(strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename)))
	if folderName == "" {
		folderName = sanitizeFolderName(info.PackageName)
	}
	if folderName == "" {
		return modRecord{}, errors.New("could not derive mod folder name")
	}

	return a.installExtractedModSource(base, info, folderName)
}

func (a *App) installWorkshopMod(workshopID string, taskID int64) (modRecord, error) {
	normalizedID, err := normalizeSteamWorkshopID(workshopID)
	if err != nil {
		return modRecord{}, err
	}
	workshopID = normalizedID
	settings, err := a.loadSettings()
	if err != nil {
		return modRecord{}, err
	}
	if err := a.ensureServerStoppedForModMutation(settings); err != nil {
		return modRecord{}, err
	}
	base, err := configuredServerBase(settings)
	if err != nil {
		return modRecord{}, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return modRecord{}, err
	}
	steamPath := resolveSteamCMD(settings.SteamCMDPath)
	if steamPath == "" {
		return modRecord{}, errors.New("steamcmd was not found; set steamcmd_path or add it to PATH")
	}
	if err := a.downloadSteamWorkshopItem(taskID, steamPath, workshopID); err != nil {
		return modRecord{}, err
	}
	contentDir, err := findSteamWorkshopContentDir(steamPath, palworldWorkshopAppID, workshopID)
	if err != nil {
		return modRecord{}, err
	}
	info, cleanup, err := a.inspectSteamWorkshopModContent(contentDir)
	if err != nil {
		return modRecord{}, err
	}
	defer cleanup()
	folderName := sanitizeFolderName(workshopID)
	if folderName == "" {
		return modRecord{}, errors.New("could not derive Steam Workshop mod folder name")
	}
	return a.installExtractedModSource(base, info, folderName)
}

func (a *App) downloadSteamWorkshopItem(taskID int64, steamPath, workshopID string) error {
	args := []string{
		"+login", "anonymous",
		"+workshop_download_item", palworldWorkshopAppID, workshopID, "validate",
		"+quit",
	}
	a.logTaskf(taskID, "Running %s %s", steamPath, strings.Join(args, " "))
	cmd := exec.Command(steamPath, args...)
	cmd.Dir = filepath.Dir(steamPath)
	writer := &taskLogWriter{app: a, taskID: taskID}
	cmd.Stdout = writer
	cmd.Stderr = writer
	err := a.runExternalCommand(cmd)
	writer.Flush()
	if err != nil {
		return fmt.Errorf("download Steam Workshop item %s: %w", workshopID, err)
	}
	return nil
}

func (a *App) inspectSteamWorkshopModContent(contentDir string) (modInfo, func(), error) {
	if err := validateExtractedModTree(contentDir); err != nil {
		return modInfo{}, func() {}, err
	}
	info, err := inspectExtractedMod(contentDir)
	if err == nil {
		return info, func() {}, nil
	}
	if !isMissingInfoJSONError(err) {
		return modInfo{}, func() {}, err
	}
	archivePath, archiveErr := findSingleModArchive(contentDir)
	if archiveErr != nil {
		return modInfo{}, func() {}, fmt.Errorf("%w; no supported archive was found in downloaded Workshop content: %v", err, archiveErr)
	}
	uploadDir := filepath.Join(a.dataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return modInfo{}, func() {}, err
	}
	workspaceDir, err := os.MkdirTemp(uploadDir, "workshop-mod-*")
	if err != nil {
		return modInfo{}, func() {}, err
	}
	keepWorkspace := false
	defer func() {
		if !keepWorkspace {
			_ = os.RemoveAll(workspaceDir)
		}
	}()

	tempArchive := filepath.Join(workspaceDir, filepath.Base(archivePath))
	if err := copyFile(archivePath, tempArchive); err != nil {
		return modInfo{}, func() {}, err
	}
	extractDir := filepath.Join(workspaceDir, "content")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return modInfo{}, func() {}, err
	}
	if err := extractModArchive(tempArchive, filepath.Base(tempArchive), extractDir); err != nil {
		return modInfo{}, func() {}, err
	}
	if err := validateExtractionWorkspace(workspaceDir, extractDir, tempArchive); err != nil {
		return modInfo{}, func() {}, err
	}
	if err := validateExtractedModTree(extractDir); err != nil {
		return modInfo{}, func() {}, err
	}
	info, err = inspectExtractedMod(extractDir)
	if err != nil {
		return modInfo{}, func() {}, err
	}
	keepWorkspace = true
	return info, func() { _ = os.RemoveAll(workspaceDir) }, nil
}

func (a *App) installExtractedModSource(base string, info modInfo, folderName string) (modRecord, error) {
	if strings.TrimSpace(info.SourceRoot) == "" {
		return modRecord{}, errors.New("mod source root is required")
	}
	previous, err := a.getModByPackage(info.PackageName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modRecord{}, err
	}
	existing := err == nil
	enabled := false
	if existing {
		enabled = previous.Enabled
		if previous.FolderName != "" {
			folderName = previous.FolderName
		}
	}
	if err := validateModFolderName(folderName); err != nil {
		return modRecord{}, err
	}
	if enabled {
		if _, err := readPalModSettings(base); err != nil {
			return modRecord{}, err
		}
	}
	if _, err := a.createBackupIfPossible("pre_mod", fmt.Sprintf("Before installing mod %s", info.PackageName)); err != nil {
		return modRecord{}, err
	}

	targetDir, err := workshopModDirectory(base, folderName)
	if err != nil {
		return modRecord{}, err
	}
	workshopDir := filepath.Join(base, "Mods", "Workshop")
	if err := os.MkdirAll(workshopDir, 0o755); err != nil {
		return modRecord{}, err
	}
	stagingDir, err := os.MkdirTemp(workshopDir, ".palpanel-install-*")
	if err != nil {
		return modRecord{}, err
	}
	defer os.RemoveAll(stagingDir)
	if err := copyDir(info.SourceRoot, stagingDir); err != nil {
		return modRecord{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	id, err := a.commitModInstallRecord(existing, previous, info, folderName, enabled, now)
	if err != nil {
		return modRecord{}, err
	}
	if enabled {
		if err := updatePalModSettings(base, info.PackageName, true); err != nil {
			if restoreErr := a.restoreModInstallRecord(existing, previous, id); restoreErr != nil {
				return modRecord{}, fmt.Errorf("%w; failed to restore mod database state: %v", err, restoreErr)
			}
			return modRecord{}, err
		}
	}
	if err := replaceInstalledModDir(stagingDir, targetDir); err != nil {
		if restoreErr := a.restoreModInstallRecord(existing, previous, id); restoreErr != nil {
			return modRecord{}, fmt.Errorf("%w; failed to restore mod database state: %v", err, restoreErr)
		}
		return modRecord{}, err
	}
	return a.getMod(id)
}

func (a *App) commitModInstallRecord(existing bool, previous modRecord, info modInfo, folderName string, enabled bool, now string) (int64, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	if existing {
		id = previous.ID
		_, err = tx.Exec(
			`UPDATE mods
			 SET name = ?, version = ?, author = ?, folder_name = ?, enabled = ?, server_supported = ?, info_json = ?, updated_at = ?
			 WHERE id = ?`,
			info.Name,
			info.Version,
			info.Author,
			folderName,
			boolInt(enabled),
			boolInt(info.ServerSupported),
			info.InfoJSON,
			now,
			id,
		)
		if err != nil {
			return 0, err
		}
	} else {
		result, err := tx.Exec(
			`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			info.Name,
			info.PackageName,
			info.Version,
			info.Author,
			folderName,
			boolInt(enabled),
			boolInt(info.ServerSupported),
			info.InfoJSON,
			now,
			now,
		)
		if err != nil {
			return 0, err
		}
		id, _ = result.LastInsertId()
	}
	if err := commitModInstallTx(tx); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	return id, nil
}

func (a *App) restoreModInstallRecord(existing bool, previous modRecord, id int64) error {
	if existing {
		_, err := a.db.Exec(
			`UPDATE mods
			 SET name = ?, package_name = ?, version = ?, author = ?, folder_name = ?, enabled = ?, server_supported = ?, info_json = ?, installed_at = ?, updated_at = ?
			 WHERE id = ?`,
			previous.Name,
			previous.PackageName,
			previous.Version,
			previous.Author,
			previous.FolderName,
			boolInt(previous.Enabled),
			boolInt(previous.ServerSupported),
			previous.InfoJSON,
			previous.InstalledAt,
			previous.UpdatedAt,
			previous.ID,
		)
		return err
	}
	_, err := a.db.Exec(`DELETE FROM mods WHERE id = ?`, id)
	return err
}

func (a *App) setModEnabled(id int64, enabled bool) (modRecord, error) {
	mod, err := a.getMod(id)
	if err != nil {
		return modRecord{}, err
	}
	if err := validateModPackageName(mod.PackageName); err != nil {
		return modRecord{}, err
	}
	settings, err := a.loadSettings()
	if err != nil {
		return modRecord{}, err
	}
	if err := a.ensureServerStoppedForModMutation(settings); err != nil {
		return modRecord{}, err
	}
	base, err := configuredServerBase(settings)
	if err != nil {
		return modRecord{}, err
	}
	targetDir, err := workshopModDirectory(base, mod.FolderName)
	if err != nil {
		return modRecord{}, err
	}
	if enabled && !fileExists(filepath.Join(targetDir, "Info.json")) {
		return modRecord{}, fmt.Errorf("installed Info.json is missing for mod %s", mod.PackageName)
	}
	if _, err := readPalModSettings(base); err != nil {
		return modRecord{}, err
	}
	if _, err := a.createBackupIfPossible("pre_mod", fmt.Sprintf("Before changing mod %s", mod.PackageName)); err != nil {
		return modRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := a.db.Begin()
	if err != nil {
		return modRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec(`UPDATE mods SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), now, id); err != nil {
		return modRecord{}, err
	}
	if err := commitModEnabledTx(tx); err != nil {
		return modRecord{}, err
	}
	committed = true
	if err := updatePalModSettings(base, mod.PackageName, enabled); err != nil {
		if restoreErr := a.restoreModEnabledState(mod); restoreErr != nil {
			return modRecord{}, fmt.Errorf("%w; failed to restore mod database state: %v", err, restoreErr)
		}
		return modRecord{}, err
	}
	return a.getMod(id)
}

func (a *App) restoreModEnabledState(mod modRecord) error {
	_, err := a.db.Exec(
		`UPDATE mods SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(mod.Enabled),
		mod.UpdatedAt,
		mod.ID,
	)
	return err
}

func (a *App) deleteMod(id int64) error {
	mod, err := a.getMod(id)
	if err != nil {
		return err
	}
	settings, err := a.loadSettings()
	if err != nil {
		return err
	}
	if err := a.ensureServerStoppedForModMutation(settings); err != nil {
		return err
	}
	base, err := configuredServerBase(settings)
	if err != nil {
		return err
	}
	managedDir, err := managedModDirectory(base, mod.PackageName)
	if err != nil {
		return err
	}
	targetDir, err := workshopModDirectory(base, mod.FolderName)
	if err != nil {
		return err
	}
	if _, err := readPalModSettings(base); err != nil {
		return err
	}
	if _, err := a.createBackupIfPossible("pre_mod", fmt.Sprintf("Before deleting mod %s", mod.PackageName)); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM mods WHERE id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := updatePalModSettings(base, mod.PackageName, false); err != nil {
		if restoreErr := a.restoreDeletedModRecord(mod); restoreErr != nil {
			return fmt.Errorf("%w; failed to restore mod database state: %v", err, restoreErr)
		}
		return err
	}
	if err := removeModDirectoriesWithStaging(base, managedDir, targetDir); err != nil {
		return a.compensateFailedModDelete(mod, base, err)
	}
	return nil
}

type stagedModDeleteDir struct {
	original string
	staged   string
}

func removeModDirectoriesWithStaging(base, managedDir, workshopDir string) error {
	modsDir := filepath.Join(base, "Mods")
	if err := ensureWithin(base, modsDir); err != nil {
		return err
	}
	candidates := []struct {
		path  string
		label string
	}{
		{path: managedDir, label: filepath.Join("ManagedMods", filepath.Base(managedDir))},
		{path: workshopDir, label: filepath.Join("Workshop", filepath.Base(workshopDir))},
	}
	var trashRoot string
	var staged []stagedModDeleteDir
	restoreAndReturn := func(cause error) error {
		if restoreErr := restoreStagedModDeleteDirs(staged); restoreErr != nil {
			return fmt.Errorf("%w; failed to restore staged mod directories: %v", cause, restoreErr)
		}
		if trashRoot != "" {
			_ = os.RemoveAll(trashRoot)
		}
		return cause
	}
	for _, candidate := range candidates {
		if _, err := os.Lstat(candidate.path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return restoreAndReturn(err)
		}
		if trashRoot == "" {
			var err error
			trashRoot, err = os.MkdirTemp(modsDir, ".palpanel-delete-*")
			if err != nil {
				return restoreAndReturn(err)
			}
		}
		stagedPath := filepath.Join(trashRoot, candidate.label)
		if err := ensureWithin(trashRoot, stagedPath); err != nil {
			return restoreAndReturn(err)
		}
		if err := os.MkdirAll(filepath.Dir(stagedPath), 0o755); err != nil {
			return restoreAndReturn(err)
		}
		if err := os.Rename(candidate.path, stagedPath); err != nil {
			return restoreAndReturn(err)
		}
		staged = append(staged, stagedModDeleteDir{original: candidate.path, staged: stagedPath})
	}
	if trashRoot == "" {
		return nil
	}
	if err := removeModPath(trashRoot); err != nil {
		return restoreAndReturn(err)
	}
	return nil
}

func restoreStagedModDeleteDirs(staged []stagedModDeleteDir) error {
	var failures []error
	for i := len(staged) - 1; i >= 0; i-- {
		item := staged[i]
		if err := os.MkdirAll(filepath.Dir(item.original), 0o755); err != nil {
			failures = append(failures, fmt.Errorf("prepare restore %s: %w", item.original, err))
			continue
		}
		if err := os.Rename(item.staged, item.original); err != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", item.original, err))
		}
	}
	return errors.Join(failures...)
}

func (a *App) compensateFailedModDelete(mod modRecord, base string, cause error) error {
	var failures []error
	if mod.Enabled {
		if err := updatePalModSettings(base, mod.PackageName, true); err != nil {
			failures = append(failures, fmt.Errorf("restore MOD settings: %w", err))
		}
	}
	if err := a.restoreDeletedModRecord(mod); err != nil {
		failures = append(failures, fmt.Errorf("restore MOD row: %w", err))
	}
	if len(failures) > 0 {
		return fmt.Errorf("%w; compensation failed: %v", cause, errors.Join(failures...))
	}
	return cause
}

func (a *App) restoreDeletedModRecord(mod modRecord) error {
	_, err := a.db.Exec(
		`INSERT INTO mods(id, name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			package_name = excluded.package_name,
			version = excluded.version,
			author = excluded.author,
			folder_name = excluded.folder_name,
			enabled = excluded.enabled,
			server_supported = excluded.server_supported,
			info_json = excluded.info_json,
			installed_at = excluded.installed_at,
			updated_at = excluded.updated_at`,
		mod.ID,
		mod.Name,
		mod.PackageName,
		mod.Version,
		mod.Author,
		mod.FolderName,
		boolInt(mod.Enabled),
		boolInt(mod.ServerSupported),
		mod.InfoJSON,
		mod.InstalledAt,
		mod.UpdatedAt,
	)
	return err
}

func managedModDirectory(base, packageName string) (string, error) {
	if err := validateModPackageName(packageName); err != nil {
		return "", fmt.Errorf("invalid mod PackageName for ManagedMods cleanup: %w", err)
	}
	name := strings.TrimSpace(packageName)
	managedRoot := filepath.Join(base, "Mods", "ManagedMods")
	targetDir := filepath.Join(managedRoot, name)
	if err := ensureWithin(managedRoot, targetDir); err != nil {
		return "", err
	}
	return targetDir, nil
}

func workshopModDirectory(base, folderName string) (string, error) {
	if err := validateModFolderName(folderName); err != nil {
		return "", err
	}
	name := strings.TrimSpace(folderName)
	workshopDir := filepath.Join(base, "Mods", "Workshop")
	targetDir := filepath.Join(workshopDir, name)
	if err := ensureWithin(workshopDir, targetDir); err != nil {
		return "", err
	}
	return targetDir, nil
}

func normalizeSteamWorkshopID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("workshop_id is required")
	}
	if isDecimalWorkshopID(input) {
		return input, nil
	}
	if parsed, err := url.Parse(input); err == nil {
		if id := strings.TrimSpace(parsed.Query().Get("id")); id != "" {
			if isDecimalWorkshopID(id) {
				return id, nil
			}
			return "", fmt.Errorf("Steam Workshop item id must be numeric: %q", id)
		}
	}
	query := input
	if index := strings.IndexByte(query, '?'); index >= 0 {
		query = query[index+1:]
	}
	if values, err := url.ParseQuery(query); err == nil {
		if id := strings.TrimSpace(values.Get("id")); id != "" {
			if isDecimalWorkshopID(id) {
				return id, nil
			}
			return "", fmt.Errorf("Steam Workshop item id must be numeric: %q", id)
		}
	}
	return "", errors.New("workshop_id must be a numeric Steam Workshop item ID or a URL containing id=<number>")
}

func isDecimalWorkshopID(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func findSteamWorkshopContentDir(steamPath, appID, workshopID string) (string, error) {
	rel := filepath.Join("steamapps", "workshop", "content", appID, workshopID)
	candidates := make([]string, 0, 8)
	addWorkshopContentCandidate := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		candidate, err := filepath.Abs(filepath.Join(root, rel))
		if err != nil {
			return
		}
		for _, existing := range candidates {
			if sameFilesystemPath(existing, candidate) {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	addWorkshopContentCandidate(os.Getenv("PALPANEL_STEAMCMD_DIR"))
	if strings.TrimSpace(steamPath) != "" {
		addWorkshopContentCandidate(filepath.Dir(steamPath))
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		addWorkshopContentCandidate(filepath.Join(home, "Steam"))
		addWorkshopContentCandidate(filepath.Join(home, ".steam", "steam"))
		addWorkshopContentCandidate(filepath.Join(home, ".local", "share", "Steam"))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		if err == nil && !info.IsDir() {
			return "", fmt.Errorf("downloaded Steam Workshop item path is not a directory: %s", candidate)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("downloaded Steam Workshop item %s was not found; checked %s", workshopID, strings.Join(candidates, ", "))
}

func isMissingInfoJSONError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Info.json was not found")
}

func findSingleModArchive(root string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
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
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(d.Name())) {
		case ".zip", ".7z", ".rar":
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", errors.New("no .zip, .7z, or .rar file found")
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", fmt.Errorf("multiple supported archives found: %s", strings.Join(matches, ", "))
	}
	return matches[0], nil
}

func (a *App) openModDirectoryByID(id int64) (string, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return "", err
	}
	base, err := configuredServerBase(settings)
	if err != nil {
		return "", err
	}
	mod, err := a.getMod(id)
	if err != nil {
		return "", err
	}
	targetDir, err := workshopModDirectory(base, mod.FolderName)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(targetDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("mod install directory not found: %s", targetDir)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("mod install path is not a directory: %s", targetDir)
	}
	if a.openDirectory == nil {
		return "", errors.New("directory opener is not configured")
	}
	if err := a.openDirectory(targetDir); err != nil {
		return "", fmt.Errorf("open mod directory: %w", err)
	}
	return targetDir, nil
}

func defaultOpenDirectory(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		opener, err := exec.LookPath("xdg-open")
		if err != nil {
			return errors.New("no directory opener available on this platform; install xdg-open or open the path manually")
		}
		cmd = exec.Command(opener, path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (a *App) listMods() ([]modRecord, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return nil, err
	}
	base := strings.TrimSpace(settings.PalServerPath)
	active := map[string]bool{}
	if base != "" {
		if abs, err := filepath.Abs(base); err == nil {
			active, err = readActiveModSet(abs)
			if err != nil {
				return nil, err
			}
		}
	}
	rows, err := a.db.Query(
		`SELECT id, name, package_name, version, author, folder_name, enabled, server_supported, info_json, CAST(installed_at AS TEXT), CAST(updated_at AS TEXT)
		 FROM mods
		 ORDER BY id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mods := []modRecord{}
	for rows.Next() {
		mod, err := scanMod(rows)
		if err != nil {
			return nil, err
		}
		if base != "" {
			mod.Enabled = active[mod.PackageName]
			if installPath, err := workshopModDirectory(base, mod.FolderName); err == nil {
				mod.InstallPath = installPath
			}
		}
		mods = append(mods, mod)
	}
	return mods, rows.Err()
}

func (a *App) getMod(id int64) (modRecord, error) {
	row := a.db.QueryRow(
		`SELECT id, name, package_name, version, author, folder_name, enabled, server_supported, info_json, CAST(installed_at AS TEXT), CAST(updated_at AS TEXT)
		 FROM mods
		 WHERE id = ?`,
		id,
	)
	mod, err := scanMod(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return modRecord{}, fmt.Errorf("mod not found: %d", id)
		}
		return modRecord{}, err
	}
	settings, err := a.loadSettings()
	if err == nil && strings.TrimSpace(settings.PalServerPath) != "" {
		base, baseErr := filepath.Abs(settings.PalServerPath)
		if baseErr == nil {
			active, activeErr := readActiveModSet(base)
			if activeErr != nil {
				return modRecord{}, activeErr
			}
			mod.Enabled = active[mod.PackageName]
			if installPath, err := workshopModDirectory(base, mod.FolderName); err == nil {
				mod.InstallPath = installPath
			}
		}
	}
	return mod, nil
}

func (a *App) getModByPackage(packageName string) (modRecord, error) {
	row := a.db.QueryRow(
		`SELECT id, name, package_name, version, author, folder_name, enabled, server_supported, info_json, CAST(installed_at AS TEXT), CAST(updated_at AS TEXT)
		 FROM mods
		 WHERE package_name = ?`,
		packageName,
	)
	return scanMod(row)
}

type modRow interface {
	Scan(dest ...any) error
}

func scanMod(row modRow) (modRecord, error) {
	var mod modRecord
	var enabled int
	var supported int
	err := row.Scan(
		&mod.ID,
		&mod.Name,
		&mod.PackageName,
		&mod.Version,
		&mod.Author,
		&mod.FolderName,
		&enabled,
		&supported,
		&mod.InfoJSON,
		&mod.InstalledAt,
		&mod.UpdatedAt,
	)
	if err != nil {
		return modRecord{}, err
	}
	mod.Enabled = enabled != 0
	mod.ServerSupported = supported != 0
	return mod, nil
}

func inspectExtractedMod(root string) (modInfo, error) {
	infoPath, err := findInfoJSON(root)
	if err != nil {
		return modInfo{}, err
	}
	data, err := readModInfoJSON(infoPath)
	if err != nil {
		return modInfo{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return modInfo{}, fmt.Errorf("parse Info.json: %w", err)
	}
	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return modInfo{}, err
	}
	packageName := readJSONText(raw, "PackageName", "packageName", "package_name")
	if packageName == "" {
		return modInfo{}, errors.New("Info.json PackageName is required")
	}
	if err := validateModPackageName(packageName); err != nil {
		return modInfo{}, err
	}
	return modInfo{
		Name:            firstNonEmpty(readJSONText(raw, "Name", "DisplayName", "ModName"), packageName),
		PackageName:     packageName,
		Version:         readJSONText(raw, "Version", "version"),
		Author:          readJSONText(raw, "Author", "AuthorName", "author"),
		ServerSupported: jsonContainsServerInstallRule(raw),
		InfoJSON:        string(pretty),
		SourceRoot:      filepath.Dir(infoPath),
	}, nil
}

func readModInfoJSON(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxModInfoJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxModInfoJSONBytes {
		return nil, fmt.Errorf("Info.json exceeds %d bytes", maxModInfoJSONBytes)
	}
	return data, nil
}

func findInfoJSON(root string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
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
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "Info.json") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", errors.New("Info.json was not found in uploaded mod archive")
	}
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i]) < len(matches[j])
	})
	return matches[0], nil
}

func extractModArchive(archivePath, originalName, dest string) error {
	ext := strings.ToLower(filepath.Ext(originalName))
	switch ext {
	case ".zip":
		return extractZipToDir(archivePath, dest)
	case ".7z", ".rar":
		return extractWithExternalTool(archivePath, dest)
	default:
		return fmt.Errorf("unsupported mod archive type: %s", ext)
	}
}

func extractZipToDir(zipPath, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var limits modExtractionScanState
	for _, file := range reader.File {
		name := strings.TrimPrefix(filepath.Clean(filepath.FromSlash(file.Name)), string(filepath.Separator))
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("mod archive contains symlink entry: %s", file.Name)
		}
		if name == "." {
			continue
		}
		target := filepath.Join(dest, name)
		if err := ensureWithin(dest, target); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := limits.checkZipFile(file, name); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
		if err != nil {
			src.Close()
			return err
		}
		limited := &modExtractionLimitReader{
			reader:     src,
			entryName:  name,
			totalBytes: &limits.readBytes,
		}
		_, copyErr := io.Copy(dst, limited)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type modExtractionScanState struct {
	fileCount     int
	declaredBytes int64
	readBytes     int64
}

func (s *modExtractionScanState) checkZipFile(file *zip.File, name string) error {
	s.fileCount++
	if s.fileCount > maxModExtractedFileCount {
		return fmt.Errorf("mod archive contains more than %d files", maxModExtractedFileCount)
	}
	declared := file.UncompressedSize64
	if declared > uint64(maxModExtractedEntryBytes) {
		return fmt.Errorf("mod archive entry %s exceeds %d bytes", file.Name, maxModExtractedEntryBytes)
	}
	remaining := maxModExtractedTotalBytes - s.declaredBytes
	if remaining < 0 || declared > uint64(remaining) {
		return fmt.Errorf("mod archive expands beyond %d bytes", maxModExtractedTotalBytes)
	}
	s.declaredBytes += int64(declared)
	return nil
}

func (s *modExtractionScanState) checkExtractedFile(info os.FileInfo, name string) error {
	if info.IsDir() {
		return nil
	}
	s.fileCount++
	if s.fileCount > maxModExtractedFileCount {
		return fmt.Errorf("extracted mod archive contains more than %d files", maxModExtractedFileCount)
	}
	size := info.Size()
	if size < 0 {
		return fmt.Errorf("extracted mod archive file %s has invalid size", name)
	}
	if size > maxModExtractedEntryBytes {
		return fmt.Errorf("extracted mod archive file %s exceeds %d bytes", name, maxModExtractedEntryBytes)
	}
	remaining := maxModExtractedTotalBytes - s.declaredBytes
	if remaining < 0 || size > remaining {
		return fmt.Errorf("extracted mod archive exceeds %d bytes", maxModExtractedTotalBytes)
	}
	s.declaredBytes += size
	return nil
}

type modExtractionLimitReader struct {
	reader     io.Reader
	entryName  string
	entryBytes int64
	totalBytes *int64
}

func (r *modExtractionLimitReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	entryRemaining := maxModExtractedEntryBytes - r.entryBytes
	if entryRemaining <= 0 {
		return r.readLimitProbe(fmt.Errorf("mod archive entry %s exceeds %d bytes", r.entryName, maxModExtractedEntryBytes))
	}
	totalRemaining := maxModExtractedTotalBytes - *r.totalBytes
	if totalRemaining <= 0 {
		return r.readLimitProbe(fmt.Errorf("mod archive expands beyond %d bytes", maxModExtractedTotalBytes))
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

func (r *modExtractionLimitReader) readLimitProbe(limitErr error) (int, error) {
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

func extractWithExternalTool(archivePath, dest string) error {
	if path, err := exec.LookPath("7z"); err == nil {
		return runExtractor(path, []string{"x", archivePath, "-o" + dest, "-y"})
	}
	if path, err := exec.LookPath("7zz"); err == nil {
		return runExtractor(path, []string{"x", archivePath, "-o" + dest, "-y"})
	}
	if path, err := exec.LookPath("7za"); err == nil {
		return runExtractor(path, []string{"x", archivePath, "-o" + dest, "-y"})
	}
	if path, err := exec.LookPath("unar"); err == nil {
		return runExtractor(path, []string{"-o", dest, archivePath})
	}
	if path, err := exec.LookPath("bsdtar"); err == nil {
		return runExtractor(path, []string{"-xf", archivePath, "-C", dest})
	}
	return errors.New("7z/rar upload requires 7z, 7zz, 7za, unar, or bsdtar in PATH")
}

func validateExtractedModTree(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	var limits modExtractionScanState
	return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ensureWithin(root, path); err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("extracted mod archive contains symlink: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("extracted mod archive contains symlink: %s", rel)
		}
		if err := limits.checkExtractedFile(info, rel); err != nil {
			return err
		}
		return nil
	})
}

func validateExtractionWorkspace(workspaceDir, extractDir, archivePath string) error {
	workspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return err
	}
	extractDir, err = filepath.Abs(extractDir)
	if err != nil {
		return err
	}
	archivePath, err = filepath.Abs(archivePath)
	if err != nil {
		return err
	}
	if err := ensureWithin(workspaceDir, extractDir); err != nil {
		return err
	}
	if err := ensureWithin(workspaceDir, archivePath); err != nil {
		return err
	}
	return filepath.WalkDir(workspaceDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if sameFilesystemPath(path, workspaceDir) || sameFilesystemPath(path, archivePath) || pathWithinOrEqual(extractDir, path) {
			return nil
		}
		rel, _ := filepath.Rel(workspaceDir, path)
		return fmt.Errorf("mod extraction wrote outside target directory: %s", rel)
	})
}

func pathWithinOrEqual(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func runExtractor(path string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), modExtractorTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	output := &limitedExtractorOutput{limit: maxModExtractorOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("extract archive timed out after %s", modExtractorTimeout)
	}
	if err != nil {
		return fmt.Errorf("extract archive: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

type limitedExtractorOutput struct {
	builder   strings.Builder
	limit     int64
	written   int64
	truncated bool
}

func (o *limitedExtractorOutput) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if o.limit <= 0 {
		o.truncated = true
		return len(p), nil
	}
	remaining := o.limit - o.written
	if remaining > 0 {
		writeLen := len(p)
		if int64(writeLen) > remaining {
			writeLen = int(remaining)
			o.truncated = true
		}
		o.builder.Write(p[:writeLen])
		o.written += int64(writeLen)
	} else {
		o.truncated = true
	}
	return len(p), nil
}

func (o *limitedExtractorOutput) String() string {
	text := o.builder.String()
	if o.truncated {
		text += modExtractorOutputTruncatedSuffix
	}
	return text
}

func copyDir(src, dst string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := ensureWithin(dst, target); err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func replaceInstalledModDir(stagingDir, targetDir string) error {
	parent := filepath.Dir(targetDir)
	var replacedDir string
	if _, err := os.Stat(targetDir); err == nil {
		tempDir, err := os.MkdirTemp(parent, ".palpanel-replaced-*")
		if err != nil {
			return err
		}
		if err := os.Remove(tempDir); err != nil {
			return err
		}
		if err := renameModPath(targetDir, tempDir); err != nil {
			return err
		}
		replacedDir = tempDir
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := renameModPath(stagingDir, targetDir); err != nil {
		if replacedDir != "" {
			if restoreErr := renameModPath(replacedDir, targetDir); restoreErr != nil {
				return fmt.Errorf("%w; failed to restore previous mod directory: %v", err, restoreErr)
			}
		}
		return err
	}
	if replacedDir != "" {
		_ = os.RemoveAll(replacedDir)
	}
	return nil
}

func updatePalModSettings(base, packageName string, enabled bool) error {
	if err := validateModPackageName(packageName); err != nil {
		return err
	}
	settingsPath, err := palModSettingsPath(base)
	if err != nil {
		return err
	}
	content, err := readPalModSettings(base)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	next := renderPalModSettings(content, packageName, enabled)
	return atomicWriteFile(settingsPath, []byte(next), 0o644)
}

func readActiveModSet(base string) (map[string]bool, error) {
	content, err := readPalModSettings(base)
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(trimmed, "ActiveModList="); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				active[value] = true
			}
		}
	}
	return active, nil
}

func palModSettingsPath(base string) (string, error) {
	settingsPath := filepath.Join(base, "Mods", "PalModSettings.ini")
	if err := ensureWithin(base, settingsPath); err != nil {
		return "", err
	}
	return settingsPath, nil
}

func readPalModSettings(base string) (string, error) {
	settingsPath, err := palModSettingsPath(base)
	if err != nil {
		return "", err
	}
	file, err := os.Open(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil {
		if err := checkPalModSettingsSize(settingsPath, info.Size()); err != nil {
			return "", err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPalModSettingsBytes+1))
	if err != nil {
		return "", err
	}
	if err := checkPalModSettingsSize(settingsPath, int64(len(data))); err != nil {
		return "", err
	}
	return string(data), nil
}

func checkPalModSettingsSize(path string, size int64) error {
	if size > maxPalModSettingsBytes {
		return fmt.Errorf("%w: %s exceeds %d bytes", errPalModSettingsTooLarge, path, maxPalModSettingsBytes)
	}
	return nil
}

func renderPalModSettings(content, packageName string, enabled bool) string {
	lines := splitTextLines(content)
	start, end := findINISection(lines, "PalModSettings")
	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[PalModSettings]")
		start = len(lines) - 1
		end = len(lines)
	}
	before := append([]string(nil), lines[:start+1]...)
	section := append([]string(nil), lines[start+1:end]...)
	after := append([]string(nil), lines[end:]...)

	active := make([]string, 0)
	seenActive := map[string]bool{}
	preserved := make([]string, 0)
	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ActiveModList=") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "ActiveModList="))
			if value != "" && value != packageName && !seenActive[value] {
				active = append(active, value)
				seenActive[value] = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "bGlobalEnableMod=") {
			continue
		}
		preserved = append(preserved, line)
	}
	if enabled && !seenActive[packageName] {
		active = append(active, packageName)
	}

	next := append([]string(nil), before...)
	next = append(next, "bGlobalEnableMod=true")
	next = append(next, preserved...)
	for _, value := range active {
		next = append(next, "ActiveModList="+value)
	}
	next = append(next, after...)
	return strings.TrimRight(strings.Join(next, "\n"), "\n") + "\n"
}

func splitTextLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func findINISection(lines []string, section string) (int, int) {
	needle := "[" + section + "]"
	start := -1
	for i, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), needle) {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}
	return start, end
}

func configuredServerBase(settings settingsPayload) (string, error) {
	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return "", errors.New("pal_server_path is required before managing mods")
	}
	base, err := filepath.Abs(serverPath)
	if err != nil {
		return "", err
	}
	return base, nil
}

func readJSONText(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		}
	}
	return ""
}

func jsonContainsServerInstallRule(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, "IsServer") {
				if b, ok := child.(bool); ok && b {
					return true
				}
			}
			if jsonContainsServerInstallRule(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonContainsServerInstallRule(child) {
				return true
			}
		}
	}
	return false
}

func sanitizeFolderName(input string) string {
	input = strings.TrimSpace(input)
	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		case r == ' ':
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), ". ")
}

func validateModPackageName(packageName string) error {
	name := strings.TrimSpace(packageName)
	if name == "" {
		return errors.New("Info.json PackageName is required")
	}
	if name != packageName || strings.HasSuffix(name, ".") || name == "." || name == ".." || filepath.IsAbs(name) || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid mod PackageName: %q", packageName)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"|?*`, r) {
			return fmt.Errorf("invalid mod PackageName: %q", packageName)
		}
	}
	if isWindowsReservedFileName(name) {
		return fmt.Errorf("invalid mod PackageName: %q", packageName)
	}
	return nil
}

func validateModFolderName(folderName string) error {
	name := strings.TrimSpace(folderName)
	if name == "" {
		return errors.New("mod folder_name is required")
	}
	if name != folderName || filepath.IsAbs(name) || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || sanitizeFolderName(name) != name {
		return fmt.Errorf("invalid mod folder name: %q", folderName)
	}
	if isWindowsReservedFileName(name) {
		return fmt.Errorf("invalid mod folder name: %q", folderName)
	}
	return nil
}

func isWindowsReservedFileName(name string) bool {
	base := strings.ToUpper(strings.TrimSpace(name))
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		prefix := base[:3]
		suffix := base[3]
		if (prefix == "COM" || prefix == "LPT") && suffix >= '1' && suffix <= '9' {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
