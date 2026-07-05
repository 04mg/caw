package state

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	stateClients   = make(map[*websocket.Conn]bool)
	stateClientsMu sync.Mutex
)

var stateUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func RegisterWS(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("/ws/state", func(w http.ResponseWriter, r *http.Request) {
		c, err := stateUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		stateClientsMu.Lock()
		stateClients[c] = true
		stateClientsMu.Unlock()

		defer func() {
			stateClientsMu.Lock()
			delete(stateClients, c)
			stateClientsMu.Unlock()
			c.Close()
		}()

		cur := store.Get()
		if msg, err := json.Marshal(cur); err == nil {
			c.WriteMessage(websocket.TextMessage, msg)
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
			broadcastStateExcept(c, store)
		}
	})
}

func broadcastStateExcept(exclude *websocket.Conn, store *Store) {
	cur := store.Get()
	msg, err := json.Marshal(cur)
	if err != nil {
		return
	}
	stateClientsMu.Lock()
	conns := make([]*websocket.Conn, 0, len(stateClients))
	for c := range stateClients {
		if c != exclude {
			conns = append(conns, c)
		}
	}
	stateClientsMu.Unlock()
	for _, c := range conns {
		c.WriteMessage(websocket.TextMessage, msg)
	}
}

func broadcastStateToAll(store *Store) {
	cur := store.Get()
	msg, err := json.Marshal(cur)
	if err != nil {
		return
	}
	stateClientsMu.Lock()
	conns := make([]*websocket.Conn, 0, len(stateClients))
	for c := range stateClients {
		conns = append(conns, c)
	}
	stateClientsMu.Unlock()
	for _, c := range conns {
		c.WriteMessage(websocket.TextMessage, msg)
	}
}
