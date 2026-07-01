package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

type runtimeEvent struct {
	Type       string           `json:"type"`
	Task       *taskRecord      `json:"task,omitempty"`
	Tasks      []taskRecord     `json:"tasks,omitempty"`
	ServerLogs []serverLogEntry `json:"server_logs,omitempty"`
	Running    *bool            `json:"running,omitempty"`
	Operation  *bool            `json:"operation_running,omitempty"`
	Error      string           `json:"error,omitempty"`
}

func (a *App) handleRuntimeEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, runtimeEventAcceptOptions())
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := conn.CloseRead(r.Context())
	events := a.subscribeRuntimeEvents()
	defer a.unsubscribeRuntimeEvents(events)

	snapshot, err := a.runtimeSnapshot()
	if err != nil {
		_ = writeRuntimeEvent(ctx, conn, runtimeEvent{Type: "error", Error: redactSensitive(err.Error())})
		_ = conn.Close(websocket.StatusInternalError, "snapshot failed")
		return
	}
	if err := writeRuntimeEvent(ctx, conn, snapshot); err != nil {
		return
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeRuntimeEvent(ctx, conn, event); err != nil {
				return
			}
		case <-ctx.Done():
			return
		case <-a.stopCh:
			_ = conn.Close(websocket.StatusGoingAway, "server stopping")
			return
		}
	}
}

func runtimeEventAcceptOptions() *websocket.AcceptOptions {
	// Keep the library's default origin verification: the request host is allowed,
	// cross-origin browser attempts are rejected unless explicitly configured.
	return &websocket.AcceptOptions{}
}

func (a *App) runtimeSnapshot() (runtimeEvent, error) {
	tasks, err := a.listTasks(10)
	if err != nil {
		return runtimeEvent{}, err
	}
	running := a.isServerRunning()
	operationRunning := a.operationRunning()
	if settings, err := a.loadSettings(); err == nil {
		status := a.currentServerStatus(settings)
		running = status.Running
		operationRunning = status.OperationRunning
	}
	return runtimeEvent{
		Type:       "snapshot",
		Tasks:      tasks,
		ServerLogs: a.recentServerLogs(0),
		Running:    &running,
		Operation:  &operationRunning,
	}, nil
}

func (a *App) subscribeRuntimeEvents() chan runtimeEvent {
	ch := make(chan runtimeEvent, 32)
	a.eventMu.Lock()
	a.eventClients[ch] = struct{}{}
	a.eventMu.Unlock()
	return ch
}

func (a *App) unsubscribeRuntimeEvents(ch chan runtimeEvent) {
	a.eventMu.Lock()
	if _, ok := a.eventClients[ch]; ok {
		delete(a.eventClients, ch)
		close(ch)
	}
	a.eventMu.Unlock()
}

func (a *App) broadcastRuntimeEvent(event runtimeEvent) {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	for ch := range a.eventClients {
		select {
		case ch <- event:
		default:
		}
	}
}

func (a *App) broadcastTask(taskID int64) {
	task, err := a.getTask(taskID)
	if err != nil {
		return
	}
	a.broadcastRuntimeEvent(runtimeEvent{Type: "task", Task: &task})
}

func (a *App) broadcastOperationRunning(running bool) {
	a.broadcastRuntimeEvent(runtimeEvent{Type: "operation", Operation: &running})
}

func writeRuntimeEvent(ctx context.Context, conn *websocket.Conn, event runtimeEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}
