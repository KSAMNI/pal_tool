package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	input := strings.Join([]string{
		"Authorization: Basic abc123",
		"proxy sent Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==",
		`http://admin:urlsecret@127.0.0.1:8212/v1/api`,
		`OptionSettings=(AdminPassword="adminsecret",ServerPassword=serversecret)`,
		`{"password":"usersecret","admin_password":"jsonadmin","server_password":"jsonserver","rest_api_password":"jsonrest"}`,
		`rest_api_password=formsecret&next=1`,
	}, "\n")

	got := redactSensitive(input)
	for _, secret := range []string{
		"abc123",
		"QWxhZGRpbjpvcGVuIHNlc2FtZQ==",
		"urlsecret",
		"adminsecret",
		"serversecret",
		"usersecret",
		"jsonadmin",
		"jsonserver",
		"jsonrest",
		"formsecret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted output still contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, redactedValue) {
		t.Fatalf("redacted output did not contain marker: %s", got)
	}
	if plain := redactSensitive("password must be at least 8 characters"); strings.Contains(plain, redactedValue) {
		t.Fatalf("plain validation message was over-redacted: %s", plain)
	}
}

func TestWriteErrorRedactsSensitiveData(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, http.StatusBadGateway, errors.New(`upstream failed: Authorization: Basic abc123 {"rest_api_password":"restsecret","AdminPassword":"adminsecret"}`))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	body := payload["error"]
	for _, secret := range []string{"abc123", "restsecret", "adminsecret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("error response still contains %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, redactedValue) {
		t.Fatalf("error response did not contain marker: %s", body)
	}
}

func TestTaskAndServerLogsRedactSensitiveData(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	taskID, err := panel.createTask("redaction_test")
	if err != nil {
		t.Fatalf("createTask() error = %v", err)
	}
	if err := panel.appendTaskLog(taskID, `Authorization: Basic abc123
OptionSettings=(AdminPassword="adminsecret",ServerPassword=serversecret)
{"rest_api_password":"restsecret"}`); err != nil {
		t.Fatalf("appendTaskLog() error = %v", err)
	}
	panel.appendServerLog(`PalServer stdout AdminPassword="serveradmin"
password=formsecret`)

	task, err := panel.getTask(taskID)
	if err != nil {
		t.Fatalf("getTask() error = %v", err)
	}
	var serverMessages []string
	for _, entry := range panel.recentServerLogs(0) {
		serverMessages = append(serverMessages, entry.Message)
	}
	combined := task.Log + "\n" + strings.Join(serverMessages, "\n")
	for _, secret := range []string{"abc123", "adminsecret", "serversecret", "restsecret", "serveradmin", "formsecret"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("logs still contain %q: %s", secret, combined)
		}
	}
	if !strings.Contains(combined, redactedValue) {
		t.Fatalf("logs did not contain marker: %s", combined)
	}
}

func TestPalAPIRouteErrorRedactsUpstreamBody(t *testing.T) {
	pal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "restsecret" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`Authorization: Basic c2hvdWxkbGVhaw==
{"rest_api_password":"restsecret","admin_password":"adminsecret","server_password":"serversecret"}`))
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
		"rest_api_password": "restsecret",
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

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/players", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("players status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body := string(data)
	for _, secret := range []string{"c2hvdWxkbGVhaw==", "restsecret", "adminsecret", "serversecret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("proxied error response still contains %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, redactedValue) {
		t.Fatalf("proxied error response did not contain marker: %s", body)
	}
}
