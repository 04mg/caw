package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

// CommandCodeWatcher tracks Command Code (commandcode.ai) sessions by
// watching its append-only JSONL transcripts under ~/.commandcode/projects.
type CommandCodeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("commandcode", &CommandCodeWatcher{})
}

// commandCodeHeader mirrors the first JSONL line of a session transcript:
// {"type":"session","version":N,"id":<id>,"timestamp":<iso>,"cwd":<cwd>}.
type commandCodeHeader struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Cwd  string `json:"cwd"`
}

// commandCodeEntry mirrors a transcript entry. Entry ids form a tree via
// parentId; each line is appended when a turn commits, so the last message
// entry reflects the most recent durable state.
type commandCodeEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Message   *commandCodeMsg `json:"message,omitempty"`
	// session_info entries carry an optional display name (set by /rename).
	Name string `json:"name,omitempty"`
}

type commandCodeMsg struct {
	Role    string             `json:"role"`
	Content []commandCodeBlock `json:"content"`
}

// commandCodeBlock mirrors the Anthropic-style content blocks Command Code
// stores in message entries: {"type":"text","text":...},
// {"type":"tool_use","id","name","input"}, or
// {"type":"tool_result","tool_use_id","content","is_error"?}.
type commandCodeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// Command Code persists each model iteration to the transcript only when the
// iteration completes: the assistant tool_use message and its tool_result are
// written together, and nothing is appended while a tool is still running (a
// long-running tool freezes the transcript for minutes). The pane's PTY and
// the sidecar checkpoints file are the only live signals while the transcript
// is frozen: Command Code repaints its status line continuously while a turn
// is in flight, goes static when it blocks on the user (ask_user_question /
// exit_plan_mode), and appends a checkpoint when a new turn is submitted.
//
// ptyWorkingWindow: how recently the PTY must have produced output for the
// agent to count as actively working.
// inputWaitThreshold: how long the PTY must have been quiet mid-turn before we
// conclude Command Code is blocked awaiting a user answer.
const (
	ptyWorkingWindow   = 5 * time.Second
	inputWaitThreshold = 10 * time.Second
)

