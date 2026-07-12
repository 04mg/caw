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
	ticker := time.NewTicker(500 * time.Millisecond)
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
	lookback := 30 * time.Second
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
						break
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
						w.parseClaudeLog(watchedFilePath, lastFileSize, wrappedCallback)
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

				// Mid-session re-bind: detect /new and /resume issued inside
				// the running agent. When the current file has been silent
				// for a few polls and another same-cwd file has just received
				// writes, atomically switch to it. Gated on PTY activity to
				// ensure only the watcher whose PTY is producing output
				// switches.
				if silentTicks >= rebindSilenceTicks {
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

	for i := len(lines) - 1; i >= 0; i-- {
		var logLine ClaudeLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Type == "result" || (logLine.Type == "system" && logLine.Subtype == "turn_duration") {
			callback("idle", "", "", sessionTitle)
			return
		}

		if logLine.Message != nil {
			msg := logLine.Message
			if msg.Role == "user" {
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

	if lastAssistantTool != "" {
		callback("executing", lastAssistantTool, "", sessionTitle)
		return
	}

	if lastAssistantText != "" {
		status := "idle"
		textContentLower := strings.ToLower(lastAssistantText)
		if strings.Contains(textContentLower, "[y/n]") ||
			strings.Contains(textContentLower, "[y/N]") ||
			strings.Contains(textContentLower, "[Y/n]") ||
			strings.Contains(textContentLower, "(y/n)") ||
			strings.Contains(textContentLower, "confirm") ||
			strings.Contains(textContentLower, "approve") {
			status = "waiting_input"
		}
		callback(status, "", lastAssistantText, sessionTitle)
		return
	}

	if seenUser || len(lines) > 0 {
		callback("thinking", "", "", sessionTitle)
		return
	}

	callback("idle", "", "", sessionTitle)
}
