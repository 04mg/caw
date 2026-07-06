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

type CopilotWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("copilot", &CopilotWatcher{})
}

func (w *CopilotWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, prompt string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".copilot", "session-store.db")
	var lastCheck time.Time = time.Now().Add(-5 * time.Second)
	var lastFileSize int64 = 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(dbPath)
			if err == nil {
				if info.Size() > lastFileSize || info.ModTime().After(lastCheck) {
					w.parseCopilotDB(dbPath, cwd, callback)
					lastFileSize = info.Size()
					lastCheck = info.ModTime()
				}
			}
		}
	}
}

func (w *CopilotWatcher) parseCopilotDB(dbPath string, sessionCwd string, callback func(status, tool, details, prompt string)) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	var copilotSessionID string
	row := db.QueryRow("SELECT id FROM sessions WHERE cwd = ? ORDER BY updated_at DESC LIMIT 1", sessionCwd)
	if err := row.Scan(&copilotSessionID); err != nil {
		row = db.QueryRow("SELECT id FROM sessions ORDER BY updated_at DESC LIMIT 1")
		if err := row.Scan(&copilotSessionID); err != nil {
			return
		}
	}

	var role, content string
	err = db.QueryRow(
		"SELECT role, content FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1",
		copilotSessionID,
	).Scan(&role, &content)

	if err != nil {
		return
	}

	if role == "user" {
		callback("thinking", "", "", "")
	} else {
		status := "idle"
		if strings.Contains(content, "?") || strings.Contains(content, "approve") {
			status = "waiting_input"
		}
		callback(status, "", "", content)
	}
}
