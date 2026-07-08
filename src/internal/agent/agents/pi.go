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
	lastCheck := time.Now().Add(-1 * time.Second)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				searchDir := dir
				if cwd != "" {
					cleanCwd := filepath.Clean(cwd)
					projDir := "-" + strings.ReplaceAll(cleanCwd, "/", "-") + "-"
					targetDir := filepath.Join(dir, projDir)
					if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
						searchDir = targetDir
					}
				}
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

type PiLogLine struct {
	Type    string     `json:"type"`
	Message *PiMessage `json:"message,omitempty"`
}

func parseOnePiLogLine(line string) (PiMessage, bool) {
	// 1. Try to parse as PiLogLine first (new format)
	var l PiLogLine
	if err := json.Unmarshal([]byte(line), &l); err == nil && l.Type == "message" && l.Message != nil {
		return *l.Message, true
	}
	// 2. Try to parse directly as PiMessage (old format)
	var msg PiMessage
	if err := json.Unmarshal([]byte(line), &msg); err == nil && msg.Role != "" {
		return msg, true
	}
	return PiMessage{}, false
}

func (w *PiWatcher) parsePiLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt.
	var sessionTitle string
	for _, line := range lines {
		if msg, ok := parseOnePiLogLine(line); ok {
			if msg.Role == "user" {
				for _, b := range msg.Content {
					if b.Type == "text" && b.Text != "" && sessionTitle == "" {
						sessionTitle = b.Text
					}
				}
			}
		}
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Reverse pass: determine current status from the last meaningful entry.
	for i := len(lines) - 1; i >= 0; i-- {
		msg, ok := parseOnePiLogLine(lines[i])
		if !ok {
			continue
		}

		roleLower := strings.ToLower(msg.Role)

		if roleLower == "user" {
			callback("thinking", "", "", sessionTitle)
			return
		}

		if roleLower == "toolresult" || roleLower == "tool" {
			callback("thinking", "", "", sessionTitle)
			return
		}

		if roleLower == "assistant" || roleLower == "agent" {
			var lastToolName string
			var hasText bool
			var textContent string
			for _, b := range msg.Content {
				if b.Type == "tool_call" || b.Type == "tool_use" || b.Type == "toolCall" {
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
				textContentLower := strings.ToLower(textContent)
				if strings.Contains(textContentLower, "[y/n]") ||
					strings.Contains(textContentLower, "[y/N]") ||
					strings.Contains(textContentLower, "[Y/n]") ||
					strings.Contains(textContentLower, "(y/n)") ||
					strings.Contains(textContentLower, "confirm") ||
					strings.Contains(textContentLower, "approve") {
					status = "waiting_input"
				}
				callback(status, "", "", sessionTitle)
				return
			}
		}
	}
}
