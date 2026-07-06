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
	Name string `json:"name,omitempty"` // tool name
}

func (w *ClaudeWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, prompt string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude", "projects")
	var lastCheck time.Time = time.Now().Add(-5 * time.Second)
	var lastFileSize int64 = 0
	var watchedFilePath string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				fp, mod, err := FindLatestFile(dir, ".jsonl", lastCheck)
				if err == nil && fp != "" {
					watchedFilePath = fp
					lastFileSize = 0
					lastCheck = mod.Add(-100 * time.Millisecond)
				}
			}
			if watchedFilePath != "" {
				info, err := os.Stat(watchedFilePath)
				if err == nil {
					if info.Size() > lastFileSize {
						w.parseClaudeLog(watchedFilePath, lastFileSize, callback)
						lastFileSize = info.Size()
					}
				} else {
					watchedFilePath = "" // lost file, search again
				}
			}
		}
	}
}

func (w *ClaudeWatcher) parseClaudeLog(filePath string, offset int64, callback func(status, tool, details, prompt string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	for i := len(lines) - 1; i >= 0; i-- {
		var logLine ClaudeLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Type == "result" {
			callback("idle", "", "", "")
			return
		}

		if logLine.Message != nil {
			msg := logLine.Message
			if msg.Role == "user" {
				callback("thinking", "", "", "")
				return
			}
			if msg.Role == "assistant" {
				var lastToolName string
				var hasText bool
				var textContent string
				for _, b := range msg.Content {
					if b.Type == "tool_use" {
						lastToolName = b.Name
					} else if b.Type == "text" {
						hasText = true
						textContent = b.Text
					}
				}

				if lastToolName != "" {
					callback("executing", lastToolName, "", "")
					return
				}

				if hasText {
					status := "idle"
					if strings.Contains(textContent, "?") || strings.Contains(textContent, "Confirm") || strings.Contains(textContent, "approve") {
						status = "waiting_input"
					}
					callback(status, "", "", textContent)
					return
				}
			}
		}
	}
}
