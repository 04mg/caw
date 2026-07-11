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
)

// SetPushStore wires the state store into the agent package so that status
// transitions can dispatch web push notifications. Called once from
// server.New().
func SetPushStore(s *state.Store) { pushStore = s }

func init() {
	terminal.OnSessionStart = handleSessionStart
	terminal.OnSessionExit = handleSessionExit
	terminal.OnPtyActivity = handlePtyActivity
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

// RegisterStatusWatcher allows status providers to register themselves
func RegisterStatusWatcher(agentID string, w StatusWatcher) {
	watchersMu.Lock()
	watchers[agentID] = w
	watchersMu.Unlock()
}

func StatusHub() *ws.Hub { return statusHub }

func marshalEvent(ev Event) ([]byte, error) { return json.Marshal(ev) }

func broadcastEvent(ev Event) {
	msg, err := marshalEvent(ev)
	if err != nil {
		return
	}
	statusHub.Broadcast(websocket.TextMessage, msg)
}

func updateStatus(sessionID, agentID, cwd, status, tool, details, title string) {
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
			if exists && (prev.Status == "thinking" || prev.Status == "executing") {
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
// Set to 5 minutes because LLM responses can take several minutes (especially
// for long tool chains or slow providers). A 30s timeout caused false "idle"
// transitions while the agent was legitimately waiting for an LLM response.
const idleTimeout = 5 * time.Minute

func watchAgent(ctx context.Context, wCtx *watcherContext) {
	watchersMu.Lock()
	watcher, ok := watchers[wCtx.agentId]
	watchersMu.Unlock()

	if !ok {
		return
	}

	// lastActivity tracks when the watcher last received any status update so
	// we can apply the idle timeout even if the watcher goroutine is still running.
	lastActivity := time.Now()

	// Idle-timeout watchdog: if the agent hasn't emitted a status change in
	// idleTimeout seconds and the last known status is non-idle, revert to idle.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Since(lastActivity) < idleTimeout {
					continue
				}
				statusesMu.RLock()
				s, exists := statuses[wCtx.sessionId]
				statusesMu.RUnlock()
				// Do NOT auto-revert "waiting_input": the agent is blocked
				// waiting for the user to answer — this can take minutes.
				// Only revert transient "working" states (thinking/executing)
				// that have stalled, which indicates a crash.
				if exists && s.Status != "idle" && s.Status != "stopped" && s.Status != "waiting_input" {
					updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, "idle", "", "", s.Title)
				}
			}
		}
	}()

	watcher.Watch(ctx, wCtx.sessionId, wCtx.cwd, wCtx.resume, func(status, tool, details, title string) {
		lastActivity = time.Now()
		updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, status, tool, details, title)
	}, func() {
		lastActivity = time.Now()
	})
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
