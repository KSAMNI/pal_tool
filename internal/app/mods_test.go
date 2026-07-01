package app

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestModUploadEnableDisableDeleteRoutes(t *testing.T) {
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

	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()
	if mod.PackageName != "SampleServerMod" || !mod.ServerSupported {
		t.Fatalf("unexpected mod metadata: %#v", mod)
	}
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", mod.FolderName, "Info.json")
	if !fileExists(infoPath) {
		t.Fatalf("installed Info.json missing: %s", infoPath)
	}
	managedManifestPath := filepath.Join(serverPath, "Mods", "ManagedMods", mod.PackageName, "InstallManifest.json")
	if err := os.MkdirAll(filepath.Dir(managedManifestPath), 0o755); err != nil {
		t.Fatalf("mkdir ManagedMods dir: %v", err)
	}
	if err := os.WriteFile(managedManifestPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write ManagedMods manifest: %v", err)
	}

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/enable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	settings, err := os.ReadFile(filepath.Join(serverPath, "Mods", "PalModSettings.ini"))
	if err != nil {
		t.Fatalf("read PalModSettings.ini: %v", err)
	}
	if !strings.Contains(string(settings), "ActiveModList=SampleServerMod") {
		t.Fatalf("active mod was not written: %s", settings)
	}

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/disable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	settings, err = os.ReadFile(filepath.Join(serverPath, "Mods", "PalModSettings.ini"))
	if err != nil {
		t.Fatalf("read PalModSettings.ini: %v", err)
	}
	if strings.Contains(string(settings), "ActiveModList=SampleServerMod") {
		t.Fatalf("active mod was not removed: %s", settings)
	}

	resp = doJSON(t, client, http.MethodDelete, server.URL+"/api/mods/"+strconvID(mod.ID), nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodDelete, server.URL+"/api/mods/"+strconvID(mod.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if fileExists(infoPath) {
		t.Fatalf("mod files still exist after delete: %s", infoPath)
	}
	if fileExists(managedManifestPath) {
		t.Fatalf("managed mod files still exist after delete: %s", managedManifestPath)
	}
	for _, taskType := range []string{"mod_upload", "mod_enable", "mod_disable", "mod_delete"} {
		task := requireTaskWithStatus(t, panel, taskType, "success")
		if !strings.Contains(task.Log, "MOD") {
			t.Fatalf("%s task log did not mention MOD operation: %q", taskType, task.Log)
		}
	}
}

func TestListModsReturnsEmptyArrayWhenNoModsInstalled(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	setTestAppSetting(t, panel, "pal_server_path", t.TempDir())
	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/mods", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list mods status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read list mods body: %v", err)
	}
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("list mods body = %s, want []", body)
	}
}

func TestWorkshopModDownloadRouteInstallsDownloadedContent(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	steamRoot := t.TempDir()
	steamPath := filepath.Join(steamRoot, steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)

	const workshopID = "123456789"
	var capturedArgs []string
	var capturedDir string
	panel.commandRunner = func(cmd *exec.Cmd) error {
		capturedArgs = append([]string(nil), cmd.Args[1:]...)
		capturedDir = cmd.Dir
		contentRoot := filepath.Join(cmd.Dir, "steamapps", "workshop", "content", palworldWorkshopAppID, workshopID, "SampleWorkshopMod")
		writeSampleModContent(t, contentRoot, "SampleWorkshopPackage", "4.5.6")
		if cmd.Stdout != nil {
			_, _ = cmd.Stdout.Write([]byte("download ok\n"))
		}
		return nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/mods/workshop/download", map[string]string{
		"workshop_id": "https://steamcommunity.com/sharedfiles/filedetails/?id=" + workshopID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("workshop download status = %d, want %d; body=%s", resp.StatusCode, http.StatusCreated, body)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()

	if mod.PackageName != "SampleWorkshopPackage" || mod.Version != "4.5.6" || mod.FolderName != workshopID {
		t.Fatalf("unexpected mod metadata: %#v", mod)
	}
	if capturedDir != steamRoot {
		t.Fatalf("steamcmd dir = %q, want %q", capturedDir, steamRoot)
	}
	argsText := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsText, "+workshop_download_item "+palworldWorkshopAppID+" "+workshopID) {
		t.Fatalf("steamcmd args missing workshop download item: %q", argsText)
	}
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", workshopID, "Info.json")
	if !fileExists(infoPath) {
		t.Fatalf("installed Workshop Info.json missing: %s", infoPath)
	}
	task := requireTaskWithStatus(t, panel, "mod_workshop_download", "success")
	if !strings.Contains(task.Log, "Steam Workshop MOD installed") || !strings.Contains(task.Log, "download ok") {
		t.Fatalf("workshop task log missing expected entries: %s", task.Log)
	}
}

