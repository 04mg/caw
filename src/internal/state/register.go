package state

import (
	"encoding/json"
	"net/http"
	"sync"

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

func RegisterHTTP(rg *gin.RouterGroup, store *Store, mux *ws.Multiplexer) {
	h := NewHandler(NewService(store, mux))
	rg.GET("/workspaces", h.GetWorkspaces)
	rg.POST("/workspaces", h.PutWorkspaces)
}

// RegisterMuxChannel wires the "state" channel into the multiplexer.
// On subscribe, the current state is sent to the client. Inbound messages
// are treated as state updates and broadcast to other subscribers. The
// broadcast is skipped if the normalized state JSON is identical to the
// last broadcast, preventing redundant full-state fan-out when multiple
// clients persist the same state in quick succession.
var (
	lastStateJSON []byte
	lastStateMu   sync.Mutex
)

func RegisterMuxChannel(mux *ws.Multiplexer, store *Store) {
	mux.HandleChannel("state",
		func(c *ws.MuxClient) {
			cur := store.Get()
			_ = c.Send("state", cur)
		},
		nil,
		func(c *ws.MuxClient, data []byte) {
			var s AppState
			if err := json.Unmarshal(data, &s); err != nil {
				return
			}
			if s.Workspaces == nil {
				s.Workspaces = []Workspace{}
			}
			store.Set(s)
			next := store.Get()
			nextJSON, _ := json.Marshal(next)

			lastStateMu.Lock()
			if string(nextJSON) == string(lastStateJSON) {
				lastStateMu.Unlock()
				return
			}
			lastStateJSON = nextJSON
			lastStateMu.Unlock()

			mux.BroadcastExcept("state", json.RawMessage(nextJSON), c)
		},
	)
}

// HandleStateWS is the legacy /ws/state endpoint kept for backward
// compatibility. New clients should use /ws with channel "state".
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
