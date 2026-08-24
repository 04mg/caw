package agents

import (
	"bytes"
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
	_ "modernc.org/sqlite"
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
	taskStartRx   = regexp.MustCompile(`(?i)task id:\s*"?([a-zA-Z0-9_\-\./]+)"?`)
	taskFinishRx  = regexp.MustCompile(`(?i)task id\s+"?([a-zA-Z0-9_\-\./]+)"?\s+finished`)
	taskStatusRx  = regexp.MustCompile(`(?i)task:\s*"?([a-zA-Z0-9_\-\./]+)"?`)
	taskSenderRx  = regexp.MustCompile(`(?i)sender=([a-zA-Z0-9_\-\./]+)`)
)

func (w *AntigravityWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Antigravity stores transcripts under ~/.gemini/antigravity-cli/brain/<conversationId>/.system_generated/logs/transcript.jsonl
	// and per-conversation database files under ~/.gemini/antigravity-cli/conversations/<conversationId>.db.
	// Because storage is located in the central ~/.gemini/antigravity-cli tree without per-cwd subdirectories,
	// claims are keyed globally (claimCwd = "") so that two Antigravity instances across any workspace in Caw
	// never bind or track the same conversation transcript.
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")

	const agentID = "agy"
	const claimCwd = ""
	// On resume (--continue / --conversation), the agent reattaches to a pre-existing
	// conversation whose transcript may predate this watcher. Widen the search window
	// to 1 hour so the resumed session is found. For a fresh start, look back 10s to
	// avoid missing a transcript created slightly before the watcher started.
	lookback := 10 * time.Second
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

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
	}

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, claimCwd, watchedFilePath)
		}
	}()

	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			UnclaimSession(agentID, claimCwd, watchedFilePath)
			watchedFilePath = ""
			if notifyCh != nil {
				notifier.Watch("")
			}
			return false
		}
		if info.Size() <= lastFileSize {
			return false
		}
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
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			readWatched()
		case <-ticker.C:
			heartbeat()
			if watchedFilePath == "" {
				// Prefer the exact conversation id persisted for this leaf by
				// a previous Caw process so a reopened pane follows its own
				// conversation when several panes share a cwd.
				if exact := agent.PersistedExternalSession(sessionID); exact != "" && antigravityConversationMatchesWorkspace(exact, cwd) {
					if exactPath := antigravityTranscriptForConversation(dir, exact); exactPath != "" && ClaimSessionForLeaf(agentID, claimCwd, exactPath, sessionID) {
						watchedFilePath = exactPath
						lastFileSize = 0
						lastCheck = time.Now()
						if info, err := os.Stat(watchedFilePath); err == nil {
							lastActivity = info.ModTime()
						}
						silentTicks = 0
						if notifyCh != nil {
							notifier.Watch(watchedFilePath)
						}
						agent.RecordExternalSession(sessionID, exact)
					}
				}
				if watchedFilePath == "" {
					// Search for the earliest unclaimed transcript.jsonl matching this workspace.
					candidates, err := findAntigravityTranscripts(dir, cwd, lastCheck, agentID)
					if err == nil && len(candidates) > 0 {
						for _, c := range candidates {
							if ClaimSessionForLeaf(agentID, claimCwd, c, sessionID) {
								watchedFilePath = c
								lastFileSize = 0
								lastCheck = time.Now()
								if info, err := os.Stat(watchedFilePath); err == nil {
									lastActivity = info.ModTime()
								}
								silentTicks = 0
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
								if conv := antigravityConversationIDForTranscript(dir, c); conv != "" {
									agent.RecordExternalSession(sessionID, conv)
								}
								break
							}
						}
					}
				}
			}
			if watchedFilePath != "" {
				if !readWatched() {
					silentTicks++
				}

				// Mid-session re-bind for /new and /resume. Gated on PTY
				// activity OR user focus: only the watcher whose PTY is producing
				// output (or whose pane the user is currently driving) switches,
				// so a sibling Antigravity writing to its own transcript can't
				// make this idle, unfocused watcher steal its session. The focus
				// exemption covers a /new or /resume issued in the focused pane
				// before the agent emits any PTY output.
				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						cands, _ := listAntigravityCandidates(dir, cwd, lastActivity)
						var others []RebindCandidate
						for _, c := range cands {
							if cwd != "" && !c.workspaceOK {
								continue
							}
							others = append(others, RebindCandidate{Key: c.path, ModTime: c.modTime})
						}
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSessionForLeaf(agentID, claimCwd, newKey, sessionID) {
								UnclaimSession(agentID, claimCwd, watchedFilePath)
								watchedFilePath = newKey
								lastFileSize = 0
								lastCheck = time.Now()
								silentTicks = 0
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
								if conv := antigravityConversationIDForTranscript(dir, newKey); conv != "" {
									agent.RecordExternalSession(sessionID, conv)
								}
							}
						}
					}
				}
			}
		}
	}
}