func TestWorkshopModDownloadExtractsSingleArchiveContent(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	steamRoot := t.TempDir()
	steamPath := filepath.Join(steamRoot, steamCMDName())
	if err := os.WriteFile(steamPath, []byte("fake steamcmd"), 0o755); err != nil {
		t.Fatalf("write fake steamcmd: %v", err)
	}
	setTestAppSetting(t, panel, "steamcmd_path", steamPath)

	const workshopID = "222333444"
	panel.commandRunner = func(cmd *exec.Cmd) error {
		contentRoot := filepath.Join(cmd.Dir, "steamapps", "workshop", "content", palworldWorkshopAppID, workshopID)
		if err := os.MkdirAll(contentRoot, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(contentRoot, "DownloadedMod.zip"), sampleModZipWith(t, "ArchivedWorkshopPackage", "7.8.9"), 0o644)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/mods/workshop/download", map[string]string{
		"workshop_id": workshopID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("workshop download status = %d, want %d; body=%s", resp.StatusCode, http.StatusCreated, body)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()
	if mod.PackageName != "ArchivedWorkshopPackage" || mod.FolderName != workshopID {
		t.Fatalf("unexpected archived workshop mod metadata: %#v", mod)
	}
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", workshopID, "Info.json")
	if !fileExists(infoPath) {
		t.Fatalf("installed archived Workshop Info.json missing: %s", infoPath)
	}
}

func TestWorkshopModDownloadRejectsInvalidIDBeforeCommand(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	ranCommand := false
	panel.commandRunner = func(cmd *exec.Cmd) error {
		ranCommand = true
		return nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/mods/workshop/download", map[string]string{
		"workshop_id": "not-a-number",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid workshop id status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	resp.Body.Close()
	if ranCommand {
		t.Fatalf("steamcmd command ran for invalid Workshop ID")
	}
	assertNoTasks(t, panel)
}

func TestWorkshopModDownloadRejectsActiveOperationBeforeReadingBody(t *testing.T) {
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

	body := &countingReadCloser{}
	req := httptest.NewRequest(http.MethodPost, "/api/mods/workshop/download", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	panel.handleDownloadWorkshopMod(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("workshop download status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if body.reads != 0 {
		t.Fatalf("workshop download request body was read %d times despite active operation", body.reads)
	}
	assertNoTasks(t, panel)
}

func TestModUploadKeepsWorkshopCleanWhenDatabaseInsertFails(t *testing.T) {
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
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	if _, err := panel.db.Exec(`CREATE TRIGGER block_mod_insert BEFORE INSERT ON mods BEGIN SELECT RAISE(FAIL, 'insert blocked'); END`); err != nil {
		t.Fatalf("create insert trigger: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	resp.Body.Close()

	finalInfoPath := filepath.Join(serverPath, "Mods", "Workshop", "SampleMod", "Info.json")
	if fileExists(finalInfoPath) {
		t.Fatalf("final workshop files were created after failed database insert: %s", finalInfoPath)
	}
	var count int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM mods`).Scan(&count); err != nil {
		t.Fatalf("count mods: %v", err)
	}
	if count != 0 {
		t.Fatalf("mods row count = %d, want 0", count)
	}
}

func TestModUpdateKeepsInstalledFilesAndRowWhenDatabaseUpdateFails(t *testing.T) {
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
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", mod.FolderName, "Info.json")
	infoBefore, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("read installed Info.json: %v", err)
	}
	if _, err := panel.db.Exec(`CREATE TRIGGER block_mod_update BEFORE UPDATE ON mods BEGIN SELECT RAISE(FAIL, 'update blocked'); END`); err != nil {
		t.Fatalf("create update trigger: %v", err)
	}

	resp = doMultipartConfirmed(t, client, server.URL+"/api/mods/"+strconvID(mod.ID)+"/update", "SampleMod-v2.zip", sampleModZipWith(t, "SampleServerMod", "2.0.0"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("update status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	resp.Body.Close()
	assertFileEqual(t, infoPath, infoBefore)
	after, err := panel.getMod(mod.ID)
	if err != nil {
		t.Fatalf("getMod() after failed update error = %v", err)
	}
	if after.Version != "1.2.3" {
		t.Fatalf("mod version = %q, want original 1.2.3", after.Version)
	}
}

func TestModUpdateKeepsInstalledFilesWhenDatabaseCommitFails(t *testing.T) {
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
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", mod.FolderName, "Info.json")
	infoBefore, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("read installed Info.json: %v", err)
	}

	previousCommit := commitModInstallTx
	commitModInstallTx = func(tx *sql.Tx) error {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return errors.New("commit blocked")
	}
	defer func() {
		commitModInstallTx = previousCommit
	}()

	resp = doMultipartConfirmed(t, client, server.URL+"/api/mods/"+strconvID(mod.ID)+"/update", "SampleMod-v2.zip", sampleModZipWith(t, "SampleServerMod", "2.0.0"))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("update status = %d, want %d, body=%s", resp.StatusCode, http.StatusBadRequest, body)
	}
	if !strings.Contains(string(body), "commit blocked") {
		t.Fatalf("update body = %s, want commit blocked", body)
	}
	assertFileEqual(t, infoPath, infoBefore)
	after, err := panel.getMod(mod.ID)
	if err != nil {
		t.Fatalf("getMod() after failed commit error = %v", err)
	}
	if after.Version != "1.2.3" {
		t.Fatalf("mod version = %q, want original 1.2.3", after.Version)
	}
}

func TestSetModEnabledKeepsSettingsAndRowWhenDatabaseUpdateFails(t *testing.T) {
	for _, tc := range []struct {
		name            string
		initialEnabled  int
		initialSettings string
		targetEnabled   bool
	}{
		{
			name:           "enable",
			initialEnabled: 0,
			targetEnabled:  true,
		},
		{
			name:            "disable",
			initialEnabled:  1,
			initialSettings: "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=SampleServerMod\n",
			targetEnabled:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			setTestAppSetting(t, panel, "pal_server_path", serverPath)

			infoPath := filepath.Join(serverPath, "Mods", "Workshop", "SampleFolder", "Info.json")
			if err := os.MkdirAll(filepath.Dir(infoPath), 0o755); err != nil {
				t.Fatalf("mkdir workshop dir: %v", err)
			}
			if err := os.WriteFile(infoPath, []byte(`{"PackageName":"SampleServerMod"}`), 0o644); err != nil {
				t.Fatalf("write Info.json: %v", err)
			}
			settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
			if tc.initialSettings != "" {
				if err := os.WriteFile(settingsPath, []byte(tc.initialSettings), 0o644); err != nil {
					t.Fatalf("write PalModSettings.ini: %v", err)
				}
			}
			result, err := panel.db.Exec(
				`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
				 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				"Sample Mod",
				"SampleServerMod",
				"1.0.0",
				"Tester",
				"SampleFolder",
				tc.initialEnabled,
				1,
				`{"PackageName":"SampleServerMod"}`,
				time.Now().UTC().Format(time.RFC3339),
				time.Now().UTC().Format(time.RFC3339),
			)
			if err != nil {
				t.Fatalf("insert mod: %v", err)
			}
			modID, err := result.LastInsertId()
			if err != nil {
				t.Fatalf("LastInsertId() error = %v", err)
			}
			if _, err := panel.db.Exec(`CREATE TRIGGER block_mod_enabled_update BEFORE UPDATE OF enabled ON mods BEGIN SELECT RAISE(FAIL, 'enabled update blocked'); END`); err != nil {
				t.Fatalf("create update trigger: %v", err)
			}

			_, err = panel.setModEnabled(modID, tc.targetEnabled)
			if err == nil || !strings.Contains(err.Error(), "enabled update blocked") {
				t.Fatalf("setModEnabled() error = %v, want enabled update blocked", err)
			}
			if tc.initialSettings == "" {
				if fileExists(settingsPath) {
					t.Fatalf("PalModSettings.ini was created after failed database update")
				}
			} else {
				assertFileEqual(t, settingsPath, []byte(tc.initialSettings))
			}
			var rowEnabled int
			if err := panel.db.QueryRow(`SELECT enabled FROM mods WHERE id = ?`, modID).Scan(&rowEnabled); err != nil {
				t.Fatalf("query mod enabled: %v", err)
			}
			if rowEnabled != tc.initialEnabled {
				t.Fatalf("mod enabled row = %d, want %d", rowEnabled, tc.initialEnabled)
			}
		})
	}
}

func TestSetModEnabledKeepsSettingsAndRowWhenDatabaseCommitFails(t *testing.T) {
	for _, tc := range []struct {
		name            string
		initialEnabled  int
		initialSettings string
		targetEnabled   bool
	}{
		{
			name:           "enable",
			initialEnabled: 0,
			targetEnabled:  true,
		},
		{
			name:            "disable",
			initialEnabled:  1,
			initialSettings: "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=SampleServerMod\n",
			targetEnabled:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			setTestAppSetting(t, panel, "pal_server_path", serverPath)

			infoPath := filepath.Join(serverPath, "Mods", "Workshop", "SampleFolder", "Info.json")
			if err := os.MkdirAll(filepath.Dir(infoPath), 0o755); err != nil {
				t.Fatalf("mkdir workshop dir: %v", err)
			}
			if err := os.WriteFile(infoPath, []byte(`{"PackageName":"SampleServerMod"}`), 0o644); err != nil {
				t.Fatalf("write Info.json: %v", err)
			}
			settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
			if tc.initialSettings != "" {
				if err := os.WriteFile(settingsPath, []byte(tc.initialSettings), 0o644); err != nil {
					t.Fatalf("write PalModSettings.ini: %v", err)
				}
			}
			now := time.Now().UTC().Format(time.RFC3339)
			result, err := panel.db.Exec(
				`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
				 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				"Sample Mod",
				"SampleServerMod",
				"1.0.0",
				"Tester",
				"SampleFolder",
				tc.initialEnabled,
				1,
				`{"PackageName":"SampleServerMod"}`,
				now,
				now,
			)
			if err != nil {
				t.Fatalf("insert mod: %v", err)
			}
			modID, err := result.LastInsertId()
			if err != nil {
				t.Fatalf("LastInsertId() error = %v", err)
			}
			previousCommit := commitModEnabledTx
			commitModEnabledTx = func(tx *sql.Tx) error {
				if err := tx.Rollback(); err != nil {
					return err
				}
				return errors.New("commit blocked")
			}
			defer func() {
				commitModEnabledTx = previousCommit
			}()

			_, err = panel.setModEnabled(modID, tc.targetEnabled)
			if err == nil || !strings.Contains(err.Error(), "commit blocked") {
				t.Fatalf("setModEnabled() error = %v, want commit blocked", err)
			}
			if tc.initialSettings == "" {
				if fileExists(settingsPath) {
					t.Fatalf("PalModSettings.ini was created after failed database commit")
				}
			} else {
				assertFileEqual(t, settingsPath, []byte(tc.initialSettings))
			}
			var rowEnabled int
			var updatedAt string
			if err := panel.db.QueryRow(`SELECT enabled, CAST(updated_at AS TEXT) FROM mods WHERE id = ?`, modID).Scan(&rowEnabled, &updatedAt); err != nil {
				t.Fatalf("query mod enabled: %v", err)
			}
			if rowEnabled != tc.initialEnabled || updatedAt != now {
				t.Fatalf("mod row after failed commit enabled=%d updated_at=%q, want enabled=%d updated_at=%q", rowEnabled, updatedAt, tc.initialEnabled, now)
			}
		})
	}
}

func TestSetModEnabledRestoresRowWhenSettingsWriteFails(t *testing.T) {
	for _, tc := range []struct {
		name            string
		initialEnabled  int
		initialSettings string
		targetEnabled   bool
	}{
		{
			name:           "enable",
			initialEnabled: 0,
			targetEnabled:  true,
		},
		{
			name:            "disable",
			initialEnabled:  1,
			initialSettings: "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=SampleServerMod\n",
			targetEnabled:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			setTestAppSetting(t, panel, "pal_server_path", serverPath)

			infoPath := filepath.Join(serverPath, "Mods", "Workshop", "SampleFolder", "Info.json")
			if err := os.MkdirAll(filepath.Dir(infoPath), 0o755); err != nil {
				t.Fatalf("mkdir workshop dir: %v", err)
			}
			if err := os.WriteFile(infoPath, []byte(`{"PackageName":"SampleServerMod"}`), 0o644); err != nil {
				t.Fatalf("write Info.json: %v", err)
			}
			settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
			if tc.initialSettings != "" {
				if err := os.WriteFile(settingsPath, []byte(tc.initialSettings), 0o644); err != nil {
					t.Fatalf("write PalModSettings.ini: %v", err)
				}
			}
			now := time.Now().UTC().Format(time.RFC3339)
			result, err := panel.db.Exec(
				`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
				 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				"Sample Mod",
				"SampleServerMod",
				"1.0.0",
				"Tester",
				"SampleFolder",
				tc.initialEnabled,
				1,
				`{"PackageName":"SampleServerMod"}`,
				now,
				now,
			)
			if err != nil {
				t.Fatalf("insert mod: %v", err)
			}
			modID, err := result.LastInsertId()
			if err != nil {
				t.Fatalf("LastInsertId() error = %v", err)
			}
			previousReplace := atomicReplaceFile
			atomicReplaceFile = func(src, dst string) error {
				if filepath.Base(dst) != "PalModSettings.ini" {
					t.Fatalf("atomicReplaceFile called for unexpected target: %s", dst)
				}
				if _, err := os.Stat(src); err != nil {
					t.Fatalf("replacement source missing before injected failure: %v", err)
				}
				return errors.New("settings replace blocked")
			}
			defer func() {
				atomicReplaceFile = previousReplace
			}()

			_, err = panel.setModEnabled(modID, tc.targetEnabled)
			if err == nil || !strings.Contains(err.Error(), "settings replace blocked") {
				t.Fatalf("setModEnabled() error = %v, want settings replace blocked", err)
			}
			if tc.initialSettings == "" {
				if fileExists(settingsPath) {
					t.Fatalf("PalModSettings.ini was created after failed settings write")
				}
			} else {
				assertFileEqual(t, settingsPath, []byte(tc.initialSettings))
			}
			var rowEnabled int
			var updatedAt string
			if err := panel.db.QueryRow(`SELECT enabled, CAST(updated_at AS TEXT) FROM mods WHERE id = ?`, modID).Scan(&rowEnabled, &updatedAt); err != nil {
				t.Fatalf("query mod enabled: %v", err)
			}
			if rowEnabled != tc.initialEnabled || updatedAt != now {
				t.Fatalf("mod row after failed settings write enabled=%d updated_at=%q, want enabled=%d updated_at=%q", rowEnabled, updatedAt, tc.initialEnabled, now)
			}
		})
	}
}

func TestModUploadRejectsRunningServerWithoutInstalling(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*App)
	}{
		{
			name: "panel-managed",
			configure: func(panel *App) {
				panel.serverMu.Lock()
				panel.serverCmd = exec.Command("PalServer-test")
				panel.serverMu.Unlock()
			},
		},
		{
			name: "external",
			configure: func(panel *App) {
				panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
					return true, nil
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			panel, err := New(t.TempDir())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer panel.Close()

			serverPath := t.TempDir()
			setTestAppSetting(t, panel, "pal_server_path", serverPath)
			tc.configure(panel)

			server := httptest.NewServer(panel.Routes())
			defer server.Close()

			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatalf("cookiejar.New() error = %v", err)
			}
			client := &http.Client{Jar: jar}
			resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
				"username": "admin",
				"password": "password123",
			})
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("setup status = %d", resp.StatusCode)
			}
			resp.Body.Close()

			resp = doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("upload status = %d", resp.StatusCode)
			}
			resp.Body.Close()
			if fileExists(filepath.Join(serverPath, "Mods")) {
				t.Fatalf("MOD directory was created after rejected upload")
			}
			task := requireTaskWithStatus(t, panel, "mod_upload", "failed")
			if !strings.Contains(task.Log, "PalServer") {
				t.Fatalf("failed upload task log does not mention PalServer runtime: %q", task.Log)
			}
		})
	}
}

