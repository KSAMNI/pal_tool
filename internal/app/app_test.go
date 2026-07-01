package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewConfiguresSQLiteBusyHandling(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	if got := panel.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	var timeoutMS int
	if err := panel.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeoutMS); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if timeoutMS < 5000 {
		t.Fatalf("busy_timeout = %d, want at least 5000", timeoutMS)
	}
}

func TestNewSeedsDefaultPalServerPathFromEnvironment(t *testing.T) {
	serverPath := t.TempDir()
	t.Setenv("PALPANEL_DEFAULT_PAL_SERVER_PATH", "  "+serverPath+"  ")

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	settings, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if settings.PalServerPath != serverPath {
		t.Fatalf("pal_server_path = %q, want %q", settings.PalServerPath, serverPath)
	}
}

func TestNewDoesNotOverwritePersistedPalServerPathFromEnvironment(t *testing.T) {
	dataDir := t.TempDir()
	initialPath := t.TempDir()
	savedPath := t.TempDir()
	restartDefaultPath := t.TempDir()
	t.Setenv("PALPANEL_DEFAULT_PAL_SERVER_PATH", initialPath)

	panel, err := New(dataDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	setTestAppSetting(t, panel, "pal_server_path", savedPath)
	if err := panel.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	t.Setenv("PALPANEL_DEFAULT_PAL_SERVER_PATH", restartDefaultPath)
	panel, err = New(dataDir)
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	defer panel.Close()

	settings, err := panel.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if settings.PalServerPath != savedPath {
		t.Fatalf("pal_server_path = %q, want persisted %q", settings.PalServerPath, savedPath)
	}
}

func TestSessionCookieSecureRespectsRequestScheme(t *testing.T) {
	for _, tc := range []struct {
		name       string
		headers    map[string]string
		wantSecure bool
	}{
		{name: "local_http", wantSecure: false},
		{name: "forwarded_https", headers: map[string]string{"X-Forwarded-Proto": "https"}, wantSecure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			panel, err := New(t.TempDir())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer panel.Close()

			server := httptest.NewServer(panel.Routes())
			defer server.Close()

			client := &http.Client{}
			resp := doJSONWithHeaders(t, client, http.MethodPost, server.URL+"/api/auth/setup", map[string]string{
				"username": "admin",
				"password": "password123",
			}, tc.headers)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("setup status = %d, want %d", resp.StatusCode, http.StatusCreated)
			}
			assertSessionSetCookie(t, resp, tc.wantSecure)
		})
	}
}

func TestSessionCookiesAreSecureOverHTTPS(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	server := httptest.NewTLSServer(panel.Routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := server.Client()
	client.Jar = jar

	credentials := map[string]string{"username": "admin", "password": "password123"}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", credentials)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	assertSessionSetCookie(t, resp, true)
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", credentials)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	assertSessionSetCookie(t, resp, true)
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/auth/logout", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	assertSessionDeleteCookie(t, resp, true)
	resp.Body.Close()
}

func TestNoBodyActionRoutesRejectUnexpectedBodies(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	server, client := newAuthenticatedTestServer(t, panel)
	routes := []struct {
		name      string
		method    string
		path      string
		confirmed bool
	}{
		{name: "server_install", method: http.MethodPost, path: "/api/server/install"},
		{name: "server_update", method: http.MethodPost, path: "/api/server/update", confirmed: true},
		{name: "server_start", method: http.MethodPost, path: "/api/server/start"},
		{name: "server_stop", method: http.MethodPost, path: "/api/server/stop", confirmed: true},
		{name: "server_restart", method: http.MethodPost, path: "/api/server/restart", confirmed: true},
		{name: "server_save", method: http.MethodPost, path: "/api/server/save"},
		{name: "server_rest_stop", method: http.MethodPost, path: "/api/server/rest-stop", confirmed: true},
		{name: "player_unban", method: http.MethodPost, path: "/api/players/steam_1/unban", confirmed: true},
		{name: "config_init", method: http.MethodPost, path: "/api/config/init"},
		{name: "config_backup", method: http.MethodPost, path: "/api/config/backup"},
		{name: "backup_restore", method: http.MethodPost, path: "/api/backups/1/restore", confirmed: true},
		{name: "backup_delete", method: http.MethodDelete, path: "/api/backups/1", confirmed: true},
		{name: "mod_enable", method: http.MethodPost, path: "/api/mods/1/enable"},
		{name: "mod_disable", method: http.MethodPost, path: "/api/mods/1/disable"},
		{name: "mod_open_dir", method: http.MethodPost, path: "/api/mods/1/open-dir"},
		{name: "mod_delete", method: http.MethodDelete, path: "/api/mods/1", confirmed: true},
		{name: "logout", method: http.MethodPost, path: "/api/auth/logout"},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			req, err := http.NewRequest(route.method, server.URL+route.path, strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if route.confirmed {
				req.Header.Set(confirmationHeader, "true")
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("client.Do() error = %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s %s status = %d, want %d", route.method, route.path, resp.StatusCode, http.StatusBadRequest)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if !strings.Contains(string(body), errUnexpectedRequestBody.Error()) {
				t.Fatalf("response missing no-body error: %s", body)
			}
		})
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	oversized := `{"value":"` + strings.Repeat("x", int(maxJSONRequestBodyBytes)+1) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(oversized))
	recorder := httptest.NewRecorder()

	var payload map[string]string
	if decodeJSON(recorder, req, &payload) {
		t.Fatalf("decodeJSON() succeeded for oversized body")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), errJSONRequestBodyTooLarge.Error()) {
		t.Fatalf("response missing oversized error: %s", recorder.Body.String())
	}
}

