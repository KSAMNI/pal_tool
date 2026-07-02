package app

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxScheduledActions           = 20
	scheduledActionTickInterval   = 30 * time.Second
	scheduledActionStopTimeout    = 30 * time.Second
	scheduledActionTaskTypePrefix = "scheduled_"
)

var scheduledSaveSettleDelay = 3 * time.Second

var scheduleTimePattern = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

var errScheduledActionInterrupted = fmt.Errorf("panel is shutting down")

var validScheduleActions = map[string]struct{}{
	"start":   {},
	"stop":    {},
	"restart": {},
}

type scheduleRecord struct {
	ID      int64  `json:"id,omitempty"`
	Time    string `json:"time"`
	Action  string `json:"action"`
	Enabled bool   `json:"enabled"`
}

type schedulesPayload struct {
	Schedules  []scheduleRecord `json:"schedules"`
	ServerTime string           `json:"server_time,omitempty"`
	Timezone   string           `json:"timezone,omitempty"`
}

func newSchedulesPayload(items []scheduleRecord) schedulesPayload {
	now := time.Now()
	return schedulesPayload{
		Schedules:  items,
		ServerTime: now.Format(time.RFC3339),
		Timezone:   formatUTCOffset(now),
	}
}

func formatUTCOffset(now time.Time) string {
	if _, offset := now.Zone(); offset == 0 {
		return "UTC"
	}
	return "UTC" + now.Format("-07:00")
}

func (a *App) handleGetSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := a.listSchedules()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, newSchedulesPayload(items))
}

func (a *App) handlePutSchedules(w http.ResponseWriter, r *http.Request) {
	var payload schedulesPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	for index := range payload.Schedules {
		payload.Schedules[index].Time = strings.TrimSpace(payload.Schedules[index].Time)
		payload.Schedules[index].Action = strings.TrimSpace(payload.Schedules[index].Action)
	}
	if err := validateSchedules(payload.Schedules); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	saved, err := a.replaceSchedules(payload.Schedules)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, newSchedulesPayload(saved))
}

