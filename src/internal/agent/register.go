package agent

import (
	"net/http"
	"sort"

	"github.com/gorilla/websocket"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/ws"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	httpx.RespondJSON(w, h.svc.ListAgents())
}

func (h *Handler) SetupWorkspace(w http.ResponseWriter, r *http.Request) {
	var req SetupWorkspaceRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	resp, err := h.svc.SetupWorkspace(req)
	if err != nil {
		if err == ErrProjectPathRequired {
			httpx.RespondBadRequest(w, err.Error())
			return
		}
		httpx.RespondInternal(w, err.Error())
		return
	}
	httpx.RespondJSON(w, resp)
}

func (h *Handler) CheckChanges(w http.ResponseWriter, r *http.Request) {
	worktreePath := r.URL.Query().Get("worktreePath")
	branchName := r.URL.Query().Get("branchName")
	baseBranch := r.URL.Query().Get("baseBranch")

	resp, err := h.svc.CheckChanges(worktreePath, branchName, baseBranch)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, resp)
}

func (h *Handler) ListStatuses(w http.ResponseWriter, r *http.Request) {
	statusesMu.RLock()
	list := make([]AgentStatus, 0, len(statuses))
	for _, s := range statuses {
		list = append(list, s)
	}
	statusesMu.RUnlock()
	sort.Slice(list, func(i, j int) bool {
		return list[i].Sequence < list[j].Sequence
	})
	httpx.RespondJSON(w, list)
}

// DismissStatus handles DELETE /agents/statuses/{id}. It removes a crashed
// card from the Kanban board. Only sessions currently in the "crashed"
// terminal state can be dismissed this way; live sessions are not affected
// (killing a live session is done via DELETE /terminals/{id}).
func (h *Handler) DismissStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !DismissCrashedSession(id) {
		httpx.RespondNotFound(w, "no crashed session with that id")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func Register(mux *http.ServeMux) {
	h := NewHandler(NewService())
	mux.HandleFunc("GET /agents", h.ListAgents)
	mux.HandleFunc("POST /agents", h.SetupWorkspace)
	mux.HandleFunc("GET /agents/changes", h.CheckChanges)
	mux.HandleFunc("GET /agents/statuses", h.ListStatuses)
	mux.HandleFunc("DELETE /agents/statuses/{id}", h.DismissStatus)
}

func RegisterMuxChannel(mux *ws.Multiplexer) {
	mux.HandleChannel("agents",
		func(c *ws.MuxClient) {
			statusesMu.RLock()
			states := make([]AgentStatus, 0, len(statuses))
			for _, s := range statuses {
				states = append(states, s)
			}
			statusesMu.RUnlock()
			sort.Slice(states, func(i, j int) bool {
				return states[i].Sequence < states[j].Sequence
			})
			// Send the full current snapshot as a single authoritative event.
			// The frontend replaces its store with this list, which prunes any
			// stale cards (e.g. sessions the backend stopped tracking after a
			// restart) that would otherwise linger because no agent_stopped
			// event fires for them. Sending one message avoids the ordering
			// race of a per-status dump interleaved with live broadcasts.
			_ = c.Send("agents", struct {
				Type     string        `json:"event"`
				Sessions []AgentStatus `json:"sessions"`
			}{Type: "agent_snapshot", Sessions: states})
		},
		nil,
		nil,
	)
}

func HandleStatusWS(w http.ResponseWriter, r *http.Request, hub *ws.Hub) {
	c, err := ws.DefaultUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	wc := hub.Register(c)
	stopPing := ws.StartKeepalive(c, ws.PingWriter(wc.WriteMessage))
	defer func() {
		stopPing()
		hub.Unregister(wc)
		c.Close()
	}()

	statusesMu.RLock()
	states := make([]AgentStatus, 0, len(statuses))
	for _, s := range statuses {
		states = append(states, s)
	}
	statusesMu.RUnlock()
	sort.Slice(states, func(i, j int) bool {
		return states[i].Sequence < states[j].Sequence
	})
	for _, s := range states {
		msg, _ := marshalEvent(Event{
			Type:       "agent_status",
			SessionID:  s.SessionID,
			AgentID:    s.AgentID,
			Cwd:        s.Cwd,
			Status:     s.Status,
			Tool:       s.Tool,
			Details:    s.Details,
			Title:      s.Title,
			Timestamp:  s.Timestamp,
			Sequence:   s.Sequence,
			EndedAt:    s.EndedAt,
			ExitCode:   s.ExitCode,
			ExitReason: s.ExitReason,
			LastColumn: s.LastColumn,
		})
		_ = wc.WriteMessage(websocket.TextMessage, msg)
	}

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}