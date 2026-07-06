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

type AntigravityWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("agy", &AntigravityWatcher{})
}

type AntigravityStep struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Status string `json:"status"`
}

func (w *AntigravityWatcher) Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, prompt string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	var lastCheck time.Time = time.Now().Add(-5 * time.Second)
	var lastFileSize int64 = 0
	var watchedFilePath string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if watchedFilePath == "" {
				fp, mod, err := FindLatestFile(dir, "transcript.jsonl", lastCheck)
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
						w.parseAntigravityLog(watchedFilePath, lastFileSize, callback)
						lastFileSize = info.Size()
					}
				} else {
					watchedFilePath = ""
				}
			}
		}
	}
}

func (w *AntigravityWatcher) parseAntigravityLog(filePath string, offset int64, callback func(status, tool, details, prompt string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	for i := len(lines) - 1; i >= 0; i-- {
		var step AntigravityStep
		if err := json.Unmarshal([]byte(lines[i]), &step); err != nil {
			continue
		}

		if step.Type == "USER_INPUT" {
			callback("thinking", "", "", "")
			return
		}

		if step.Type == "PLANNER_RESPONSE" {
			if step.Status == "ERROR" {
				callback("idle", "", "", "")
				return
			}
			if strings.Contains(lines[i], "ask_permission") || strings.Contains(lines[i], "ask_question") {
				callback("waiting_input", "", "", "")
				return
			}
			if strings.Contains(lines[i], "run_command") || strings.Contains(lines[i], "replace_file_content") {
				callback("executing", "", "", "")
				return
			}
			callback("idle", "", "", "")
			return
		}
	}
}
