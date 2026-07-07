package agents

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

type OpenCodeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("opencode", &OpenCodeWatcher{})
}

func (w *OpenCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, prompt string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dbPath = filepath.Join(appData, "opencode", "opencode.db")
		}
	}

	walPath := dbPath + "-wal"

	var lastCheck time.Time = time.Now().Add(-5 * time.Second)
	var lastFileSize int64 = 0
	var lastWalSize int64 = 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := false
			info, err := os.Stat(dbPath)
			if err == nil {
				if info.Size() != lastFileSize || info.ModTime().After(lastCheck) {
					changed = true
					lastFileSize = info.Size()
				}
			}
			walInfo, walErr := os.Stat(walPath)
			if walErr == nil {
				if walInfo.Size() != lastWalSize || walInfo.ModTime().After(lastCheck) {
					changed = true
					lastWalSize = walInfo.Size()
				}
			}
			if changed {
				if walErr == nil && walInfo.ModTime().After(lastCheck) {
					lastCheck = walInfo.ModTime()
				} else if err == nil {
					lastCheck = info.ModTime()
				}
				w.parseOpenCodeDB(dbPath, callback)
			}
		}
	}
}

func (w *OpenCodeWatcher) parseOpenCodeDB(dbPath string, callback func(status, tool, details, prompt string)) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	var role, parts string
	err = db.QueryRow(
		`SELECT role, parts FROM messages 
		 WHERE session_id = (SELECT id FROM sessions ORDER BY updated_at DESC LIMIT 1) 
		 ORDER BY created_at DESC LIMIT 1`,
	).Scan(&role, &parts)

	if err != nil {
		return
	}

	if role == "user" {
		callback("thinking", "", "", "")
	} else if role == "tool" {
		callback("executing", "", "", "")
	} else {
		status := "idle"
		if strings.Contains(parts, "?") || strings.Contains(parts, "approval") {
			status = "waiting_input"
		}
		callback(status, "", "", parts)
	}
}
