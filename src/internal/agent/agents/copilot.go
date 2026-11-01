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
// of the watched events.jsonl file:
//   - activeSubagents: subagents that have started but not yet completed, so
//     an intermediate assistant.turn_end (the parent pausing after delegating
//     work) is not mistaken for the end of the whole session.
//   - pendingPermissions: permission.requested events not yet resolved by a
//     permission.completed. While one is pending Copilot is blocked on the
//     user's approval (the TUI "Do you want to run this command?" dialog).
//   - lastMsgHadTools: whether the most recent assistant.message requested
//     tools. Copilot writes assistant.turn_end after EVERY model call (one
//     "turn" per LLM round-trip), including the call that only requested a
//     tool, so a turn_end is the end of the user's turn only when the last
//     assistant message carried no tool requests.
//   - toolNames: toolCallId -> tool name, to label permission and
//     execution-complete events that only carry the call id.
type copilotWatchState struct {
	activeSubagents    map[string]string
	pendingPermissions map[string]copilotPendingPermission
	pendingOrder       []string
	lastMsgHadTools    bool
	toolNames          map[string]string
	// toolFailure holds the error of the most recent failed tool call in the
	// current turn (tool + message). Copilot writes turn_end/turn_start right
	// after a tool completes, so a failure would otherwise be overwritten
	// within milliseconds; it stays surfaced (tool_failed) while the model
	// works on the error, until the next tool starts or the turn ends.
	toolFailure     string
	toolFailureTool string
	// lastText is the latest visible assistant text of the current turn,
	// shown as the idle card's details once the turn ends (the final message
	// and its turn_end may arrive in different reads).
	lastText string
	// sawWork is set by each parse when the batch it read contained turn
	// activity (prompt, model call, tool run). Not carried across reads.
	sawWork bool
}

// copilotPendingPermission is one unresolved permission.requested event.
type copilotPendingPermission struct {
	tool    string
	details string
}

func (s *copilotWatchState) reset() {
	*s = copilotWatchState{}
}

func (s *copilotWatchState) clearPermissions() {
	s.pendingPermissions = nil
	s.pendingOrder = nil
}

// latestPendingPermission returns the most recently requested permission that
// is still unresolved.
func (s *copilotWatchState) latestPendingPermission() (copilotPendingPermission, bool) {
	for i := len(s.pendingOrder) - 1; i >= 0; i-- {
		if p, ok := s.pendingPermissions[s.pendingOrder[i]]; ok {
			return p, true
		}
	}
	return copilotPendingPermission{}, false
}

// copilotPermissionRequested is the data payload for "permission.requested"
// events (persisted to events.jsonl only when the user is actually prompted;
// auto-approved read-only tools never write one).
type copilotPermissionRequested struct {
	RequestID         string `json:"requestId"`
	ResolvedByHook    bool   `json:"resolvedByHook,omitempty"`
	PermissionRequest struct {
		Kind            string `json:"kind"`
		ToolCallID      string `json:"toolCallId"`
		FullCommandText string `json:"fullCommandText,omitempty"`
		Intention       string `json:"intention,omitempty"`
		Path            string `json:"path,omitempty"`
		FileName        string `json:"fileName,omitempty"`
		ToolName        string `json:"toolName,omitempty"`
	} `json:"permissionRequest"`
}

// copilotPermissionCompleted is the data payload for "permission.completed".
type copilotPermissionCompleted struct {
	RequestID  string `json:"requestId"`
	ToolCallID string `json:"toolCallId,omitempty"`
	Result     struct {
		Kind string `json:"kind"`
	} `json:"result"`
}

