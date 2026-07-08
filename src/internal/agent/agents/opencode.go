package agents

import (
	"context"
	"database/sql"
	"encoding/json"
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

func (w *OpenCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string)) {
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

	var lastDBMod time.Time
	var lastWALMod time.Time
	var lastReportedStatus string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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

			// Always re-parse while waiting for user input even if the DB
			// hasn't changed. This keeps lastActivity alive in the watchdog
			// so the idle-timeout never fires while a question is pending.
			if changed || lastReportedStatus == "waiting_input" {
				wrappedCallback := func(status, tool, details, title string) {
					lastReportedStatus = status
					callback(status, tool, details, title)
				}
				w.parseOpenCodeDB(dbPath, cwd, wrappedCallback)
			}
		}
	}
}

func (w *OpenCodeWatcher) parseOpenCodeDB(dbPath string, cwd string, callback func(status, tool, details, title string)) {
	// Open in read-only WAL mode to avoid interfering with the running OpenCode process.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return
	}
	defer db.Close()

	// Resolve the most-relevant session: prefer one matching the working directory,
	// fall back to the most recently updated session overall.
	var openCodeSessionID string
	var openCodeTitle string
	if cwd != "" {
		_ = db.QueryRow(
			`SELECT id, title FROM session WHERE directory = ? ORDER BY time_updated DESC LIMIT 1`,
			cwd,
		).Scan(&openCodeSessionID, &openCodeTitle)
	}
	if openCodeSessionID == "" {
		if err := db.QueryRow(
			`SELECT id, title FROM session ORDER BY time_updated DESC LIMIT 1`,
		).Scan(&openCodeSessionID, &openCodeTitle); err != nil {
			return
		}
	}

	// Retrieve the user's prompt. OpenCode stores the initial prompt as the
	// first 'text' part in the session. session_input exists in the schema but
	// is not reliably populated in all versions, so we prefer the part table.
	var userPrompt string
	var firstTextPartData string
	if err := db.QueryRow(
		`SELECT data FROM part WHERE session_id = ? AND json_extract(data,'$.type') = 'text' ORDER BY time_created ASC LIMIT 1`,
		openCodeSessionID,
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
			openCodeSessionID,
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
		openCodeSessionID,
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

		// Otherwise, the assistant turn is complete (finish = "stop" or "completed").
		// Check if the final text contains questions/approvals.
		status := "idle"
		var textContent string
		for _, p := range parts {
			if p.Type == "text" {
				textContent = p.Text
			}
		}
		if textContent != "" {
			textContentLower := strings.ToLower(textContent)
			if strings.Contains(textContentLower, "?") ||
				strings.Contains(textContentLower, "approval") ||
				strings.Contains(textContentLower, "confirm") ||
				strings.Contains(textContentLower, "approve") {
				status = "waiting_input"
			}
		}
		callback(status, "", textContent, sessionTitle)

	default:
		callback("idle", "", "", sessionTitle)
	}
}