// antigravityConversationIDForTranscript returns the conversation id (the
// immediate subdirectory under the brain dir) that owns the given transcript,
// or "" when it cannot be determined.
func antigravityConversationIDForTranscript(brainDir, transcriptPath string) string {
	if brainDir == "" || transcriptPath == "" {
		return ""
	}
	rel, err := filepath.Rel(brainDir, transcriptPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." || strings.HasPrefix(parts[0], "..") {
		return ""
	}
	return parts[0]
}

// antigravityTranscriptForConversation returns the transcript.jsonl path for
// the given conversation id, or "" if it does not exist.
func antigravityTranscriptForConversation(brainDir, convID string) string {
	if convID == "" {
		return ""
	}
	p := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

// extractWorkspaceURIsFromBlob scans a binary blob (such as trajectory_metadata_blob
// in an Antigravity conversation database) for file:// URIs encoded as protobuf strings.
func extractWorkspaceURIsFromBlob(data []byte) []string {
	var uris []string
	prefix := []byte("file://")
	idx := 0
	for {
		pos := bytes.Index(data[idx:], prefix)
		if pos == -1 {
			break
		}
		actualPos := idx + pos
		if actualPos > 0 {
			var length int
			var shift uint
			varintStart := actualPos - 1
			if data[varintStart] < 0x80 {
				length = int(data[varintStart])
			} else {
				start := varintStart
				for start > 0 && data[start-1]&0x80 != 0 {
					start--
				}
				for i := start; i <= varintStart; i++ {
					b := data[i]
					length |= int(b&0x7f) << shift
					shift += 7
				}
			}
			if length > 0 && actualPos+length <= len(data) {
				uri := string(data[actualPos : actualPos+length])
				if strings.HasPrefix(uri, "file://") {
					uris = append(uris, uri)
				}
			}
		}
		idx = actualPos + len(prefix)
	}
	return uris
}

// antigravityConversationWorkspace finds all workspace filesystem paths associated
// with a given conversation ID. It inspects:
// 1. ~/.gemini/antigravity-cli/conversations/<convID>.db (trajectory_metadata_blob) - real-time
// 2. ~/.gemini/antigravity-cli/conversation_summaries.db (workspace_uris) - fallback
func antigravityConversationWorkspace(convID string) []string {
	if convID == "" {
		return nil
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	var foundPaths []string
	seen := make(map[string]bool)
	addPath := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if !seen[clean] {
			seen[clean] = true
			foundPaths = append(foundPaths, clean)
		}
	}

	// 1. Check real-time per-conversation database.
	dbPath := filepath.Join(home, ".gemini", "antigravity-cli", "conversations", convID+".db")
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
		if err == nil {
			var blob []byte
			_ = db.QueryRow(`SELECT data FROM trajectory_metadata_blob WHERE id = 'main'`).Scan(&blob)
			if len(blob) > 0 {
				uris := extractWorkspaceURIsFromBlob(blob)
				for _, u := range uris {
					addPath(uriToPath(u))
				}
			}
			db.Close()
		}
	}

	if len(foundPaths) > 0 {
		return foundPaths
	}

	// 2. Fallback to conversation_summaries.db (written on session finish/summary).
	summariesPath := filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
	if info, err := os.Stat(summariesPath); err == nil && !info.IsDir() {
		db, err := sql.Open("sqlite", "file:"+summariesPath+"?mode=ro&_journal_mode=WAL")
		if err == nil {
			var urisJSON string
			_ = db.QueryRow(`SELECT workspace_uris FROM conversation_summaries WHERE conversation_id = ?`, convID).Scan(&urisJSON)
			if urisJSON != "" {
				var uriList []string
				if json.Unmarshal([]byte(urisJSON), &uriList) == nil {
					for _, u := range uriList {
						addPath(uriToPath(u))
					}
				}
			}
			db.Close()
		}
	}

	return foundPaths
}

// antigravityConversationMatchesWorkspace reports whether the given conversation ID
// belongs to the specified workspace directory (cwd). If cwd is empty, returns true.
// If the conversation's workspace is unknown, returns true.
func antigravityConversationMatchesWorkspace(convID, cwd string) bool {
	if convID == "" {
		return false
	}
	if cwd == "" {
		return true
	}
	absCwd, err := filepath.Abs(cwd)
	if err == nil {
		absCwd = filepath.Clean(absCwd)
	} else {
		absCwd = filepath.Clean(cwd)
	}
	wsPaths := antigravityConversationWorkspace(convID)
	if len(wsPaths) == 0 {
		return true
	}
	for _, p := range wsPaths {
		if p == absCwd {
			return true
		}
	}
	return false
}

