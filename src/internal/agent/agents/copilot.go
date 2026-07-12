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

type CopilotWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("copilot", &CopilotWatcher{})
}

// copilotEvent mirrors one line of the events.jsonl written by GitHub Copilot
// CLI under ~/.copilot/session-state/<session-id>/events.jsonl.
type copilotEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// copilotAssistantMsg is the data payload for "assistant.message" events.
type copilotAssistantMsg struct {
	MessageID     string             `json:"messageId"`
	Content       string             `json:"content"`
	ToolRequests  []copilotToolReq   `json:"toolRequests,omitempty"`
	ReasoningText string             `json:"reasoningText,omitempty"`
}

type copilotToolReq struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
}

// copilotToolResult is the data payload for "tool.result" events.
type copilotToolResult struct {
	ToolCallID string `json:"toolCallId"`
}

// copilotTurnEnd is the data payload for "assistant.turn_end" events.
type copilotTurnEnd struct {
	TurnID string `json:"turnId"`
}

// copilotUserMsg is the data payload for "user.message" events.
type copilotUserMsg struct {
	Content            string `json:"content"`
	TransformedContent string `json:"transformedContent"`
}

// copilotAskUser is the data payload for "assistant.ask_user" events.
type copilotAskUser struct {
	Question string `json:"question"`
}

func (w *CopilotWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".copilot", "session-state")
	const agentID = "copilot"

	watcherStart := time.Now()
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string
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
				candidate := findCopilotEventsFile(stateDir, cwd, watcherStart, resume, agentID, sessionID)
				if candidate != "" {
					if ClaimSession(agentID, cwd, candidate) {
						watchedFilePath = candidate
						lastFileSize = 0
						lastActivity = time.Now()
						silentTicks = 0
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
						w.parseCopilotEvents(watchedFilePath, lastFileSize, wrappedCallback)
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

				// Mid-session re-bind for /new.
				if silentTicks >= rebindSilenceTicks {
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second {
						newFile := findCopilotEventsFile(stateDir, cwd, lastActivity, true, agentID, sessionID)
						if newFile != "" && newFile != watchedFilePath {
							if ClaimSession(agentID, cwd, newFile) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								watchedFilePath = newFile
								lastFileSize = 0
								silentTicks = 0
							}
						}
					}
				}
			}
		}
	}
}

// findCopilotEventsFile searches ~/.copilot/session-state/*/events.jsonl for
// the most recently modified one whose session.start event contains a cwd
// matching the given cwd. Returns the first matching candidate path; the caller
// is responsible for calling ClaimSession on the returned path.
func findCopilotEventsFile(stateDir, cwd string, after time.Time, resume bool, agentID, ptyID string) string {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return ""
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	var cands []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		eventsPath := filepath.Join(stateDir, entry.Name(), "events.jsonl")
		info, err := os.Stat(eventsPath)
		if err != nil {
			continue
		}
		if !resume && !info.ModTime().After(after) {
			continue
		}
		if cwd != "" {
			if !copilotEventsMatchesCwd(eventsPath, cwd) {
				continue
			}
		}
		cands = append(cands, candidate{path: eventsPath, modTime: info.ModTime()})
	}
	if len(cands) == 0 {
		return ""
	}
	// Sort oldest-first so the earliest watcher claims the earliest session.
	for i := 0; i < len(cands)-1; i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].modTime.Before(cands[i].modTime) {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	return cands[0].path
}

// copilotEventsMatchesCwd reads the first line of events.jsonl looking for a
// session.start event with a matching cwd. Returns false if no match or if
// the file can't be read.
func copilotEventsMatchesCwd(eventsPath, cwd string) bool {
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev copilotEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "session.start" {
			var startData struct {
				Context struct {
					Cwd string `json:"cwd"`
				} `json:"context"`
			}
			if json.Unmarshal(ev.Data, &startData) == nil {
				if filepath.Clean(startData.Context.Cwd) == filepath.Clean(cwd) {
					return true
				}
			}
		}
	}
	return false
}

// parseCopilotEvents reads new lines from the events.jsonl file and derives
// the current status from the last meaningful event.
func (w *CopilotWatcher) parseCopilotEvents(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt as the session title.
	var sessionTitle string
	for _, line := range lines {
		var ev copilotEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "user.message" {
			var msg copilotUserMsg
			if json.Unmarshal(ev.Data, &msg) == nil {
				prompt := msg.Content
				if prompt == "" {
					prompt = msg.TransformedContent
				}
				if prompt != "" && sessionTitle == "" {
					sessionTitle = CleanPrompt(prompt)
				}
			}
		}
	}

	// Reverse pass: determine status from the last meaningful event.
	// Skip intermediate events (hooks, tool execution_start/complete) that
	// don't represent agent status.
	for i := len(lines) - 1; i >= 0; i-- {
		var ev copilotEvent
		if json.Unmarshal([]byte(lines[i]), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant.turn_end":
			callback("idle", "", "", sessionTitle)
			return
		case "assistant.ask_user":
			var ask copilotAskUser
			question := ""
			if json.Unmarshal(ev.Data, &ask) == nil {
				question = ask.Question
			}
			callback("waiting_input", "ask_user", question, sessionTitle)
			return
		case "tool.result":
			callback("thinking", "", "", sessionTitle)
			return
		case "assistant.message":
			var msg copilotAssistantMsg
			if json.Unmarshal(ev.Data, &msg) != nil {
				continue
			}
			if len(msg.ToolRequests) > 0 {
				toolName := msg.ToolRequests[0].Name
				callback("executing", toolName, "", sessionTitle)
				return
			}
			if msg.ReasoningText != "" && msg.Content == "" {
				callback("thinking", "", "", sessionTitle)
				return
			}
			if msg.Content != "" {
				status := "idle"
				contentLower := strings.ToLower(msg.Content)
				if strings.Contains(contentLower, "[y/n]") ||
					strings.Contains(contentLower, "[y/N]") ||
					strings.Contains(contentLower, "[Y/n]") ||
					strings.Contains(contentLower, "(y/n)") ||
					strings.Contains(contentLower, "confirm") ||
					strings.Contains(contentLower, "approve") {
					status = "waiting_input"
				}
				callback(status, "", msg.Content, sessionTitle)
				return
			}
			callback("thinking", "", "", sessionTitle)
			return
		case "assistant.turn_start":
			callback("thinking", "", "", sessionTitle)
			return
		case "user.message":
			callback("thinking", "", "", sessionTitle)
			return
		}
		// Skip hook.start, hook.end, tool.execution_start,
		// tool.execution_complete, session.*, system.* — they are
		// intermediate and don't represent agent status.
	}
}