func (w *CommandCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".commandcode", "projects")
	const agentID = "commandcode"
	// Command Code catalogs sessions per project under ~/.commandcode/projects
	// keyed by a slug of the working directory. Rather than replicating the
	// slug encoding, candidate files are matched by the cwd stored in each
	// transcript header. Claims are keyed globally per agent like the Codex
	// watcher: every Command Code pane shares one claim namespace regardless of
	// its Caw-side cwd, but a session file is only ever claimed by a watcher
	// whose cwd matches the file's header cwd.
	const claimCwd = ""
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	// transcriptMessageCount tracks how many message entries have been
	// committed to the transcript so far. Command Code appends a checkpoint to
	// the sidecar the moment a turn is submitted but only commits the user's
	// prompt to the transcript when the turn's first iteration completes, so a
	// checkpoint messageCount at or above this count signals a pending turn.
	var transcriptMessageCount int64
	var watchedFilePath string
	var sessionTitle string
	var lastActivity time.Time
	var silentTicks int
	// lastReportedStatus tracks the status most recently sent to the UI so
	// the PTY-interrupt pass (below) can tell whether the turn is working.
	var lastReportedStatus string
	// lastWorkingAt marks when the current working turn began. PTY input is
	// handled independently from transcript polling, so an interrupt sent
	// while the card was idle can otherwise be observed after a new prompt has
	// already made the card working. Only interrupts sent during this turn can
	// cancel it.
	var lastWorkingAt time.Time
	// PTY-interrupt detection. Command Code never persists an interrupted
	// turn (an in-flight turn is dropped from the transcript), so
	// transcript-only detection leaves the card showing the stale working
	// state after a Ctrl+C. We therefore also watch for the interrupt byte
	// the user sends into the Command Code PTY and report "interrupted"
	// immediately while the turn is working. interruptApplied stays sticky
	// until a genuinely NEW user prompt lands past interruptBoundarySize,
	// because the transcript may keep showing the pre-interrupt working
	// state until the next prompt is written.
	// Ignore an interrupt that predates this watcher. A reopened Command Code
	// session can inherit its terminal leaf's previous Ctrl+C, but that says
	// nothing about the restored conversation.
	lastInterruptSeen := agent.LastPtyInterrupt(sessionID)
	var interruptApplied bool
	var interruptBoundarySize int64

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
		// The first read consumes a pre-existing transcript when reopening a
		// session. Its final user message is historical, not proof that the
		// restored session is currently running. Subsequent appended messages
		// still report their normal live status.
		initialRead := lastFileSize == 0
		// New agent activity past the interrupt boundary clears the sticky
		// interrupt. Command Code never persists an interrupted turn, so the
		// transcript stops growing after a real interrupt until the user
		// starts a new turn; conversely, new assistant work (tool_use/text)
		// or a fresh plain-text prompt means the agent is running again and
		// the card must reflect the real state instead of staying stuck on
		// "interrupted". Checked before parse so the override below does not
		// re-apply to the resumed turn.
		if interruptApplied && hasNewCommandCodeWork(watchedFilePath, interruptBoundarySize) {
			interruptApplied = false
		}
		wrappedCallback := func(status, tool, details, title string) {
			if title != "" {
				sessionTitle = title
			}
			if resume && initialRead {
				switch status {
				case "thinking", "executing", "tool_failed", "waiting_input":
					status, tool, details = "idle", "", ""
				}
			}
			if interruptApplied && (status == "thinking" || status == "executing" || status == "tool_failed") {
				status = "interrupted"
			}
			switch status {
			case "thinking", "executing", "tool_failed":
				lastWorkingAt = time.Now()
			}
			lastReportedStatus = status
			callback(status, tool, details, sessionTitle)
		}
		transcriptMessageCount += int64(w.parseCommandCodeLog(watchedFilePath, lastFileSize, cwd, sessionID, wrappedCallback))
		lastFileSize = info.Size()
		lastActivity = info.ModTime()
		silentTicks = 0
		return true
	}

	// matchesCwd reports whether a transcript file belongs to the given cwd
	// by reading its header line. Corrupt/unreadable files are skipped.
	matchesCwd := func(path, wantCwd string) bool {
		if wantCwd == "" {
			return true
		}
		head, err := ReadFirstLine(path)
		if err != nil {
			return false
		}
		var h commandCodeHeader
		if json.Unmarshal([]byte(head), &h) != nil {
			return false
		}
		return h.Type == "session" && h.Cwd == wantCwd
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
				candidates, err := FindEarliestFiles(dir, ".jsonl", lastCheck)
				if err != nil {
					continue
				}
				for _, c := range candidates {
					if !isCommandCodeTranscript(c.Path) || !matchesCwd(c.Path, cwd) {
						continue
					}
					if ClaimSession(agentID, claimCwd, c.Path) {
						watchedFilePath = c.Path
						lastFileSize = 0
						transcriptMessageCount = 0
						lastCheck = time.Now()
						lastActivity = c.ModTime
						silentTicks = 0
						if notifyCh != nil {
							notifier.Watch(watchedFilePath)
						}
						break
					}
				}
			}
			if watchedFilePath != "" {
				advanced := readWatched()
				if !advanced {
					silentTicks++
				}

				// PTY-interrupt detection (see the interruptApplied comment
				// above): a fresh Ctrl+C while the turn is working
				// immediately flips the card to "interrupted" instead of
				// waiting for a transcript marker Command Code never writes
				// for an interrupted turn.
				last := agent.LastPtyInterrupt(sessionID)
				if last.After(lastInterruptSeen) {
					lastInterruptSeen = last
					if last.After(lastWorkingAt) &&
						(lastReportedStatus == "thinking" || lastReportedStatus == "executing" || lastReportedStatus == "tool_failed") {
						interruptApplied = true
						lastReportedStatus = "interrupted"
						interruptBoundarySize = lastFileSize
						callback("interrupted", "", "", sessionTitle)
					}
				}

				// Live-state override for frozen transcripts (see the
				// persistence note near the constants above). Command Code
				// commits a user prompt to the transcript only when the turn's
				// first iteration completes, and it persists ask/plan tool
				// calls together with their result only once answered, so a
				// blocked or freshly submitted turn leaves the transcript
				// frozen at the previous state (e.g. "idle" while a long tool
				// is actually running). While frozen, the PTY is the only live
				// signal: repaints mean the agent is working; sustained
				// silence while a turn is active means it is blocked awaiting
				// the user (ask_user_question / exit_plan_mode).
				if !advanced {
					lastPty := agent.LastPtyActivity(sessionID)
					if !lastPty.IsZero() {
						pendingTurn := commandCodeCheckpointCount(watchedFilePath) >= transcriptMessageCount
						active := pendingTurn
						switch lastReportedStatus {
						case "thinking", "executing", "tool_failed", "waiting_input":
							active = true
						}
						if active && lastReportedStatus != "interrupted" {
							if time.Since(lastPty) >= inputWaitThreshold {
								if lastReportedStatus != "waiting_input" {
									lastReportedStatus = "waiting_input"
									callback("waiting_input", "ask_user_question", "", sessionTitle)
								}
							} else if time.Since(lastPty) < ptyWorkingWindow &&
								(lastReportedStatus == "idle" || lastReportedStatus == "" || lastReportedStatus == "waiting_input") {
								lastReportedStatus = "thinking"
								callback("thinking", "", "", sessionTitle)
							}
						}
					}
				}

				// Mid-session re-bind for /new and /resume. Gated on PTY
				// activity OR user focus so an idle sibling pane never steals
				// a session from an active one. Shares the codex-style
				// ShouldRebind heuristic over the project transcript pool.
				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						cands, _ := FindLatestFiles(dir, ".jsonl", lastActivity)
						var others []RebindCandidate
						for _, c := range cands {
							if !isCommandCodeTranscript(c.Path) || !matchesCwd(c.Path, cwd) {
								continue
							}
							others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
						}
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSession(agentID, claimCwd, newKey) {
								UnclaimSession(agentID, claimCwd, watchedFilePath)
								watchedFilePath = newKey
								lastFileSize = 0
								transcriptMessageCount = 0
								lastCheck = time.Now()
								silentTicks = 0
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
							}
						}
					}
				}
			}
		}
	}
}