// findAntigravityTranscripts walks the brain directory looking for the most
// recently modified transcript.jsonl files whose modification time is after
// the given threshold, filtered by cwd, and returns candidates sorted oldest-first.
func findAntigravityTranscripts(brainDir string, cwd string, after time.Time, agentID string) ([]string, error) {
	cands, err := listAntigravityCandidates(brainDir, cwd, after)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, c := range cands {
		if cwd != "" && !c.workspaceOK {
			continue
		}
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
// modified after `after`, filtered by cwd, sorted oldest-first
// (workspace-matched entries first within that ordering). It does NOT claim
// any candidate; callers are responsible for calling ClaimSessionForLeaf.
func listAntigravityCandidates(brainDir string, cwd string, after time.Time) ([]antigravityCandidate, error) {
	var absCwd string
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err == nil {
			absCwd = filepath.Clean(abs)
		} else {
			absCwd = filepath.Clean(cwd)
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
		if convID == "" {
			return nil
		}

		workspaceOK := false
		if absCwd != "" {
			wsPaths := antigravityConversationWorkspace(convID)
			if len(wsPaths) > 0 {
				for _, p := range wsPaths {
					if p == absCwd {
						workspaceOK = true
						break
					}
				}
				// If the workspace is known for this conversation and does not match absCwd, skip it!
				if !workspaceOK {
					return nil
				}
			}
		}

		cands = append(cands, antigravityCandidate{
			path:        path,
			convID:      convID,
			modTime:     info.ModTime(),
			workspaceOK: workspaceOK,
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
	// pendingArtifactApproval is set when a PLANNER_RESPONSE issues a
	// write_to_file whose ArtifactMetadata carries RequestFeedback=true. This
	// is Antigravity's plan/artifact-approval flow: the agent ends its turn
	// and waits for the user to approve or reject the artifact. It is cleared
	// once a new USER_INPUT starts a fresh turn.
	pendingArtifactApproval := false

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
				sessionTitle = CleanPrompt(parts[0])
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
			// A new user turn starts after an artifact-approval request; the
			// user has responded, so the pending approval is resolved.
			pendingArtifactApproval = false
		case "PLANNER_RESPONSE":
			lastType = step.Type
			lastToolNames = nil
			for _, tc := range step.ToolCalls {
				if tc.Name != "" {
					lastToolNames = append(lastToolNames, tc.Name)
				}
				if tc.Name == "write_to_file" && antigravityArtifactRequestsFeedback(tc.Args) {
					pendingArtifactApproval = true
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
		// Scan for finished/cancelled tasks in SYSTEM_MESSAGE content.
		if m := taskFinishRx.FindStringSubmatch(step.Content); len(m) > 1 {
			delete(runningTasks, m[1])
		}
		// SYSTEM_MESSAGE cancellation: "sender=<task-id> ... was cancelled"
		// The task id is in the sender= field, not as "task id ... finished".
		if step.Type == "SYSTEM_MESSAGE" {
			if ms := taskSenderRx.FindStringSubmatch(step.Content); len(ms) > 1 {
				if strings.Contains(strings.ToLower(step.Content), "cancelled") ||
					strings.Contains(strings.ToLower(step.Content), "canceled") {
					delete(runningTasks, ms[1])
				}
			}
		}
		// GENERIC steps report "Task: <id>\nStatus: DONE|RUNNING" — when the
		// status is DONE, the task has completed and should be removed from
		// the running set.
		if step.Type == "GENERIC" && step.Status == "DONE" {
			if mt := taskStatusRx.FindStringSubmatch(step.Content); len(mt) > 1 {
				if strings.Contains(step.Content, "Status: DONE") {
					delete(runningTasks, mt[1])
				}
			}
			// manage_task's empty result is the authoritative task list. It can
			// omit a completion notification for a scheduled task, so clear any
			// stale task ids previously observed in the transcript.
			if strings.Contains(strings.ToLower(step.Content), "no background tasks are currently running") {
				clear(runningTasks)
			}
		}
	}

	// Determine current status from the last step type written to the transcript.
	// Because steps are only written once complete ("DONE"), the last written
	// step tells us what just finished, which implies what the agent is doing now.
	//
	// Once a PLANNER_RESPONSE with no tool calls is seen, the agent has given
	// a final answer and is idle. Subsequent SYSTEM_MESSAGE or GENERIC steps
	// (e.g. session metadata, task notifications) must NOT override this idle
	// state — only a new USER_INPUT (which starts a new turn) should.

	// Detect an interrupt (user cancelled the in-flight planner turn) and a
	// tool-call failure from the last written step. Antigravity doesn't write
	// a dedicated interrupt step; an interrupt surfaces as a SYSTEM_MESSAGE or
	// GENERIC step whose content references the planner being
	// "interrupted"/"cancelled"/"canceled". A tool failure surfaces as a tool
	// step (RUN_COMMAND, VIEW_FILE, ...) whose content begins with an error
	// marker (e.g. "Error:", "Command failed", "ENOENT").
	lastStepErrTool := ""
	lastStepErrText := ""
	interrupted := false
	if len(lines) > 0 {
		var lastStep antigravityStep
		if json.Unmarshal([]byte(lines[len(lines)-1]), &lastStep) == nil {
			lc := strings.ToLower(lastStep.Content)
			if strings.Contains(lc, "interrupted") ||
				((strings.Contains(lc, "cancelled") || strings.Contains(lc, "canceled")) &&
					!taskSenderRx.MatchString(lastStep.Content)) {
				interrupted = true
			}
			if toolStepTypes[lastStep.Type] {
				if strings.Contains(lc, "error") || strings.Contains(lc, "failed") ||
					strings.Contains(lc, "enoent") || strings.Contains(lc, "no such file") {
					lastStepErrTool = strings.ToLower(lastStep.Type)
					lastStepErrText = strings.TrimSpace(firstLine(lastStep.Content))
					if lastStepErrText == "" {
						lastStepErrText = "tool call failed"
					}
				}
			}
		}
	}

	// An interrupt takes precedence: the user cancelled, so surface it with a
	// red dot and no push notification.
	if interrupted {
		callback("interrupted", "", "", sessionTitle)
		return
	}
	// A tool failure surfaces with a red dot; the planner continues afterward.
	if lastStepErrTool != "" {
		callback("tool_failed", lastStepErrTool, lastStepErrText, sessionTitle)
		return
	}

	seenFinalAnswer := false
	switch lastType {
	case "USER_INPUT":
		// The user just sent a message; planner hasn't responded yet.
		callback("thinking", "", "", sessionTitle)

	case "PLANNER_RESPONSE":
		if len(lastToolNames) == 0 {
			// A PLANNER_RESPONSE with no tool calls is a final answer: the
			// planner's turn is over. When that answer accompanies a pending
			// artifact-approval request (write_to_file with RequestFeedback),
			// the agent is blocked waiting for the user to approve the plan →
			// waiting_input. Otherwise it is idle. A still-running background
			// task does NOT make this "executing": the agent has finished
			// speaking and will be re-prompted when the task completes.
			if pendingArtifactApproval {
				callback("waiting_input", "write_to_file", "", sessionTitle)
				return
			}
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
		// A PLANNER_RESPONSE that requests artifact feedback (write_to_file
		// with RequestFeedback) ends the turn and waits for the user, even
		// though it carries a tool call.
		if pendingArtifactApproval {
			callback("waiting_input", "write_to_file", "", sessionTitle)
			return
		}
		// Planner issued tool calls; tool results not yet written → executing.
		callback("executing", lastToolNames[0], "", sessionTitle)

	default:
		// Scan backwards to find the last PLANNER_RESPONSE. If it had no
		// tool calls, the agent already gave a final answer and is idle —
		// any subsequent SYSTEM_MESSAGE/GENERIC steps should not override.
		for i := len(lines) - 1; i >= 0; i-- {
			var step antigravityStep
			if json.Unmarshal([]byte(lines[i]), &step) != nil {
				continue
			}
			if step.Type == "USER_INPUT" {
				// A new user turn started after the last PLANNER_RESPONSE —
				// the agent is thinking (waiting for planner to respond).
				break
			}
			if step.Type == "PLANNER_RESPONSE" {
				if len(step.ToolCalls) == 0 {
					seenFinalAnswer = true
				}
				break
			}
		}

		if seenFinalAnswer {
			if pendingArtifactApproval {
				callback("waiting_input", "write_to_file", "", sessionTitle)
				return
			}
			callback("idle", "", "", sessionTitle)
			return
		}

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

// antigravityArtifactRequestsFeedback reports whether a write_to_file tool
// call's args request user feedback on the artifact it creates. Antigravity's
// plan-approval flow sets ArtifactMetadata to a JSON object with a truthy
// RequestFeedback field; when present, the agent ends its turn and waits for
// the user to approve or reject the artifact.
func antigravityArtifactRequestsFeedback(args map[string]any) bool {
	if args == nil {
		return false
	}
	raw, ok := args["ArtifactMetadata"]
	if !ok || raw == nil {
		return false
	}
	var metadata string
	switch v := raw.(type) {
	case string:
		metadata = v
	case map[string]any:
		if b, ok := v["RequestFeedback"].(bool); ok {
			return b
		}
		return false
	default:
		return false
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(metadata), &parsed) != nil {
		return false
	}
	b, _ := parsed["RequestFeedback"].(bool)
	return b
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

// firstLine returns the first non-empty line of s, trimmed. Used to extract a
// short error message from a tool step's content blob.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
