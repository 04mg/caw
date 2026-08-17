package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
	_ "modernc.org/sqlite"
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
	MessageID     string           `json:"messageId"`
	Content       string           `json:"content"`
	ToolRequests  []copilotToolReq `json:"toolRequests,omitempty"`
	ReasoningText string           `json:"reasoningText,omitempty"`
}

type copilotToolReq struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
}

// copilotToolResult is the data payload for "tool.result" events. When the
// tool call failed, Copilot populates IsError and/or an Error object.
type copilotToolResult struct {
	ToolCallID string `json:"toolCallId"`
	IsError    bool   `json:"isError,omitempty"`
	Error      *struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
	Output string `json:"output,omitempty"`
}

// copilotToolExecution is the data payload for "tool.execution_start" events,
// emitted when Copilot begins running a requested tool.
type copilotToolExecution struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
}

// copilotSessionError is the data payload for "session.error" events: a
// quota/transport/auth failure that ends the turn abnormally.
type copilotSessionError struct {
	ErrorType string `json:"errorType"`
	Message   string `json:"message"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// copilotTurnEnd is the data payload for "assistant.turn_end" events.
type copilotTurnEnd struct {
	TurnID string `json:"turnId"`
}

// copilotSubagentEvent is the data payload for "subagent.started" and
// "subagent.completed" events. Copilot records a subagent as a summary event
// in the parent's events.jsonl; the subagent's own work is not written there.
type copilotSubagentEvent struct {
	ToolCallID string `json:"toolCallId"`
	AgentName  string `json:"agentName,omitempty"`
}

// copilotWatchState carries state that must survive across incremental reads
// of the watched events.jsonl file. In particular it tracks subagents that
// have started but not yet completed, so an intermediate assistant.turn_end
// (the parent pausing after delegating work) is not mistaken for the end of
// the whole session.
type copilotWatchState struct {
	activeSubagents map[string]string
}

func (s *copilotWatchState) reset() {
	s.activeSubagents = nil
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
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".copilot", "session-state")
	sessionStorePath := filepath.Join(home, ".copilot", "session-store.db")
	const agentID = "copilot"

	watcherStart := time.Now().Add(-10 * time.Second)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string
	var lastStatus, lastTool, lastDetails string
	var lastAssistantDetails string
	var lastActivity time.Time
	var silentTicks int
	var parseState copilotWatchState

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
	}

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
	}()

	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			UnclaimSession(agentID, cwd, watchedFilePath)
			watchedFilePath = ""
			if notifyCh != nil {
				notifier.Watch("")
			}
			return false
		}
		if info.Size() <= lastFileSize {
			return false
		}
		wrappedCallback := func(status, tool, details, title string) {
			// The events file only contains user prompts. Copilot's generated
			// session name is persisted separately in session-store.db and
			// must take precedence over that prompt fallback.
			if generatedTitle := copilotSessionTitle(sessionStorePath, watchedFilePath); generatedTitle != "" {
				sessionTitle = generatedTitle
			} else if sessionTitle == "" && title != "" {
				sessionTitle = title
			}
			if status == "idle" && details == "" {
				details = lastAssistantDetails
			}
			if status == "idle" && details != "" {
				lastAssistantDetails = details
			}
			lastStatus, lastTool, lastDetails = status, tool, details
			callback(status, tool, details, sessionTitle)
		}
		w.parseCopilotEvents(watchedFilePath, lastFileSize, &parseState, wrappedCallback)
		lastFileSize = info.Size()
		lastActivity = info.ModTime()
		silentTicks = 0
		return true
	}

	refreshSessionTitle := func() {
		if watchedFilePath == "" || lastStatus == "" {
			return
		}
		title := copilotSessionTitle(sessionStorePath, watchedFilePath)
		if title == "" || title == sessionTitle {
			return
		}
		sessionTitle = title
		callback(lastStatus, lastTool, lastDetails, sessionTitle)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			readWatched()
			refreshSessionTitle()
		case <-ticker.C:
			heartbeat()
			if watchedFilePath == "" {
				// Walk every candidate newest-first and claim the first free
				// one. Trying only the single newest means a watcher whose
				// newest candidate is already claimed by a sibling Copilot in
				// the same cwd can never bind — it retries the same claimed
				// file forever instead of falling back to the next session.
				for _, cand := range findCopilotEventsFiles(stateDir, cwd, watcherStart, resume) {
					if ClaimSession(agentID, cwd, cand.path) {
						watchedFilePath = cand.path
						lastFileSize = 0
						lastActivity = time.Now()
						silentTicks = 0
						parseState.reset()
						if notifyCh != nil {
							notifier.Watch(watchedFilePath)
						}
						break
					}
				}
			}
			if watchedFilePath != "" {
				if !readWatched() {
					silentTicks++
				}
				refreshSessionTitle()

				// Mid-session re-bind for /new. Gated on PTY activity OR user
				// focus: only the watcher whose PTY is producing output (or
				// whose pane the user is currently driving) switches, so a
				// sibling Copilot in the same cwd writing to its own events
				// file can't make this idle, unfocused watcher steal its
				// session. The focus exemption covers /new issued in the
				// focused pane before the agent emits any PTY output.
				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						// Prefer sessions modified after this pane's last
						// activity (a /new or a resumed session being written
						// to again). Fall back to the full pool newest-first
						// so a /resume that select an older, unchanged file
						// can still be picked up. Either way, walk the whole
						// list so an already-claimed newest doesn't block the
						// watcher from claiming the next candidate.
						recent := findCopilotEventsFiles(stateDir, cwd, lastActivity, false)
						if len(recent) == 0 {
							recent = findCopilotEventsFiles(stateDir, cwd, lastActivity, true)
						}
						for _, cand := range recent {
							if cand.path == watchedFilePath {
								continue
							}
							if ClaimSession(agentID, cwd, cand.path) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								watchedFilePath = cand.path
								lastFileSize = 0
								silentTicks = 0
								parseState.reset()
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
								break
							}
						}
					}
				}
			}
		}
	}
}

// copilotSessionTitle reads the summary generated by Copilot CLI for the
// session owning eventsPath. The events log contains only user prompts, so it
// cannot provide this authoritative display title.
func copilotSessionTitle(sessionStorePath, eventsPath string) string {
	sessionID := filepath.Base(filepath.Dir(eventsPath))
	if sessionID == "" || sessionID == "." {
		return ""
	}
	db, err := sql.Open("sqlite", "file:"+sessionStorePath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	var title string
	if err := db.QueryRow(`SELECT summary FROM sessions WHERE id = ?`, sessionID).Scan(&title); err != nil {
		return ""
	}
	return CleanPrompt(title)
}

// copilotSessionCandidate is one events.jsonl candidate with its modification
// time, so callers can apply recency filters independently of discovery.
type copilotSessionCandidate struct {
	path    string
	modTime time.Time
}

// findCopilotEventsFiles searches ~/.copilot/session-state/*/events.jsonl for
// every session whose session.start event contains a cwd matching the given
// cwd, newest-first. When resume is false only files modified after `after`
// are returned: a fresh watcher must not bind to a pre-existing sibling
// session. When resume is true the after filter is skipped so a --continue
// watcher can reattach to the most recent session regardless of when it was
// created. The caller is responsible for calling ClaimSession on the returned
// paths, walking the list until one succeeds.
func findCopilotEventsFiles(stateDir, cwd string, after time.Time, resume bool) []copilotSessionCandidate {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil
	}
	var cands []copilotSessionCandidate
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
		cands = append(cands, copilotSessionCandidate{path: eventsPath, modTime: info.ModTime()})
	}
	// Sort newest-first (most recently modified first) so claims walk the list
	// in recency order and the most recent session wins.
	for i := 0; i < len(cands)-1; i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].modTime.After(cands[i].modTime) {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	return cands
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
// the current status from the last meaningful event. The supplied state is
// updated in place and must survive across incremental reads: Copilot writes
// subagent lifecycle events (subagent.started/completed) in the parent's
// stream, so the watcher has to remember active subagents to avoid reporting
// the parent's intermediate pause as "idle"/finished.
func (w *CopilotWatcher) parseCopilotEvents(filePath string, offset int64, state *copilotWatchState, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt as the session title
	// fallback, retain the final visible assistant response, and maintain the
	// set of active subagents. Copilot follows a visible assistant response
	// with assistant.turn_end, which carries no text.
	var sessionTitle string
	var lastAssistantText string
	for _, line := range lines {
		var ev copilotEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "user.message":
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
		case "assistant.message":
			var msg copilotAssistantMsg
			if json.Unmarshal(ev.Data, &msg) == nil && msg.Content != "" {
				lastAssistantText = msg.Content
			}
		case "subagent.started":
			var sub copilotSubagentEvent
			if json.Unmarshal(ev.Data, &sub) == nil && sub.ToolCallID != "" {
				if state.activeSubagents == nil {
					state.activeSubagents = make(map[string]string)
				}
				state.activeSubagents[sub.ToolCallID] = sub.AgentName
			}
		case "subagent.completed":
			var sub copilotSubagentEvent
			if json.Unmarshal(ev.Data, &sub) == nil && sub.ToolCallID != "" {
				delete(state.activeSubagents, sub.ToolCallID)
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
			// The parent agent may finish one of several turns (e.g. the
			// turn that delegated work to a subagent) while background work
			// is still running. In that case the whole session is NOT done:
			// report "thinking" instead of "idle" so the user doesn't get a
			// false "finished" notification. Reporting "thinking" (rather
			// than "executing"/"background_task") also preserves the true
			// finished notification once the subagent actually completes.
			if len(state.activeSubagents) > 0 {
				callback("thinking", "", lastAssistantText, sessionTitle)
				return
			}
			callback("idle", "", lastAssistantText, sessionTitle)
			return
		case "abort":
			// The user aborted the turn. Report "interrupted" (not idle) so
			// the UI surfaces it with a red dot and no push is sent.
			callback("interrupted", "", "", sessionTitle)
			return
		case "session.error":
			// A session-level error (quota exceeded, transport failure, auth
			// error) ended the turn abnormally. Surface it with a red dot so
			// the user notices; no push notification is sent.
			var se copilotSessionError
			msg := "session error"
			if json.Unmarshal(ev.Data, &se) == nil && se.Message != "" {
				msg = se.Message
			}
			callback("tool_failed", "", msg, sessionTitle)
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
			// A failed tool result carries is_error:true (or an error object).
			// Surface it as tool_failed; otherwise the agent continues thinking.
			var tr copilotToolResult
			if json.Unmarshal(ev.Data, &tr) == nil && tr.IsError {
				errText := "tool call failed"
				if tr.Error != nil && tr.Error.Message != "" {
					errText = tr.Error.Message
				} else if tr.Output != "" {
					errText = tr.Output
				}
				callback("tool_failed", "", errText, sessionTitle)
				return
			}
			callback("thinking", "", "", sessionTitle)
			return
		case "tool.execution_start":
			// Copilot emits this immediately before it starts running a tool.
			// For user-input tools (ask_user, exit_plan_mode) "starting" means
			// the assistant is blocked waiting for the user to answer; it
			// never executes autonomously. This is a backstop for the
			// assistant.message tool-request case above: it works even when
			// the assistant.message event isn't present (e.g. a plan-mode
			// exit whose only durable trace is the execution event).
			var exec copilotToolExecution
			if json.Unmarshal(ev.Data, &exec) == nil && isUserInputTool(strings.ToLower(exec.ToolName)) {
				callback("waiting_input", exec.ToolName, "", sessionTitle)
				return
			}
			// Non-user-input tools are intermediate and don't represent
			// agent status; fall through.
		case "assistant.message":
			var msg copilotAssistantMsg
			if json.Unmarshal(ev.Data, &msg) != nil {
				continue
			}
			if len(msg.ToolRequests) > 0 {
				toolName := msg.ToolRequests[0].Name
				status := "executing"
				// Canonical user-input tools (ask_user) and plan-approval
				// tools (exit_plan_mode) block on the user: the assistant has
				// finished (or paused) its work and is now waiting for input,
				// not executing. Shared with the tool.execution_start case
				// below so the status holds even if the message carrying the
				// tool request is skipped.
				if isUserInputTool(strings.ToLower(toolName)) {
					status = "waiting_input"
				}
				callback(status, toolName, "", sessionTitle)
				return
			}
			if msg.ReasoningText != "" && msg.Content == "" {
				callback("thinking", "", "", sessionTitle)
				return
			}
			if msg.Content != "" {
				// Only canonical user-input tools (ask_user, handled above)
				// signal waiting_input. Keyword scanning of assistant text for
				// "confirm"/"approve"/"[y/n]" was removed: it produced false
				// positives when the assistant's explanation happened to use
				// those words.
				if len(state.activeSubagents) > 0 {
					callback("thinking", "", msg.Content, sessionTitle)
					return
				}
				callback("idle", "", msg.Content, sessionTitle)
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
