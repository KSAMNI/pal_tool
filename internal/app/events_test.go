package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRuntimeEventsWebSocketStreamsSnapshotLogsAndTasks(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	existingTaskID, err := panel.createTask("existing")
	if err != nil {
		t.Fatalf("createTask() error = %v", err)
	}
	if err := panel.appendTaskLog(existingTaskID, "existing log\n"); err != nil {
		t.Fatalf("appendTaskLog() error = %v", err)
	}
	panel.appendServerLog("existing server log")

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, resp, err := websocket.Dial(ctx, websocketURL(server.URL, "/api/events"), nil); err == nil {
		conn.CloseNow()
		t.Fatalf("unauthenticated websocket connected successfully")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %#v, err = %v", resp, err)
	}

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

	header := http.Header{}
	header.Set("Cookie", cookieHeader(t, jar, server.URL))
	crossOriginHeader := header.Clone()
	crossOriginHeader.Set("Origin", "https://evil.example")
	if conn, resp, err := websocket.Dial(ctx, websocketURL(server.URL, "/api/events"), &websocket.DialOptions{HTTPHeader: crossOriginHeader}); err == nil {
		conn.CloseNow()
		t.Fatalf("cross-origin websocket connected successfully")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %#v, err = %v", resp, err)
	}

	sameOriginHeader := header.Clone()
	sameOriginHeader.Set("Origin", server.URL)
	conn, _, err := websocket.Dial(ctx, websocketURL(server.URL, "/api/events"), &websocket.DialOptions{HTTPHeader: sameOriginHeader})
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	snapshot := readRuntimeEvent(t, ctx, conn)
	if snapshot.Type != "snapshot" {
		t.Fatalf("first event type = %q, want snapshot", snapshot.Type)
	}
	if len(snapshot.Tasks) == 0 || snapshot.Tasks[0].ID != existingTaskID {
		t.Fatalf("snapshot tasks did not include existing task: %#v", snapshot.Tasks)
	}
	if len(snapshot.ServerLogs) == 0 || snapshot.ServerLogs[len(snapshot.ServerLogs)-1].Message != "existing server log" {
		t.Fatalf("snapshot logs did not include existing server log: %#v", snapshot.ServerLogs)
	}
	if snapshot.Operation == nil || *snapshot.Operation {
		t.Fatalf("snapshot operation_running = %#v, want false", snapshot.Operation)
	}

	releaseOperation, err := panel.reserveTaskSlot()
	if err != nil {
		t.Fatalf("reserveTaskSlot() error = %v", err)
	}
	operationStart := readUntilRuntimeEvent(t, ctx, conn, func(event runtimeEvent) bool {
		return event.Type == "operation" && event.Operation != nil
	})
	if !*operationStart.Operation {
		t.Fatalf("operation start event operation_running = false, want true")
	}
	releaseOperation()
	operationDone := readUntilRuntimeEvent(t, ctx, conn, func(event runtimeEvent) bool {
		return event.Type == "operation" && event.Operation != nil && !*event.Operation
	})
	if *operationDone.Operation {
		t.Fatalf("operation done event operation_running = true, want false")
	}

	panel.appendServerLog("live server log")
	logEvent := readUntilRuntimeEvent(t, ctx, conn, func(event runtimeEvent) bool {
		return event.Type == "server_log" && len(event.ServerLogs) == 1 && event.ServerLogs[0].Message == "live server log"
	})
	if logEvent.Running == nil {
		t.Fatalf("server_log event did not include running state: %#v", logEvent)
	}

	taskID, err := panel.createTask("live_task")
	if err != nil {
		t.Fatalf("create live task: %v", err)
	}
	if err := panel.appendTaskLog(taskID, "live task log\n"); err != nil {
		t.Fatalf("append live task log: %v", err)
	}
	taskEvent := readUntilRuntimeEvent(t, ctx, conn, func(event runtimeEvent) bool {
		return event.Type == "task" && event.Task != nil && event.Task.ID == taskID && strings.Contains(event.Task.Log, "live task log")
	})
	if taskEvent.Task.Status != "running" {
		t.Fatalf("task status = %q, want running", taskEvent.Task.Status)
	}

	if err := panel.finishTask(taskID, "success"); err != nil {
		t.Fatalf("finish live task: %v", err)
	}
	finishedEvent := readUntilRuntimeEvent(t, ctx, conn, func(event runtimeEvent) bool {
		return event.Type == "task" && event.Task != nil && event.Task.ID == taskID && event.Task.Status == "success"
	})
	if finishedEvent.Task.FinishedAt == "" {
		t.Fatalf("finished task event did not include finished_at: %#v", finishedEvent.Task)
	}
}

func TestRuntimeEventsCloseAndUnsubscribeOnAppShutdown(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = panel.Close()
	})

	server, client := newAuthenticatedTestServer(t, panel)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header := http.Header{}
	header.Set("Cookie", cookieHeader(t, client.Jar, server.URL))
	conn, _, err := websocket.Dial(ctx, websocketURL(server.URL, "/api/events"), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	snapshot := readRuntimeEvent(t, ctx, conn)
	if snapshot.Type != "snapshot" {
		t.Fatalf("first event type = %q, want snapshot", snapshot.Type)
	}
	if got := runtimeEventClientCount(panel); got != 1 {
		t.Fatalf("event client count = %d, want 1 before shutdown", got)
	}

	if err := panel.Close(); err != nil {
		t.Fatalf("panel.Close() error = %v", err)
	}
	_, _, err = conn.Read(ctx)
	if status := websocket.CloseStatus(err); status != websocket.StatusGoingAway {
		t.Fatalf("websocket close status = %v, want %v (err=%v)", status, websocket.StatusGoingAway, err)
	}
	waitForRuntimeEventClientCount(t, panel, 0)
}

func readRuntimeEvent(t *testing.T, ctx context.Context, conn *websocket.Conn) runtimeEvent {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v", err)
	}
	var event runtimeEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", data, err)
	}
	return event
}

func readUntilRuntimeEvent(t *testing.T, ctx context.Context, conn *websocket.Conn, match func(runtimeEvent) bool) runtimeEvent {
	t.Helper()
	for {
		event := readRuntimeEvent(t, ctx, conn)
		if match(event) {
			return event
		}
	}
}

func websocketURL(base, path string) string {
	return "ws" + strings.TrimPrefix(base, "http") + path
}

func cookieHeader(t *testing.T, jar http.CookieJar, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	cookies := jar.Cookies(parsed)
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.String())
	}
	return strings.Join(parts, "; ")
}

func runtimeEventClientCount(panel *App) int {
	panel.eventMu.Lock()
	defer panel.eventMu.Unlock()
	return len(panel.eventClients)
}

func waitForRuntimeEventClientCount(t *testing.T, panel *App, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := runtimeEventClientCount(panel); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event client count = %d, want %d", runtimeEventClientCount(panel), want)
}
