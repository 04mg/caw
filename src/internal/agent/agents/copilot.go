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
	const agentID = "copilot"

	var lastDBMod time.Time
	var copilotSessionID string
	watcherStart := time.Now().Add(-2 * time.Second)

	defer func() {
		if copilotSessionID != "" {
			UnclaimSession(agentID, cwd, copilotSessionID)
		}
	}()

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
			if copilotSessionID == "" {
				copilotSessionID = findUnclaimedCopilotSession(dbPath, cwd, watcherStart, agentID)
			}
			if copilotSessionID != "" {
				w.parseCopilotDB(dbPath, cwd, copilotSessionID, callback)
			}
		}
	}
}

// findUnclaimedCopilotSession enumerates candidate Copilot sessions in the
// session-store db (filtered by cwd when possible and started after
// watcherStart) and returns the id of the most recent one that is not already
// claimed by another watcher of the same agent type+cwd. When a session is
// found it is immediately claimed.
func findUnclaimedCopilotSession(dbPath string, cwd string, watcherStart time.Time, agentID string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()

	type row struct {
		id        string
		updatedAt string
	}
	var candidates []row

	if cwd != "" {
		rows, qerr := db.Query(
			"SELECT id, updated_at FROM sessions WHERE cwd = ? ORDER BY updated_at DESC",
			cwd,
		)
		if qerr == nil {
			for rows.Next() {
				var r row
				rows.Scan(&r.id, &r.updatedAt)
				candidates = append(candidates, r)
			}
			rows.Close()
		}
	}
	if len(candidates) == 0 {
		rows, qerr := db.Query(
			"SELECT id, updated_at FROM sessions ORDER BY updated_at DESC",
		)
		if qerr == nil {
			for rows.Next() {
				var r row
				rows.Scan(&r.id, &r.updatedAt)
				candidates = append(candidates, r)
			}
			rows.Close()
		}
	}

	for _, r := range candidates {
		if t, err := time.Parse(time.RFC3339, r.updatedAt); err == nil {
			if !t.After(watcherStart) {
				continue
			}
		}
		if ClaimSession(agentID, cwd, r.id) {
			return r.id
		}
	}
	return ""
}

func (w *CopilotWatcher) parseCopilotDB(dbPath string, sessionCwd string, copilotSessionID string, callback func(status, tool, details, title string)) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	// The session id was already resolved and claimed by findUnclaimedCopilotSession.
	resolvedID := copilotSessionID

	if resolvedID == "" {
		return
	}

	// Retrieve the session title: try summary first, then first user prompt.
	var sessionTitle string
	_ = db.QueryRow(
		"SELECT summary FROM sessions WHERE id = ?",
		resolvedID,
	).Scan(&sessionTitle)
	if sessionTitle == "" {
		_ = db.QueryRow(
			"SELECT content FROM turns WHERE session_id = ? AND role = 'user' ORDER BY created_at ASC LIMIT 1",
			resolvedID,
		).Scan(&sessionTitle)
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Determine status from the most recent turn.
	var role, content string
	err = db.QueryRow(
		"SELECT role, content FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1",
		resolvedID,
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
