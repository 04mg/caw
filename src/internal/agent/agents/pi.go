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

func (w *PiWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".pi", "agent", "sessions")
	const agentID = "pi"
	lookback := 2 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string

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
			if watchedFilePath == "" {
				searchDir := dir
				if cwd != "" {
					cleanCwd := filepath.Clean(cwd)
					projDir := "-" + strings.ReplaceAll(cleanCwd, "/", "-") + "-"
					targetDir := filepath.Join(dir, projDir)
					// Handle alternative trailing slash format `--root-caw--`
					if _, err := os.Stat(targetDir); err != nil {
						projDirAlt := "-" + strings.ReplaceAll(cleanCwd+"/", "/", "-") + "-"
						targetDirAlt := filepath.Join(dir, projDirAlt)
						if info, err := os.Stat(targetDirAlt); err == nil && info.IsDir() {
							targetDir = targetDirAlt
						}
					}
					if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
						searchDir = targetDir
					}
				}
				candidates, err := FindLatestFiles(searchDir, ".jsonl", lastCheck)
				if err != nil {
					continue
				}
				for _, c := range candidates {
					if ClaimSession(agentID, cwd, c.Path) {
						watchedFilePath = c.Path
						lastFileSize = 0
						lastCheck = time.Now()
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
						w.parsePiLog(watchedFilePath, lastFileSize, wrappedCallback)
						lastFileSize = info.Size()
					}
				} else {
					UnclaimSession(agentID, cwd, watchedFilePath)
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

	// Forward pass: collect the first user prompt (session title).
	var sessionTitle string
	for _, line := range lines {
		if msg, ok := parseOnePiLogLine(line); ok {
			if msg.Role == "user" {
				for _, b := range msg.Content {
					if b.Type == "text" && b.Text != "" && sessionTitle == "" {
						cleaned := CleanPrompt(b.Text)
						if cleaned != "" {
							sessionTitle = cleaned
						}
					}
				}
			}
		}
	}

	// Forward pass: emit status events for the new log lines.
	var states []struct{ status, tool string }
	for _, line := range lines {
		msg, ok := parseOnePiLogLine(line)
		if !ok {
			continue
		}
		status, tool := piStatusForMessage(msg)
		if status == "" {
			continue
		}
		states = append(states, struct{ status, tool string }{status, tool})
	}

	if len(states) == 0 {
		return
	}

	if offset == 0 {
		// Initial full read: report only the final state to avoid replaying the
		// whole session history as a burst of transitions.
		last := states[len(states)-1]
		callback(last.status, last.tool, "", sessionTitle)
		return
	}

	// Incremental read: emit every transition.
	var lastState string
	for _, st := range states {
		state := st.status + "|" + st.tool
		if state == lastState {
			continue
		}
		lastState = state
		callback(st.status, st.tool, "", sessionTitle)
	}
}

// piStatusForMessage derives the working/idle state represented by a single
// pi log message. It returns an empty status string when the message does not
// represent a meaningful state change.
func piStatusForMessage(msg PiMessage) (status, tool string) {
	roleLower := strings.ToLower(msg.Role)

	if roleLower == "user" {
		return "thinking", ""
	}

	if roleLower == "toolresult" || roleLower == "tool" {
		return "thinking", ""
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
			return "executing", lastToolName
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
			return status, ""
		}
	}

	return "", ""
}
