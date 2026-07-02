package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"palpanel-lite/internal/frontend"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type App struct {
	db                     *sql.DB
	dataDir                string
	sessions               map[string]int64
	mu                     sync.RWMutex
	setupMu                sync.Mutex
	taskMu                 sync.Mutex
	taskRunning            bool
	backupMu               sync.Mutex
	serverMu               sync.Mutex
	serverCmd              *exec.Cmd
	serverDone             chan error
	serverStarting         bool
	serverLogs             []serverLogEntry
	eventMu                sync.Mutex
	eventClients           map[chan runtimeEvent]struct{}
	stopCh                 chan struct{}
	backgroundWG           sync.WaitGroup
	closeOnce              sync.Once
	closeErr               error
	openDirectory          func(string) error
	commandRunner          func(*exec.Cmd) error
	isServerRunningFunc    func() bool
	stopServerProcessFunc  func(time.Duration) error
	startServerProcessFunc func(settingsPayload) error
	serverProcessDetector  func(settingsPayload) (bool, error)
}

type appSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type settingsPayload struct {
	PalServerPath           string `json:"pal_server_path"`
	SteamCMDPath            string `json:"steamcmd_path"`
	RestAPIURL              string `json:"rest_api_url"`
	RestAPIUsername         string `json:"rest_api_username"`
	RestAPIPassword         string `json:"rest_api_password,omitempty"`
	ServerLaunchArgs        string `json:"server_launch_args"`
	GamePort                int    `json:"game_port,omitempty"`
	LaunchPlayers           int    `json:"launch_players,omitempty"`
	PublicLobby             bool   `json:"public_lobby"`
	NoMods                  bool   `json:"no_mods"`
	PerformanceFlags        bool   `json:"performance_flags"`
	WorkerThreads           int    `json:"worker_threads,omitempty"`
	AutoBackupEnabled       bool   `json:"auto_backup_enabled"`
	AutoBackupRetention     int    `json:"auto_backup_retention"`
	AutoBackupIntervalHours int    `json:"auto_backup_interval_hours"`
}

type authPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type serverStatus struct {
	OS                string      `json:"os"`
	Configured        bool        `json:"configured"`
	PalServerPath     string      `json:"pal_server_path"`
	PalServerExists   bool        `json:"pal_server_exists"`
	PalServerBinary   string      `json:"pal_server_binary"`
	SteamCMDPath      string      `json:"steamcmd_path"`
	SteamCMDAvailable bool        `json:"steamcmd_available"`
	Running           bool        `json:"running"`
	ManagedRunning    bool        `json:"managed_running"`
	ExternalRunning   bool        `json:"external_running"`
	OperationRunning  bool        `json:"operation_running"`
	RuntimeWarning    string      `json:"runtime_warning,omitempty"`
	PortChecks        []portCheck `json:"port_checks"`
}

type portCheck struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port,omitempty"`
	Source   string `json:"source"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

type serverLogEntry struct {
	Time    string `json:"time"`
	Message string `json:"message"`
}

type taskRecord struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Log        string `json:"log"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

func New(dataDir string) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "app.db"))
	if err != nil {
		return nil, err
	}
	if err := configureSQLite(db); err != nil {
		db.Close()
		return nil, err
	}
	app := &App{
		db:            db,
		dataDir:       dataDir,
		sessions:      make(map[string]int64),
		eventClients:  make(map[chan runtimeEvent]struct{}),
		stopCh:        make(chan struct{}),
		openDirectory: defaultOpenDirectory,
	}
	app.commandRunner = func(cmd *exec.Cmd) error { return cmd.Run() }
	app.isServerRunningFunc = app.isServerRunning
	app.stopServerProcessFunc = app.stopServerProcess
	app.startServerProcessFunc = app.startServerProcess
	app.serverProcessDetector = detectConfiguredServerProcess
	if err := app.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := app.failStaleRunningTasks(); err != nil {
		db.Close()
		return nil, err
	}
	if err := app.pruneFinishedTasks(); err != nil {
		db.Close()
		return nil, err
	}
	app.startAutoBackupScheduler()
	app.startScheduledActionRunner()
	return app, nil
}

func configureSQLite(db *sql.DB) error {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err := db.Exec(`PRAGMA busy_timeout = 5000`)
	return err
}

func (a *App) Close() error {
	a.closeOnce.Do(func() {
		close(a.stopCh)
		a.backgroundWG.Wait()
		if err := a.stopManagedServerOnClose(30 * time.Second); err != nil {
			a.closeErr = err
		}
		if err := a.db.Close(); err != nil && a.closeErr == nil {
			a.closeErr = err
		}
	})
	return a.closeErr
}

