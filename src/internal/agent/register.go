package agent

import (
	"net/http"

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
	httpx.OK(c, list)
}

func Register(rg *gin.RouterGroup) {
	h := NewHandler(NewService())
	rg.GET("/agents", h.ListAgents)
	rg.POST("/agents", h.SetupWorkspace)
	rg.GET("/agents/changes", h.CheckChanges)
	rg.GET("/agents/statuses", h.ListStatuses)
}

func HandleStatusWS(w http.ResponseWriter, r *http.Request, hub *ws.Hub) {
	c, err := ws.DefaultUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	wc := hub.Register(c)
	defer func() {
		hub.Unregister(wc)
		c.Close()
	}()

	statusesMu.RLock()
	for _, s := range statuses {
		msg, _ := marshalEvent(Event{
			Type:      "agent_status",
			SessionID: s.SessionID,
			AgentID:   s.AgentID,
			Status:    s.Status,
			Tool:      s.Tool,
			Details:   s.Details,
			Title:     s.Title,
			Timestamp: s.Timestamp,
		})
		_ = wc.WriteMessage(websocket.TextMessage, msg)
	}
	statusesMu.RUnlock()

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}