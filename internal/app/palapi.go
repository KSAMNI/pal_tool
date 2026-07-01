package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type palAPIClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

const maxPalAPIResponseBytes int64 = 2 << 20

const (
	maxPalRESTMessageRunes         = 500
	maxPalRESTUserIDRunes          = 128
	maxPalRESTShutdownWaitSeconds  = 3600
	defaultPalRESTShutdownWaitTime = 30
)

type palDashboardPayload struct {
	Available     bool             `json:"available"`
	Error         string           `json:"error,omitempty"`
	Info          map[string]any   `json:"info"`
	Metrics       map[string]any   `json:"metrics"`
	Settings      map[string]any   `json:"settings"`
	Players       []map[string]any `json:"players"`
	System        systemSummary    `json:"system"`
	RecentLogs    []serverLogEntry `json:"recent_logs"`
	RecentTasks   []taskRecord     `json:"recent_tasks"`
	RecentBackups []backupRecord   `json:"recent_backups"`
}

type playerActionPayload struct {
	Message string `json:"message"`
}

type announcePayload struct {
	Message string `json:"message"`
}

type shutdownPayload struct {
	WaitTime int    `json:"waittime"`
	Message  string `json:"message"`
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	extras, err := a.dashboardExtras()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		payload := palDashboardPayload{Available: false, Error: redactSensitive(err.Error())}
		payload.applyExtras(extras)
		writeJSON(w, http.StatusOK, payload)
		return
	}
	restData := client.dashboardData()
	if restData.infoErr != nil || restData.metricsErr != nil || restData.settingsErr != nil || restData.playersErr != nil {
		payload := palDashboardPayload{
			Available: false,
			Error:     joinErrors(restData.infoErr, restData.metricsErr, restData.settingsErr, restData.playersErr),
			Info:      nonNilMap(restData.info),
			Metrics:   nonNilMap(restData.metrics),
			Settings:  nonNilMap(restData.runtimeSettings),
			Players:   restData.players,
		}
		payload.applyExtras(extras)
		writeJSON(w, http.StatusOK, payload)
		return
	}
	payload := palDashboardPayload{
		Available: true,
		Info:      restData.info,
		Metrics:   restData.metrics,
		Settings:  restData.runtimeSettings,
		Players:   restData.players,
	}
	payload.applyExtras(extras)
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handlePlayers(w http.ResponseWriter, r *http.Request) {
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	players, err := client.getPlayers()
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, players)
}

type dashboardRESTData struct {
	info            map[string]any
	metrics         map[string]any
	runtimeSettings map[string]any
	players         []map[string]any
	infoErr         error
	metricsErr      error
	settingsErr     error
	playersErr      error
}

type dashboardMapResult struct {
	value map[string]any
	err   error
}

type dashboardPlayersResult struct {
	value []map[string]any
	err   error
}

func (c palAPIClient) dashboardData() dashboardRESTData {
	infoCh := make(chan dashboardMapResult, 1)
	metricsCh := make(chan dashboardMapResult, 1)
	settingsCh := make(chan dashboardMapResult, 1)
	playersCh := make(chan dashboardPlayersResult, 1)

	go func() {
		value, err := c.getMap("/info")
		infoCh <- dashboardMapResult{value: value, err: err}
	}()
	go func() {
		value, err := c.getMap("/metrics")
		metricsCh <- dashboardMapResult{value: value, err: err}
	}()
	go func() {
		value, err := c.getMap("/settings")
		settingsCh <- dashboardMapResult{value: value, err: err}
	}()
	go func() {
		value, err := c.getPlayers()
		playersCh <- dashboardPlayersResult{value: value, err: err}
	}()

	info := <-infoCh
	metrics := <-metricsCh
	settings := <-settingsCh
	players := <-playersCh
	return dashboardRESTData{
		info:            info.value,
		metrics:         metrics.value,
		runtimeSettings: settings.value,
		players:         players.value,
		infoErr:         info.err,
		metricsErr:      metrics.err,
		settingsErr:     settings.err,
		playersErr:      players.err,
	}
}

func (a *App) handlePlayerKick(w http.ResponseWriter, r *http.Request) {
	a.handlePlayerAction(w, r, "/kick")
}

func (a *App) handlePlayerBan(w http.ResponseWriter, r *http.Request) {
	a.handlePlayerAction(w, r, "/ban")
}

