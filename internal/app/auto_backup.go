package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (a *App) startAutoBackupScheduler() {
	a.startBackground(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := a.runDueAutoBackup(time.Now().UTC()); err != nil {
					a.appendServerLog("Automatic backup skipped: " + err.Error())
				}
			case <-a.stopCh:
				return
			}
		}
	})
}

func (a *App) runDueAutoBackup(now time.Time) error {
	settings, err := a.loadSettings()
	if err != nil {
		return err
	}
	if !settings.AutoBackupEnabled {
		return nil
	}
	if strings.TrimSpace(settings.PalServerPath) == "" {
		return nil
	}
	interval := autoBackupInterval(settings)
	due, err := a.autoBackupDue(now, interval)
	if err != nil {
		return err
	}
	if !due {
		return nil
	}

	taskID, releaseTask, err := a.beginTask("auto_backup")
	if err != nil {
		return err
	}
	defer releaseTask()

	a.logTaskf(taskID, "Starting scheduled automatic backup")
	backup, err := a.createBackupWithSettings(settings, "auto", fmt.Sprintf("Scheduled automatic backup at %s", now.Format(time.RFC3339)))
	if err != nil {
		if errors.Is(err, errNoBackupSources) {
			a.logTaskf(taskID, "Automatic backup skipped: %v", err)
			_ = a.finishTask(taskID, "success")
			return nil
		}
		a.logTaskf(taskID, "Automatic backup failed: %v", err)
		_ = a.finishTask(taskID, "failed")
		return err
	}
	a.logTaskf(taskID, "Automatic backup created: %s", backup.Filename)
	_ = a.finishTask(taskID, "success")
	return nil
}

func autoBackupInterval(settings settingsPayload) time.Duration {
	hours := settings.AutoBackupIntervalHours
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func (a *App) autoBackupDue(now time.Time, interval time.Duration) (bool, error) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	last, err := a.latestBackupTime("auto")
	if err != nil {
		return false, err
	}
	if last.IsZero() {
		return true, nil
	}
	return !last.After(now.Add(-interval)), nil
}

func (a *App) latestBackupTime(backupType string) (time.Time, error) {
	var created string
	err := a.db.QueryRow(
		`SELECT CAST(created_at AS TEXT)
		 FROM backups
		 WHERE type = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		backupType,
	).Scan(&created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}
