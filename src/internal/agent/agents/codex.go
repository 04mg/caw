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

func (w *CodexWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, prompt string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".codex", "sessions")
	var lastCheck time.Time // zero — finds ANY existing session file on first search
	var lastFileSize int64 = 0
	var watchedFilePath string

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
						w.parseCodexLog(watchedFilePath, lastFileSize, callback)
						lastFileSize = info.Size()
					}
				} else {
					watchedFilePath = ""
				}
			}
		}
	}
}

func (w *CodexWatcher) parseCodexLog(filePath string, offset int64, callback func(status, tool, details, prompt string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the most recent user message to use as the prompt.
	var userPrompt string
	for _, line := range lines {
		var logLine CodexLogLine
		if json.Unmarshal([]byte(line), &logLine) != nil {
			continue
		}
		if logLine.Payload != nil && logLine.Payload.Type == "user_message" && logLine.Payload.Message != "" {
			userPrompt = logLine.Payload.Message
		}
	}
	if len(userPrompt) > 200 {
		userPrompt = userPrompt[:200] + "…"
	}

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
				callback("thinking", "", "", userPrompt)
				return
			case "function_call":
				callback("executing", p.Message, "", userPrompt)
				return
			case "message":
				status := "idle"
				if strings.Contains(p.Message, "?") || strings.Contains(p.Message, "[y/n]") {
					status = "waiting_input"
				}
				callback(status, "", "", userPrompt)
				return
			}
		}
	}
}
