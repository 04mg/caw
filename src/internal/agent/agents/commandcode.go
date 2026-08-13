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
	Type      string  `json:"type"`
	Text      string  `json:"text,omitempty"`
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	ToolUseID string  `json:"tool_use_id,omitempty"`
	IsError   bool    `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

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
	var watchedFilePath string
	var sessionTitle string
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
		w.parseCommandCodeLog(watchedFilePath, lastFileSize, cwd, sessionID, wrappedCallback)
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
				if !readWatched() {
					silentTicks++
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

func (w *CommandCodeWatcher) parseCommandCodeLog(filePath string, offset int64, cwd, sessionID string, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
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
			var hasText bool
			var failedErr string
			for _, b := range msg.Content {
				switch b.Type {
				case "tool_use":
					lastTool = b.Name
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

	// Interrupted turns are never persisted: an in-flight turn is simply
	// dropped from the transcript. If the user hit the interrupt key after
	// the last committed entry but the transcript still reports working,
	// surface it as interrupted so the card shows a red dot instead of
	// staying stuck in Working.
	if status == "thinking" || status == "executing" || status == "tool_failed" {
		if lastInterrupt := agent.LastPtyInterrupt(sessionID); !lastInterrupt.IsZero() && time.Since(lastInterrupt) < 10*time.Second {
			status = "interrupted"
			tool = ""
			details = ""
		}
	}

	callback(status, tool, details, sessionTitle)
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