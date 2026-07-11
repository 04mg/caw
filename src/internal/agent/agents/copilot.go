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

func (w *CopilotWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".copilot", "session-store.db")
	const agentID = "copilot"

	var lastDBMod time.Time
	var copilotSessionID string
	// For a fresh start, only match sessions created after the watcher
	// started (no negative offset). On resume, the session may predate the
	// watcher so the recency filter is skipped in findUnclaimedCopilotSession.
	watcherStart := time.Now()
	// Re-bind bookkeeping for /new and /resume detection.
	var lastActivity time.Time
	var silentTicks int

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
			heartbeat()
			info, err := os.Stat(dbPath)
			if err != nil {
				continue
			}
			if info.ModTime() == lastDBMod {
				silentTicks++
				// Still run the re-bind check below even when the DB mtime
				// hasn't changed, since a /resume into an older session may
				// not bump the top-level db file mtime immediately.
			} else {
				lastDBMod = info.ModTime()
			}
			if copilotSessionID == "" {
				copilotSessionID = findUnclaimedCopilotSession(dbPath, cwd, watcherStart, agentID, resume, sessionID)
				if copilotSessionID != "" {
					lastActivity = time.Now()
					silentTicks = 0
				}
			}
			if copilotSessionID != "" {
				before := lastActivity
				w.parseCopilotDB(dbPath, cwd, copilotSessionID, func(status, tool, details, title string) {
					lastActivity = time.Now()
					callback(status, tool, details, title)
				})
				if lastActivity.Equal(before) {
					silentTicks++
				} else {
					silentTicks = 0
				}
			} else {
				silentTicks++
			}

			// Mid-session re-bind for /new and /resume.
			if copilotSessionID != "" && silentTicks >= rebindSilenceTicks {
				newKey := findRebindCopilotSession(dbPath, cwd, lastActivity, copilotSessionID, sessionID)
				if newKey != "" && newKey != copilotSessionID {
					if ClaimSession(agentID, cwd, newKey) {
						UnclaimSession(agentID, cwd, copilotSessionID)
						copilotSessionID = newKey
						silentTicks = 0
						lastActivity = time.Now()
					}
				}
			}
		}
	}
}

// findUnclaimedCopilotSession enumerates candidate Copilot sessions in the
// session-store db (filtered by cwd when possible and started after
// watcherStart) and returns the id of the earliest one (oldest first) that is
// not already claimed by another watcher of the same agent type+cwd.
//
// PTY activity correlation gates claiming: only a watcher whose PTY has
// recently produced output may claim a new session, preventing a silent
// watcher from stealing a session created by an active sibling agent.
func findUnclaimedCopilotSession(dbPath string, cwd string, watcherStart time.Time, agentID string, resume bool, ptyID string) string {
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
			"SELECT id, updated_at FROM sessions WHERE cwd = ? ORDER BY updated_at ASC",
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
			"SELECT id, updated_at FROM sessions ORDER BY updated_at ASC",
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

	lastPtyOut := agent.LastPtyActivity(ptyID)
	ptyRecentlyActive := time.Since(lastPtyOut) < 3*time.Second

	for _, r := range candidates {
		// On resume, skip the recency filter — the session may predate the
		// watcher. On fresh start, only match sessions started after the
		// watcher launched to avoid grabbing a stale session.
		if !resume {
			if t, err := time.Parse(time.RFC3339, r.updatedAt); err == nil {
				if !t.After(watcherStart) {
					continue
				}
			}
		}
		if !ptyRecentlyActive {
			continue
		}
		if ClaimSession(agentID, cwd, r.id) {
			return r.id
		}
	}
	return ""
}

// findRebindCopilotSession looks for a different Copilot session in the same
// cwd whose updated_at is more recent than lastActivity. Used by the
// mid-session re-bind pass to detect /new and /resume. Gates on PTY activity.
// It does NOT claim the returned session; the caller is responsible for
// calling ClaimSession.
func findRebindCopilotSession(dbPath string, cwd string, lastActivity time.Time, currentID string, ptyID string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()

	lastPtyOut := agent.LastPtyActivity(ptyID)
	if time.Since(lastPtyOut) > 3*time.Second {
		return ""
	}

	var bestID string
	var bestTime time.Time

	if cwd != "" {
		rows, qerr := db.Query(
			"SELECT id, updated_at FROM sessions WHERE cwd = ? ORDER BY updated_at DESC",
			cwd,
		)
		if qerr == nil {
			for rows.Next() {
				var id, updatedAt string
				if rows.Scan(&id, &updatedAt) != nil {
					continue
				}
				if id == currentID {
					continue
				}
				t, err := time.Parse(time.RFC3339, updatedAt)
				if err != nil {
					continue
				}
				if t.After(lastActivity) && t.After(bestTime) {
					bestID = id
					bestTime = t
				}
			}
			rows.Close()
		}
	}
	return bestID
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
