package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/04mg/caw/internal/push"
	"github.com/04mg/caw/internal/state"
	"github.com/04mg/caw/internal/terminal"
	"github.com/04mg/caw/internal/ws"
)

// AgentStatus represents the current tracked state of an agent
type AgentStatus struct {
	SessionID string    `json:"sessionId"`
	AgentID   string    `json:"agentId"`
	Cwd       string    `json:"cwd,omitempty"`
	Status    string    `json:"status"` // "thinking", "executing", "waiting_input", "idle", "stopped"
	Tool      string    `json:"tool,omitempty"`
	Details   string    `json:"details,omitempty"`
	Title     string    `json:"title,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	// Sequence is a monotonically increasing value assigned when a session is
	// first tracked. It reflects the order in which agents were opened and is
	// used to keep a stable ordering in the UI instead of relying on the map
	// iteration order or timestamps (which can reorder when re-fetching).
	Sequence int64 `json:"sequence"`
}

// Event represents a WebSocket event message
type Event struct {
	Type      string    `json:"event"`
	SessionID string    `json:"sessionId"`
	AgentID   string    `json:"agentId"`
	Cwd       string    `json:"cwd,omitempty"`
	Status    string    `json:"status,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Details   string    `json:"details,omitempty"`
	Title     string    `json:"title,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Sequence  int64     `json:"sequence"`
}

type watcherContext struct {
	cancel    context.CancelFunc
	agentId   string
	sessionId string
	cwd       string
	cmd       []string
	resume    bool
}

// StatusWatcher is the interface that each agent status provider must implement.
// The resume flag is true when the agent was launched with a resume/continue
// flag (e.g. --continue), meaning it reattaches to a pre-existing internal
// session. In that case watchers should look for the most recent session in
// the cwd regardless of when it was last updated, instead of only sessions
// started after the watcher launched.
//
// The heartbeat callback should be called on every poll iteration (even when
// nothing changed) to signal that the watcher is alive and the agent process
// is still running. This prevents the idle-timeout watchdog from falsely
// reverting the status to "idle" during long LLM response waits where the
// underlying transcript/DB file doesn't change for minutes.
type StatusWatcher interface {
	Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func())
}

var (
	statuses       = make(map[string]AgentStatus)
	statusesMu     sync.RWMutex
	activeSessions = make(map[string]*watcherContext)
	activeSesMu    sync.Mutex
	statusMux      *ws.Multiplexer
	statusHub      = ws.NewHub()
	watchers       = make(map[string]StatusWatcher)
	watchersMu     sync.Mutex
	// statusSeq is a monotonic counter used to assign a stable opening-order
	// sequence to each agent session. It is only mutated under statusesMu.
	statusSeq int64
	// pushStore is set by SetPushStore at server startup so that agent status
	// transitions can trigger web push notifications.
	pushStore *state.Store
	// ptyActivity tracks the last time each leaf/session id received bytes
	// from its PTY. Updated via OnPtyActivity from terminal.ReadLoop and read
	// by watchers to correlate lazily-created internal agent sessions to the
	// correct PTY when multiple agents of the same type share a cwd.
	ptyActivity   = make(map[string]time.Time)
	ptyActivityMu sync.RWMutex
	// ptyFocus tracks which leaf/session id currently has the user's focus
	// (i.e. is the active terminal pane the user is typing into). Updated via
	// OnPtyFocus when the frontend reports a focus/blur event for a pane.
	// Read by the idle-timeout watchdog and the watchers' re-bind pass:
	//   - A focused PTY is exempt from the auto-revert-to-idle even when its
	//     output has gone quiet (the user may be mid-composition or reading).
	//   - A focused PTY is allowed to re-bind to a sibling internal session
	//     even with no recent PTY output (the user issued /new or /resume and
	//     is waiting for the agent to start, which may not produce PTY bytes
	//     immediately).
	// Conversely, an unfocused PTY is more conservative: it will not steal
	// a sibling's session and the idle-timeout watchdog may revert it.
	ptyFocus   = make(map[string]bool)
	ptyFocusMu sync.RWMutex
)

// SetStatusMux wires the multiplexer into the agent package so that status
// transitions broadcast on the "agents" channel. Called once from
// server.New(). Falls back to the legacy statusHub when nil (legacy
// /ws/agents/statuses endpoint).
func SetStatusMux(m *ws.Multiplexer) { statusMux = m }

// SetPushStore wires the state store into the agent package so that status
// transitions can dispatch web push notifications. Called once from
// server.New().
func SetPushStore(s *state.Store) { pushStore = s }

func init() {
	terminal.OnSessionStart = handleSessionStart
	terminal.OnSessionExit = handleSessionExit
	terminal.OnPtyActivity = handlePtyActivity
	terminal.OnPtyInput = handlePtyInput
	terminal.OnPtyFocus = handlePtyFocus
}

// handlePtyInput detects when the user submits a command to a PTY.
// If the agent is currently idle, we transition it to thinking.
//
// To avoid false transitions from TUI initialization sequences (resize,
// cursor positioning, etc. that contain \r\n), we only fire if the PTY
// has already produced output — i.e., the agent process is actually
// running and has rendered its UI. Without this guard, opening a new
// agent session immediately flips to "thinking" because the terminal
// sends control sequences on connect.
func handlePtyInput(id string, data string) {
	// Disabled to prevent transitioning to "thinking" on non-agent PTY inputs.
	// (e.g. regular shell commands). The status will transition to "thinking"
	// or "executing" via the log watcher when the agent actually starts.
}

// handlePtyFocus records whether the given leaf/session id currently has the
// user's focus. Called from terminal.HandleTerminalWS when the frontend sends
// a `focus`/`blur` message after the active pane changes. Also implicitly
// clears focus on any other pane: only one pane is focused at a time.
func handlePtyFocus(id string, focused bool) {
	ptyFocusMu.Lock()
	if focused {
		// Single-focus model: clear any previously focused pane so the
		// invariant "at most one entry is true" holds even if the frontend
		// ever forgets to send a blur before a focus on another pane.
		for k := range ptyFocus {
			if k != id {
				delete(ptyFocus, k)
			}
		}
		ptyFocus[id] = true
	} else {
		// Only delete if it was our entry — avoids clobbering a newer focus
		// that arrived between our blur and its processing.
		if ptyFocus[id] {
			delete(ptyFocus, id)
		}
	}
	ptyFocusMu.Unlock()
}

// handlePtyActivity records that the PTY for the given leaf/session id just
// produced output. Called from terminal.ReadLoop on every read.
func handlePtyActivity(id string, n int) {
	if n <= 0 {
		return
	}
	ptyActivityMu.Lock()
	ptyActivity[id] = time.Now()
	ptyActivityMu.Unlock()
}

// LastPtyActivity returns the timestamp of the most recent PTY output for the
// given leaf/session id, or the zero time if no activity has been recorded.
// Used by watchers to determine whether their agent process is currently
// producing output, which disambiguates which internal session belongs to
// which PTY when multiple agents of the same type share a cwd.
func LastPtyActivity(sessionID string) time.Time {
	ptyActivityMu.RLock()
	defer ptyActivityMu.RUnlock()
	return ptyActivity[sessionID]
}

// IsPtyFocused reports whether the given leaf/session id currently has the
// user's focus (i.e. is the active terminal pane the user is typing into).
// Used by watchers and the idle-timeout watchdog to bias the heuristics
// toward the pane the user is actually driving: a focused PTY is exempt
// from the auto-revert-to-idle and is allowed to re-bind to a sibling
// internal session even without recent PTY output.
func IsPtyFocused(sessionID string) bool {
	ptyFocusMu.RLock()
	defer ptyFocusMu.RUnlock()
	return ptyFocus[sessionID]
}

// RegisterStatusWatcher allows status providers to register themselves
func RegisterStatusWatcher(agentID string, w StatusWatcher) {
	watchersMu.Lock()
	watchers[agentID] = w
	watchersMu.Unlock()
}

func StatusHub() *ws.Hub { return statusHub }

func marshalEvent(ev Event) ([]byte, error) { return json.Marshal(ev) }

func broadcastEvent(ev Event) {
	if statusMux != nil {
		statusMux.Broadcast("agents", ev)
		return
	}
	msg, err := marshalEvent(ev)
	if err != nil {
		return
	}
	statusHub.Broadcast(websocket.TextMessage, msg)
}

func updateStatus(sessionID, agentID, cwd, status, tool, details, title string) {
	// Strip markdown formatting from user-visible text so the Kanban card
	// Info line renders as clean plain text regardless of which agent
	// produced it. Titles are already cleaned by CleanPrompt upstream, but
	// details (assistant text excerpts) can contain **bold**, `code`, [links],
	// # headings, etc.
	details = StripMarkdown(details)
	title = StripMarkdown(title)

	statusesMu.Lock()
	prev, exists := statuses[sessionID]
	now := time.Now()
	if exists && prev.Status == status && prev.Tool == tool && prev.Details == details && prev.Title == title {
		statusesMu.Unlock()
		return // no change
	}

	seq := prev.Sequence
	if !exists {
		statusSeq++
		seq = statusSeq
	}

	s := AgentStatus{
		SessionID: sessionID,
		AgentID:   agentID,
		Cwd:       cwd,
		Status:    status,
		Tool:      tool,
		Details:   details,
		Title:     title,
		Timestamp: now,
		Sequence:  seq,
	}
	statuses[sessionID] = s
	statusesMu.Unlock()

	broadcastEvent(Event{
		Type:      "agent_status",
		SessionID: sessionID,
		AgentID:   agentID,
		Cwd:       cwd,
		Status:    status,
		Tool:      tool,
		Details:   details,
		Title:     title,
		Timestamp: now,
		Sequence:  seq,
	})

	if pushStore != nil {
		switch status {
		case "waiting_input":
			push.CancelFinishedDebounced(sessionID)
			go push.Dispatch(pushStore, "needs_input", sessionID, agentID, title, "")
		case "thinking", "executing":
			push.CancelFinishedDebounced(sessionID)
		case "idle", "stopped":
			// Suppress the "finished" notification when the agent was running
			// a background task or subagent — the agent is still working, it
			// just completed a sub-task. A "finished" notification here would
			// mislead the user.
			//   - "background_task": Antigravity's background tasks.
			//   - "task": OpenCode's subagent launcher. When the subagent
			//     finishes, the parent briefly transitions executing→idle
			//     before continuing, which would otherwise fire a spurious
			//     "finished" push notification.
			if exists && (prev.Status == "thinking" || prev.Status == "executing") && !isSubagentTool(prev.Tool) {
				push.DispatchFinishedDebounced(pushStore, sessionID, agentID, title, "")
			}
		}
	}
}

func handleSessionStart(id string, cmd []string, cwd string) {
	if len(cmd) == 0 {
		return
	}

	baseCmd := filepath.Base(cmd[0])
	baseCmd = strings.TrimSuffix(baseCmd, ".exe")

	var agentID string
	switch strings.ToLower(baseCmd) {
	case "claude":
		agentID = "claude"
	case "codex":
		agentID = "codex"
	case "copilot":
		agentID = "copilot"
	case "agy":
		agentID = "agy"
	case "opencode":
		agentID = "opencode"
	case "pi":
		agentID = "pi"
	default:
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	wCtx := &watcherContext{
		cancel:    cancel,
		agentId:   agentID,
		sessionId: id,
		cwd:       cwd,
		cmd:       cmd,
		resume:    isResumeCmd(cmd),
	}

	activeSesMu.Lock()
	activeSessions[id] = wCtx
	activeSesMu.Unlock()

	broadcastEvent(Event{
		Type:      "agent_started",
		SessionID: id,
		AgentID:   agentID,
		Cwd:       cwd,
		Timestamp: time.Now(),
	})

	updateStatus(id, agentID, cwd, "idle", "", "", "")

	go watchAgent(ctx, wCtx)
}

func handleSessionExit(id string) {
	activeSesMu.Lock()
	wCtx, exists := activeSessions[id]
	if exists {
		delete(activeSessions, id)
	}
	activeSesMu.Unlock()

	if exists {
		wCtx.cancel()
		push.CancelFinishedDebounced(id)
		statusesMu.Lock()
		delete(statuses, id)
		statusesMu.Unlock()

		ptyFocusMu.Lock()
		delete(ptyFocus, id)
		ptyFocusMu.Unlock()

		broadcastEvent(Event{
			Type:      "agent_stopped",
			SessionID: id,
			AgentID:   wCtx.agentId,
			Timestamp: time.Now(),
		})
	}
}

// idleTimeout is the duration after which an agent stuck in a non-idle state
// with no new updates will be automatically reverted to idle. This prevents
// the KanbanBoard from showing "working" indefinitely if the agent crashes
// without triggering a clean session exit.
//
// Set to 2 minutes as a balance: LLM responses can take a minute or more
// for complex tool chains, but an interrupt that doesn't write to the
// transcript should be detected sooner than 5 minutes.
const idleTimeout = 2 * time.Minute

// ptyActivityWindow is the recency window used by the idle-timeout watchdog
// to decide whether recent PTY output should keep the agent in a "working"
// state. If the agent's PTY has produced bytes within this window, the
// watchdog skips the auto-revert to "idle" even when no transcript/DB writes
// have happened for a while (e.g. a long bash command streaming output).
// It is intentionally longer than the poll interval so brief output gaps
// during tool execution don't trigger a premature revert.
const ptyActivityWindow = 30 * time.Second

func watchAgent(ctx context.Context, wCtx *watcherContext) {
	watchersMu.Lock()
	watcher, ok := watchers[wCtx.agentId]
	watchersMu.Unlock()

	if !ok {
		return
	}

	// lastActivity tracks when the watcher last received an actual status change
	// from the agent log.
	lastActivity := time.Now()
	// lastHeartbeat tracks the last poll confirmation from the watcher.
	lastHeartbeat := time.Now()

	// Idle-timeout watchdog: if the agent hasn't emitted a status change in
	// idleTimeout seconds, or if the watcher itself has stopped calling the
	// heartbeat callback (indicating a crash), revert to idle.
	//
	// PTY activity exemption: a long-running tool call (e.g. a bash command
	// or a lengthy tool that streams output) may not produce any new
	// transcript/DB writes for minutes while it runs. The agent is still
	// actively working — its PTY keeps emitting output — so reverting to
	// "idle" here would be wrong. When the PTY has produced output within
	// the last ptyActivityWindow, we skip the revert entirely. This applies
	// to every agent (the watchdog is shared); the LLM-thinking phase after
	// a tool finishes does write to the transcript, so the 2-minute idle
	// timeout still catches genuinely stalled sessions.
	//
	// Focused PTY exemption: even when the PTY has been quiet (no output and
	// no transcript writes) for longer than the timeout, if the user is
	// currently focused on this pane we keep the last non-idle status. The
	// user may be mid-composition (typing a prompt that hasn't been
	// submitted), reading a long answer, or waiting for an answer that
	// doesn't stream to the transcript until it completes. Reverting a
	// pane the user is actively looking at to "idle" is more confusing
	// than keeping the previous status, and a genuinely-stalled focused
	// session will still be caught when the user eventually blurs it.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Since(lastHeartbeat) > 30*time.Second || time.Since(lastActivity) > idleTimeout {
					// If the PTY has produced output recently, the agent
					// process is still alive and actively running a tool —
					// don't revert to idle.
					lastPtyOut := LastPtyActivity(wCtx.sessionId)
					if !lastPtyOut.IsZero() && time.Since(lastPtyOut) < ptyActivityWindow {
						continue
					}
					// If this pane currently has the user's focus, don't
					// auto-revert: the user is actively interacting with it
					// and a spurious "idle" would be misleading.
					if IsPtyFocused(wCtx.sessionId) {
						continue
					}
					statusesMu.RLock()
					s, exists := statuses[wCtx.sessionId]
					statusesMu.RUnlock()
					// Do NOT auto-revert "waiting_input": the agent is blocked
					// waiting for the user to answer — this can take minutes.
					// Only revert transient "working" states (thinking/executing)
					// that have stalled.
					if exists && s.Status != "idle" && s.Status != "stopped" && s.Status != "waiting_input" {
						updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, "idle", "", "", s.Title)
					}
				}
			}
		}
	}()

	watcher.Watch(ctx, wCtx.sessionId, wCtx.cwd, wCtx.resume, func(status, tool, details, title string) {
		lastActivity = time.Now()
		lastHeartbeat = time.Now()
		updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, status, tool, details, title)
	}, func() {
		lastHeartbeat = time.Now()
	})
}

// isSubagentTool reports whether the given tool name represents a background
// task or subagent launcher. When such a tool was the agent's last action,
// transitioning to "idle" should NOT fire a "finished" push notification —
// the agent is still working, it just completed a sub-task.
func isSubagentTool(tool string) bool {
	switch tool {
	case "background_task", "task":
		return true
	}
	return false
}

// isResumeCmd reports whether the agent launch command contains a resume/
// continue flag, indicating the agent will reattach to a pre-existing
// internal session rather than starting a new one. This is set by
// terminal.resumeCmdForAgent on reopen.
func isResumeCmd(cmd []string) bool {
	for _, a := range cmd {
		switch a {
		case "--continue", "-c", "--resume", "--last":
			return true
		}
	}
	// codex uses "resume" as a subcommand
	for _, a := range cmd {
		if a == "resume" {
			return true
		}
	}
	return false
}