// isCommandCodeTranscript excludes sidecar files (checkpoints, prompts) and
// migration backups from the candidate pool.
func isCommandCodeTranscript(path string) bool {
	name := filepath.Base(path)
	if strings.Contains(name, ".checkpoints.") || strings.Contains(name, ".prompts.") || strings.Contains(name, ".v2.bak") {
		return false
	}
	return strings.HasSuffix(name, ".jsonl")
}

// commandCodeTitleFromMeta reads the display title from the session's
// sidecar <id>.meta.json, falling back to "" when absent.
func commandCodeTitleFromMeta(path string) string {
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	head, err := ReadFileHead(metaPath, 4096)
	if err != nil {
		return ""
	}
	var meta struct {
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(head), &meta) != nil {
		return ""
	}
	return meta.Title
}

// parseCommandCodeLog reads the transcript bytes appended since offset and
// reports the derived status. It returns the number of message entries parsed,
// which the watcher accumulates to track how many messages have been committed
// to the transcript (used to detect turns that are pending their first commit).
func (w *CommandCodeWatcher) parseCommandCodeLog(filePath string, offset int64, cwd, sessionID string, callback func(status, tool, details, title string)) int {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return 0
	}

	// Prefer the persisted title from the sidecar meta file; fall back to the
	// first plain user prompt below.
	sessionTitle := commandCodeTitleFromMeta(filePath)

	// Parse all lines, skipping the session header, keeping message entries in
	// order so both the title scan (first user text) and the status scan (last
	// meaningful message) can run over the same slice.
	var entries []commandCodeEntry
	var userText string
	for _, line := range lines {
		var e commandCodeEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Type == "session" {
			continue
		}
		if e.Type == "session_info" {
			if sessionTitle == "" && e.Name != "" {
				sessionTitle = e.Name
			}
			continue
		}
		if e.Type == "message" && e.Message != nil {
			entries = append(entries, e)
			if e.Message.Role == "user" && userText == "" {
				userText = commandCodeUserText(e.Message.Content)
			}
		}
	}
	if sessionTitle == "" {
		sessionTitle = CleanPrompt(userText)
	}

	// Walk entries backwards to find the last meaningful message.
	//
	// Command Code persists each iteration's tool call together with its
	// result once the tool finishes, so an assistant tool_use whose id has no
	// matching tool_result anywhere is a tool that has NOT run yet: the agent
	// is paused waiting for the user to approve/answer the request. Build the
	// set of answered tool_use ids first so that check has full transcript
	// visibility regardless of scan order.
	answered := make(map[string]bool)
	for _, e := range entries {
		if e.Message == nil {
			continue
		}
		for _, b := range e.Message.Content {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				answered[b.ToolUseID] = true
			}
		}
	}

	var status, tool, details string
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		msg := e.Message
		if msg == nil {
			continue
		}
		switch msg.Role {
		case "assistant":
			var lastTool string
			var lastToolID string
			var hasText bool
			var failedErr string
			for _, b := range msg.Content {
				switch b.Type {
				case "tool_use":
					lastTool = b.Name
					lastToolID = b.ID
					if lastTool == "" {
						lastTool = "exec"
					}
				case "text":
					if b.Text != "" {
						hasText = true
					}
				case "tool_result":
					if b.IsError && b.ToolUseID != "" && failedErr == "" {
						failedErr = "tool call failed"
					}
				}
			}
			if lastTool != "" {
				if isUserInputTool(strings.ToLower(lastTool)) {
					status, tool, details = "waiting_input", lastTool, ""
				} else if lastToolID != "" && !answered[lastToolID] {
					// The tool request was persisted but never produced a
					// result: Command Code is blocked on user approval
					// (e.g. Execute Shell Command). Report need-input, not
					// executing.
					status, tool, details = "waiting_input", lastTool, ""
				} else if failedErr != "" {
					status, tool, details = "tool_failed", lastTool, failedErr
				} else {
					status, tool, details = "executing", lastTool, ""
				}
				break
			}
			if hasText {
				status, tool, details = "idle", "", commandCodeText(msg.Content)
				break
			}
			status, tool, details = "idle", "", ""
			break
		case "user":
			// A pure tool-result message means the agent just ran a tool and
			// is now processing the output. A plain text message is a fresh
			// prompt awaiting a response. Either way the agent is working.
			if failedToolResult(msg.Content) {
				toolName := failedToolName(msg.Content)
				errText := failedToolError(msg.Content)
				if toolName != "" {
					status, tool, details = "tool_failed", toolName, errText
					break
				}
			}
			status, tool, details = "thinking", "", ""
			break
		default:
			continue
		}
		break
	}
	if status == "" {
		status = "idle"
	}

	callback(status, tool, details, sessionTitle)
	return len(entries)
}