func TestDecodeJSONRejectsTrailingJSONValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"a":"one"}{"b":"two"}`))
	recorder := httptest.NewRecorder()

	var payload map[string]string
	if decodeJSON(recorder, req, &payload) {
		t.Fatalf("decodeJSON() succeeded for body with multiple JSON values")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), errJSONTrailingData.Error()) {
		t.Fatalf("response missing trailing-data error: %s", recorder.Body.String())
	}
}

func TestDecodeJSONAllowsTrailingWhitespace(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("{\"a\":\"one\"}  \n\t"))
	recorder := httptest.NewRecorder()

	var payload map[string]string
	if !decodeJSON(recorder, req, &payload) {
		t.Fatalf("decodeJSON() rejected body with trailing whitespace: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if payload["a"] != "one" {
		t.Fatalf("payload[a] = %q, want %q", payload["a"], "one")
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("unexpected response body after successful decode: %s", recorder.Body.String())
	}
}

func TestDecodeOptionalJSONAllowsEmptyBody(t *testing.T) {
	for _, body := range []string{"", "  \n\t"} {
		req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(body))
		recorder := httptest.NewRecorder()

		payload := map[string]string{"existing": "value"}
		if !decodeOptionalJSON(recorder, req, &payload) {
			t.Fatalf("decodeOptionalJSON() rejected empty body %q: status=%d body=%s", body, recorder.Code, recorder.Body.String())
		}
		if payload["existing"] != "value" {
			t.Fatalf("payload mutated for empty body %q: %#v", body, payload)
		}
		if recorder.Body.Len() != 0 {
			t.Fatalf("unexpected response body after optional empty decode: %s", recorder.Body.String())
		}
	}
}

func TestDecodeOptionalJSONKeepsStrictParsingForNonEmptyBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "unknown_field", body: `{"extra":"value"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			var payload struct {
				Type string `json:"type"`
			}
			if decodeOptionalJSON(recorder, req, &payload) {
				t.Fatalf("decodeOptionalJSON() succeeded for %s body", tc.name)
			}
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestDecodeJSONRejectsDuplicateObjectField(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "top_level", body: `{"a":"one","a":"two"}`},
		{name: "nested", body: `{"outer":{"a":"one","a":"two"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			var payload map[string]any
			if decodeJSON(recorder, req, &payload) {
				t.Fatalf("decodeJSON() succeeded for duplicate object field")
			}
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), errJSONDuplicateField.Error()) {
				t.Fatalf("response missing duplicate-field error: %s", recorder.Body.String())
			}
		})
	}
}

func TestDecodeJSONAllowsSameFieldNameInSeparateObjects(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"left":{"a":"one"},"right":{"a":"two"}}`))
	recorder := httptest.NewRecorder()

	var payload map[string]map[string]string
	if !decodeJSON(recorder, req, &payload) {
		t.Fatalf("decodeJSON() rejected distinct sibling object fields: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if payload["left"]["a"] != "one" || payload["right"]["a"] != "two" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestDecodeJSONRejectsUnknownStructField(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"a":"one","extra":"two"}`))
	recorder := httptest.NewRecorder()

	var payload struct {
		A string `json:"a"`
	}
	if decodeJSON(recorder, req, &payload) {
		t.Fatalf("decodeJSON() succeeded for unknown struct field")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "unknown field") || !strings.Contains(recorder.Body.String(), "extra") {
		t.Fatalf("response missing unknown-field error: %s", recorder.Body.String())
	}
}

func TestSetupRouteCreatesOnlyOneAdminUnderConcurrency(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	const attempts = 4
	start := make(chan struct{})
	statuses := make(chan int, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			status, err := postSetup(server.URL, map[string]string{
				"username": "admin" + strconv.Itoa(index),
				"password": "password123",
			})
			if err != nil {
				errs <- err
				return
			}
			statuses <- status
		}(i)
	}
	close(start)
	wg.Wait()
	close(statuses)
	close(errs)

	for err := range errs {
		t.Fatalf("setup request error: %v", err)
	}

	created := 0
	conflicts := 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("setup status = %d, want %d or %d", status, http.StatusCreated, http.StatusConflict)
		}
	}
	if created != 1 || conflicts != attempts-1 {
		t.Fatalf("created/conflict counts = %d/%d, want 1/%d", created, conflicts, attempts-1)
	}

	var count int
	var username string
	if err := panel.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(username), '') FROM users`).Scan(&count, &username); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}

	client := &http.Client{}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", map[string]string{
		"username": username,
		"password": "password123",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	assertSessionSetCookie(t, resp, false)
}

func TestAppCloseWaitsForBackgroundWorkersBeforeClosingDatabase(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	workerErr := make(chan error, 1)
	panel.startBackground(func() {
		close(started)
		<-release
		workerErr <- panel.db.Ping()
	})
	<-started

	closed := make(chan error, 1)
	go func() {
		closed <- panel.Close()
	}()

	select {
	case err := <-closed:
		t.Fatalf("Close() returned before background worker finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after background worker finished")
	}
	if err := <-workerErr; err != nil {
		t.Fatalf("database was closed before background worker exited: %v", err)
	}
	if err := panel.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func postSetup(baseURL string, payload map[string]string) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/auth/setup", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func doJSONWithHeaders(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) *http.Response {
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
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	return resp
}

func assertSessionSetCookie(t *testing.T, resp *http.Response, wantSecure bool) {
	t.Helper()
	cookie, raw := sessionCookieFromResponse(t, resp)
	if cookie.Value == "" {
		t.Fatalf("session cookie value is empty in %q", raw)
	}
	assertSessionCookieCommon(t, cookie, raw, wantSecure)
}

func assertSessionDeleteCookie(t *testing.T, resp *http.Response, wantSecure bool) {
	t.Helper()
	cookie, raw := sessionCookieFromResponse(t, resp)
	if cookie.Value != "" {
		t.Fatalf("delete session cookie value = %q, want empty", cookie.Value)
	}
	if !strings.Contains(raw, "Max-Age=0") {
		t.Fatalf("delete session cookie missing Max-Age=0: %q", raw)
	}
	assertSessionCookieCommon(t, cookie, raw, wantSecure)
}

func assertSessionCookieCommon(t *testing.T, cookie *http.Cookie, raw string, wantSecure bool) {
	t.Helper()
	if cookie.Path != "/" {
		t.Fatalf("session cookie path = %q, want /", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Fatalf("session cookie missing HttpOnly: %q", raw)
	}
	if cookie.Secure != wantSecure {
		t.Fatalf("session cookie Secure = %v, want %v: %q", cookie.Secure, wantSecure, raw)
	}
	if !strings.Contains(raw, "SameSite=Lax") {
		t.Fatalf("session cookie missing SameSite=Lax: %q", raw)
	}
}

func sessionCookieFromResponse(t *testing.T, resp *http.Response) (*http.Cookie, string) {
	t.Helper()
	headers := resp.Header.Values("Set-Cookie")
	for _, cookie := range resp.Cookies() {
		if cookie.Name != sessionCookieName {
			continue
		}
		for _, raw := range headers {
			if strings.HasPrefix(raw, sessionCookieName+"=") {
				return cookie, raw
			}
		}
		return cookie, ""
	}
	t.Fatalf("response missing %s Set-Cookie header; headers=%v", sessionCookieName, headers)
	return nil, ""
}
