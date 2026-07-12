package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

var (
	taskStartRx  = regexp.MustCompile(`(?i)task id:\s*"?([a-zA-Z0-9_\-\./]+)"?`)
	taskFinishRx = regexp.MustCompile(`(?i)task id\s+"?([a-zA-Z0-9_\-\./]+)"?\s+finished`)
)

func (w *AntigravityWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Antigravity stores transcripts under ~/.gemini/antigravity-cli/brain/<conversationId>/.system_generated/logs/transcript.jsonl
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")

	const agentID = "agy"
	// On resume (--continue), the agent reattaches to a pre-existing
	// conversation whose transcript may predate this watcher. Widen the
	// search window to 1 hour so the resumed session is found. For a fresh
	// start, only look for files modified after the watcher started (no
	// negative offset).
	lookback := 30 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string
	// Re-bind bookkeeping for /new and /resume detection.
	var lastActivity time.Time
	var silentTicks int

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
			heartbeat()
			if watchedFilePath == "" {
				// Search for the most recently modified unclaimed transcript.jsonl.
				candidates, err := findAntigravityTranscripts(dir, cwd, lastCheck, agentID)
				if err == nil && len(candidates) > 0 {
					for _, c := range candidates {
						if ClaimSession(agentID, cwd, c) {
							watchedFilePath = c
							lastFileSize = 0
							lastCheck = time.Now()
							if info, err := os.Stat(watchedFilePath); err == nil {
								lastActivity = info.ModTime()
							}
							silentTicks = 0
							break
						}
					}
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
						lastActivity = info.ModTime()
						silentTicks = 0
					} else {
						silentTicks++
					}
				} else {
					// File disappeared — release claim and search again next tick.
					UnclaimSession(agentID, cwd, watchedFilePath)
					watchedFilePath = ""
					continue
				}

				// Mid-session re bind for /new and /resume.
				if silentTicks >= rebindSilenceTicks {
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second {
						cands, _ := listAntigravityCandidates(dir, cwd, lastActivity)
						var others []RebindCandidate
						for _, c := range cands {
							others = append(others, RebindCandidate{Key: c.path, ModTime: c.modTime})
						}
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSession(agentID, cwd, newKey) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								watchedFilePath = newKey
								lastFileSize = 0
								lastCheck = time.Now()
								silentTicks = 0
							}
						}
					}
				}
			}
		}
	}
}

// findAntigravityTranscripts walks the brain directory looking for the most
// recently modified transcript.jsonl files whose modification time is after
// the given threshold, optionally filtered by cwd via conversation_summaries.db,
// and skips any transcript already claimed by another watcher of the same
// agent type+cwd. Returns candidates sorted oldest-first (workspace-matched
// still first within that ordering) so the earliest-started watcher claims
// the earliest qualifying session. The caller claims the first one via
// ClaimSession — but note this helper claims *all* returned candidates, so
// it must only be used for the initial-bind path.
func findAntigravityTranscripts(brainDir string, cwd string, after time.Time, agentID string) ([]string, error) {
	cands, err := listAntigravityCandidates(brainDir, cwd, after)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, c := range cands {
		result = append(result, c.path)
	}
	return result, nil
}

// antigravityCandidate is a transcript path with its modification time, used
// both by findAntigravityTranscripts (claiming path) and the re-bind path
// (which needs modtimes without claiming).
type antigravityCandidate struct {
	path        string
	convID      string
	modTime     time.Time
	workspaceOK bool
}

// listAntigravityCandidates enumerates transcript.jsonl files under brainDir
// modified after `after`, optionally filtered by cwd, sorted oldest-first
// (workspace-matched entries first within that ordering). It does NOT claim
// any candidate; callers are responsible for calling ClaimSession.
func listAntigravityCandidates(brainDir string, cwd string, after time.Time) ([]antigravityCandidate, error) {
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
					absCwd, _ := filepath.Abs(cwd)
					absCwd = filepath.Clean(absCwd)
					for rows.Next() {
						var convID, uris string
						if rows.Scan(&convID, &uris) != nil {
							continue
						}
						var uriList []string
						if json.Unmarshal([]byte(uris), &uriList) != nil {
							continue
						}
						for _, u := range uriList {
							p := uriToPath(u)
							if p == "" {
								continue
							}
							if filepath.Clean(p) == absCwd {
								workspaceMatch[convID] = true
								break
							}
						}
					}
					rows.Close()
				}
				db.Close()
			}
		}
	}

	var cands []antigravityCandidate
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
		convID := ""
		if rel, rerr := filepath.Rel(brainDir, path); rerr == nil {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 0 {
				convID = parts[0]
			}
		}
		if cwd != "" && workspaceQueried && convID != "" {
			if known, ok := workspaceMatch[convID]; ok && !known {
				return nil
			}
		}
		cands = append(cands, antigravityCandidate{
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

	// Sort: workspace-matched first, then by modTime ASCENDING (oldest first)
	// so the earliest-started watcher claims the earliest qualifying session.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].workspaceOK != cands[j].workspaceOK {
			return cands[i].workspaceOK
		}
		return cands[i].modTime.Before(cands[j].modTime)
	})
	return cands, nil
}

func (w *AntigravityWatcher) parseAntigravityLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	// Always read from the beginning to correctly track background tasks
	lines, err := ReadNewLines(filePath, 0)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: accumulate user prompt and the final state of each step.
	var sessionTitle string
	var lastType string
	var lastToolNames []string

	runningTasks := make(map[string]bool)

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

		// Check for background tasks started in this step
		if step.Status == "RUNNING" {
			if m := taskStartRx.FindStringSubmatch(step.Content); len(m) > 1 {
				runningTasks[m[1]] = true
			}
		}
		// Also scan step content (e.g. SYSTEM_MESSAGE, PLANNER_RESPONSE, etc.) for finished tasks
		if m := taskFinishRx.FindStringSubmatch(step.Content); len(m) > 1 {
			delete(runningTasks, m[1])
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
			if len(runningTasks) > 0 {
				var activeTask string
				for t := range runningTasks {
					activeTask = t
					break
				}
				callback("executing", "background_task", activeTask, sessionTitle)
				return
			}
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
		if len(runningTasks) > 0 {
			var activeTask string
			for t := range runningTasks {
				activeTask = t
				break
			}
			callback("executing", "background_task", activeTask, sessionTitle)
			return
		}
		// Unknown step type — stay thinking.
		callback("thinking", "", "", sessionTitle)
	}
}

// uriToPath converts a file:// URI as stored in the Antigravity
// conversation_summaries.db workspace_uris column back into an OS filesystem
// path. Handles the triple-slash form ("file:///C:/...") and URL-encoded
// characters (e.g. "%20" for spaces) used by the CLI. Returns "" when the URI
// is not a file:// URI or cannot be parsed.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file:") {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	// On Windows, url.Parse leaves a leading slash before the drive letter
	// (e.g. "/C:/Users/..."); strip it so the result is a valid drive path.
	if len(p) > 2 && p[0] == '/' && (p[2] == ':' || p[2] == '|') {
		p = p[1:]
	}
	// Restore any | that the URL encoder produced for colons on some
	// platforms, then convert to the OS-native separator.
	p = strings.ReplaceAll(p, "|", ":")
	return filepath.FromSlash(p)
}