// copilotToolComplete is the data payload for "tool.execution_complete", the
// event Copilot CLI actually persists when a tool finishes. success is false
// when the tool itself failed; a shell command that ran but exited non-zero is
// reported with success:true and a non-zero shellExecution.exitCode.
type copilotToolComplete struct {
	ToolCallID string `json:"toolCallId"`
	Success    *bool  `json:"success,omitempty"`
	Error      *struct {
		Message string `json:"message,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
	Result *struct {
		Content string `json:"content,omitempty"`
	} `json:"result,omitempty"`
	ShellExecution *struct {
		ExitCode *int `json:"exitCode,omitempty"`
	} `json:"shellExecution,omitempty"`
}

// failure returns a short error text when the completed tool call failed, or
// "" when it succeeded.
func (c copilotToolComplete) failure() string {
	if c.Success != nil && !*c.Success {
		if c.Error != nil && c.Error.Message != "" {
			return firstLine(c.Error.Message)
		}
		if c.Result != nil && c.Result.Content != "" {
			return firstLine(c.Result.Content)
		}
		return "tool call failed"
	}
	if c.ShellExecution != nil && c.ShellExecution.ExitCode != nil && *c.ShellExecution.ExitCode != 0 {
		if c.Result != nil && c.Result.Content != "" {
			return firstLine(c.Result.Content)
		}
		return "command failed"
	}
	return ""
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
	// replay marks the first read after binding to a pre-existing session
	// (reopen/resume/rebind): its history must not produce live transitions.
	var replay bool

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
		replaying := lastFileSize == 0 && replay
		replay = false
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
			// A single read can cover a whole stretch of work that ended
			// the turn (the user answered a prompt and the agent finished
			// within one poll, or a fast turn). Report the working state
			// first so the "finished" notification (working -> idle) fires.
			// Not when replaying a pre-existing log on bind.
			if status == "idle" && parseState.sawWork && !replaying &&
				lastStatus != "thinking" && lastStatus != "executing" && lastStatus != "tool_failed" {
				callback("thinking", "", "", sessionTitle)
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
				// Prefer the exact session id persisted for this leaf by a
				// previous Caw process so a reopened pane follows its own
				// conversation when several Copilot panes share a cwd.
				if exact := agent.PersistedExternalSession(sessionID); exact != "" {
					if exactPath := copilotEventsForSession(stateDir, exact); exactPath != "" && ClaimSessionForLeaf(agentID, cwd, exactPath, sessionID) {
						watchedFilePath = exactPath
						lastFileSize = 0
						replay = true
						lastActivity = time.Now()
						silentTicks = 0
						parseState.reset()
						if notifyCh != nil {
							notifier.Watch(watchedFilePath)
						}
						agent.RecordExternalSession(sessionID, exact)
					}
				}
				if watchedFilePath == "" {
					// Walk every candidate newest-first and claim the first free
					// one. Trying only the single newest means a watcher whose
					// newest candidate is already claimed by a sibling Copilot in
					// the same cwd can never bind — it retries the same claimed
					// file forever instead of falling back to the next session.
					for _, cand := range findCopilotEventsFiles(stateDir, cwd, watcherStart, resume) {
						if !resume && copilotSessionEnded(cand.path) {
							continue
						}
						if ClaimSessionForLeaf(agentID, cwd, cand.path, sessionID) {
							watchedFilePath = cand.path
							lastFileSize = 0
							replay = resume
							lastActivity = time.Now()
							silentTicks = 0
							parseState.reset()
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							// Persist the native session id (the session directory
							// name) so a reopen can restore the exact session even
							// when another Copilot pane shares the same cwd.
							if sid := copilotSessionIDFromPath(cand.path); sid != "" {
								agent.RecordExternalSession(sessionID, sid)
							}
							break
						}
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
						// Only sessions modified after this pane's last
						// activity are candidates: /new writes a fresh
						// events.jsonl and /resume appends a session.resume
						// event to the resumed one, so both show up here.
						// (Falling back to every session in the cwd, as this
						// code once did, re-bound a pane running a long tool —
						// silent transcript, busy PTY — to an old finished
						// session, flipping the card to idle and firing a
						// bogus "finished" notification.) Walk the whole list
						// so an already-claimed newest doesn't block the
						// watcher from claiming the next candidate.
						recent := findCopilotEventsFiles(stateDir, cwd, lastActivity, false)
						for _, cand := range recent {
							if cand.path == watchedFilePath {
								continue
							}
							// A session whose last event is session.shutdown
							// has ended: its process exited (e.g. a sibling
							// pane that was closed or killed, which writes
							// the shutdown as it dies and thereby looks
							// "recently modified"). It can never be the
							// session this live pane is running.
							if copilotSessionEnded(cand.path) {
								continue
							}
							if ClaimSessionForLeaf(agentID, cwd, cand.path, sessionID) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								watchedFilePath = cand.path
								lastFileSize = 0
								// /new creates a session that started after this
								// pane's last activity: its first read is live.
								// /resume re-attaches to an older one: replay.
								replay = !sessionStartedAfter(copilotSessionStart(cand.path), lastActivity)
								silentTicks = 0
								parseState.reset()
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
								if sid := copilotSessionIDFromPath(cand.path); sid != "" {
									agent.RecordExternalSession(sessionID, sid)
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

// copilotSessionStart returns the creation time recorded in the first line
// (session.start) of an events.jsonl, or the zero time.
func copilotSessionStart(eventsPath string) time.Time {
	head, err := ReadFirstLine(eventsPath)
	if err != nil {
		return time.Time{}
	}
	var ev struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal([]byte(head), &ev) != nil || ev.Type != "session.start" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, ev.Timestamp)
	return t
}

// copilotSessionEnded reports whether the last event in an events.jsonl is
// session.shutdown, i.e. the Copilot process that owned the session exited.
// Resuming the session appends session.resume after it, so a resumed session
// is not considered ended.
func copilotSessionEnded(eventsPath string) bool {
	info, err := os.Stat(eventsPath)
	if err != nil {
		return false
	}
	const tailBytes = 64 * 1024
	start := info.Size() - tailBytes
	if start < 0 {
		start = 0
	}
	lines, err := ReadNewLines(eventsPath, start)
	if err != nil {
		return false
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var ev copilotEvent
		if json.Unmarshal([]byte(lines[i]), &ev) != nil {
			continue // partial first line of the tail window
		}
		return ev.Type == "session.shutdown"
	}
	return false
}

// copilotSessionIDFromPath returns the Copilot session id (the parent
// directory name of the events.jsonl file). Copilot's `--continue` accepts an
// optional session id, so persisting it lets a reopen resume the exact session.
func copilotSessionIDFromPath(eventsPath string) string {
	return filepath.Base(filepath.Dir(eventsPath))
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

// copilotEventsForSession returns the events.jsonl path for the given session
// id under the session-state dir, or "" if it does not exist. Used to bind a
// reopened pane to its exact prior session.
func copilotEventsForSession(stateDir, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	p := filepath.Join(stateDir, sessionID, "events.jsonl")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
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
// updated in place and must survive across incremental reads (see
// copilotWatchState): subagent lifecycle, pending permission prompts and
// whether the last model call requested tools all span several reads.
func (w *CopilotWatcher) parseCopilotEvents(filePath string, offset int64, state *copilotWatchState, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt as the session title
	// fallback, retain the final visible assistant response, and maintain the
	// cross-read state (subagents, pending permissions, tool names).
	state.sawWork = false
	var sessionTitle string
	for _, line := range lines {
		var ev copilotEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant.turn_start", "tool.execution_complete", "subagent.started", "subagent.completed":
			state.sawWork = true
		}
		switch ev.Type {
		case "user.message":
			state.sawWork = true
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
			state.clearPermissions()
			state.lastMsgHadTools = false
			state.toolFailure, state.toolFailureTool = "", ""
			state.lastText = ""
		case "assistant.message":
			state.sawWork = true
			var msg copilotAssistantMsg
			if json.Unmarshal(ev.Data, &msg) == nil {
				if msg.Content != "" {
					state.lastText = msg.Content
				}
				state.lastMsgHadTools = len(msg.ToolRequests) > 0
				for _, tr := range msg.ToolRequests {
					if tr.ToolCallID != "" && tr.Name != "" {
						if state.toolNames == nil {
							state.toolNames = make(map[string]string)
						}
						state.toolNames[tr.ToolCallID] = tr.Name
					}
				}
			}
		case "tool.execution_complete":
			var tc copilotToolComplete
			if json.Unmarshal(ev.Data, &tc) == nil {
				if errText := tc.failure(); errText != "" {
					state.toolFailure, state.toolFailureTool = errText, state.toolNames[tc.ToolCallID]
				}
			}
		case "tool.execution_start":
			state.toolFailure, state.toolFailureTool = "", ""
			var exec copilotToolExecution
			if json.Unmarshal(ev.Data, &exec) == nil && exec.ToolCallID != "" && exec.ToolName != "" {
				if state.toolNames == nil {
					state.toolNames = make(map[string]string)
				}
				state.toolNames[exec.ToolCallID] = exec.ToolName
			}
		case "permission.requested":
			var pr copilotPermissionRequested
			if json.Unmarshal(ev.Data, &pr) == nil && pr.RequestID != "" && !pr.ResolvedByHook {
				if state.pendingPermissions == nil {
					state.pendingPermissions = make(map[string]copilotPendingPermission)
				}
				req := pr.PermissionRequest
				tool := state.toolNames[req.ToolCallID]
				if tool == "" {
					tool = req.ToolName
				}
				if tool == "" {
					tool = req.Kind
				}
				details := req.FullCommandText
				if details == "" {
					details = req.Path
				}
				if details == "" {
					details = req.FileName
				}
				if details == "" {
					details = req.Intention
				}
				state.pendingPermissions[pr.RequestID] = copilotPendingPermission{tool: tool, details: details}
				state.pendingOrder = append(state.pendingOrder, pr.RequestID)
			}
		case "permission.completed":
			var pc copilotPermissionCompleted
			if json.Unmarshal(ev.Data, &pc) == nil {
				delete(state.pendingPermissions, pc.RequestID)
			}
		case "abort", "session.error", "session.shutdown":
			// The turn ended: any prompt still on screen was dismissed.
			state.clearPermissions()
			state.toolFailure, state.toolFailureTool = "", ""
		case "subagent.started":
			var sub copilotSubagentEvent
			if json.Unmarshal(ev.Data, &sub) == nil && sub.ToolCallID != "" {
				if state.activeSubagents == nil {
					state.activeSubagents = make(map[string]string)
				}
				state.activeSubagents[sub.ToolCallID] = sub.AgentName
			}
		case "subagent.completed", "subagent.failed":
			var sub copilotSubagentEvent
			if json.Unmarshal(ev.Data, &sub) == nil && sub.ToolCallID != "" {
				delete(state.activeSubagents, sub.ToolCallID)
			}
		}
	}

	// A permission prompt still awaiting the user's decision dominates: the
	// agent is blocked until it is answered, whatever event preceded it.
	if p, ok := state.latestPendingPermission(); ok {
		callback("waiting_input", p.tool, p.details, sessionTitle)
		return
	}

	// working reports the in-turn "model is working" state: thinking, or
	// tool_failed while a failed tool call from this turn is still the latest
	// tool outcome.
	working := func(details string) {
		if state.toolFailure != "" {
			callback("tool_failed", state.toolFailureTool, state.toolFailure, sessionTitle)
			return
		}
		callback("thinking", "", details, sessionTitle)
	}

	// Reverse pass: determine status from the last meaningful event.
	// Skip intermediate events (hooks, usage records, session.*) that don't
	// represent agent status.
	for i := len(lines) - 1; i >= 0; i-- {
		var ev copilotEvent
		if json.Unmarshal([]byte(lines[i]), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant.turn_end":
			// Copilot ends a "turn" after every model call. When the last
			// assistant message requested tools, the model is about to be
			// called again with the tool results: the user's turn is NOT over.
			if state.lastMsgHadTools {
				working("")
				return
			}
			// The parent agent may finish one of several turns (e.g. the
			// turn that delegated work to a subagent) while background work
			// is still running. In that case the whole session is NOT done:
			// report "thinking" instead of "idle" so the user doesn't get a
			// false "finished" notification. Reporting "thinking" (rather
			// than "executing"/"background_task") also preserves the true
			// finished notification once the subagent actually completes.
			if len(state.activeSubagents) > 0 {
				callback("thinking", "", state.lastText, sessionTitle)
				return
			}
			state.toolFailure, state.toolFailureTool = "", ""
			callback("idle", "", state.lastText, sessionTitle)
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
			// Legacy event shape: a failed tool result carries is_error:true
			// (or an error object). Surface it as tool_failed; otherwise the
			// agent continues thinking.
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
		case "tool.execution_complete":
			// The event Copilot CLI persists when a tool finishes. A failed
			// call (success:false, or a shell command exiting non-zero) is
			// surfaced as tool_failed until the agent moves on.
			var tc copilotToolComplete
			if json.Unmarshal(ev.Data, &tc) == nil {
				if errText := tc.failure(); errText != "" {
					callback("tool_failed", state.toolNames[tc.ToolCallID], errText, sessionTitle)
					return
				}
			}
			callback("thinking", "", "", sessionTitle)
			return
		case "permission.completed":
			// The user (or a rule) answered the permission prompt. Approved:
			// the tool is now running. Denied: the model gets the rejection
			// and continues.
			var pc copilotPermissionCompleted
			if json.Unmarshal(ev.Data, &pc) == nil {
				if strings.HasPrefix(pc.Result.Kind, "approved") {
					callback("executing", state.toolNames[pc.ToolCallID], "", sessionTitle)
					return
				}
			}
			callback("thinking", "", "", sessionTitle)
			return
		case "tool.execution_start":
			// Copilot emits this immediately before it starts running a tool.
			// For user-input tools (ask_user, exit_plan_mode) "starting" means
			// the assistant is blocked waiting for the user to answer; it
			// never executes autonomously. This also covers a plan-mode exit
			// whose only durable trace is the execution event.
			var exec copilotToolExecution
			if json.Unmarshal(ev.Data, &exec) == nil {
				if isUserInputTool(strings.ToLower(exec.ToolName)) {
					callback("waiting_input", exec.ToolName, "", sessionTitle)
					return
				}
				if exec.ToolName != "" {
					callback("executing", exec.ToolName, "", sessionTitle)
					return
				}
			}
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
				// not executing.
				if isUserInputTool(strings.ToLower(toolName)) {
					status = "waiting_input"
				}
				callback(status, toolName, "", sessionTitle)
				return
			}
			// A message without tool requests is the model's final answer for
			// this round-trip, but the turn is only over at assistant.turn_end
			// (written right after). Keep reporting "thinking" until then so a
			// read that lands between the two never flips to idle early.
			// Keyword scanning of assistant text for "confirm"/"approve" was
			// removed: it produced false positives.
			working(msg.Content)
			return
		case "assistant.turn_start":
			working("")
			return
		case "user.message":
			callback("thinking", "", "", sessionTitle)
			return
		}
		// Skip hook.start, hook.end, session.*, system.*, usage records —
		// they are intermediate and don't represent agent status.
	}
}
