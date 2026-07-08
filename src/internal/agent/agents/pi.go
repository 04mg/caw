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

type PiWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("pi", &PiWatcher{})
}

type PiMessage struct {
	Role    string    `json:"role"`
	Content []PiBlock `json:"content"`
}

type PiBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name
}

func (w *PiWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".pi", "agent", "sessions")
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
				fp, _, err := FindLatestFile(dir, "session.jsonl", lastCheck)
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
						w.parsePiLog(watchedFilePath, lastFileSize, wrappedCallback)
						lastFileSize = info.Size()
					}
				} else {
					watchedFilePath = ""
				}
			}
		}
	}
}

func (w *PiWatcher) parsePiLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt.
	var sessionTitle string
	for _, line := range lines {
		var msg PiMessage
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		if msg.Role == "user" {
			for _, b := range msg.Content {
				if b.Type == "text" && b.Text != "" && sessionTitle == "" {
					sessionTitle = b.Text
				}
			}
		}
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Reverse pass: determine current status from the last meaningful entry.
	for i := len(lines) - 1; i >= 0; i-- {
		var msg PiMessage
		if err := json.Unmarshal([]byte(lines[i]), &msg); err != nil {
			continue
		}

		if msg.Role == "user" {
			callback("thinking", "", "", sessionTitle)
			return
		}

		if msg.Role == "assistant" || msg.Role == "agent" {
			var lastToolName string
			var hasText bool
			var textContent string
			for _, b := range msg.Content {
				if b.Type == "tool_call" || b.Type == "tool_use" {
					lastToolName = b.Name
				} else if b.Type == "text" {
					hasText = true
					textContent = b.Text
				}
			}

			if lastToolName != "" {
				callback("executing", lastToolName, "", sessionTitle)
				return
			}
			if hasText {
				status := "idle"
				if strings.Contains(textContent, "?") || strings.Contains(textContent, "[y/n]") {
					status = "waiting_input"
				}
				callback(status, "", "", sessionTitle)
				return
			}
		}
	}
}