func validateSchedules(items []scheduleRecord) error {
	if len(items) > maxScheduledActions {
		return fmt.Errorf("too many scheduled actions (max %d)", maxScheduledActions)
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, _, err := parseScheduleTime(item.Time); err != nil {
			return err
		}
		if _, ok := validScheduleActions[item.Action]; !ok {
			return fmt.Errorf("invalid scheduled action %q (expected start, stop or restart)", item.Action)
		}
		key := item.Time + "|" + item.Action
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate scheduled action %s at %s", item.Action, item.Time)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func parseScheduleTime(value string) (int, int, error) {
	if !scheduleTimePattern.MatchString(value) {
		return 0, 0, fmt.Errorf("invalid schedule time %q (expected HH:MM)", value)
	}
	hour, _ := strconv.Atoi(value[:2])
	minute, _ := strconv.Atoi(value[3:])
	return hour, minute, nil
}

func (a *App) listSchedules() ([]scheduleRecord, error) {
	rows, err := a.db.Query(`SELECT id, time, action, enabled FROM scheduled_actions ORDER BY time, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]scheduleRecord, 0)
	for rows.Next() {
		var item scheduleRecord
		var enabled int
		if err := rows.Scan(&item.ID, &item.Time, &item.Action, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *App) replaceSchedules(items []scheduleRecord) ([]scheduleRecord, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec(`DELETE FROM scheduled_actions`); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		if _, err := tx.Exec(
			`INSERT INTO scheduled_actions(time, action, enabled, created_at) VALUES(?, ?, ?, ?)`,
			item.Time,
			item.Action,
			item.Enabled,
			now,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return a.listSchedules()
}

func (a *App) startScheduledActionRunner() {
	a.startBackground(func() {
		ticker := time.NewTicker(scheduledActionTickInterval)
		defer ticker.Stop()
		last := time.Now()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				a.runDueScheduledActions(last, now)
				last = now
			case <-a.stopCh:
				return
			}
		}
	})
}

func (a *App) runDueScheduledActions(from, to time.Time) {
	schedules, err := a.listSchedules()
	if err != nil {
		a.appendServerLog("Scheduled actions check failed: " + err.Error())
		return
	}
	for _, item := range schedules {
		if !item.Enabled {
			continue
		}
		if !scheduleDueWithin(item.Time, from, to) {
			continue
		}
		a.executeScheduledAction(item)
	}
}

func scheduleDueWithin(spec string, from, to time.Time) bool {
	hour, minute, err := parseScheduleTime(spec)
	if err != nil || !to.After(from) {
		return false
	}
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, to.Location())
	for !day.After(end) {
		candidate := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
		if candidate.After(from) && !candidate.After(to) {
			return true
		}
		day = day.AddDate(0, 0, 1)
	}
	return false
}

func (a *App) executeScheduledAction(item scheduleRecord) {
	settings, err := a.loadSettings()
	if err != nil {
		a.appendServerLog(fmt.Sprintf("Scheduled %s (%s) skipped: %v", item.Action, item.Time, err))
		return
	}
	taskID, releaseTask, err := a.beginTask(scheduledActionTaskTypePrefix + item.Action)
	if err != nil {
		a.appendServerLog(fmt.Sprintf("Scheduled %s (%s) skipped: %v", item.Action, item.Time, err))
		return
	}
	defer releaseTask()
	a.logTaskf(taskID, "Running scheduled %s (daily at %s)", item.Action, item.Time)
	if err := a.runScheduledAction(taskID, item.Action, settings); err != nil {
		a.logTaskf(taskID, "Scheduled %s failed: %v", item.Action, err)
		_ = a.finishTask(taskID, "failed")
		return
	}
	a.logTaskf(taskID, "Scheduled %s completed", item.Action)
	_ = a.finishTask(taskID, "success")
}

func (a *App) runScheduledAction(taskID int64, action string, settings settingsPayload) error {
	managed := a.isManagedServerRunning()
	external, _ := a.externalServerRunning(settings)
	switch action {
	case "start":
		if managed || external {
			a.logTaskf(taskID, "PalServer is already running; nothing to start")
			return nil
		}
		return a.startManagedServerProcessAdmitted(settings)
	case "stop":
		if external {
			return errExternalServerRunning
		}
		if !managed {
			a.logTaskf(taskID, "PalServer is not running from this panel; nothing to stop")
			return nil
		}
		if !a.saveWorldBeforeScheduledStop(taskID, settings) {
			return errScheduledActionInterrupted
		}
		return a.stopManagedServerProcess(scheduledActionStopTimeout)
	case "restart":
		if external {
			return errExternalServerRunning
		}
		if managed {
			if !a.saveWorldBeforeScheduledStop(taskID, settings) {
				return errScheduledActionInterrupted
			}
			if err := a.stopManagedServerProcess(scheduledActionStopTimeout); err != nil {
				return err
			}
		} else {
			a.logTaskf(taskID, "PalServer is not running; starting it")
		}
		return a.startManagedServerProcessAdmitted(settings)
	default:
		return fmt.Errorf("unknown scheduled action %q", action)
	}
}

func (a *App) saveWorldBeforeScheduledStop(taskID int64, settings settingsPayload) bool {
	client, err := a.newPalAPIClientFromSettings(settings)
	if err != nil {
		a.logTaskf(taskID, "Skipping world save before stop: %v", err)
		return true
	}
	if err := client.post("/save", nil, nil); err != nil {
		a.logTaskf(taskID, "World save request failed (continuing): %v", err)
		return true
	}
	a.logTaskf(taskID, "World save requested; waiting %s before stopping", scheduledSaveSettleDelay)
	select {
	case <-time.After(scheduledSaveSettleDelay):
		return true
	case <-a.stopCh:
		a.logTaskf(taskID, "Panel is shutting down; scheduled action interrupted before stopping PalServer")
		return false
	}
}
