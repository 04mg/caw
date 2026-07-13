package state

import (
	"encoding/json"
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

func (h *Handler) GetWorkspaces(c *gin.Context) {
	httpx.OK(c, h.svc.Get())
}

func (h *Handler) PutWorkspaces(c *gin.Context) {
	var s AppState
	if !httpx.Bind(c, &s) {
		return
	}
	h.svc.Set(s)
	httpx.OK(c, map[string]bool{"ok": true})
}

func RegisterHTTP(rg *gin.RouterGroup, store *Store, hub *ws.Hub) {
	h := NewHandler(NewService(store, hub))
	rg.GET("/workspaces", h.GetWorkspaces)
	rg.POST("/workspaces", h.PutWorkspaces)
}

func HandleStateWS(w http.ResponseWriter, r *http.Request, store *Store, hub *ws.Hub) {
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

	cur := store.Get()
	curJSON, _ := json.Marshal(cur)
	if curJSON != nil {
		_ = wc.WriteMessage(websocket.TextMessage, curJSON)
	}

	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		var s AppState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.Workspaces == nil {
			s.Workspaces = []Workspace{}
		}
		store.Set(s)
		next := store.Get()
		nextJSON, _ := json.Marshal(next)
		if string(nextJSON) == string(curJSON) {
			continue
		}
		curJSON = nextJSON
		hub.BroadcastExcept(websocket.TextMessage, nextJSON, wc)
	}
}