func TestParseModArchiveUploadRejectsOversizedBody(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "SampleMod.zip")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte(strings.Repeat("x", 128))); err != nil {
		t.Fatalf("write multipart payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/mods/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	file, header, cleanup, ok := parseModArchiveUpload(recorder, req, 64)
	if ok {
		cleanup()
		t.Fatalf("parseModArchiveUpload() succeeded with file=%v header=%#v, want oversized rejection", file, header)
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "exceeds 64 bytes") {
		t.Fatalf("oversized response missing byte limit: %s", recorder.Body.String())
	}
}

func TestParseModArchiveUploadSpillsLargePartToTempFile(t *testing.T) {
	previousMemory := modArchiveMultipartMemoryBytes
	modArchiveMultipartMemoryBytes = 8
	defer func() {
		modArchiveMultipartMemoryBytes = previousMemory
	}()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "SampleMod.zip")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte(strings.Repeat("x", 128))); err != nil {
		t.Fatalf("write multipart payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/mods/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	file, header, cleanup, ok := parseModArchiveUpload(recorder, req, int64(body.Len()+1024))
	if !ok {
		t.Fatalf("parseModArchiveUpload() failed with status %d; body=%s", recorder.Code, recorder.Body.String())
	}
	defer cleanup()
	if header.Filename != "SampleMod.zip" {
		t.Fatalf("filename = %q, want SampleMod.zip", header.Filename)
	}
	if _, ok := file.(*os.File); !ok {
		t.Fatalf("file type = %T, want *os.File temp-backed multipart file", file)
	}
}

func TestModUploadRejectsActiveOperationBeforeReadingBody(t *testing.T) {
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

	body := &countingReadCloser{}
	req := httptest.NewRequest(http.MethodPost, "/api/mods/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=ignored")
	recorder := httptest.NewRecorder()
	panel.handleUploadMod(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("upload status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if body.reads != 0 {
		t.Fatalf("upload request body was read %d times despite active operation", body.reads)
	}
	assertNoTasks(t, panel)
}

func TestModUpdateRejectsActiveOperationBeforeReadingBody(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sample Mod",
		"SampleServerMod",
		"1.2.3",
		"Tester",
		"SampleMod",
		0,
		1,
		`{"PackageName":"SampleServerMod"}`,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	body := &countingReadCloser{}
	req := httptest.NewRequest(http.MethodPost, "/api/mods/"+strconvID(modID)+"/update", body)
	req.SetPathValue("id", strconvID(modID))
	req.Header.Set(confirmationHeader, "true")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=ignored")
	recorder := httptest.NewRecorder()
	panel.handleUpdateMod(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("update status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if body.reads != 0 {
		t.Fatalf("update request body was read %d times despite active operation", body.reads)
	}
	assertNoTasks(t, panel)
}

func TestModMutationsRejectExternalRuntimeWithoutFileChanges(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	externalRunning := false
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return externalRunning, nil
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", mod.FolderName, "Info.json")
	if !fileExists(infoPath) {
		t.Fatalf("installed Info.json missing: %s", infoPath)
	}

	externalRunning = true
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/enable", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("enable status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if fileExists(filepath.Join(serverPath, "Mods", "PalModSettings.ini")) {
		t.Fatalf("PalModSettings.ini was created after rejected enable")
	}
	afterEnableReject, err := panel.getMod(mod.ID)
	if err != nil {
		t.Fatalf("getMod() after rejected enable error = %v", err)
	}
	if afterEnableReject.Enabled {
		t.Fatalf("rejected enable changed mod enabled state: %#v", afterEnableReject)
	}

	externalRunning = false
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/enable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status after clearing external runtime = %d", resp.StatusCode)
	}
	resp.Body.Close()
	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	settingsBefore, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read PalModSettings.ini: %v", err)
	}
	infoBefore, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("read Info.json: %v", err)
	}

	externalRunning = true
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/disable", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("disable status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	assertFileEqual(t, settingsPath, []byte(settingsBefore))

	resp = doMultipartConfirmed(t, client, server.URL+"/api/mods/"+strconvID(mod.ID)+"/update", "SampleMod-v2.zip", sampleModZipWith(t, "SampleServerMod", "2.0.0"))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	assertFileEqual(t, settingsPath, []byte(settingsBefore))
	assertFileEqual(t, infoPath, infoBefore)
	afterUpdateReject, err := panel.getMod(mod.ID)
	if err != nil {
		t.Fatalf("getMod() after rejected update error = %v", err)
	}
	if afterUpdateReject.Version != "1.2.3" || !afterUpdateReject.Enabled {
		t.Fatalf("rejected update changed mod row: %#v", afterUpdateReject)
	}

	resp = doJSONConfirmed(t, client, http.MethodDelete, server.URL+"/api/mods/"+strconvID(mod.ID), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	assertFileEqual(t, settingsPath, settingsBefore)
	if !fileExists(infoPath) {
		t.Fatalf("mod files were removed after rejected delete")
	}
	if _, err := panel.getMod(mod.ID); err != nil {
		t.Fatalf("mod row missing after rejected delete: %v", err)
	}
	for _, taskType := range []string{"mod_enable", "mod_disable", "mod_update", "mod_delete"} {
		task := requireTaskWithStatus(t, panel, taskType, "failed")
		if !strings.Contains(task.Log, "outside this panel") {
			t.Fatalf("%s failed task log missing external runtime reason: %q", taskType, task.Log)
		}
	}
}

func TestDeleteModRejectsEscapingManagedModsPackageName(t *testing.T) {
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
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

	workshopInfoPath := filepath.Join(serverPath, "Mods", "Workshop", "SafeFolder", "Info.json")
	if err := os.MkdirAll(filepath.Dir(workshopInfoPath), 0o755); err != nil {
		t.Fatalf("mkdir workshop dir: %v", err)
	}
	if err := os.WriteFile(workshopInfoPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write workshop info: %v", err)
	}
	outsideManagedPath := filepath.Join(serverPath, "Mods", "OutsideManaged", "sentinel.txt")
	if err := os.MkdirAll(filepath.Dir(outsideManagedPath), 0o755); err != nil {
		t.Fatalf("mkdir outside managed dir: %v", err)
	}
	if err := os.WriteFile(outsideManagedPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write outside managed sentinel: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Bad Mod",
		".."+string(filepath.Separator)+"OutsideManaged",
		"1.0.0",
		"Tester",
		"SafeFolder",
		0,
		1,
		`{"PackageName":"BadMod"}`,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	err = panel.deleteMod(modID)
	if err == nil {
		t.Fatalf("deleteMod() succeeded for escaping PackageName")
	}
	if !strings.Contains(err.Error(), "invalid mod PackageName") {
		t.Fatalf("deleteMod() error = %v, want invalid PackageName", err)
	}
	if !fileExists(workshopInfoPath) {
		t.Fatalf("workshop directory was removed after rejected delete")
	}
	if !fileExists(outsideManagedPath) {
		t.Fatalf("outside managed path was removed after rejected delete")
	}
}

func TestDeleteModKeepsFilesSettingsAndRowWhenDatabaseDeleteFails(t *testing.T) {
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
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

	const packageName = "SampleServerMod"
	const folderName = "SampleFolder"
	workshopInfoPath := filepath.Join(serverPath, "Mods", "Workshop", folderName, "Info.json")
	if err := os.MkdirAll(filepath.Dir(workshopInfoPath), 0o755); err != nil {
		t.Fatalf("mkdir workshop dir: %v", err)
	}
	if err := os.WriteFile(workshopInfoPath, []byte(`{"PackageName":"SampleServerMod"}`), 0o644); err != nil {
		t.Fatalf("write workshop info: %v", err)
	}
	managedManifestPath := filepath.Join(serverPath, "Mods", "ManagedMods", packageName, "InstallManifest.json")
	if err := os.MkdirAll(filepath.Dir(managedManifestPath), 0o755); err != nil {
		t.Fatalf("mkdir managed mod dir: %v", err)
	}
	if err := os.WriteFile(managedManifestPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write managed manifest: %v", err)
	}
	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	settingsBefore := "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=SampleServerMod\n"
	if err := os.WriteFile(settingsPath, []byte(settingsBefore), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sample Mod",
		packageName,
		"1.0.0",
		"Tester",
		folderName,
		1,
		1,
		`{"PackageName":"SampleServerMod"}`,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	if _, err := panel.db.Exec(`CREATE TRIGGER block_mod_delete BEFORE DELETE ON mods BEGIN SELECT RAISE(FAIL, 'delete blocked'); END`); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}

	err = panel.deleteMod(modID)
	if err == nil || !strings.Contains(err.Error(), "delete blocked") {
		t.Fatalf("deleteMod() error = %v, want delete blocked", err)
	}
	if !fileExists(workshopInfoPath) {
		t.Fatalf("workshop files were removed after failed database delete")
	}
	if !fileExists(managedManifestPath) {
		t.Fatalf("managed mod files were removed after failed database delete")
	}
	assertFileEqual(t, settingsPath, []byte(settingsBefore))
	if _, err := panel.getMod(modID); err != nil {
		t.Fatalf("mod row missing after failed database delete: %v", err)
	}
}

func TestDeleteModRestoresSettingsAndRowWhenDirectoryRemoveFails(t *testing.T) {
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
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	const packageName = "SampleServerMod"
	const folderName = "SampleFolder"
	workshopInfoPath := filepath.Join(serverPath, "Mods", "Workshop", folderName, "Info.json")
	if err := os.MkdirAll(filepath.Dir(workshopInfoPath), 0o755); err != nil {
		t.Fatalf("mkdir workshop dir: %v", err)
	}
	if err := os.WriteFile(workshopInfoPath, []byte(`{"PackageName":"SampleServerMod"}`), 0o644); err != nil {
		t.Fatalf("write workshop info: %v", err)
	}
	managedManifestPath := filepath.Join(serverPath, "Mods", "ManagedMods", packageName, "InstallManifest.json")
	if err := os.MkdirAll(filepath.Dir(managedManifestPath), 0o755); err != nil {
		t.Fatalf("mkdir managed mod dir: %v", err)
	}
	if err := os.WriteFile(managedManifestPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write managed manifest: %v", err)
	}
	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	settingsBefore := "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=SampleServerMod\n"
	if err := os.WriteFile(settingsPath, []byte(settingsBefore), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sample Mod",
		packageName,
		"1.0.0",
		"Tester",
		folderName,
		1,
		1,
		`{"PackageName":"SampleServerMod"}`,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	previousRemove := removeModPath
	removeModPath = func(path string) error {
		if strings.Contains(path, ".palpanel-delete-") {
			if fileExists(workshopInfoPath) {
				t.Fatalf("workshop directory was not staged before cleanup: %s", workshopInfoPath)
			}
			if fileExists(managedManifestPath) {
				t.Fatalf("managed directory was not staged before cleanup: %s", managedManifestPath)
			}
			return errors.New("remove blocked")
		}
		return previousRemove(path)
	}
	defer func() {
		removeModPath = previousRemove
	}()

	err = panel.deleteMod(modID)
	if err == nil || !strings.Contains(err.Error(), "remove blocked") {
		t.Fatalf("deleteMod() error = %v, want remove blocked", err)
	}
	if !fileExists(workshopInfoPath) {
		t.Fatalf("workshop files were removed after failed directory delete")
	}
	if !fileExists(managedManifestPath) {
		t.Fatalf("managed mod files were removed after failed directory delete")
	}
	assertFileEqual(t, settingsPath, []byte(settingsBefore))
	after, err := panel.getMod(modID)
	if err != nil {
		t.Fatalf("mod row missing after failed directory delete: %v", err)
	}
	if !after.Enabled || after.PackageName != packageName || after.FolderName != folderName {
		t.Fatalf("mod row was not restored after failed directory delete: %#v", after)
	}
}

func TestModUpdateRouteRequiresMatchingPackageName(t *testing.T) {
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
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/enable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doMultipart(t, client, server.URL+"/api/mods/"+strconvID(mod.ID)+"/update", "SampleMod-v2.zip", sampleModZipWith(t, "SampleServerMod", "2.0.0"))
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed update status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doMultipartConfirmed(t, client, server.URL+"/api/mods/"+strconvID(mod.ID)+"/update", "SampleMod-v2.zip", sampleModZipWith(t, "SampleServerMod", "2.0.0"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	var updated modRecord
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated mod: %v", err)
	}
	resp.Body.Close()
	if updated.ID != mod.ID || updated.PackageName != mod.PackageName || updated.Version != "2.0.0" || !updated.Enabled {
		t.Fatalf("unexpected updated mod: %#v", updated)
	}

	resp = doMultipartConfirmed(t, client, server.URL+"/api/mods/"+strconvID(mod.ID)+"/update", "WrongMod.zip", sampleModZipWith(t, "WrongServerMod", "9.9.9"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mismatched update status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	afterMismatch, err := panel.getMod(mod.ID)
	if err != nil {
		t.Fatalf("getMod() after mismatch error = %v", err)
	}
	if afterMismatch.Version != "2.0.0" || afterMismatch.PackageName != "SampleServerMod" {
		t.Fatalf("mismatched update changed mod: %#v", afterMismatch)
	}
	successTask := requireTaskWithStatus(t, panel, "mod_update", "success")
	if !strings.Contains(successTask.Log, "version 2.0.0") {
		t.Fatalf("successful update task log missing version: %q", successTask.Log)
	}
	failedTask := requireTaskWithStatus(t, panel, "mod_update", "failed")
	if !strings.Contains(failedTask.Log, "does not match selected mod") {
		t.Fatalf("failed update task log missing mismatch reason: %q", failedTask.Log)
	}
}

func TestOpenModDirectoryRouteUsesConstrainedInstallPath(t *testing.T) {
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
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	var openedPath string
	panel.openDirectory = func(path string) error {
		openedPath = path
		return nil
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doMultipart(t, client, server.URL+"/api/mods/upload", "SampleMod.zip", sampleModZip(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var mod modRecord
	if err := json.NewDecoder(resp.Body).Decode(&mod); err != nil {
		t.Fatalf("decode mod: %v", err)
	}
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(mod.ID)+"/open-dir", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open-dir status = %d", resp.StatusCode)
	}
	var opened openDirectoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&opened); err != nil {
		t.Fatalf("decode open-dir response: %v", err)
	}
	resp.Body.Close()
	wantPath := filepath.Join(serverPath, "Mods", "Workshop", mod.FolderName)
	if openedPath != wantPath {
		t.Fatalf("opened path = %q, want %q", openedPath, wantPath)
	}
	if opened.Path != wantPath || opened.Status != "ok" {
		t.Fatalf("unexpected open response: %#v", opened)
	}
}

func TestOpenModDirectoryRejectsPersistedEscapingFolderName(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Bad Mod",
		"BadMod",
		"1.0.0",
		"Tester",
		".."+string(filepath.Separator)+"SaveGames",
		0,
		1,
		`{"PackageName":"BadMod"}`,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	called := false
	panel.openDirectory = func(path string) error {
		called = true
		return nil
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/mods/"+strconvID(modID)+"/open-dir", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("open-dir escaping folder status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if called {
		t.Fatalf("directory opener was called for an escaping folder name")
	}
}

func TestExtractZipToDirRejectsSymlinkEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "mod-symlink.zip")
	archive, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(archive)
	header := &zip.FileHeader{
		Name:   "SampleMod/Info.json",
		Method: zip.Deflate,
	}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatalf("zip create symlink entry: %v", err)
	}
	if _, err := entry.Write([]byte("../../outside-info.json")); err != nil {
		t.Fatalf("zip write symlink entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("archive close: %v", err)
	}

	dest := t.TempDir()
	err = extractZipToDir(zipPath, dest)
	if err == nil || !strings.Contains(err.Error(), "symlink entry") {
		t.Fatalf("extractZipToDir() error = %v, want symlink entry rejection", err)
	}
	if fileExists(filepath.Join(dest, "SampleMod", "Info.json")) {
		t.Fatalf("symlink entry was extracted as a regular file")
	}
}

func TestExtractZipToDirRejectsResourceLimits(t *testing.T) {
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
				"SampleMod/a.txt": "a",
				"SampleMod/b.txt": "b",
			},
			want: "more than 1 files",
		},
		{
			name:       "single entry",
			entryBytes: 5,
			totalBytes: 100,
			fileCount:  10,
			files: map[string]string{
				"SampleMod/large.txt": "123456",
			},
			want: "exceeds 5 bytes",
		},
		{
			name:       "total bytes",
			entryBytes: 10,
			totalBytes: 10,
			fileCount:  10,
			files: map[string]string{
				"SampleMod/a.txt": "123456",
				"SampleMod/b.txt": "abcdef",
			},
			want: "expands beyond 10 bytes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setModExtractionLimits(t, tc.entryBytes, tc.totalBytes, tc.fileCount)
			zipPath := writeTestModZip(t, tc.files)
			err := extractZipToDir(zipPath, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("extractZipToDir() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestModExtractionLimitReaderCountsActualBytes(t *testing.T) {
	setModExtractionLimits(t, 4, 100, 10)
	var copied int64
	reader := &modExtractionLimitReader{
		reader:     strings.NewReader("12345"),
		entryName:  "SampleMod/Data/file.txt",
		totalBytes: &copied,
	}
	_, err := io.Copy(io.Discard, reader)
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("entry limit copy error = %v, want exceeds 4 bytes", err)
	}

	setModExtractionLimits(t, 100, 4, 10)
	copied = 0
	reader = &modExtractionLimitReader{
		reader:     strings.NewReader("12345"),
		entryName:  "SampleMod/Data/file.txt",
		totalBytes: &copied,
	}
	_, err = io.Copy(io.Discard, reader)
	if err == nil || !strings.Contains(err.Error(), "expands beyond 4 bytes") {
		t.Fatalf("total limit copy error = %v, want expands beyond 4 bytes", err)
	}
}

func TestInspectExtractedModIgnoresSymlinkedInfoJSON(t *testing.T) {
	root := t.TempDir()
	outsideInfo := filepath.Join(t.TempDir(), "Info.json")
	if err := os.WriteFile(outsideInfo, []byte(`{"PackageName":"OutsideMod","InstallRules":[{"IsServer":true}]}`), 0o644); err != nil {
		t.Fatalf("write outside Info.json: %v", err)
	}
	if err := os.Symlink(outsideInfo, filepath.Join(root, "Info.json")); err != nil {
		t.Skipf("symlink creation unavailable in this environment: %v", err)
	}

	_, err := inspectExtractedMod(root)
	if err == nil || !strings.Contains(err.Error(), "Info.json was not found") {
		t.Fatalf("inspectExtractedMod() error = %v, want missing Info.json after skipping symlink", err)
	}
}

func TestInspectExtractedModRejectsOversizedInfoJSON(t *testing.T) {
	root := t.TempDir()
	infoDir := filepath.Join(root, "LargeMod")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatalf("mkdir info dir: %v", err)
	}
	oversized := bytes.Repeat([]byte(" "), int(maxModInfoJSONBytes)+1)
	if err := os.WriteFile(filepath.Join(infoDir, "Info.json"), oversized, 0o644); err != nil {
		t.Fatalf("write Info.json: %v", err)
	}

	_, err := inspectExtractedMod(root)
	if err == nil || !strings.Contains(err.Error(), "Info.json exceeds") {
		t.Fatalf("inspectExtractedMod() error = %v, want size limit rejection", err)
	}
}

func TestModUploadRejectsOversizedInfoJSONWithoutBackupOrRow(t *testing.T) {
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
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	oversized := bytes.Repeat([]byte(" "), int(maxModInfoJSONBytes)+1)
	resp = doMultipart(t, client, server.URL+"/api/mods/upload", "LargeInfo.zip", sampleModZipWithInfoBytes(t, oversized))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	var modCount int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM mods`).Scan(&modCount); err != nil {
		t.Fatalf("count mods: %v", err)
	}
	if modCount != 0 {
		t.Fatalf("mod count = %d, want 0", modCount)
	}
	var backupCount int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&backupCount); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if backupCount != 0 {
		t.Fatalf("backup count = %d, want 0", backupCount)
	}
}

func TestValidateExtractedModTreeRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "SampleMod"), 0o755); err != nil {
		t.Fatalf("mkdir extracted mod dir: %v", err)
	}
	outsideFile := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "SampleMod", "linked.txt")); err != nil {
		t.Skipf("symlink creation unavailable in this environment: %v", err)
	}

	err := validateExtractedModTree(root)
	if err == nil || !strings.Contains(err.Error(), "contains symlink") {
		t.Fatalf("validateExtractedModTree() error = %v, want symlink rejection", err)
	}
}

func TestValidateExtractedModTreeRejectsResourceLimits(t *testing.T) {
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
			want: "exceeds 10 bytes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setModExtractionLimits(t, tc.entryBytes, tc.totalBytes, tc.fileCount)
			root := t.TempDir()
			modDir := filepath.Join(root, "SampleMod")
			if err := os.MkdirAll(modDir, 0o755); err != nil {
				t.Fatalf("mkdir mod dir: %v", err)
			}
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(modDir, name), []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			err := validateExtractedModTree(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateExtractedModTree() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateExtractionWorkspaceRejectsSiblingOutput(t *testing.T) {
	workspace := t.TempDir()
	extractDir := filepath.Join(workspace, "content")
	if err := os.MkdirAll(filepath.Join(extractDir, "SampleMod"), 0o755); err != nil {
		t.Fatalf("mkdir content dir: %v", err)
	}
	archivePath := filepath.Join(workspace, "archive.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extractDir, "SampleMod", "Info.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write extracted Info.json: %v", err)
	}
	if err := validateExtractionWorkspace(workspace, extractDir, archivePath); err != nil {
		t.Fatalf("validateExtractionWorkspace() normal workspace error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(workspace, "escaped.txt"), []byte("outside content"), 0o644); err != nil {
		t.Fatalf("write escaped output: %v", err)
	}
	err := validateExtractionWorkspace(workspace, extractDir, archivePath)
	if err == nil || !strings.Contains(err.Error(), "outside target directory") {
		t.Fatalf("validateExtractionWorkspace() error = %v, want outside target rejection", err)
	}
}

func TestRunExtractorTimesOut(t *testing.T) {
	setModExtractorRuntime(t, 20*time.Millisecond, 1024)
	path, args := extractorShellCommand(t, sleepCommandScript())
	err := runExtractor(path, args)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("runExtractor() error = %v, want timeout", err)
	}
}

func TestRunExtractorCapsErrorOutput(t *testing.T) {
	setModExtractorRuntime(t, time.Minute, 16)
	path, args := extractorShellCommand(t, noisyFailureScript())
	err := runExtractor(path, args)
	if err == nil {
		t.Fatalf("runExtractor() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), modExtractorOutputTruncatedSuffix) {
		t.Fatalf("runExtractor() error missing truncation suffix: %v", err)
	}
	if strings.Count(err.Error(), "x") > 32 {
		t.Fatalf("runExtractor() retained too much output: %v", err)
	}
}

func TestInspectExtractedModRejectsUnsafePackageName(t *testing.T) {
	for _, packageName := range []string{
		"Bad\nActiveModList=Injected",
		"..\\BadMod",
		"Bad/Mod",
		"Bad:Mod",
		"Bad.",
		"CON",
	} {
		t.Run(strings.NewReplacer("\n", "_", "\\", "_", "/", "_", ":", "_").Replace(packageName), func(t *testing.T) {
			root := t.TempDir()
			infoDir := filepath.Join(root, "UnsafeMod")
			if err := os.MkdirAll(infoDir, 0o755); err != nil {
				t.Fatalf("mkdir info dir: %v", err)
			}
			infoBytes, err := json.Marshal(map[string]any{
				"PackageName":  packageName,
				"InstallRules": []map[string]any{{"IsServer": true}},
			})
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if err := os.WriteFile(filepath.Join(infoDir, "Info.json"), infoBytes, 0o644); err != nil {
				t.Fatalf("write Info.json: %v", err)
			}

			_, err = inspectExtractedMod(root)
			if err == nil || !strings.Contains(err.Error(), "invalid mod PackageName") {
				t.Fatalf("inspectExtractedMod() error = %v, want invalid PackageName", err)
			}
		})
	}
}

func TestUpdatePalModSettingsRejectsUnsafePackageNameWithoutMutation(t *testing.T) {
	base := t.TempDir()
	settingsPath := filepath.Join(base, "Mods", "PalModSettings.ini")
	original := "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=OldMod\n"
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}

	err := updatePalModSettings(base, "Bad\nActiveModList=Injected", true)
	if err == nil || !strings.Contains(err.Error(), "invalid mod PackageName") {
		t.Fatalf("updatePalModSettings() error = %v, want invalid PackageName", err)
	}
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read PalModSettings.ini: %v", err)
	}
	if string(content) != original {
		t.Fatalf("PalModSettings.ini changed after rejected package name:\n%s", content)
	}
}

func TestReadPalModSettingsRejectsOversizedSettings(t *testing.T) {
	setPalModSettingsLimit(t, 64)
	base := t.TempDir()
	settingsPath := filepath.Join(base, "Mods", "PalModSettings.ini")
	original := "[PalModSettings]\n" + strings.Repeat("x", 96)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}

	_, err := readPalModSettings(base)
	if !errors.Is(err, errPalModSettingsTooLarge) {
		t.Fatalf("readPalModSettings() error = %v, want errPalModSettingsTooLarge", err)
	}
	assertFileEqual(t, settingsPath, []byte(original))
}

func TestSetModEnabledRejectsOversizedSettingsBeforeBackupOrMutation(t *testing.T) {
	setPalModSettingsLimit(t, 128)
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
	setTestAppSetting(t, panel, "pal_server_path", serverPath)

	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	originalSettings := "[PalModSettings]\nbGlobalEnableMod=true\n" + strings.Repeat("x", 160)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(originalSettings), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}

	folderName := "SampleServerMod"
	infoPath := filepath.Join(serverPath, "Mods", "Workshop", folderName, "Info.json")
	if err := os.MkdirAll(filepath.Dir(infoPath), 0o755); err != nil {
		t.Fatalf("mkdir mod dir: %v", err)
	}
	if err := os.WriteFile(infoPath, []byte(`{"PackageName":"SampleServerMod"}`), 0o644); err != nil {
		t.Fatalf("write Info.json: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sample",
		"SampleServerMod",
		"1.0.0",
		"Tester",
		folderName,
		0,
		1,
		`{"PackageName":"SampleServerMod"}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	_, err = panel.setModEnabled(modID, true)
	if !errors.Is(err, errPalModSettingsTooLarge) {
		t.Fatalf("setModEnabled() error = %v, want errPalModSettingsTooLarge", err)
	}
	var enabled int
	if err := panel.db.QueryRow(`SELECT enabled FROM mods WHERE id = ?`, modID).Scan(&enabled); err != nil {
		t.Fatalf("query enabled: %v", err)
	}
	if enabled != 0 {
		t.Fatalf("enabled = %d, want 0", enabled)
	}
	assertNoBackups(t, panel)
	assertFileEqual(t, settingsPath, []byte(originalSettings))
}

func TestModReadRoutesRejectOversizedSettings(t *testing.T) {
	setPalModSettingsLimit(t, 64)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("[PalModSettings]\n"+strings.Repeat("x", 96)), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sample",
		"SampleServerMod",
		"1.0.0",
		"Tester",
		"SampleServerMod",
		0,
		1,
		`{"PackageName":"SampleServerMod"}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/mods", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("GET /api/mods status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/mods/"+strconvID(modID)+"/info", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("GET /api/mods/{id}/info status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

func TestSetModEnabledRejectsUnsafePackageNameBeforeMutation(t *testing.T) {
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
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	originalSettings := "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=OldMod\n"
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(originalSettings), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Bad Mod",
		"Bad\nActiveModList=Injected",
		"1.0.0",
		"Tester",
		"SafeFolder",
		0,
		1,
		`{"PackageName":"Bad\nActiveModList=Injected"}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	_, err = panel.setModEnabled(modID, true)
	if err == nil || !strings.Contains(err.Error(), "invalid mod PackageName") {
		t.Fatalf("setModEnabled() error = %v, want invalid PackageName", err)
	}
	after, err := panel.getMod(modID)
	if err != nil {
		t.Fatalf("getMod() error = %v", err)
	}
	if after.Enabled {
		t.Fatalf("setModEnabled() changed enabled state for unsafe PackageName")
	}
	assertFileEqual(t, settingsPath, []byte(originalSettings))

	var backupCount int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&backupCount); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if backupCount != 0 {
		t.Fatalf("backup count = %d, want 0", backupCount)
	}
}

func TestSetModEnabledRejectsUnsafeFolderNameBeforeMutation(t *testing.T) {
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
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	settingsPath := filepath.Join(serverPath, "Mods", "PalModSettings.ini")
	originalSettings := "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=OldMod\n"
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(originalSettings), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}
	outsideInfoPath := filepath.Join(serverPath, "Mods", "OutsideWorkshop", "Info.json")
	if err := os.MkdirAll(filepath.Dir(outsideInfoPath), 0o755); err != nil {
		t.Fatalf("mkdir outside workshop dir: %v", err)
	}
	if err := os.WriteFile(outsideInfoPath, []byte(`{"PackageName":"SafePackage"}`), 0o644); err != nil {
		t.Fatalf("write outside Info.json: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Bad Folder Mod",
		"SafePackage",
		"1.0.0",
		"Tester",
		".."+string(filepath.Separator)+"OutsideWorkshop",
		0,
		1,
		`{"PackageName":"SafePackage"}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	_, err = panel.setModEnabled(modID, true)
	if err == nil || !strings.Contains(err.Error(), "invalid mod folder name") {
		t.Fatalf("setModEnabled() error = %v, want invalid mod folder name", err)
	}
	after, err := panel.getMod(modID)
	if err != nil {
		t.Fatalf("getMod() error = %v", err)
	}
	if after.Enabled {
		t.Fatalf("setModEnabled() changed enabled state for unsafe folder_name")
	}
	assertFileEqual(t, settingsPath, []byte(originalSettings))

	var backupCount int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&backupCount); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if backupCount != 0 {
		t.Fatalf("backup count = %d, want 0", backupCount)
	}
}

func TestModUpdateRejectsUnsafePersistedFolderNameBeforeBackup(t *testing.T) {
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
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Bad Folder Mod",
		"SampleServerMod",
		"1.0.0",
		"Tester",
		".."+string(filepath.Separator)+"OutsideWorkshop",
		0,
		1,
		`{"PackageName":"SampleServerMod"}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doMultipartConfirmed(t, client, server.URL+"/api/mods/"+strconvID(modID)+"/update", "SampleMod-v2.zip", sampleModZipWith(t, "SampleServerMod", "2.0.0"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	after, err := panel.getMod(modID)
	if err != nil {
		t.Fatalf("getMod() error = %v", err)
	}
	if after.Version != "1.0.0" || after.FolderName != ".."+string(filepath.Separator)+"OutsideWorkshop" {
		t.Fatalf("rejected update changed mod row: %#v", after)
	}
	var backupCount int
	if err := panel.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&backupCount); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if backupCount != 0 {
		t.Fatalf("backup count = %d, want 0", backupCount)
	}
}

func TestListAndGetModOmitInstallPathForUnsafeFolderName(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := panel.db.Exec(
		`INSERT INTO mods(name, package_name, version, author, folder_name, enabled, server_supported, info_json, installed_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Bad Folder Mod",
		"SafePackage",
		"1.0.0",
		"Tester",
		".."+string(filepath.Separator)+"OutsideWorkshop",
		0,
		1,
		`{"PackageName":"SafePackage"}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mod: %v", err)
	}
	modID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}

	mods, err := panel.listMods()
	if err != nil {
		t.Fatalf("listMods() error = %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("listMods() length = %d, want 1", len(mods))
	}
	if mods[0].InstallPath != "" {
		t.Fatalf("listMods() install_path = %q, want empty for unsafe folder_name", mods[0].InstallPath)
	}
	mod, err := panel.getMod(modID)
	if err != nil {
		t.Fatalf("getMod() error = %v", err)
	}
	if mod.InstallPath != "" {
		t.Fatalf("getMod() install_path = %q, want empty for unsafe folder_name", mod.InstallPath)
	}
}

func TestRenderPalModSettingsPreservesOtherLines(t *testing.T) {
	input := `[PalModSettings]
WorkshopRootDir=C:\Mods
ActiveModList=OldMod
[Other]
Value=1
`
	enabled := renderPalModSettings(input, "NewMod", true)
	for _, want := range []string{
		"bGlobalEnableMod=true",
		"WorkshopRootDir=C:\\Mods",
		"ActiveModList=OldMod",
		"ActiveModList=NewMod",
		"[Other]",
	} {
		if !strings.Contains(enabled, want) {
			t.Fatalf("rendered settings missing %q: %s", want, enabled)
		}
	}
	disabled := renderPalModSettings(enabled, "OldMod", false)
	if strings.Contains(disabled, "ActiveModList=OldMod") {
		t.Fatalf("disabled settings still contain OldMod: %s", disabled)
	}
	if !strings.Contains(disabled, "ActiveModList=NewMod") {
		t.Fatalf("disabled settings lost NewMod: %s", disabled)
	}
}

func sampleModZip(t *testing.T) []byte {
	return sampleModZipWith(t, "SampleServerMod", "1.2.3")
}

func sampleModZipWith(t *testing.T, packageName, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	files := map[string]string{
		"SampleMod/Info.json": `{
  "Name": "Sample Mod",
  "PackageName": "` + packageName + `",
  "Version": "` + version + `",
  "Author": "Tester",
  "InstallRules": [
    { "IsServer": true, "Files": [] }
  ]
}`,
		"SampleMod/Data/file.txt": "payload",
	}
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
	return buf.Bytes()
}

func writeSampleModContent(t *testing.T, root, packageName, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "Data"), 0o755); err != nil {
		t.Fatalf("mkdir sample mod content: %v", err)
	}
	info := `{
  "Name": "Sample Mod",
  "PackageName": "` + packageName + `",
  "Version": "` + version + `",
  "Author": "Tester",
  "InstallRules": [
    { "IsServer": true, "Files": [] }
  ]
}`
	if err := os.WriteFile(filepath.Join(root, "Info.json"), []byte(info), 0o644); err != nil {
		t.Fatalf("write sample Info.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data", "file.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write sample mod payload: %v", err)
	}
}

func sampleModZipWithInfoBytes(t *testing.T, info []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	files := map[string][]byte{
		"SampleMod/Info.json":     info,
		"SampleMod/Data/file.txt": []byte("payload"),
	}
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func writeTestModZip(t *testing.T, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "mod.zip")
	archive, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
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
	return zipPath
}

func setModExtractionLimits(t *testing.T, entryBytes, totalBytes int64, fileCount int) {
	t.Helper()
	previousEntryBytes := maxModExtractedEntryBytes
	previousTotalBytes := maxModExtractedTotalBytes
	previousFileCount := maxModExtractedFileCount
	maxModExtractedEntryBytes = entryBytes
	maxModExtractedTotalBytes = totalBytes
	maxModExtractedFileCount = fileCount
	t.Cleanup(func() {
		maxModExtractedEntryBytes = previousEntryBytes
		maxModExtractedTotalBytes = previousTotalBytes
		maxModExtractedFileCount = previousFileCount
	})
}

func setModExtractorRuntime(t *testing.T, timeout time.Duration, outputBytes int64) {
	t.Helper()
	previousTimeout := modExtractorTimeout
	previousOutputBytes := maxModExtractorOutputBytes
	modExtractorTimeout = timeout
	maxModExtractorOutputBytes = outputBytes
	t.Cleanup(func() {
		modExtractorTimeout = previousTimeout
		maxModExtractorOutputBytes = previousOutputBytes
	})
}

func extractorShellCommand(t *testing.T, script string) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		for _, name := range []string{"powershell.exe", "powershell", "pwsh"} {
			path, err := exec.LookPath(name)
			if err == nil {
				return path, []string{"-NoProfile", "-Command", script}
			}
		}
		t.Skip("PowerShell is not available for extractor command test")
	}
	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh is not available for extractor command test: %v", err)
	}
	return path, []string{"-c", script}
}

func sleepCommandScript() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Milliseconds 250"
	}
	return "sleep 0.25"
}

