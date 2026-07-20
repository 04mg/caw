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

type ClaudeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("claude", &ClaudeWatcher{})
}

type ClaudeLogLine struct {
	Type    string         `json:"type"`
	Subtype string         `json:"subtype,omitempty"`
	Message *ClaudeMessage `json:"message,omitempty"`
}

type ClaudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ClaudeBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name for tool_use blocks
}

// parseClaudeContent decodes the Content field of a ClaudeMessage, which may
// be either a JSON array of block objects (the common case) or a plain JSON
// string (used by Claude Code for simple text user messages like "hi").
// Returns the parsed blocks and the raw text if content was a plain string.
func parseClaudeContent(raw json.RawMessage) ([]ClaudeBlock, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	// Try array of blocks first.
	var blocks []ClaudeBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return blocks, ""
	}
	// Fall back to plain string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return nil, s
	}
	return nil, ""
}

func (w *ClaudeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	// Claude stores transcripts under ~/.claude/projects/<encoded-path>/*.jsonl
	// where the encoded path replaces '/' with '-'.
	// We search within the cwd-specific subdirectory when possible so that
	// two Claude instances in different projects don't cross-contaminate.
	baseDir := filepath.Join(home, ".claude", "projects")
	searchDir := claudeProjectDir(baseDir, cwd)

	const agentID = "claude"
	// On resume (--continue), the agent reattaches to a pre-existing session
	// whose transcript file may have been last modified before this watcher
	// started. Widen the search window to 1 hour so the resumed session is
	// found; for a fresh start, only look for files modified after the
	// watcher started (no negative offset) to avoid grabbing a sibling
	// agent's session that was created moments before this watcher launched.
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string
	// Re-bind bookkeeping: lastActivity is the mtime of the most recently
	// read chunk of the watched file; silentTicks counts consecutive polls
	// with no new data. Used by ShouldRebind to detect /new and /resume.
	var lastActivity time.Time
	var silentTicks int

	// fsnotify-based immediate change notifier. Reacts to writes on the
	// watched transcript file in ~tens of ms; the 2s ticker acts as a
	// fallback for missed events and drives heartbeat/claim/re-bind.
	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
	}

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
	}()

	// readWatched reads new bytes from the watched file and updates status.
	// Returns true if new data was processed.
	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			// File disappeared — release claim and search again next tick.
			UnclaimSession(agentID, cwd, watchedFilePath)
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
		w.parseClaudeLog(watchedFilePath, lastFileSize, wrappedCallback)
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
				// Recompute searchDir each tick — the cwd-specific
				// subdirectory may not exist when Watch() first starts
				// (Claude creates it lazily on first message).
				searchDir = claudeProjectDir(baseDir, cwd)
				candidates, err := FindEarliestFiles(searchDir, ".jsonl", lastCheck)
				if err != nil {
					continue
				}
				for _, c := range candidates {
					if ClaimSession(agentID, cwd, c.Path) {
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

			// Mid-session re-bind: detect /new and /resume issued inside
			// the running agent. When the current file has been silent
			// for a few polls and another same-cwd file has just received
			// writes, atomically switch to it. Gated on PTY activity to
			// ensure only the watcher whose PTY is producing output
			// switches — a sibling Claude in the same cwd writing to its
			// own transcript should never cause this idle watcher to
			// steal its session.
			if silentTicks >= rebindSilenceTicks {
				lastPtyOut := agent.LastPtyActivity(sessionID)
				if time.Since(lastPtyOut) < 3*time.Second {
					cands, _ := FindLatestFiles(searchDir, ".jsonl", lastActivity)
					var others []RebindCandidate
					for _, c := range cands {
						others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
					}
					newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
					if newKey != "" && newKey != watchedFilePath {
						if ClaimSession(agentID, cwd, newKey) {
							UnclaimSession(agentID, cwd, watchedFilePath)
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

// claudeProjectDir returns the subdirectory within ~/.claude/projects/ that
// corresponds to cwd. Claude encodes the absolute path by replacing path
// separators (and the Windows drive-letter colon) with '-'. When cwd is
// empty or the encoded directory doesn't exist we fall back to the base
// projects directory.
func claudeProjectDir(baseDir, cwd string) string {
	if cwd == "" {
		return baseDir
	}
	encoded := encodePathForDir(cwd)
	candidate := filepath.Join(baseDir, encoded)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Fallback: the legacy "/"-only encoding (used on Unix).
	legacy := strings.ReplaceAll(cwd, "/", "-")
	legacyCandidate := filepath.Join(baseDir, legacy)
	if _, err := os.Stat(legacyCandidate); err == nil {
		return legacyCandidate
	}
	return baseDir
}

func (w *ClaudeWatcher) parseClaudeLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt to use as the session title.
	var sessionTitle string
	for _, line := range lines {
		var logLine ClaudeLogLine
		if json.Unmarshal([]byte(line), &logLine) != nil {
			continue
		}
		if logLine.Message != nil && logLine.Message.Role == "user" {
			blocks, plainText := parseClaudeContent(logLine.Message.Content)
			if plainText != "" && sessionTitle == "" {
				sessionTitle = plainText
			}
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" && sessionTitle == "" {
					sessionTitle = b.Text
				}
			}
		}
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Reverse pass: determine the current status from the last meaningful entry.
	var lastAssistantTool string
	var lastAssistantText string
	var seenUser bool
	var seenInterrupt bool
	var turnCompleted bool

	for i := len(lines) - 1; i >= 0; i-- {
		var logLine ClaudeLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Type == "result" || (logLine.Type == "system" && logLine.Subtype == "turn_duration") {
			turnCompleted = true
			continue
		}

		if logLine.Message != nil {
			msg := logLine.Message
			if msg.Role == "user" {
				blocks, plainText := parseClaudeContent(msg.Content)
				userText := plainText
				if userText == "" {
					for _, b := range blocks {
						if b.Type == "text" && b.Text != "" {
							userText = b.Text
							break
						}
					}
				}
				if strings.Contains(strings.ToLower(userText), "[request interrupted by user") {
					seenInterrupt = true
					// Don't break — continue scanning for the assistant tool_use
					// that was interrupted, so we can report it correctly.
					continue
				}
				// Skip empty-content user messages — Claude writes a
				// placeholder user entry with empty content immediately
				// before the interrupt message. Treating it as a real
				// user turn would stop the scan before the interrupt is
				// found, causing the status to stay "thinking" forever.
				if userText == "" {
					continue
				}
				seenUser = true
				break
			} else if msg.Role == "assistant" {
				blocks, _ := parseClaudeContent(msg.Content)
				for _, b := range blocks {
					if b.Type == "tool_use" && b.Name != "" {
						lastAssistantTool = b.Name
					} else if b.Type == "text" && b.Text != "" {
						lastAssistantText = b.Text
					}
				}
			}
		}
	}

	// An interruption means the agent is no longer actively working — the
	// user cancelled the in-flight tool call. Report idle so the UI doesn't
	// show "working" indefinitely.
	if seenInterrupt {
		callback("idle", "", "", sessionTitle)
		return
	}

	if lastAssistantTool != "" {
		// Tools that request user input should be reported as waiting_input,
		// not as executing. The agent is blocked until the user answers.
		toolLower := strings.ToLower(lastAssistantTool)
		if isUserInputTool(toolLower) {
			callback("waiting_input", lastAssistantTool, "", sessionTitle)
			return
		}
		callback("executing", lastAssistantTool, "", sessionTitle)
		return
	}

	if lastAssistantText != "" {
		status := "thinking"
		if turnCompleted {
			status = "idle"
		}
		// Only canonical user-input tools (handled above) signal waiting_input.
		// Keyword scanning of assistant text for "confirm"/"approve"/"[y/n]"
		// was removed: it produced false positives when the assistant's
		// explanation or plan happened to use those words.
		callback(status, "", lastAssistantText, sessionTitle)
		return
	}

	if seenUser || len(lines) > 0 {
		status := "thinking"
		if turnCompleted {
			status = "idle"
		}
		callback(status, "", "", sessionTitle)
		return
	}

	callback("idle", "", "", sessionTitle)
}

// isUserInputTool reports whether a tool name represents a tool that
// requests user input (e.g. AskUserQuestion, ExitPlanMode). When the last
// assistant action is one of these tools, the agent is blocked waiting for
// the user to respond, so the status should be "waiting_input" rather than
// "executing".
func isUserInputTool(toolLower string) bool {
	switch toolLower {
	case "askuserquestion", "ask_user_question", "askuser",
		"exitplanmode", "exit_plan_mode",
		"question", "request_user_input":
		return true
	}
	return false
}
