package app

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPalAPIRoutesProxyWithBasicAuth(t *testing.T) {
	var sawKick bool
	var sawBan bool
	var sawUnban bool
	var sawAnnounce bool
	var sawSave bool
	var sawSettings bool
	var sawShutdown bool
	var sawStop bool
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/api/info":
			writeJSON(w, http.StatusOK, map[string]any{
				"version":     "0.7.2",
				"servername":  "Test Server",
				"description": "For tests",
			})
		case "/v1/api/metrics":
			writeJSON(w, http.StatusOK, map[string]any{
				"serverfps":        60,
				"currentplayernum": 1,
				"maxplayernum":     32,
				"uptime":           99,
				"days":             12,
			})
		case "/v1/api/players":
			writeJSON(w, http.StatusOK, map[string]any{
				"players": []map[string]any{{
					"name":   "Alice",
					"userid": "steam_1",
					"ping":   25,
				}},
			})
		case "/v1/api/settings":
			sawSettings = true
			writeJSON(w, http.StatusOK, map[string]any{
				"ServerName":         "Runtime Server",
				"RESTAPIEnabled":     true,
				"ServerPlayerMaxNum": 32,
				"LogFormatType":      "Text",
			})
		case "/v1/api/kick":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["userid"] != "steam_1" || body["message"] != "bye" {
				t.Fatalf("unexpected kick body: %#v", body)
			}
			sawKick = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/ban":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["userid"] != "steam_1" || body["message"] != "blocked" {
				t.Fatalf("unexpected ban body: %#v", body)
			}
			sawBan = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/unban":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["userid"] != "steam_1" {
				t.Fatalf("unexpected unban body: %#v", body)
			}
			sawUnban = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/announce":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["message"] != "hello" {
				t.Fatalf("unexpected announce body: %#v", body)
			}
			sawAnnounce = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/save":
			sawSave = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/shutdown":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["waittime"] != float64(60) || body["message"] != "restart soon" {
				t.Fatalf("unexpected shutdown body: %#v", body)
			}
			sawShutdown = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/stop":
			sawStop = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer pal.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	for key, value := range map[string]string{
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

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/dashboard", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d", resp.StatusCode)
	}
	var dashboard palDashboardPayload
	if err := json.NewDecoder(resp.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	resp.Body.Close()
	if !dashboard.Available || dashboard.Info["servername"] != "Test Server" || len(dashboard.Players) != 1 {
		t.Fatalf("unexpected dashboard: %#v", dashboard)
	}
	if dashboard.Settings["ServerName"] != "Runtime Server" || dashboard.Settings["ServerPlayerMaxNum"] != float64(32) {
		t.Fatalf("dashboard did not include runtime settings: %#v", dashboard.Settings)
	}
	if dashboard.System.CPU.Cores <= 0 || dashboard.System.Disk.Path == "" {
		t.Fatalf("dashboard did not include system summary: %#v", dashboard.System)
	}
	if dashboard.RecentLogs == nil || dashboard.RecentTasks == nil || dashboard.RecentBackups == nil {
		t.Fatalf("dashboard did not include recent operations: %#v", dashboard)
	}

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/players", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("players status = %d", resp.StatusCode)
	}
	var players []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&players); err != nil {
		t.Fatalf("decode players: %v", err)
	}
	resp.Body.Close()
	if len(players) != 1 || players[0]["userid"] != "steam_1" {
		t.Fatalf("unexpected players: %#v", players)
	}

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/server/rest-settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rest-settings status = %d", resp.StatusCode)
	}
	var runtimeSettings map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&runtimeSettings); err != nil {
		t.Fatalf("decode rest settings: %v", err)
	}
	resp.Body.Close()
	if runtimeSettings["ServerName"] != "Runtime Server" || runtimeSettings["RESTAPIEnabled"] != true {
		t.Fatalf("unexpected runtime settings: %#v", runtimeSettings)
	}

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/players/steam_1/kick", playerActionPayload{Message: "bye"})
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed kick status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/players/steam_1/kick", playerActionPayload{Message: "bye"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("kick status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/players/steam_1/ban", playerActionPayload{Message: "blocked"})
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed ban status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/players/steam_1/ban", playerActionPayload{Message: "blocked"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ban status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/players/steam_1/unban", nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed unban status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/players/steam_1/unban", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unban status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/server/announce", announcePayload{Message: "hello"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("announce status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/server/save", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/server/shutdown", shutdownPayload{WaitTime: 60, Message: "restart soon"})
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed shutdown status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/shutdown", shutdownPayload{WaitTime: 60, Message: "restart soon"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shutdown status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/server/rest-stop", nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed rest-stop status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/rest-stop", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rest-stop status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	if !sawKick || !sawBan || !sawUnban || !sawAnnounce || !sawSave || !sawSettings || !sawShutdown || !sawStop {
		t.Fatalf(
			"missing proxied actions: kick=%v ban=%v unban=%v announce=%v save=%v settings=%v shutdown=%v stop=%v",
			sawKick,
			sawBan,
			sawUnban,
			sawAnnounce,
			sawSave,
			sawSettings,
			sawShutdown,
			sawStop,
		)
	}
}