func noisyFailureScript() string {
	if runtime.GOOS == "windows" {
		return "$s = 'x' * 200; [Console]::Error.Write($s); exit 1"
	}
	return "i=0; while [ $i -lt 200 ]; do printf x; i=$((i+1)); done; exit 1"
}

func TestReplaceInstalledModDirReportsPreviousRestoreFailure(t *testing.T) {
	parent := t.TempDir()
	targetDir := filepath.Join(parent, "SampleFolder")
	stagingDir := filepath.Join(parent, ".palpanel-install-test")
	oldInfo := []byte(`{"PackageName":"SampleServerMod","Version":"1.0.0"}`)
	newInfo := []byte(`{"PackageName":"SampleServerMod","Version":"2.0.0"}`)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Info.json"), oldInfo, 0o644); err != nil {
		t.Fatalf("write old Info.json: %v", err)
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("mkdir staging dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "Info.json"), newInfo, 0o644); err != nil {
		t.Fatalf("write new Info.json: %v", err)
	}

	previousRename := renameModPath
	var replacedDir string
	renameModPath = func(src, dst string) error {
		switch {
		case src == targetDir && strings.Contains(dst, ".palpanel-replaced-"):
			replacedDir = dst
			return previousRename(src, dst)
		case src == stagingDir && dst == targetDir:
			return errors.New("replace blocked")
		case replacedDir != "" && src == replacedDir && dst == targetDir:
			return errors.New("restore blocked")
		default:
			return previousRename(src, dst)
		}
	}
	defer func() {
		renameModPath = previousRename
	}()

	err := replaceInstalledModDir(stagingDir, targetDir)
	if err == nil {
		t.Fatal("replaceInstalledModDir() error = nil, want injected replace and restore failures")
	}
	if !strings.Contains(err.Error(), "replace blocked") || !strings.Contains(err.Error(), "restore blocked") {
		t.Fatalf("replaceInstalledModDir() error = %v, want replace and restore failures", err)
	}
	if fileExists(filepath.Join(targetDir, "Info.json")) {
		t.Fatalf("target dir was restored despite injected restore failure")
	}
	if replacedDir == "" {
		t.Fatalf("previous directory was not moved aside before replacement")
	}
	assertFileEqual(t, filepath.Join(replacedDir, "Info.json"), oldInfo)
}

