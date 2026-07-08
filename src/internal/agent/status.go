package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/04mg/caw/internal/httputil"
	"github.com/04mg/caw/internal/terminal"
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
}

type watcherContext struct {
	cancel    context.CancelFunc
	agentId   string
	sessionId string
	cwd       string
	cmd       []string
}

// StatusWatcher is the interface that each agent status provider must implement
type StatusWatcher interface {
	Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, title string))
}

var (
	statuses       = make(map[string]AgentStatus)
	statusesMu     sync.RWMutex
	activeSessions = make(map[string]*watcherContext)
	activeSesMu    sync.Mutex
	wsClients      = make(map[*websocket.Conn]bool)
	wsClientsMu    sync.Mutex
	wsUpgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	watchers   = make(map[string]StatusWatcher)
	watchersMu sync.Mutex
)

func init() {
	terminal.OnSessionStart = handleSessionStart
	terminal.OnSessionExit = handleSessionExit
}

// RegisterStatusWatcher allows status providers to register themselves
func RegisterStatusWatcher(agentID string, w StatusWatcher) {
	watchersMu.Lock()
	watchers[agentID] = w
	watchersMu.Unlock()
}

// RegisterStatusWS registers the /ws/agent-status endpoint
func RegisterStatusWS(mux *http.ServeMux) {
	mux.HandleFunc("/ws/agent-status", func(w http.ResponseWriter, r *http.Request) {
		c, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		wsClientsMu.Lock()
		wsClients[c] = true
		wsClientsMu.Unlock()

		defer func() {
			wsClientsMu.Lock()
			delete(wsClients, c)
			wsClientsMu.Unlock()
			c.Close()
		}()

		// Send initial statuses
		statusesMu.RLock()
		for _, s := range statuses {
			msg, _ := json.Marshal(Event{
				Type:      "agent_status",
				SessionID: s.SessionID,
				AgentID:   s.AgentID,
				Status:    s.Status,
				Tool:      s.Tool,
				Details:   s.Details,
				Title:     s.Title,
				Timestamp: s.Timestamp,
			})
			_ = c.WriteMessage(websocket.TextMessage, msg)
		}
		statusesMu.RUnlock()

		// Keep connection alive
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
		}
	})

	// Also expose standard HTTP GET for current status list
	mux.HandleFunc("/api/agents/status", func(w http.ResponseWriter, r *http.Request) {
		statusesMu.RLock()
		list := make([]AgentStatus, 0, len(statuses))
		for _, s := range statuses {
			list = append(list, s)
		}
		statusesMu.RUnlock()
		httputil.WriteJSON(w, list)
	})
}

func broadcastEvent(ev Event) {
	msg, err := json.Marshal(ev)
	if err != nil {
		return
	}

	wsClientsMu.Lock()
	conns := make([]*websocket.Conn, 0, len(wsClients))
	for c := range wsClients {
		conns = append(conns, c)
	}
	wsClientsMu.Unlock()

	for _, c := range conns {
		_ = c.WriteMessage(websocket.TextMessage, msg)
	}
}

func updateStatus(sessionID, agentID, cwd, status, tool, details, title string) {
	statusesMu.Lock()
	prev, exists := statuses[sessionID]
	now := time.Now()
	if exists && prev.Status == status && prev.Tool == tool && prev.Details == details && prev.Title == title {
		statusesMu.Unlock()
		return // no change
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
	})
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
const idleTimeout = 30 * time.Second

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

	watcher.Watch(ctx, wCtx.sessionId, wCtx.cwd, func(status, tool, details, title string) {
		lastActivity = time.Now()
		updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, status, tool, details, title)
	})
}