func (a *App) handlePlayerUnban(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	userID := strings.TrimSpace(r.PathValue("userid"))
	userID, err := validatePalRESTUserID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := client.post("/unban", map[string]any{"userid": userID}, nil); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handlePlayerAction(w http.ResponseWriter, r *http.Request, endpoint string) {
	if !requireConfirmation(w, r) {
		return
	}
	userID := strings.TrimSpace(r.PathValue("userid"))
	userID, err := validatePalRESTUserID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload playerActionPayload
	if !decodeOptionalJSON(w, r, &payload) {
		return
	}
	body := map[string]any{"userid": userID}
	message, err := validatePalRESTMessage(payload.Message, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if message != "" {
		body["message"] = message
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := client.post(endpoint, body, nil); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleServerAnnounce(w http.ResponseWriter, r *http.Request) {
	var payload announcePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	message, err := validatePalRESTMessage(payload.Message, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := client.post("/announce", map[string]string{"message": message}, nil); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleServerSave(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := client.post("/save", nil, nil); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleServerRestSettings(w http.ResponseWriter, r *http.Request) {
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	settings, err := client.getMap("/settings")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) handleServerShutdown(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	var payload shutdownPayload
	if !decodeOptionalJSON(w, r, &payload) {
		return
	}
	waitTime, err := validatePalRESTShutdownWaitTime(payload.WaitTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	message, err := validatePalRESTMessage(payload.Message, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	body := map[string]any{"waittime": waitTime}
	if message != "" {
		body["message"] = message
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := client.post("/shutdown", body, nil); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleServerRestStop(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmation(w, r) {
		return
	}
	if !requireNoRequestBody(w, r) {
		return
	}
	client, err := a.newPalAPIClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := client.post("/stop", nil, nil); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func validatePalRESTUserID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("userid is required")
	}
	if utf8.RuneCountInString(value) > maxPalRESTUserIDRunes {
		return "", fmt.Errorf("userid exceeds %d characters", maxPalRESTUserIDRunes)
	}
	return value, nil
}

func validatePalRESTMessage(value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", errors.New("message is required")
		}
		return "", nil
	}
	if utf8.RuneCountInString(value) > maxPalRESTMessageRunes {
		return "", fmt.Errorf("message exceeds %d characters", maxPalRESTMessageRunes)
	}
	return value, nil
}

func validatePalRESTShutdownWaitTime(value int) (int, error) {
	if value == 0 {
		return defaultPalRESTShutdownWaitTime, nil
	}
	if value < 0 {
		return 0, errors.New("waittime must be positive")
	}
	if value > maxPalRESTShutdownWaitSeconds {
		return 0, fmt.Errorf("waittime exceeds %d seconds", maxPalRESTShutdownWaitSeconds)
	}
	return value, nil
}

type dashboardExtras struct {
	system        systemSummary
	recentLogs    []serverLogEntry
	recentTasks   []taskRecord
	recentBackups []backupRecord
}

func (a *App) dashboardExtras() (dashboardExtras, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return dashboardExtras{}, err
	}
	tasks, err := a.listTasks(5)
	if err != nil {
		return dashboardExtras{}, err
	}
	backups, err := a.listBackupsLimit(5)
	if err != nil {
		return dashboardExtras{}, err
	}
	return dashboardExtras{
		system:        a.currentSystemSummary(settings),
		recentLogs:    a.recentServerLogs(8),
		recentTasks:   tasks,
		recentBackups: backups,
	}, nil
}

func (p *palDashboardPayload) applyExtras(extras dashboardExtras) {
	p.Info = nonNilMap(p.Info)
	p.Metrics = nonNilMap(p.Metrics)
	p.Settings = nonNilMap(p.Settings)
	if p.Players == nil {
		p.Players = []map[string]any{}
	}
	p.System = extras.system
	p.RecentLogs = extras.recentLogs
	if p.RecentLogs == nil {
		p.RecentLogs = []serverLogEntry{}
	}
	p.RecentTasks = extras.recentTasks
	if p.RecentTasks == nil {
		p.RecentTasks = []taskRecord{}
	}
	p.RecentBackups = extras.recentBackups
	if p.RecentBackups == nil {
		p.RecentBackups = []backupRecord{}
	}
}

func (a *App) newPalAPIClient() (palAPIClient, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return palAPIClient{}, err
	}
	return a.newPalAPIClientFromSettings(settings)
}

func (a *App) newPalAPIClientFromSettings(settings settingsPayload) (palAPIClient, error) {
	base := strings.TrimRight(strings.TrimSpace(settings.RestAPIURL), "/")
	if base == "" {
		return palAPIClient{}, errors.New("rest_api_url is required")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return palAPIClient{}, fmt.Errorf("invalid rest_api_url: %s", settings.RestAPIURL)
	}
	return palAPIClient{
		baseURL:  base,
		username: strings.TrimSpace(settings.RestAPIUsername),
		password: settings.RestAPIPassword,
		client:   &http.Client{Timeout: 5 * time.Second},
	}, nil
}

func (c palAPIClient) getMap(endpoint string) (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodGet, endpoint, nil, &out); err != nil {
		return nil, err
	}
	return nonNilMap(out), nil
}

func (c palAPIClient) getPlayers() ([]map[string]any, error) {
	var raw any
	if err := c.do(http.MethodGet, "/players", nil, &raw); err != nil {
		return nil, err
	}
	return normalizePlayers(raw), nil
}

func (c palAPIClient) post(endpoint string, body any, out any) error {
	return c.do(http.MethodPost, endpoint, body, out)
}

func (c palAPIClient) do(method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.baseURL+endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, readErr := readPalAPIResponseBody(resp.Body)
	if readErr != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("Palworld REST %s %s returned HTTP %d: %w", method, endpoint, resp.StatusCode, readErr)
		}
		return fmt.Errorf("Palworld REST %s %s response read failed: %w", method, endpoint, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body := redactSensitive(strings.TrimSpace(string(data)))
		return fmt.Errorf("Palworld REST %s %s returned HTTP %d: %s", method, endpoint, resp.StatusCode, body)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}

func readPalAPIResponseBody(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxPalAPIResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxPalAPIResponseBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxPalAPIResponseBytes)
	}
	return data, nil
}

func normalizePlayers(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []any:
		return mapSlice(typed)
	case map[string]any:
		for _, key := range []string{"players", "Players", "data"} {
			if value, ok := typed[key]; ok {
				if list, ok := value.([]any); ok {
					return mapSlice(list)
				}
			}
		}
		return []map[string]any{typed}
	default:
		return nil
	}
}

func mapSlice(input []any) []map[string]any {
	out := make([]map[string]any, 0, len(input))
	for _, item := range input {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func nonNilMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}

func joinErrors(errs ...error) string {
	var parts []string
	for _, err := range errs {
		if err != nil {
			parts = append(parts, redactSensitive(err.Error()))
		}
	}
	return strings.Join(parts, "; ")
}
