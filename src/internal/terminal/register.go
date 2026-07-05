package terminal

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/04mg/caw/internal/httputil"
)

type CreateRequest struct {
	Cwd string   `json:"cwd"`
	ID  string   `json:"id"`
	Cmd []string `json:"cmd,omitempty"`
}

func Register(mux *http.ServeMux, sessions map[string]*Session, sessionsMu *sync.RWMutex, upgrader *websocket.Upgrader) {
	mux.HandleFunc("/api/terminal/create", func(w http.ResponseWriter, r *http.Request) {
		var req CreateRequest
		if err := httputil.ReadJSON(r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cwd := req.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		if req.ID != "" {
			sessionsMu.RLock()
			existing, ok := sessions[req.ID]
			sessionsMu.RUnlock()
			if ok {
				httputil.WriteJSON(w, map[string]string{"id": existing.ID})
				return
			}
		}

		id := req.ID
		if id == "" {
			id = uuid.New().String()
		}

		ps, err := startPty(cwd, req.Cmd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sess := &Session{
			ID:         id,
			Pty:        ps,
			Cwd:        cwd,
			conns:      make(map[*websocket.Conn]bool),
			scrollback: []byte{},
			onExit: func() {
				sessionsMu.Lock()
				delete(sessions, id)
				sessionsMu.Unlock()
				ps.Close()
			},
		}
		sessionsMu.Lock()
		sessions[id] = sess
		sessionsMu.Unlock()

		go sess.ReadLoop()

		httputil.WriteJSON(w, map[string]string{"id": id})
	})

	mux.HandleFunc("/ws/terminal/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/ws/terminal/"):]
		sessionsMu.RLock()
		sess, ok := sessions[id]
		sessionsMu.RUnlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		sess.mu.Lock()
		sess.conns[c] = true
		sess.mu.Unlock()

		if len(sess.scrollback) > 0 {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": string(sess.scrollback),
			})
			c.WriteMessage(websocket.TextMessage, msg)
		}

		defer func() {
			sess.mu.Lock()
			delete(sess.conns, c)
			sess.mu.Unlock()
			c.Close()
		}()

		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg["type"] {
			case "input":
				if s, ok := msg["data"].(string); ok {
					sess.Pty.ptmx.Write([]byte(s))
				}
			case "resize":
				colsF, okCols := msg["cols"].(float64)
				rowsF, okRows := msg["rows"].(float64)
				if !okCols || !okRows {
					continue
				}
				sess.Pty.ptmx.Resize(int(colsF), int(rowsF))
			}
		}
	})
}
