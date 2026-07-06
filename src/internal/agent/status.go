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
	Status    string    `json:"status"` // "thinking", "executing", "waiting_input", "idle", "stopped"
	Tool      string    `json:"tool,omitempty"`
	Details   string    `json:"details,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`
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
	Prompt    string    `json:"prompt,omitempty"`
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
	Watch(ctx context.Context, sessionID string, cwd string, callback func(status, tool, details, prompt string))
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
				Prompt:    s.Prompt,
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

func updateStatus(sessionID, agentID, status, tool, details, prompt string) {
	statusesMu.Lock()
	prev, exists := statuses[sessionID]
	now := time.Now()
	if exists && prev.Status == status && prev.Tool == tool && prev.Details == details && prev.Prompt == prompt {
		statusesMu.Unlock()
		return // no change
	}

	s := AgentStatus{
		SessionID: sessionID,
		AgentID:   agentID,
		Status:    status,
		Tool:      tool,
		Details:   details,
		Prompt:    prompt,
		Timestamp: now,
	}
	statuses[sessionID] = s
	statusesMu.Unlock()

	broadcastEvent(Event{
		Type:      "agent_status",
		SessionID: sessionID,
		AgentID:   agentID,
		Status:    status,
		Tool:      tool,
		Details:   details,
		Prompt:    prompt,
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

	updateStatus(id, agentID, "idle", "", "", "")

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

func watchAgent(ctx context.Context, wCtx *watcherContext) {
	watchersMu.Lock()
	watcher, ok := watchers[wCtx.agentId]
	watchersMu.Unlock()

	if !ok {
		return
	}

	watcher.Watch(ctx, wCtx.sessionId, wCtx.cwd, func(status, tool, details, prompt string) {
		updateStatus(wCtx.sessionId, wCtx.agentId, status, tool, details, prompt)
	})
}
