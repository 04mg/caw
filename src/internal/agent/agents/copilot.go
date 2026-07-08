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

func (w *CopilotWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".copilot", "session-store.db")

	var lastDBMod time.Time
	var copilotSessionID string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(dbPath)
			if err != nil {
				continue
			}
			if info.ModTime() == lastDBMod {
				continue
			}
			lastDBMod = info.ModTime()
			w.parseCopilotDB(dbPath, cwd, &copilotSessionID, callback)
		}
	}
}

func (w *CopilotWatcher) parseCopilotDB(dbPath string, sessionCwd string, copilotSessionID *string, callback func(status, tool, details, title string)) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	// Find the session that matches our cwd, falling back to the most recent one.
	if *copilotSessionID == "" {
		if sessionCwd != "" {
			_ = db.QueryRow(
				"SELECT id FROM sessions WHERE cwd = ? ORDER BY updated_at DESC LIMIT 1",
				sessionCwd,
			).Scan(copilotSessionID)
		}
		if *copilotSessionID == "" {
			if err := db.QueryRow(
				"SELECT id FROM sessions ORDER BY updated_at DESC LIMIT 1",
			).Scan(copilotSessionID); err != nil {
				return
			}
		}
	}

	// Retrieve the session title: try summary first, then first user prompt.
	var sessionTitle string
	_ = db.QueryRow(
		"SELECT summary FROM sessions WHERE id = ?",
		*copilotSessionID,
	).Scan(&sessionTitle)
	if sessionTitle == "" {
		_ = db.QueryRow(
			"SELECT content FROM turns WHERE session_id = ? AND role = 'user' ORDER BY created_at ASC LIMIT 1",
			*copilotSessionID,
		).Scan(&sessionTitle)
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Determine status from the most recent turn.
	var role, content string
	err = db.QueryRow(
		"SELECT role, content FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1",
		*copilotSessionID,
	).Scan(&role, &content)
	if err != nil {
		return
	}

	if role == "user" {
		// User just sent a message — agent is processing.
		callback("thinking", "", "", sessionTitle)
	} else {
		status := "idle"
		contentLower := strings.ToLower(content)
		if strings.Contains(contentLower, "[y/n]") ||
			strings.Contains(contentLower, "[y/N]") ||
			strings.Contains(contentLower, "[Y/n]") ||
			strings.Contains(contentLower, "(y/n)") ||
			strings.Contains(contentLower, "confirm") ||
			strings.Contains(contentLower, "approve") {
			status = "waiting_input"
		}
		callback(status, "", "", sessionTitle)
	}
}
