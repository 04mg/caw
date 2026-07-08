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
	Role  string `json:"role"`
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"parts,omitempty"`
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
				wrappedCallback := func(status, tool, details, prompt string) {
					lastReportedStatus = status
					callback(status, tool, details, prompt)
				}
				w.parseOpenCodeDB(dbPath, cwd, wrappedCallback)
			}
		}
	}
}

func (w *OpenCodeWatcher) parseOpenCodeDB(dbPath string, cwd string, callback func(status, tool, details, prompt string)) {
	// Open in read-only WAL mode to avoid interfering with the running OpenCode process.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return
	}
	defer db.Close()

	// Resolve the most-relevant session: prefer one matching the working directory,
	// fall back to the most recently updated session overall.
	var openCodeSessionID string
	if cwd != "" {
		_ = db.QueryRow(
			`SELECT id FROM session WHERE directory = ? ORDER BY time_updated DESC LIMIT 1`,
			cwd,
		).Scan(&openCodeSessionID)
	}
	if openCodeSessionID == "" {
		if err := db.QueryRow(
			`SELECT id FROM session ORDER BY time_updated DESC LIMIT 1`,
		).Scan(&openCodeSessionID); err != nil {
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
			if len(userPrompt) > 200 {
				userPrompt = userPrompt[:200] + "…"
			}
		}
	}
	// Fallback: session_input table (populated in some OpenCode versions).
	if userPrompt == "" {
		_ = db.QueryRow(
			`SELECT prompt FROM session_input WHERE session_id = ? ORDER BY time_created ASC LIMIT 1`,
			openCodeSessionID,
		).Scan(&userPrompt)
		if len(userPrompt) > 200 {
			userPrompt = userPrompt[:200] + "…"
		}
	}

	// Determine the current state by inspecting the most recent part.
	// Parts are fine-grained events: step-start, tool (with running/completed state),
	// text, step-finish, patch, reasoning, etc.
	var partDataJSON string
	err = db.QueryRow(
		`SELECT data FROM part WHERE session_id = ? ORDER BY time_created DESC LIMIT 1`,
		openCodeSessionID,
	).Scan(&partDataJSON)

	if err == nil && partDataJSON != "" {
		var p openCodePart
		if json.Unmarshal([]byte(partDataJSON), &p) == nil {
			switch p.Type {
			case "tool":
				toolStatus := ""
				if p.State != nil {
					toolStatus = p.State.Status
				}
				if toolStatus == "running" || toolStatus == "pending" {
					// The "question" tool is OpenCode's mechanism to ask the user
					// a question and wait for a response. While it is running, the
					// agent is blocked waiting for user input — not executing code.
					if p.Tool == "question" {
						callback("waiting_input", "question", "", userPrompt)
						return
					}
					callback("executing", p.Tool, "", userPrompt)
					return
				}
				// Completed tool — agent is likely in between steps (thinking)
				callback("thinking", "", "", userPrompt)
				return
			case "step-start", "reasoning":
				callback("thinking", "", "", userPrompt)
				return
			case "step-finish":
				// Step just finished; wait for the next message to determine if done.
				callback("thinking", "", "", userPrompt)
				return
			case "text":
				status := "idle"
				if strings.Contains(p.Text, "?") || strings.Contains(p.Text, "approval") || strings.Contains(p.Text, "confirm") {
					status = "waiting_input"
				}
				callback(status, "", "", userPrompt)
				return
			case "patch":
				// A patch is being applied — executing.
				callback("executing", "patch", "", userPrompt)
				return
			}
		}
	}

	// Fall back to reading the latest message from the message table.
	var msgDataJSON string
	err = db.QueryRow(
		`SELECT data FROM message WHERE session_id = ? ORDER BY time_created DESC LIMIT 1`,
		openCodeSessionID,
	).Scan(&msgDataJSON)
	if err != nil {
		return
	}

	var msg openCodeMessage
	if err := json.Unmarshal([]byte(msgDataJSON), &msg); err != nil {
		return
	}

	switch msg.Role {
	case "user":
		callback("thinking", "", "", userPrompt)
	case "assistant":
		status := "idle"
		var textContent string
		for _, p := range msg.Parts {
			if p.Type == "text" {
				textContent = p.Text
			}
		}
		if strings.Contains(textContent, "?") || strings.Contains(textContent, "approval") || strings.Contains(textContent, "confirm") {
			status = "waiting_input"
		}
		callback(status, "", "", userPrompt)
	default:
		callback("idle", "", "", userPrompt)
	}
}
