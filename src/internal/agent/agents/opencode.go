package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/04mg/caw/internal/agent"
)

type OpenCodeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("opencode", &OpenCodeWatcher{})
}

// openCodePart mirrors the JSON blob stored in the part.data column.
type openCodePart struct {
	Type  string `json:"type"`
	Tool  string `json:"tool,omitempty"`
	State *struct {
		Status string `json:"status"`
	} `json:"state,omitempty"`
	Text string `json:"text,omitempty"`
}

// openCodeMessage mirrors the JSON blob stored in the message.data column.
type openCodeMessage struct {
	Role   string `json:"role"`
	Finish string `json:"finish,omitempty"`
	Parts  []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"parts,omitempty"`
}

func (w *OpenCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
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

	const agentID = "opencode"
	var lastDBMod time.Time
	var lastWALMod time.Time
	var lastReportedStatus string
	var openCodeSessionID string
	watcherStart := time.Now().Add(-2 * time.Second)

	defer func() {
		if openCodeSessionID != "" {
			UnclaimSession(agentID, cwd, openCodeSessionID)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeat()
			changed := false

			if info, err := os.Stat(dbPath); err == nil {
				if info.ModTime() != lastDBMod {
					changed = true
					lastDBMod = info.ModTime()
				}
			}
			if walInfo, err := os.Stat(walPath); err == nil {
				if walInfo.ModTime() != lastWALMod {
					changed = true
					lastWALMod = walInfo.ModTime()
				}
			}

			if openCodeSessionID == "" {
				if changed {
					openCodeSessionID = findUnclaimedOpenCodeSession(dbPath, cwd, watcherStart, agentID, resume)
				}
			}

			// Always re-parse while waiting for user input even if the DB
			// hasn't changed. This keeps lastActivity alive in the watchdog
			// so the idle-timeout never fires while a question is pending.
			if changed || lastReportedStatus == "waiting_input" {
				if openCodeSessionID != "" {
					wrappedCallback := func(status, tool, details, title string) {
						lastReportedStatus = status
						callback(status, tool, details, title)
					}
					w.parseOpenCodeDB(dbPath, cwd, openCodeSessionID, wrappedCallback)
				}
			}
		}
	}
}

// findUnclaimedOpenCodeSession enumerates candidate OpenCode sessions in the
// opencode.db (filtered by directory=cwd when possible and started after
// watcherStart) and returns the id of the most recent one that is not already
// claimed by another watcher of the same agent type+cwd. When a session is
// found it is immediately claimed.
func findUnclaimedOpenCodeSession(dbPath string, cwd string, watcherStart time.Time, agentID string, resume bool) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	type row struct {
		id          string
		timeUpdated int64
	}
	var candidates []row

	if cwd != "" {
		rows, qerr := db.Query(
			`SELECT id, time_updated FROM session WHERE directory = ? ORDER BY time_updated DESC`,
			cwd,
		)
		if qerr == nil {
			for rows.Next() {
				var r row
				rows.Scan(&r.id, &r.timeUpdated)
				candidates = append(candidates, r)
			}
			rows.Close()
		}
	}
	if len(candidates) == 0 {
		rows, qerr := db.Query(
			`SELECT id, time_updated FROM session ORDER BY time_updated DESC`,
		)
		if qerr == nil {
			for rows.Next() {
				var r row
				rows.Scan(&r.id, &r.timeUpdated)
				candidates = append(candidates, r)
			}
			rows.Close()
		}
	}

	for _, r := range candidates {
		// On resume, skip the recency filter — the session may predate the
		// watcher (the agent reattaches to an old session). On fresh start,
		// only match sessions started after the watcher launched.
		if !resume {
			sessionTime := time.UnixMilli(r.timeUpdated)
			if !sessionTime.After(watcherStart) {
				continue
			}
		}
		if ClaimSession(agentID, cwd, r.id) {
			return r.id
		}
	}
	return ""
}

func (w *OpenCodeWatcher) parseOpenCodeDB(dbPath string, cwd string, openCodeSessionID string, callback func(status, tool, details, title string)) {
	// Open in read-only WAL mode to avoid interfering with the running OpenCode process.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return
	}
	defer db.Close()

	var openCodeTitle string
	resolvedID := openCodeSessionID

	if resolvedID == "" {
		callback("idle", "", "", "")
		return
	}

	_ = db.QueryRow(
		`SELECT title FROM session WHERE id = ?`,
		resolvedID,
	).Scan(&openCodeTitle)

	// Retrieve the user's prompt. OpenCode stores the initial prompt as the
	// first 'text' part in the session. session_input exists in the schema but
	// is not reliably populated in all versions, so we prefer the part table.
	var userPrompt string
	var firstTextPartData string
	if err := db.QueryRow(
		`SELECT data FROM part WHERE session_id = ? AND json_extract(data,'$.type') = 'text' ORDER BY time_created ASC LIMIT 1`,
		resolvedID,
	).Scan(&firstTextPartData); err == nil && firstTextPartData != "" {
		var p openCodePart
		if json.Unmarshal([]byte(firstTextPartData), &p) == nil && p.Text != "" {
			userPrompt = p.Text
		}
	}
	// Fallback: session_input table (populated in some OpenCode versions).
	if userPrompt == "" {
		_ = db.QueryRow(
			`SELECT prompt FROM session_input WHERE session_id = ? ORDER BY time_created ASC LIMIT 1`,
			resolvedID,
		).Scan(&userPrompt)
	}
	userPrompt = CleanPrompt(userPrompt)

	sessionTitle := openCodeTitle
	if sessionTitle == "" {
		sessionTitle = userPrompt
	}

	// 1. Get the latest message in the session.
	var msgID string
	var msgDataJSON string
	err = db.QueryRow(
		`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created DESC LIMIT 1`,
		resolvedID,
	).Scan(&msgID, &msgDataJSON)
	if err != nil {
		callback("idle", "", "", sessionTitle)
		return
	}

	var msg openCodeMessage
	if err := json.Unmarshal([]byte(msgDataJSON), &msg); err != nil {
		callback("idle", "", "", sessionTitle)
		return
	}

	// 2. Query all parts associated with this latest message.
	rows, err := db.Query(
		`SELECT data FROM part WHERE message_id = ? ORDER BY time_created ASC`,
		msgID,
	)
	var parts []openCodePart
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var partData string
			if rows.Scan(&partData) == nil {
				var p openCodePart
				if json.Unmarshal([]byte(partData), &p) == nil {
					parts = append(parts, p)
				}
			}
		}
	}

	// 3. Determine status based on message role, finish reason, and part states.
	switch msg.Role {
	case "user":
		// User just sent a message — agent is thinking/preparing response.
		callback("thinking", "", "", sessionTitle)

	case "assistant":
		// Check for any currently running/pending tool calls.
		var hasQuestion bool
		var activeTool string
		var lastToolName string

		for _, p := range parts {
			if p.Type == "tool" {
				if p.Tool != "" {
					lastToolName = p.Tool
				}
				toolStatus := ""
				if p.State != nil {
					toolStatus = p.State.Status
				}
				if toolStatus == "running" || toolStatus == "pending" {
					if p.Tool == "question" {
						hasQuestion = true
					} else {
						activeTool = p.Tool
					}
				}
			}
		}

		if hasQuestion {
			callback("waiting_input", "question", "", sessionTitle)
			return
		}
		if activeTool != "" {
			callback("executing", activeTool, "", sessionTitle)
			return
		}

		// If no tool is actively running/pending but the turn expects more tool calls:
		if msg.Finish == "tool-calls" {
			if lastToolName != "" {
				callback("executing", lastToolName, "", sessionTitle)
			} else {
				callback("thinking", "", "", sessionTitle)
			}
			return
		}

		// Empty finish means the message row is still being written to — the
		// turn is in progress but the tool state hasn't been updated yet (or
		// a new step is about to start). Reporting "idle" here causes the
		// status to flash idle→executing repeatedly. Treat an unfinished
		// assistant message as "thinking" (actively working) instead.
		if msg.Finish == "" {
			if lastToolName != "" {
				callback("executing", lastToolName, "", sessionTitle)
			} else {
				callback("thinking", "", "", sessionTitle)
			}
			return
		}

		// Otherwise, the assistant turn is complete (finish = "stop" or "completed").
		status := "idle"
		var textContent string
		for _, p := range parts {
			if p.Type == "text" {
				textContent = p.Text
			}
		}
		callback(status, "", textContent, sessionTitle)

	default:
		callback("idle", "", "", sessionTitle)
	}
}