// commandCodeCheckpointCount returns the highest messageCount recorded in the
// session's sidecar checkpoints file (<id>.checkpoints.jsonl), or -1 when the
// sidecar is missing. Command Code appends a checkpoint the instant a turn is
// submitted; its messageCount reflects the messages committed up to that
// moment, so a count at or above the committed transcript means a turn whose
// prompt has not yet been flushed to the transcript (see the persistence note
// near the Watch constants above).
func commandCodeCheckpointCount(filePath string) int64 {
	ckptPath := strings.TrimSuffix(filePath, ".jsonl") + ".checkpoints.jsonl"
	lines, err := ReadNewLines(ckptPath, 0)
	if err != nil {
		return -1
	}
	var maxCount int64 = -1
	for _, line := range lines {
		var c struct {
			MessageCount int64 `json:"messageCount"`
		}
		if json.Unmarshal([]byte(line), &c) != nil {
			continue
		}
		if c.MessageCount > maxCount {
			maxCount = c.MessageCount
		}
	}
	return maxCount
}

// commandCodeUserText extracts the plain text of a user message, ignoring
// tool_result blocks.
func commandCodeUserText(blocks []commandCodeBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

// commandCodeText joins the visible text blocks of an assistant message for
// the card Details line.
func commandCodeText(blocks []commandCodeBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

// failedToolResult reports whether a user message carries a tool_result block
// marked as an error (the tool call failed).
func failedToolResult(blocks []commandCodeBlock) bool {
	for _, b := range blocks {
		if b.Type == "tool_result" && b.IsError {
			return true
		}
	}
	return false
}

// failedToolName resolves the name of the tool whose result failed by scanning
// for a preceding tool_use block referencing the same tool_use_id.
func failedToolName(blocks []commandCodeBlock) string {
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			return b.Name
		}
	}
	return ""
}

// failedToolError returns a short human-readable message for a failed tool
// result. The raw output blob is arbitrary data, so only a generic message is
// produced to keep the card Details safe to render.
func failedToolError(blocks []commandCodeBlock) string {
	return "tool call failed"
}

// hasNewCommandCodeWork reports whether the unread portion of the transcript
// (bytes after offset) shows the agent resumed work: a fresh plain-text user
// prompt, or a new assistant message carrying tool_use/text blocks. Used to
// clear the sticky PTY interrupt. Command Code drops interrupted turns
// entirely, so any continued assistant work or a new prompt beyond the
// interrupt boundary means the interrupted state is stale.
func hasNewCommandCodeWork(filePath string, offset int64) bool {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil {
		return false
	}
	for _, line := range lines {
		var e commandCodeEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Type != "message" || e.Message == nil {
			continue
		}
		switch e.Message.Role {
		case "user":
			if commandCodeUserText(e.Message.Content) != "" {
				return true
			}
		case "assistant":
			for _, b := range e.Message.Content {
				if b.Type == "tool_use" && b.Name != "" {
					return true
				}
				if b.Type == "text" && b.Text != "" {
					return true
				}
			}
		}
	}
	return false
}
