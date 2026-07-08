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
	Message *ClaudeMessage `json:"message,omitempty"`
}

type ClaudeMessage struct {
	Role    string        `json:"role"`
	Content []ClaudeBlock `json:"content"`
}

type ClaudeBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name for tool_use blocks
}

func (w *ClaudeWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	// Claude stores transcripts under ~/.claude/projects/<encoded-path>/*.jsonl
	// where the encoded path replaces '/' with '-'.
	// We search within the cwd-specific subdirectory when possible so that
	// two Claude instances in different projects don't cross-contaminate.
	baseDir := filepath.Join(home, ".claude", "projects")
	searchDir := claudeProjectDir(baseDir, cwd)

	var lastCheck time.Time // zero — finds ANY existing session file on first search
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				fp, _, err := FindLatestFile(searchDir, ".jsonl", lastCheck)
				if err == nil && fp != "" {
					watchedFilePath = fp
					lastFileSize = 0
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
						w.parseClaudeLog(watchedFilePath, lastFileSize, wrappedCallback)
						lastFileSize = info.Size()
					}
				} else {
					watchedFilePath = "" // lost file, search again
				}
			}
		}
	}
}

// claudeProjectDir returns the subdirectory within ~/.claude/projects/ that
// corresponds to cwd. Claude encodes the absolute path by replacing '/' with
// '-'. When cwd is empty or the encoded directory doesn't exist we fall back
// to the base projects directory.
func claudeProjectDir(baseDir, cwd string) string {
	if cwd == "" {
		return baseDir
	}
	// Claude's encoding: strip leading slash, replace remaining '/' with '-',
	// and prefix the whole thing with '-'.
	encoded := strings.ReplaceAll(cwd, "/", "-")
	candidate := filepath.Join(baseDir, encoded)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
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
			for _, b := range logLine.Message.Content {
				if b.Type == "text" && b.Text != "" && sessionTitle == "" {
					sessionTitle = b.Text
				}
			}
		}
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Reverse pass: determine the current status from the last meaningful entry.
	for i := len(lines) - 1; i >= 0; i-- {
		var logLine ClaudeLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Type == "result" {
			callback("idle", "", "", sessionTitle)
			return
		}

		if logLine.Message != nil {
			msg := logLine.Message
			switch msg.Role {
			case "user":
				callback("thinking", "", "", sessionTitle)
				return

			case "assistant":
				var lastToolName string
				var hasText bool
				var textContent string

				for _, b := range msg.Content {
					switch b.Type {
					case "tool_use":
						lastToolName = b.Name
					case "text":
						hasText = true
						textContent = b.Text
					case "thinking":
						// Extended thinking block — agent is actively reasoning.
						// Don't override with a "has tool" or "has text" decision yet;
						// keep scanning the block list for more concrete signals.
					}
				}

				if lastToolName != "" {
					callback("executing", lastToolName, "", sessionTitle)
					return
				}
				if hasText {
					status := "idle"
					if strings.Contains(textContent, "?") ||
						strings.Contains(textContent, "Confirm") ||
						strings.Contains(textContent, "approve") {
						status = "waiting_input"
					}
					callback(status, "", textContent, sessionTitle)
					return
				}
				// Assistant message with only thinking blocks → still thinking.
				callback("thinking", "", "", sessionTitle)
				return
			}
		}
	}
}
