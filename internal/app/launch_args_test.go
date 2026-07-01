package app

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyLaunchArgSummary(t *testing.T) {
	raw := `-port=8211 -players 24 -publiclobby -NoMods -NumberOfWorkerThreadsServer=7 -useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS -custom "kept value"`
	settings := settingsPayload{ServerLaunchArgs: raw}

	applyLaunchArgSummary(&settings)

	if settings.ServerLaunchArgs != raw {
		t.Fatalf("ServerLaunchArgs changed: %q", settings.ServerLaunchArgs)
	}
	if settings.GamePort != 8211 {
		t.Fatalf("GamePort = %d, want 8211", settings.GamePort)
	}
	if settings.LaunchPlayers != 24 {
		t.Fatalf("LaunchPlayers = %d, want 24", settings.LaunchPlayers)
	}
	if !settings.PublicLobby {
		t.Fatalf("PublicLobby = false, want true")
	}
	if !settings.NoMods {
		t.Fatalf("NoMods = false, want true")
	}
	if settings.WorkerThreads != 7 {
		t.Fatalf("WorkerThreads = %d, want 7", settings.WorkerThreads)
	}
	if !settings.PerformanceFlags {
		t.Fatalf("PerformanceFlags = false, want true")
	}
}

func TestApplyLaunchArgSummaryRequiresCompletePerformanceFlagSet(t *testing.T) {
	settings := settingsPayload{ServerLaunchArgs: `-useperfthreads -NoAsyncLoadingThread`}

	applyLaunchArgSummary(&settings)

	if settings.PerformanceFlags {
		t.Fatalf("PerformanceFlags = true with incomplete performance flag set")
	}
}

func TestMergeStructuredLaunchSettingsPreservesCustomArgs(t *testing.T) {
	payload := settingsPayload{
		ServerLaunchArgs:  `-port=8211 -players 24 -publiclobby -NoMods -NumberOfWorkerThreadsServer=7 -useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS -custom "kept value" -workshopdir="C:\Pal Mods"`,
		GamePort:          9000,
		LaunchPlayers:     48,
		PublicLobby:       false,
		NoMods:            true,
		PerformanceFlags:  false,
		WorkerThreads:     12,
		AutoBackupEnabled: true,
	}

	merged, err := mergeStructuredLaunchSettings(payload)
	if err != nil {
		t.Fatalf("mergeStructuredLaunchSettings() error = %v", err)
	}
	args, err := splitCommandLine(merged)
	if err != nil {
		t.Fatalf("splitCommandLine(%q) error = %v", merged, err)
	}

	if got := launchArgInt(args, "-port"); got != 9000 {
		t.Fatalf("-port = %d, want 9000; merged=%q", got, merged)
	}
	if got := launchArgInt(args, "-players"); got != 48 {
		t.Fatalf("-players = %d, want 48; merged=%q", got, merged)
	}
	if got := launchArgInt(args, "-NumberOfWorkerThreadsServer"); got != 12 {
		t.Fatalf("-NumberOfWorkerThreadsServer = %d, want 12; merged=%q", got, merged)
	}
	if hasLaunchFlag(args, "-publiclobby") {
		t.Fatalf("-publiclobby was not removed: %q", merged)
	}
	if !hasLaunchFlag(args, "-NoMods") {
		t.Fatalf("-NoMods was not preserved: %q", merged)
	}
	if hasAllLaunchFlags(args, performanceLaunchFlags) {
		t.Fatalf("performance flags were not removed: %q", merged)
	}
	if got := launchArgValue(args, "-custom"); got != "kept value" {
		t.Fatalf("-custom = %q, want kept value; merged=%q", got, merged)
	}
	if got := launchArgValue(args, "-workshopdir"); got != `C:\Pal Mods` {
		t.Fatalf("-workshopdir = %q, want C:\\Pal Mods; merged=%q", got, merged)
	}
}

func TestSettingsRouteMergesStructuredLaunchFields(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "rest_api_password", "secret")

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

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings status = %d", resp.StatusCode)
	}
	var payload settingsPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	resp.Body.Close()

	payload.ServerLaunchArgs = `-port=8211 -players=16 -publiclobby -NoMods -useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS -custom "kept value"`
	payload.GamePort = 9001
	payload.LaunchPlayers = 32
	payload.PublicLobby = false
	payload.NoMods = false
	payload.PerformanceFlags = false
	payload.WorkerThreads = 8
	payload.RestAPIURL = "http://127.0.0.1:8212/v1/api"
	payload.RestAPIUsername = "admin"
	payload.RestAPIPassword = ""
	payload.AutoBackupRetention = 20
	payload.AutoBackupIntervalHours = 24

	resp = doJSON(t, client, http.MethodPut, server.URL+"/api/settings", payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d", resp.StatusCode)
	}
	var saved settingsPayload
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved settings: %v", err)
	}
	resp.Body.Close()

	if saved.RestAPIPassword != "" {
		t.Fatalf("saved response leaked password: %#v", saved)
	}
	if saved.GamePort != 9001 || saved.LaunchPlayers != 32 || saved.WorkerThreads != 8 || saved.PublicLobby || saved.NoMods || saved.PerformanceFlags {
		t.Fatalf("unexpected derived settings response: %#v", saved)
	}

	loaded, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.RestAPIPassword != "secret" {
		t.Fatalf("rest_api_password = %q, want preserved secret", loaded.RestAPIPassword)
	}
	args, err := splitCommandLine(loaded.ServerLaunchArgs)
	if err != nil {
		t.Fatalf("split stored server_launch_args %q: %v", loaded.ServerLaunchArgs, err)
	}
	for _, forbidden := range []string{"-publiclobby", "-NoMods", "-useperfthreads", "-NoAsyncLoadingThread", "-UseMultithreadForDS"} {
		if hasLaunchFlag(args, forbidden) {
			t.Fatalf("%s was not removed from stored args: %q", forbidden, loaded.ServerLaunchArgs)
		}
	}
	for _, want := range []string{"-port=9001", "-players=32", "-NumberOfWorkerThreadsServer=8"} {
		if !strings.Contains(loaded.ServerLaunchArgs, want) {
			t.Fatalf("stored server_launch_args missing %s: %q", want, loaded.ServerLaunchArgs)
		}
	}
	if got := launchArgValue(args, "-custom"); got != "kept value" {
		t.Fatalf("-custom = %q, want kept value; stored=%q", got, loaded.ServerLaunchArgs)
	}
}

