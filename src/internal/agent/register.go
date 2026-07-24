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

func Register(mux *http.ServeMux) {
	h := NewHandler(NewService())
	mux.HandleFunc("GET /agents", h.ListAgents)
	mux.HandleFunc("POST /agents", h.SetupWorkspace)
	mux.HandleFunc("GET /agents/changes", h.CheckChanges)
	mux.HandleFunc("GET /agents/statuses", h.ListStatuses)
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
			for _, s := range states {
				_ = c.Send("agents", Event{
					Type:      "agent_status",
					SessionID: s.SessionID,
					AgentID:   s.AgentID,
					Cwd:       s.Cwd,
					Status:    s.Status,
					Tool:      s.Tool,
					Details:   s.Details,
					Title:     s.Title,
					Timestamp: s.Timestamp,
					Sequence:  s.Sequence,
				})
			}
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
			Type:      "agent_status",
			SessionID: s.SessionID,
			AgentID:   s.AgentID,
			Cwd:       s.Cwd,
			Status:    s.Status,
			Tool:      s.Tool,
			Details:   s.Details,
			Title:     s.Title,
			Timestamp: s.Timestamp,
			Sequence:  s.Sequence,
		})
		_ = wc.WriteMessage(websocket.TextMessage, msg)
	}

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}