func (a *App) stopManagedServerOnClose(timeout time.Duration) error {
	a.serverMu.Lock()
	hasManagedServer := a.serverCmd != nil && a.serverDone != nil
	a.serverMu.Unlock()
	if !hasManagedServer {
		return nil
	}
	if err := a.stopManagedServerProcess(timeout); err != nil && !errors.Is(err, errServerNotRunning) {
		return err
	}
	return nil
}

func (a *App) startBackground(run func()) {
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		run()
	}()
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/auth/state", a.handleAuthState)
	mux.HandleFunc("POST /api/auth/setup", a.handleSetup)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a.withAuth(a.handleLogout))
	mux.HandleFunc("GET /api/auth/me", a.withAuth(a.handleMe))
	mux.HandleFunc("GET /api/settings", a.withAuth(a.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", a.withAuth(a.handlePutSettings))
	mux.HandleFunc("GET /api/server/status", a.withAuth(a.handleServerStatus))
	mux.HandleFunc("POST /api/server/install", a.withAuth(a.handleServerInstall))
	mux.HandleFunc("POST /api/server/update", a.withAuth(a.handleServerUpdate))
	mux.HandleFunc("POST /api/server/start", a.withAuth(a.handleServerStart))
	mux.HandleFunc("POST /api/server/stop", a.withAuth(a.handleServerStop))
	mux.HandleFunc("POST /api/server/restart", a.withAuth(a.handleServerRestart))
	mux.HandleFunc("POST /api/server/announce", a.withAuth(a.handleServerAnnounce))
	mux.HandleFunc("POST /api/server/save", a.withAuth(a.handleServerSave))
	mux.HandleFunc("GET /api/server/rest-settings", a.withAuth(a.handleServerRestSettings))
	mux.HandleFunc("POST /api/server/shutdown", a.withAuth(a.handleServerShutdown))
	mux.HandleFunc("POST /api/server/rest-stop", a.withAuth(a.handleServerRestStop))
	mux.HandleFunc("GET /api/server/logs", a.withAuth(a.handleServerLogs))
	mux.HandleFunc("GET /api/tasks", a.withAuth(a.handleTasks))
	mux.HandleFunc("DELETE /api/tasks", a.withAuth(a.handleClearTasks))
	mux.HandleFunc("GET /api/schedules", a.withAuth(a.handleGetSchedules))
	mux.HandleFunc("PUT /api/schedules", a.withAuth(a.handlePutSchedules))
	mux.HandleFunc("GET /api/events", a.withAuth(a.handleRuntimeEvents))
	mux.HandleFunc("GET /api/dashboard", a.withAuth(a.handleDashboard))
	mux.HandleFunc("GET /api/players", a.withAuth(a.handlePlayers))
	mux.HandleFunc("POST /api/players/{userid}/kick", a.withAuth(a.handlePlayerKick))
	mux.HandleFunc("POST /api/players/{userid}/ban", a.withAuth(a.handlePlayerBan))
	mux.HandleFunc("POST /api/players/{userid}/unban", a.withAuth(a.handlePlayerUnban))
	mux.HandleFunc("GET /api/config", a.withAuth(a.handleGetConfig))
	mux.HandleFunc("POST /api/config/init", a.withAuth(a.handleInitConfig))
	mux.HandleFunc("PUT /api/config", a.withAuth(a.handlePutConfig))
	mux.HandleFunc("POST /api/config/backup", a.withAuth(a.handleConfigBackup))
	mux.HandleFunc("GET /api/backups", a.withAuth(a.handleListBackups))
	mux.HandleFunc("POST /api/backups", a.withAuth(a.handleCreateBackup))
	mux.HandleFunc("POST /api/backups/{id}/restore", a.withAuth(a.handleRestoreBackup))
	mux.HandleFunc("DELETE /api/backups/{id}", a.withAuth(a.handleDeleteBackup))
	mux.HandleFunc("GET /api/mods", a.withAuth(a.handleListMods))
	mux.HandleFunc("POST /api/mods/upload", a.withAuth(a.handleUploadMod))
	mux.HandleFunc("POST /api/mods/workshop/download", a.withAuth(a.handleDownloadWorkshopMod))
	mux.HandleFunc("POST /api/mods/{id}/enable", a.withAuth(a.handleEnableMod))
	mux.HandleFunc("POST /api/mods/{id}/disable", a.withAuth(a.handleDisableMod))
	mux.HandleFunc("POST /api/mods/{id}/update", a.withAuth(a.handleUpdateMod))
	mux.HandleFunc("POST /api/mods/{id}/open-dir", a.withAuth(a.handleOpenModDirectory))
	mux.HandleFunc("DELETE /api/mods/{id}", a.withAuth(a.handleDeleteMod))
	mux.HandleFunc("GET /api/mods/{id}/info", a.withAuth(a.handleModInfo))
	mux.Handle("/", frontendHandler(frontend.Dist(), frontend.FallbackIndex()))
	return securityHeaders(mux)
}

func (a *App) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mods (
			id INTEGER PRIMARY KEY,
			name TEXT,
			package_name TEXT NOT NULL UNIQUE,
			version TEXT,
			author TEXT,
			folder_name TEXT NOT NULL,
			enabled BOOLEAN NOT NULL,
			server_supported BOOLEAN NOT NULL,
			info_json TEXT NOT NULL,
			installed_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS backups (
			id INTEGER PRIMARY KEY,
			filename TEXT NOT NULL,
			path TEXT NOT NULL,
			size INTEGER NOT NULL,
			type TEXT NOT NULL,
			note TEXT,
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			log TEXT,
			created_at DATETIME NOT NULL,
			finished_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS scheduled_actions (
			id INTEGER PRIMARY KEY,
			time TEXT NOT NULL,
			action TEXT NOT NULL,
			enabled BOOLEAN NOT NULL,
			created_at DATETIME NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	defaults := map[string]string{
		"pal_server_path":            defaultPalServerPathSetting(),
		"steamcmd_path":              "",
		"rest_api_url":               "http://127.0.0.1:8212/v1/api",
		"rest_api_username":          "admin",
		"rest_api_password":          "",
		"server_launch_args":         "-useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS",
		"auto_backup_enabled":        "true",
		"auto_backup_retention":      "20",
		"auto_backup_interval_hours": "24",
	}
	for key, value := range defaults {
		if _, err := a.db.Exec(`INSERT OR IGNORE INTO app_settings(key, value) VALUES(?, ?)`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func defaultPalServerPathSetting() string {
	return strings.TrimSpace(os.Getenv("PALPANEL_DEFAULT_PAL_SERVER_PATH"))
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleAuthState(w http.ResponseWriter, r *http.Request) {
	hasUser, err := a.hasUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": !hasUser})
}

func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	hasUser, err := a.hasUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if hasUser {
		writeError(w, http.StatusConflict, errors.New("admin user already exists"))
		return
	}
	var payload authPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := validateAuthPayload(payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	hasUser, err = a.hasUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if hasUser {
		writeError(w, http.StatusConflict, errors.New("admin user already exists"))
		return
	}
	result, err := a.db.Exec(
		`INSERT INTO users(username, password_hash, created_at) VALUES(?, ?, ?)`,
		payload.Username,
		string(hash),
		time.Now().UTC(),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	userID, _ := result.LastInsertId()
	a.createSession(w, r, userID)
	writeJSON(w, http.StatusCreated, map[string]string{"username": payload.Username})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var payload authPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	var userID int64
	var hash string
	err := a.db.QueryRow(`SELECT id, password_hash FROM users WHERE username = ?`, payload.Username).Scan(&userID, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(payload.Password)) != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid username or password"))
		return
	}
	a.createSession(w, r, userID)
	writeJSON(w, http.StatusOK, map[string]string{"username": payload.Username})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	setSessionCookie(w, r, &http.Cookie{Value: "", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey{}).(int64)
	var username string
	if err := a.db.QueryRow(`SELECT username FROM users WHERE id = ?`, userID).Scan(&username); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": username})
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	settings.RestAPIPassword = ""
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var payload settingsPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.AutoBackupRetention <= 0 {
		payload.AutoBackupRetention = 20
	}
	if payload.AutoBackupIntervalHours <= 0 {
		payload.AutoBackupIntervalHours = 24
	}
	launchArgs, err := mergeStructuredLaunchSettings(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	previous, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()
	if a.settingsRetargetsRunningServer(previous, payload.PalServerPath) {
		writeError(w, actionErrorStatus(errSettingsRetargetRunning), errSettingsRetargetRunning)
		return
	}
	restPassword := strings.TrimSpace(payload.RestAPIPassword)
	if restPassword == "" {
		restPassword = previous.RestAPIPassword
	}
	values := []appSetting{
		{Key: "pal_server_path", Value: strings.TrimSpace(payload.PalServerPath)},
		{Key: "steamcmd_path", Value: strings.TrimSpace(payload.SteamCMDPath)},
		{Key: "rest_api_url", Value: strings.TrimSpace(payload.RestAPIURL)},
		{Key: "rest_api_username", Value: strings.TrimSpace(payload.RestAPIUsername)},
		{Key: "rest_api_password", Value: restPassword},
		{Key: "server_launch_args", Value: launchArgs},
		{Key: "auto_backup_enabled", Value: strconv.FormatBool(payload.AutoBackupEnabled)},
		{Key: "auto_backup_retention", Value: strconv.Itoa(payload.AutoBackupRetention)},
		{Key: "auto_backup_interval_hours", Value: strconv.Itoa(payload.AutoBackupIntervalHours)},
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, item := range values {
		if _, err := tx.Exec(`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, item.Key, item.Value); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	committed = true
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	settings.RestAPIPassword = ""
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, a.currentServerStatus(settings))
}

func (a *App) currentServerStatus(settings settingsPayload) serverStatus {
	palServerPath := strings.TrimSpace(settings.PalServerPath)
	configured := palServerPath != ""
	if palServerPath == "" {
		palServerPath = detectPalServerPath()
	}
	binary := palServerBinary(palServerPath)
	steamPath := resolveSteamCMD(settings.SteamCMDPath)
	managedRunning := a.isServerRunning()
	runtimeSettings := settings
	runtimeSettings.PalServerPath = palServerPath
	externalRunning, runtimeWarning := a.externalServerRunning(runtimeSettings)
	return serverStatus{
		OS:                runtime.GOOS,
		Configured:        configured,
		PalServerPath:     palServerPath,
		PalServerExists:   fileExists(binary),
		PalServerBinary:   binary,
		SteamCMDPath:      steamPath,
		SteamCMDAvailable: steamPath != "",
		Running:           managedRunning || externalRunning,
		ManagedRunning:    managedRunning,
		ExternalRunning:   externalRunning,
		OperationRunning:  a.operationRunning(),
		RuntimeWarning:    runtimeWarning,
		PortChecks:        a.currentPortChecks(runtimeSettings),
	}
}

func (a *App) loadSettings() (settingsPayload, error) {
	rows, err := a.db.Query(`SELECT key, value FROM app_settings`)
	if err != nil {
		return settingsPayload{}, err
	}
	defer rows.Close()
	values := make(map[string]string)
	for rows.Next() {
		var item appSetting
		if err := rows.Scan(&item.Key, &item.Value); err != nil {
			return settingsPayload{}, err
		}
		values[item.Key] = item.Value
	}
	retention, _ := strconv.Atoi(values["auto_backup_retention"])
	if retention <= 0 {
		retention = 20
	}
	intervalHours, _ := strconv.Atoi(values["auto_backup_interval_hours"])
	if intervalHours <= 0 {
		intervalHours = 24
	}
	autoBackup, _ := strconv.ParseBool(values["auto_backup_enabled"])
	settings := settingsPayload{
		PalServerPath:           values["pal_server_path"],
		SteamCMDPath:            values["steamcmd_path"],
		RestAPIURL:              values["rest_api_url"],
		RestAPIUsername:         values["rest_api_username"],
		RestAPIPassword:         values["rest_api_password"],
		ServerLaunchArgs:        values["server_launch_args"],
		AutoBackupEnabled:       autoBackup,
		AutoBackupRetention:     retention,
		AutoBackupIntervalHours: intervalHours,
	}
	applyLaunchArgSummary(&settings)
	return settings, rows.Err()
}

func (a *App) hasUsers() (bool, error) {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func validateAuthPayload(payload authPayload) error {
	if strings.TrimSpace(payload.Username) == "" {
		return errors.New("username is required")
	}
	if len(payload.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

func palServerBinary(serverPath string) string {
	if serverPath == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(serverPath, "PalServer.exe")
	}
	return filepath.Join(serverPath, "PalServer.sh")
}

func detectPalServerPath() string {
	for _, key := range []string{"PALPANEL_PAL_SERVER_PATH", "PALWORLD_SERVER_PATH", "PAL_SERVER_PATH"} {
		if detected := validPalServerPath(os.Getenv(key)); detected != "" {
			return detected
		}
	}

	candidates := make([]string, 0, 12)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd, filepath.Join(cwd, "PalServer"))
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, dir, filepath.Join(dir, "PalServer"))
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`C:\PalServer`,
			`C:\Program Files (x86)\Steam\steamapps\common\PalServer`,
			`C:\Program Files\Steam\steamapps\common\PalServer`,
		)
	} else {
		candidates = append(candidates,
			"/opt/palserver",
			"/opt/PalServer",
			"/home/steam/palserver",
			"/home/steam/Steam/steamapps/common/PalServer",
		)
	}

	for _, candidate := range candidates {
		if detected := validPalServerPath(candidate); detected != "" {
			return detected
		}
	}
	return ""
}

func validPalServerPath(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		candidate = filepath.Dir(candidate)
	}
	if fileExists(palServerBinary(candidate)) {
		return candidate
	}
	return ""
}

func resolveSteamCMD(configured string) string {
	if configured != "" {
		if fileExists(configured) {
			return configured
		}
		if info, err := os.Stat(configured); err == nil && info.IsDir() {
			candidate := filepath.Join(configured, steamCMDName())
			if fileExists(candidate) {
				return candidate
			}
		}
	}
	path, err := exec.LookPath(steamCMDName())
	if err != nil {
		return ""
	}
	return path
}

func steamCMDName() string {
	if runtime.GOOS == "windows" {
		return "steamcmd.exe"
	}
	return "steamcmd"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