func TestPalAPIOptionalJSONRoutesAcceptEmptyBodies(t *testing.T) {
	var sawKick bool
	var sawShutdown bool
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/api/kick":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode kick body: %v", err)
			}
			if body["userid"] != "steam_1" {
				t.Fatalf("unexpected kick body: %#v", body)
			}
			if _, ok := body["message"]; ok {
				t.Fatalf("kick body unexpectedly included message: %#v", body)
			}
			sawKick = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "/v1/api/shutdown":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode shutdown body: %v", err)
			}
			if body["waittime"] != float64(30) {
				t.Fatalf("unexpected shutdown body: %#v", body)
			}
			if _, ok := body["message"]; ok {
				t.Fatalf("shutdown body unexpectedly included message: %#v", body)
			}
			sawShutdown = true
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer pal.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "rest_api_url", pal.URL+"/v1/api")
	setTestAppSetting(t, panel, "rest_api_username", "admin")
	setTestAppSetting(t, panel, "rest_api_password", "secret")

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/players/steam_1/kick", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty-body kick status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSONConfirmed(t, client, http.MethodPost, server.URL+"/api/server/shutdown", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty-body shutdown status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	if !sawKick || !sawShutdown {
		t.Fatalf("missing proxied optional actions: kick=%v shutdown=%v", sawKick, sawShutdown)
	}
}

func TestPalAPIWriteRoutesValidatePayloadBounds(t *testing.T) {
	var upstreamCalls int
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer pal.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "rest_api_url", pal.URL+"/v1/api")

	server, client := newAuthenticatedTestServer(t, panel)
	tests := []struct {
		name string
		path string
		body any
	}{
		{
			name: "announce message too long",
			path: "/api/server/announce",
			body: announcePayload{Message: strings.Repeat("x", maxPalRESTMessageRunes+1)},
		},
		{
			name: "kick userid too long",
			path: "/api/players/" + strings.Repeat("u", maxPalRESTUserIDRunes+1) + "/kick",
			body: nil,
		},
		{
			name: "ban message too long",
			path: "/api/players/steam_1/ban",
			body: playerActionPayload{Message: strings.Repeat("x", maxPalRESTMessageRunes+1)},
		},
		{
			name: "shutdown wait too large",
			path: "/api/server/shutdown",
			body: shutdownPayload{WaitTime: maxPalRESTShutdownWaitSeconds + 1},
		},
		{
			name: "shutdown wait negative",
			path: "/api/server/shutdown",
			body: shutdownPayload{WaitTime: -1},
		},
		{
			name: "shutdown message too long",
			path: "/api/server/shutdown",
			body: shutdownPayload{WaitTime: 30, Message: strings.Repeat("x", maxPalRESTMessageRunes+1)},
		},
		{
			name: "unban userid too long",
			path: "/api/players/" + strings.Repeat("u", maxPalRESTUserIDRunes+1) + "/unban",
			body: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSONConfirmed(t, client, http.MethodPost, server.URL+tc.path, tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s status = %d, want 400", tc.name, resp.StatusCode)
			}
			resp.Body.Close()
		})
	}
	if upstreamCalls != 0 {
		t.Fatalf("invalid payloads reached Palworld REST upstream %d times", upstreamCalls)
	}
}

