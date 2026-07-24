package state

import (
	"encoding/json"
	"net/http"
	"sync"

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

func (h *Handler) GetWorkspaces(w http.ResponseWriter, r *http.Request) {
	httpx.RespondJSON(w, h.svc.Get())
}

func (h *Handler) PutWorkspaces(w http.ResponseWriter, r *http.Request) {
	var s AppState
	if !httpx.BindRequest(w, r, &s) {
		return
	}
	h.svc.Set(s)
	httpx.RespondJSON(w, map[string]bool{"ok": true})
}

func RegisterHTTP(mux *http.ServeMux, store *Store, muxWS *ws.Multiplexer) {
	h := NewHandler(NewService(store, muxWS))
	mux.HandleFunc("GET /workspaces", h.GetWorkspaces)
	mux.HandleFunc("POST /workspaces", h.PutWorkspaces)
}

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
