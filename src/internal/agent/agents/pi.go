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
	Role         string    `json:"role"`
	Content      []PiBlock `json:"content"`
	StopReason   string    `json:"stopReason,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
}

type PiBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name
}

func (w *PiWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".pi", "agent", "sessions")
	const agentID = "pi"
	// On resume (--continue), the agent reattaches to a pre-existing session
	// whose transcript may predate this watcher. Widen the search window to
	// 1 hour so the resumed session is found. For a fresh start, only look
	// for files modified after the watcher started (no negative offset).
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string
	// Re-bind bookkeeping for /new and /resume detection.
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
				searchDir := dir
				if cwd != "" {
					cleanCwd := filepath.Clean(cwd)
					projDir := "-" + encodePathForDir(cleanCwd) + "-"
					targetDir := filepath.Join(dir, projDir)
					if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
						searchDir = targetDir
					}
				}
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
						w.parsePiLog(watchedFilePath, lastFileSize, resume, wrappedCallback)
						lastFileSize = info.Size()
						lastActivity = info.ModTime()
						silentTicks = 0
					} else {
						silentTicks++
					}
				} else {
					UnclaimSession(agentID, cwd, watchedFilePath)
					watchedFilePath = ""
					continue
				}

				// Mid-session re-bind for /new and /resume.
				if silentTicks >= rebindSilenceTicks {
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second {
						rebindDir := dir
						if cwd != "" {
							cleanCwd := filepath.Clean(cwd)
							projDir := "-" + encodePathForDir(cleanCwd) + "-"
							targetDir := filepath.Join(dir, projDir)
							if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
								rebindDir = targetDir
							}
						}
						cands, _ := FindLatestFiles(rebindDir, ".jsonl", lastActivity)
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

func (w *PiWatcher) parsePiLog(filePath string, offset int64, resume bool, callback func(status, tool, details, title string)) {
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

	if offset == 0 && resume {
		// Resumed session: report only the final state to avoid replaying
		// the whole prior session history as a burst of transitions.
		last := states[len(states)-1]
		callback(last.status, last.tool, "", sessionTitle)
		return
	}

	// Incremental read (or fresh session initial read): emit every deduped
	// transition. For a fresh session where the whole turn was already written
	// before the first poll (e.g. a fast one-shot response), this captures the
	// thinking→idle transition so status/push notifications fire correctly.
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
		if strings.ToLower(msg.StopReason) == "aborted" || strings.Contains(strings.ToLower(msg.ErrorMessage), "aborted") {
			return "idle", ""
		}
		var lastToolName string
		var hasText bool
		var hasThinking bool
		var textContent string
		for _, b := range msg.Content {
			if b.Type == "tool_call" || b.Type == "tool_use" || b.Type == "toolCall" {
				lastToolName = b.Name
			} else if b.Type == "text" {
				hasText = true
				textContent = b.Text
			} else if b.Type == "thinking" {
				hasThinking = true
			}
		}

		if lastToolName != "" {
			return "executing", lastToolName
		}
		// An assistant message with a thinking (reasoning) block but no
		// tool call or text yet means the model is still working — report
		// "thinking" so the status doesn't prematurely flip to idle while
		// the LLM is still generating. Once a text block appears alongside
		// the thinking block, the turn is complete and we can report idle
		// (or waiting_input if the text contains a confirmation prompt).
		if hasThinking && !hasText {
			return "thinking", ""
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