func TestUpdatePalModSettingsFailedReplaceKeepsOriginalAndRemovesTemp(t *testing.T) {
	base := t.TempDir()
	settingsPath := filepath.Join(base, "Mods", "PalModSettings.ini")
	original := "[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=OldMod\n"
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write PalModSettings.ini: %v", err)
	}

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

	err := updatePalModSettings(base, "NewMod", true)
	if err == nil {
		t.Fatal("updatePalModSettings() error = nil, want injected replacement failure")
	}
	assertFileEqual(t, settingsPath, []byte(original))
	entries, err := os.ReadDir(filepath.Dir(settingsPath))
	if err != nil {
		t.Fatalf("read settings dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".PalModSettings.ini.tmp-") {
			t.Fatalf("temporary MOD settings file was not removed: %s", entry.Name())
		}
	}
}

func assertFileEqual(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s content changed\nwant:\n%s\ngot:\n%s", path, want, got)
	}
}

func setPalModSettingsLimit(t *testing.T, limit int64) {
	t.Helper()
	previous := maxPalModSettingsBytes
	maxPalModSettingsBytes = limit
	t.Cleanup(func() {
		maxPalModSettingsBytes = previous
	})
}

func doMultipart(t *testing.T, client *http.Client, url, filename string, data []byte) *http.Response {
	return doMultipartMaybeConfirmed(t, client, url, filename, data, false)
}

func doMultipartConfirmed(t *testing.T, client *http.Client, url, filename string, data []byte) *http.Response {
	return doMultipartMaybeConfirmed(t, client, url, filename, data, true)
}

func doMultipartMaybeConfirmed(t *testing.T, client *http.Client, url, filename string, data []byte, confirmed bool) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatalf("multipart copy error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close error = %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if confirmed {
		req.Header.Set(confirmationHeader, "true")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	return resp
}

type countingReadCloser struct {
	reads int
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	c.reads++
	return 0, io.ErrUnexpectedEOF
}

func (c *countingReadCloser) Close() error {
	return nil
}
