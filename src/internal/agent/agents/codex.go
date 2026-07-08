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

type CodexWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("codex", &CodexWatcher{})
}

type CodexLogLine struct {
	Type    string        `json:"type"`
	Payload *CodexPayload `json:"payload,omitempty"`
}

type CodexPayload struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

func (w *CodexWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".codex", "sessions")
	lastCheck := time.Now().Add(-10 * time.Second)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				fp, _, err := FindLatestFile(dir, ".jsonl", lastCheck)
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
						w.parseCodexLog(watchedFilePath, lastFileSize, wrappedCallback)
						lastFileSize = info.Size()
					}
				} else {
					watchedFilePath = ""
				}
			}
		}
	}
}

func (w *CodexWatcher) parseCodexLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt to use as the session title.
	var sessionTitle string
	for _, line := range lines {
		var logLine CodexLogLine
		if json.Unmarshal([]byte(line), &logLine) != nil {
			continue
		}
		if logLine.Payload != nil && logLine.Payload.Type == "user_message" && logLine.Payload.Message != "" {
			if sessionTitle == "" {
				sessionTitle = logLine.Payload.Message
			}
		}
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Reverse pass: determine current status from the last meaningful entry.
	for i := len(lines) - 1; i >= 0; i-- {
		var logLine CodexLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Payload != nil {
			p := logLine.Payload
			switch p.Type {
			case "user_message":
				callback("thinking", "", "", sessionTitle)
				return
			case "function_call":
				callback("executing", p.Message, "", sessionTitle)
				return
			case "message":
				status := "idle"
				if strings.Contains(p.Message, "?") || strings.Contains(p.Message, "[y/n]") {
					status = "waiting_input"
				}
				callback(status, "", "", sessionTitle)
				return
			}
		}
	}
}
