package agent

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
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

func (h *Handler) ListAgents(c *gin.Context) {
	httpx.OK(c, h.svc.ListAgents())
}

func (h *Handler) SetupWorkspace(c *gin.Context) {
	var req SetupWorkspaceRequest
	if !httpx.Bind(c, &req) {
		return
	}
	resp, err := h.svc.SetupWorkspace(req)
	if err != nil {
		if err == ErrProjectPathRequired {
			httpx.BadRequest(c, err.Error())
			return
		}
		httpx.Internal(c, err.Error())
		return
	}
	httpx.OK(c, resp)
}

func (h *Handler) CheckChanges(c *gin.Context) {
	worktreePath := c.Query("worktreePath")
	branchName := c.Query("branchName")
	baseBranch := c.Query("baseBranch")

	resp, err := h.svc.CheckChanges(worktreePath, branchName, baseBranch)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, resp)
}

func (h *Handler) ListStatuses(c *gin.Context) {
	statusesMu.RLock()
	list := make([]AgentStatus, 0, len(statuses))
	for _, s := range statuses {
		list = append(list, s)
	}
	statusesMu.RUnlock()
	// Return in stable opening order so the UI doesn't reshuffle on every
	// re-fetch (map iteration order is not deterministic).
	sort.Slice(list, func(i, j int) bool {
		return list[i].Sequence < list[j].Sequence
	})
	httpx.OK(c, list)
}

func Register(rg *gin.RouterGroup) {
	h := NewHandler(NewService())
	rg.GET("/agents", h.ListAgents)
	rg.POST("/agents", h.SetupWorkspace)
	rg.GET("/agents/changes", h.CheckChanges)
	rg.GET("/agents/statuses", h.ListStatuses)
}

// RegisterMuxChannel wires the "agents" channel into the multiplexer.
// On subscribe, all current statuses are sent to the client. No inbound
// messages are expected on this channel.
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