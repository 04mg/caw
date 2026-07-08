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

type AntigravityWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("agy", &AntigravityWatcher{})
}

// antigravityStep mirrors one line of the transcript.jsonl written by the
// Antigravity CLI (agy). Steps are appended once complete (status: "DONE").
type antigravityStep struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	// Type is the canonical step kind: USER_INPUT, PLANNER_RESPONSE,
	// RUN_COMMAND, VIEW_FILE, GREP_SEARCH, LIST_DIRECTORY, WRITE_TO_FILE,
	// REPLACE_FILE_CONTENT, ASK_PERMISSION, ASK_QUESTION, etc.
	Type      string                  `json:"type"`
	Status    string                  `json:"status"`
	ToolCalls []antigravityToolCall   `json:"tool_calls,omitempty"`
	Content   string                  `json:"content,omitempty"`
	CreatedAt string                  `json:"created_at,omitempty"`
}

// antigravityToolCall reflects the actual JSON structure in transcript.jsonl:
//
//	{"name": "run_command", "args": {...}}
//
// (The old code incorrectly expected {"function": {"name": ...}}.)
type antigravityToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// toolStepTypes is the set of step types that represent tool execution results.
// When the last recorded step is one of these, the planner has just issued the
// call and is waiting for (or processing) the result → status "thinking".
var toolStepTypes = map[string]bool{
	"RUN_COMMAND":          true,
	"VIEW_FILE":            true,
	"GREP_SEARCH":          true,
	"LIST_DIRECTORY":       true,
	"WRITE_TO_FILE":        true,
	"REPLACE_FILE_CONTENT": true,
	"MULTI_REPLACE_FILE_CONTENT": true,
	"READ_URL_CONTENT":     true,
	"SEARCH_WEB":           true,
	"GENERATE_IMAGE":       true,
	"INVOKE_SUBAGENT":      true,
	"SEND_MESSAGE":         true,
	"MANAGE_SUBAGENTS":     true,
	"COMMAND_STATUS":       true,
	"SCHEDULE":             true,
}

// permissionStepTypes are tool steps that require user approval.
var permissionStepTypes = map[string]bool{
	"ASK_PERMISSION": true,
	"ASK_QUESTION":   true,
}

func (w *AntigravityWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Antigravity stores transcripts under ~/.gemini/antigravity-cli/brain/<conversationId>/.system_generated/logs/transcript.jsonl
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")

	lastCheck := time.Now().Add(-1 * time.Second)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				// Search for the most recently modified transcript.jsonl.
				fp, _, err := findAntigravityTranscript(dir, cwd, lastCheck)
				if err == nil && fp != "" {
					watchedFilePath = fp
					lastFileSize = 0
					// After locking on a file, only look for newer transcripts
					// from this point on (prevents switching to a new session
					// that hasn't started yet).
					lastCheck = time.Now()
				}
			}
			if watchedFilePath != "" {
				info, err := os.Stat(watchedFilePath)
				if err == nil {
					if info.Size() > lastFileSize {
						wrappedCallback := func(status, tool, details, title string) {
							if title != "" {
								sessionTitle = title
							}
							callback(status, tool, details, sessionTitle)
						}
						w.parseAntigravityLog(watchedFilePath, lastFileSize, wrappedCallback)
						lastFileSize = info.Size()
					}
				} else {
					// File disappeared — search again next tick.
					watchedFilePath = ""
				}
			}
		}
	}
}

// findAntigravityTranscript walks the brain directory looking for the most
// recently modified transcript.jsonl. When cwd is non-empty it tries to match
// the working directory using conversation_summaries.db.
func findAntigravityTranscript(brainDir string, cwd string, after time.Time) (string, time.Time, error) {
	// 1. Try to query conversation_summaries.db first for a precise workspace match
	if cwd != "" {
		home, _ := os.UserHomeDir()
		dbPath := filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
		if _, err := os.Stat(dbPath); err == nil {
			db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
			if err == nil {
				defer db.Close()
				var convID string
				targetURI := "file://" + filepath.ToSlash(cwd)
				pattern := "%\"" + targetURI + "\"%"
				err = db.QueryRow(
					`SELECT conversation_id FROM conversation_summaries WHERE workspace_uris LIKE ? ORDER BY last_modified_time DESC LIMIT 1`,
					pattern,
				).Scan(&convID)
				if err == nil && convID != "" {
					filePath := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
					if info, err := os.Stat(filePath); err == nil {
						if info.ModTime().After(after) {
							return filePath, info.ModTime(), nil
						}
					}
				}
			}
		}
	}

	// 2. Fall back to recursive walk
	var bestPath string
	var bestMod time.Time

	err := filepath.Walk(brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() != "transcript.jsonl" {
			return nil
		}
		if !info.ModTime().After(after) {
			return nil
		}
		if info.ModTime().After(bestMod) {
			bestPath = path
			bestMod = info.ModTime()
		}
		return nil
	})

	return bestPath, bestMod, err
}

func (w *AntigravityWatcher) parseAntigravityLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: accumulate user prompt and the final state of each step.
	var sessionTitle string
	var lastType string
	var lastToolNames []string

	for _, line := range lines {
		var step antigravityStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			continue
		}

		switch step.Type {
		case "USER_INPUT":
			// The content field holds the raw user message (may include XML
			// wrapper tags — strip them for display).
			p := CleanPrompt(step.Content)
			if p != "" && sessionTitle == "" {
				sessionTitle = p
			}
		case "PLANNER_RESPONSE":
			lastType = step.Type
			lastToolNames = nil
			for _, tc := range step.ToolCalls {
				if tc.Name != "" {
					lastToolNames = append(lastToolNames, tc.Name)
				}
			}
		default:
			if step.Type != "" {
				lastType = step.Type
			}
		}
	}

	// Determine current status from the last step type written to the transcript.
	// Because steps are only written once complete ("DONE"), the last written
	// step tells us what just finished, which implies what the agent is doing now.
	switch lastType {
	case "USER_INPUT":
		// The user just sent a message; planner hasn't responded yet.
		callback("thinking", "", "", sessionTitle)

	case "PLANNER_RESPONSE":
		if len(lastToolNames) == 0 {
			// PLANNER_RESPONSE with no tool calls is a final answer → idle.
			callback("idle", "", "", sessionTitle)
			return
		}
		// Check for permission / question tools — agent needs user input.
		for _, name := range lastToolNames {
			nameLower := strings.ToLower(name)
			if nameLower == "ask_permission" || nameLower == "ask_question" {
				callback("waiting_input", name, "", sessionTitle)
				return
			}
		}
		// Planner issued tool calls; tool results not yet written → executing.
		callback("executing", lastToolNames[0], "", sessionTitle)

	default:
		if permissionStepTypes[lastType] {
			// The permission/question tool itself just completed — still need input
			// until the next PLANNER_RESPONSE is written.
			callback("waiting_input", strings.ToLower(lastType), "", sessionTitle)
			return
		}
		if toolStepTypes[lastType] {
			// A tool result was just written; the planner is about to respond.
			callback("thinking", "", "", sessionTitle)
			return
		}
		// Unknown step type — stay thinking.
		callback("thinking", "", "", sessionTitle)
	}
}

