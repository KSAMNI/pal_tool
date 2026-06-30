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

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type App struct {
	db       *sql.DB
	dataDir  string
	sessions map[string]int64
	mu       sync.RWMutex
}

type appSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type settingsPayload struct {
	PalServerPath       string `json:"pal_server_path"`
	SteamCMDPath        string `json:"steamcmd_path"`
	RestAPIURL          string `json:"rest_api_url"`
	ServerLaunchArgs    string `json:"server_launch_args"`
	AutoBackupEnabled   bool   `json:"auto_backup_enabled"`
	AutoBackupRetention int    `json:"auto_backup_retention"`
}

type authPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type serverStatus struct {
	OS                string `json:"os"`
	Configured        bool   `json:"configured"`
	PalServerPath     string `json:"pal_server_path"`
	PalServerExists   bool   `json:"pal_server_exists"`
	PalServerBinary   string `json:"pal_server_binary"`
	SteamCMDPath      string `json:"steamcmd_path"`
	SteamCMDAvailable bool   `json:"steamcmd_available"`
	Running           bool   `json:"running"`
}

func New(dataDir string) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "app.db"))
	if err != nil {
		return nil, err
	}
	app := &App{
		db:       db,
		dataDir:  dataDir,
		sessions: make(map[string]int64),
	}
	if err := app.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return app, nil
}

func (a *App) Close() error {
	return a.db.Close()
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
	mux.Handle("/", http.FileServer(http.Dir("web/dist")))
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
	}
	for _, stmt := range stmts {
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	defaults := map[string]string{
		"pal_server_path":       "",
		"steamcmd_path":         "",
		"rest_api_url":          "http://127.0.0.1:8212/v1/api",
		"server_launch_args":    "-useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS",
		"auto_backup_enabled":   "true",
		"auto_backup_retention": "20",
	}
	for key, value := range defaults {
		if _, err := a.db.Exec(`INSERT OR IGNORE INTO app_settings(key, value) VALUES(?, ?)`, key, value); err != nil {
			return err
		}
	}
	return nil
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
	a.createSession(w, userID)
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
	a.createSession(w, userID)
	writeJSON(w, http.StatusOK, map[string]string{"username": payload.Username})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("palpanel_session")
	if err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "palpanel_session", Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode})
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
	values := map[string]string{
		"pal_server_path":       strings.TrimSpace(payload.PalServerPath),
		"steamcmd_path":         strings.TrimSpace(payload.SteamCMDPath),
		"rest_api_url":          strings.TrimSpace(payload.RestAPIURL),
		"server_launch_args":    strings.TrimSpace(payload.ServerLaunchArgs),
		"auto_backup_enabled":   strconv.FormatBool(payload.AutoBackupEnabled),
		"auto_backup_retention": strconv.Itoa(payload.AutoBackupRetention),
	}
	for key, value := range values {
		if _, err := a.db.Exec(`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	binary := palServerBinary(settings.PalServerPath)
	steamPath := resolveSteamCMD(settings.SteamCMDPath)
	status := serverStatus{
		OS:                runtime.GOOS,
		Configured:        settings.PalServerPath != "",
		PalServerPath:     settings.PalServerPath,
		PalServerExists:   fileExists(binary),
		PalServerBinary:   binary,
		SteamCMDPath:      steamPath,
		SteamCMDAvailable: steamPath != "",
		Running:           false,
	}
	writeJSON(w, http.StatusOK, status)
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
	autoBackup, _ := strconv.ParseBool(values["auto_backup_enabled"])
	return settingsPayload{
		PalServerPath:       values["pal_server_path"],
		SteamCMDPath:        values["steamcmd_path"],
		RestAPIURL:          values["rest_api_url"],
		ServerLaunchArgs:    values["server_launch_args"],
		AutoBackupEnabled:   autoBackup,
		AutoBackupRetention: retention,
	}, rows.Err()
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

func resolveSteamCMD(configured string) string {
	if configured != "" && fileExists(configured) {
		return configured
	}
	name := "steamcmd"
	if runtime.GOOS == "windows" {
		name = "steamcmd.exe"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
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