func TestPalAPIRouteRejectsOversizedSuccessResponse(t *testing.T) {
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/api/settings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"padding":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxPalAPIResponseBytes)+1)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer pal.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "rest_api_url", pal.URL+"/v1/api")

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/server/rest-settings", nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("rest-settings status = %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	resp.Body.Close()
	if !strings.Contains(payload["error"], "response body exceeds") {
		t.Fatalf("error payload = %#v, want response size error", payload)
	}
}

func TestPalAPIRouteRejectsOversizedErrorResponse(t *testing.T) {
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/api/settings" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxPalAPIResponseBytes)+1)))
	}))
	defer pal.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "rest_api_url", pal.URL+"/v1/api")

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/server/rest-settings", nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("rest-settings status = %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	resp.Body.Close()
	if !strings.Contains(payload["error"], "returned HTTP 500") || !strings.Contains(payload["error"], "response body exceeds") {
		t.Fatalf("error payload = %#v, want upstream status and response size error", payload)
	}
}

func TestDashboardFetchesPalRESTEndpointsConcurrently(t *testing.T) {
	var mu sync.Mutex
	activeRequests := 0
	maxActiveRequests := 0
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}

		mu.Lock()
		activeRequests++
		if activeRequests > maxActiveRequests {
			maxActiveRequests = activeRequests
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			activeRequests--
			mu.Unlock()
		}()

		time.Sleep(120 * time.Millisecond)
		switch r.URL.Path {
		case "/v1/api/info":
			writeJSON(w, http.StatusOK, map[string]any{"servername": "Concurrent Server"})
		case "/v1/api/metrics":
			writeJSON(w, http.StatusOK, map[string]any{"serverfps": 60})
		case "/v1/api/settings":
			writeJSON(w, http.StatusOK, map[string]any{"RESTAPIEnabled": true})
		case "/v1/api/players":
			writeJSON(w, http.StatusOK, map[string]any{"players": []map[string]any{{"userid": "steam_1"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer pal.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	for key, value := range map[string]string{
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

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/dashboard", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d", resp.StatusCode)
	}
	var dashboard palDashboardPayload
	if err := json.NewDecoder(resp.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if !dashboard.Available || dashboard.Info["servername"] != "Concurrent Server" || len(dashboard.Players) != 1 {
		t.Fatalf("unexpected dashboard: %#v", dashboard)
	}

	mu.Lock()
	observedMax := maxActiveRequests
	mu.Unlock()
	if observedMax < 2 {
		t.Fatalf("dashboard REST calls were not concurrent; max active requests = %d", observedMax)
	}
}

func TestSettingsRedactsAndPreservesRestPassword(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"rest_api_password",
		"secret",
	); err != nil {
		t.Fatalf("set rest_api_password: %v", err)
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

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings status = %d", resp.StatusCode)
	}
	var settings settingsPayload
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	resp.Body.Close()
	if settings.RestAPIPassword != "" {
		t.Fatalf("password was not redacted: %#v", settings)
	}
	settings.RestAPIURL = "http://127.0.0.1:8212/v1/api"
	settings.RestAPIUsername = "admin"
	settings.AutoBackupIntervalHours = 12
	resp = doJSON(t, client, http.MethodPut, server.URL+"/api/settings", settings)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put settings status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	loaded, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.RestAPIPassword != "secret" {
		t.Fatalf("password was not preserved: %q", loaded.RestAPIPassword)
	}
	if loaded.AutoBackupIntervalHours != 12 {
		t.Fatalf("AutoBackupIntervalHours = %d", loaded.AutoBackupIntervalHours)
	}
}
