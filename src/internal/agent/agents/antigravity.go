package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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

func (w *AntigravityWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Antigravity stores transcripts under ~/.gemini/antigravity-cli/brain/<conversationId>/.system_generated/logs/transcript.jsonl
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")

	const agentID = "agy"
	// On resume (--continue), the agent reattaches to a pre-existing
	// conversation whose transcript may predate this watcher. Widen the
	// search window to 1 hour so the resumed session is found.
	lookback := 1 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				// Search for the most recently modified unclaimed transcript.jsonl.
				candidates, err := findAntigravityTranscripts(dir, cwd, lastCheck, agentID)
				if err == nil && len(candidates) > 0 {
					watchedFilePath = candidates[0]
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
					// File disappeared — release claim and search again next tick.
					UnclaimSession(agentID, cwd, watchedFilePath)
					watchedFilePath = ""
				}
			}
		}
	}
}

// findAntigravityTranscripts walks the brain directory looking for the most
// recently modified transcript.jsonl files whose modification time is after
// the given threshold, optionally filtered by cwd via conversation_summaries.db,
// and skips any transcript already claimed by another watcher of the same
// agent type+cwd. Returns candidates sorted by modification time, most recent
// first. The caller claims the first one via ClaimSession.
func findAntigravityTranscripts(brainDir string, cwd string, after time.Time, agentID string) ([]string, error) {
	type cand struct {
		path       string
		convID     string
		modTime    time.Time
		workspaceOK bool
	}

	// 1. Build the full set of candidate transcript files modified after the
	// threshold. We collect (path, conversationId, modTime) and, when a cwd
	// match is requested, also flag whether the conversation's workspace_uris
	// contain the target cwd.
	workspaceMatch := map[string]bool{}
	workspaceQueried := false
	if cwd != "" {
		home, _ := os.UserHomeDir()
		dbPath := filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
		if _, err := os.Stat(dbPath); err == nil {
			db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
			if err == nil {
				workspaceQueried = true
				rows, qerr := db.Query(
					`SELECT conversation_id, workspace_uris FROM conversation_summaries ORDER BY last_modified_time DESC`,
				)
				if qerr == nil {
					targetURI := "file://" + filepath.ToSlash(cwd)
					for rows.Next() {
						var convID, uris string
						if rows.Scan(&convID, &uris) != nil {
							continue
						}
						if strings.Contains(uris, "\""+targetURI+"\"") {
							workspaceMatch[convID] = true
						}
					}
					rows.Close()
				}
				db.Close()
			}
		}
	}

	// 2. Walk the brain directory collecting candidates.
	var cands []cand
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
		// Derive conversationId from path: .../brain/<convId>/.system_generated/logs/transcript.jsonl
		convID := ""
		if rel, rerr := filepath.Rel(brainDir, path); rerr == nil {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 0 {
				convID = parts[0]
			}
		}
		// When cwd filtering is active and we successfully queried the db,
		// skip transcripts that don't belong to the target workspace.
		if cwd != "" && workspaceQueried && convID != "" {
			if !workspaceMatch[convID] {
				return nil
			}
		}
		cands = append(cands, cand{
			path:        path,
			convID:      convID,
			modTime:     info.ModTime(),
			workspaceOK: workspaceMatch[convID],
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort: workspace-matched first, then by modTime descending. This keeps the
	// most relevant transcripts at the top when cwd filtering is active.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].workspaceOK != cands[j].workspaceOK {
			return cands[i].workspaceOK
		}
		return cands[i].modTime.After(cands[j].modTime)
	})

	// 3. Pick the first candidate whose path (or conversationId) is not
	// already claimed by another watcher of the same agent type+cwd.
	var result []string
	for _, c := range cands {
		key := c.path
		if ClaimSession(agentID, cwd, key) {
			result = append(result, key)
		}
	}
	return result, nil
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

		if strings.Contains(step.Content, "USER Objective:") {
			idx := strings.Index(step.Content, "USER Objective:")
			sub := step.Content[idx+len("USER Objective:"):]
			parts := strings.Split(strings.TrimSpace(sub), "\n")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				sessionTitle = strings.TrimSpace(parts[0])
			}
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