func TestSettingsRouteRejectsRunningTaskWithoutMutatingSettings(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	oldPath := t.TempDir()
	newPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", oldPath)
	setTestAppSetting(t, panel, "rest_api_username", "old-admin")

	server, client := newAuthenticatedTestServer(t, panel)
	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/settings", settingsPayload{
		PalServerPath:           newPath,
		RestAPIUsername:         "new-admin",
		AutoBackupRetention:     20,
		AutoBackupIntervalHours: 24,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT /api/settings status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	loaded, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.PalServerPath != oldPath {
		t.Fatalf("pal_server_path = %q, want unchanged %q", loaded.PalServerPath, oldPath)
	}
	if loaded.RestAPIUsername != "old-admin" {
		t.Fatalf("rest_api_username = %q, want unchanged old-admin", loaded.RestAPIUsername)
	}
}

func TestSettingsRouteRollsBackAllChangesOnDatabaseError(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	oldPath := t.TempDir()
	newPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", oldPath)
	setTestAppSetting(t, panel, "steamcmd_path", "old-steamcmd")
	setTestAppSetting(t, panel, "rest_api_url", "http://127.0.0.1:8212/v1/api")
	setTestAppSetting(t, panel, "rest_api_username", "old-admin")
	setTestAppSetting(t, panel, "server_launch_args", "-port=8211")

	if _, err := panel.db.Exec(`
		CREATE TRIGGER fail_rest_api_username_update
		BEFORE UPDATE ON app_settings
		WHEN old.key = 'rest_api_username'
		BEGIN
			SELECT RAISE(ABORT, 'forced settings failure');
		END;
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/settings", settingsPayload{
		PalServerPath:           newPath,
		SteamCMDPath:            "new-steamcmd",
		RestAPIURL:              "http://127.0.0.1:9000/v1/api",
		RestAPIUsername:         "new-admin",
		ServerLaunchArgs:        "-port=9001",
		AutoBackupEnabled:       true,
		AutoBackupRetention:     7,
		AutoBackupIntervalHours: 6,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("PUT /api/settings status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	loaded, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.PalServerPath != oldPath {
		t.Fatalf("pal_server_path = %q, want unchanged %q", loaded.PalServerPath, oldPath)
	}
	if loaded.SteamCMDPath != "old-steamcmd" {
		t.Fatalf("steamcmd_path = %q, want unchanged old-steamcmd", loaded.SteamCMDPath)
	}
	if loaded.RestAPIURL != "http://127.0.0.1:8212/v1/api" {
		t.Fatalf("rest_api_url = %q, want unchanged", loaded.RestAPIURL)
	}
	if loaded.RestAPIUsername != "old-admin" {
		t.Fatalf("rest_api_username = %q, want unchanged old-admin", loaded.RestAPIUsername)
	}
	if loaded.ServerLaunchArgs != "-port=8211" {
		t.Fatalf("server_launch_args = %q, want unchanged -port=8211", loaded.ServerLaunchArgs)
	}
}

func TestSettingsRouteRejectsPalServerPathRetargetWhileExternalRunning(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	oldPath := t.TempDir()
	newPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", oldPath)
	setTestAppSetting(t, panel, "rest_api_username", "old-admin")
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return settings.PalServerPath == oldPath, nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/settings", settingsPayload{
		PalServerPath:           newPath,
		RestAPIUsername:         "new-admin",
		AutoBackupRetention:     20,
		AutoBackupIntervalHours: 24,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT /api/settings status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	loaded, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.PalServerPath != oldPath {
		t.Fatalf("pal_server_path = %q, want unchanged %q", loaded.PalServerPath, oldPath)
	}
	if loaded.RestAPIUsername != "old-admin" {
		t.Fatalf("rest_api_username = %q, want unchanged old-admin", loaded.RestAPIUsername)
	}
}

func TestSettingsRouteRejectsPalServerPathRetargetWhileManagedRunning(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	oldPath := t.TempDir()
	newPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", oldPath)
	setTestAppSetting(t, panel, "rest_api_username", "old-admin")
	panel.isServerRunningFunc = func() bool { return true }
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		t.Fatalf("external detector should not run for a managed running server")
		return false, nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/settings", settingsPayload{
		PalServerPath:           newPath,
		RestAPIUsername:         "new-admin",
		AutoBackupRetention:     20,
		AutoBackupIntervalHours: 24,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT /api/settings status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	loaded, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.PalServerPath != oldPath {
		t.Fatalf("pal_server_path = %q, want unchanged %q", loaded.PalServerPath, oldPath)
	}
	if loaded.RestAPIUsername != "old-admin" {
		t.Fatalf("rest_api_username = %q, want unchanged old-admin", loaded.RestAPIUsername)
	}
}
