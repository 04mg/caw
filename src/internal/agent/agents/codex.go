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
	// Phase is set on event_msg "agent_message" and response_item "message"
	// entries: "commentary" (interim thought) or "final_answer" (turn done).
	Phase string `json:"phase,omitempty"`
	// Role is set on response_item "message" entries: developer/user/assistant.
	Role string `json:"role,omitempty"`
	// Name is the tool name for response_item "function_call" entries.
	Name string `json:"name,omitempty"`
}

func (w *CodexWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".codex", "sessions")
	const agentID = "codex"
	// On resume (codex resume --last), the agent reattaches to a pre-existing
	// session whose transcript file may predate this watcher. Widen the
	// search window to 1 hour so the resumed session is found. For a fresh
	// start, only look for files modified after the watcher started (no
	// negative offset) to avoid grabbing a sibling agent's session.
	lookback := 30 * time.Second
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
			candidates, err := FindEarliestFiles(dir, ".jsonl", lastCheck)
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
						w.parseCodexLog(watchedFilePath, lastFileSize, wrappedCallback)
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
						cands, _ := FindLatestFiles(dir, ".jsonl", lastActivity)
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

	// Codex writes a sequence of entries per turn. The status we report must
	// reflect the LAST meaningful entry:
	//
	//   user_message                      → thinking
	//   agent_message / message            → interim "commentary" still WORKING,
	//                                       final_answer → idle (turn complete)
	//   function_call                      → executing <tool>
	//   function_call_output               → thinking (waiting for next step)
	//   task_complete                      → idle (turn definitively done)
	//
	// The previous implementation matched any response_item "message" with
	// role assistant and reported "idle" — but Codex emits many such messages
	// with phase "commentary" mid-turn, which made the status flicker between
	// executing and idle. We now treat "commentary" as still working and only
	// "final_answer" (or an explicit task_complete) as idle.
	for i := len(lines) - 1; i >= 0; i-- {
		var logLine CodexLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Payload == nil {
			continue
		}
		p := logLine.Payload
		switch p.Type {
		case "task_complete":
			callback("idle", "", "", sessionTitle)
			return
		case "function_call":
			tool := p.Name
			if tool == "" {
				tool = "exec"
			}
			callback("executing", tool, "", sessionTitle)
			return
		case "function_call_output":
			callback("thinking", "", "", sessionTitle)
			return
		case "user_message":
			callback("thinking", "", "", sessionTitle)
			return
		case "message":
			if p.Role == "assistant" && p.Phase == "final_answer" {
				status := "idle"
				msgLower := strings.ToLower(p.Message)
				if strings.Contains(msgLower, "[y/n]") ||
					strings.Contains(msgLower, "[y/N]") ||
					strings.Contains(msgLower, "[Y/n]") ||
					strings.Contains(msgLower, "(y/n)") ||
					strings.Contains(msgLower, "confirm") ||
					strings.Contains(msgLower, "approve") {
					status = "waiting_input"
				}
				callback(status, "", "", sessionTitle)
				return
			}
			if p.Role == "assistant" && p.Phase == "commentary" {
				callback("thinking", "", "", sessionTitle)
				return
			}
			if p.Role == "user" {
				callback("thinking", "", "", sessionTitle)
				return
			}
			// developer/other roles — keep scanning backwards.
			continue
		case "agent_message":
			if p.Phase == "final_answer" {
				status := "idle"
				msgLower := strings.ToLower(p.Message)
				if strings.Contains(msgLower, "[y/n]") ||
					strings.Contains(msgLower, "[y/N]") ||
					strings.Contains(msgLower, "[Y/n]") ||
					strings.Contains(msgLower, "(y/n)") ||
					strings.Contains(msgLower, "confirm") ||
					strings.Contains(msgLower, "approve") {
					status = "waiting_input"
				}
				callback(status, "", "", sessionTitle)
				return
			}
			// commentary agent_message — still working.
			callback("thinking", "", "", sessionTitle)
			return
		case "task_started":
			callback("thinking", "", "", sessionTitle)
			return
		}
	}
}
