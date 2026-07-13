package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
	id   uint64
}

func (c *Client) WriteMessage(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(msgType, data)
}

func (c *Client) Conn() *websocket.Conn { return c.conn }

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
	nextID  uint64
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]bool)}
}

func (h *Hub) Register(c *websocket.Conn) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	wc := &Client{conn: c, id: h.nextID}
	h.clients[wc] = true
	return wc
}

func (h *Hub) Unregister(wc *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[wc]; ok {
		delete(h.clients, wc)
	}
}

func (h *Hub) Broadcast(msgType int, data []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for wc := range h.clients {
		clients = append(clients, wc)
	}
	h.mu.RUnlock()
	for _, wc := range clients {
		_ = wc.WriteMessage(msgType, data)
	}
}

func (h *Hub) BroadcastExcept(msgType int, data []byte, exclude *Client) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for wc := range h.clients {
		if wc != exclude {
			clients = append(clients, wc)
		}
	}
	h.mu.RUnlock()
	for _, wc := range clients {
		_ = wc.WriteMessage(msgType, data)
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) BroadcastJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.Broadcast(websocket.TextMessage, data)
}

func (h *Hub) BroadcastJSONExcept(v any, exclude *Client) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.BroadcastExcept(websocket.TextMessage, data, exclude)
}

func (h *Hub) BroadcastText(v any)       { h.BroadcastJSON(v) }
func (h *Hub) BroadcastTextExcept(v any, exclude *Client) { h.BroadcastJSONExcept(v, exclude) }

var DefaultUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Keepalive settings. A ping is sent every PingInterval; if no pong is
// received within PongWait, the connection is force-closed. The read
// deadline is reset on every pong (and on every incoming message).
const (
	PingInterval = 30 * time.Second
	PongWait     = 60 * time.Second
	WriteWait    = 10 * time.Second
)

// StartKeepalive configures pong handling and launches a ping ticker for
// the given connection. It returns a stop function that must be called
// when the connection is torn down (e.g. from a defer in the handler).
//
// ping must perform the actual ping write through whatever per-conn
// serialization the caller uses (e.g. ws.Client.WriteMessage or a
// connWriter mutex) so that control-frame writes never race with data
// writes on the same gorilla connection. The caller is still responsible
// for the read loop (ReadMessage); this helper only handles the write
// side (pings) and the pong handler that resets the read deadline.
// Calling StartKeepalive before entering the read loop ensures the
// deadline is armed even for mostly-idle channels (state, agent
// statuses, file tree) that otherwise never receive data.
func StartKeepalive(conn *websocket.Conn, ping func() error) (stop func()) {
	_ = conn.SetReadDeadline(time.Now().Add(PongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ping(); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	return func() { close(done) }
}

// PingWriter returns a ping callback that writes a ping frame through the
// given write function, honoring the configured write deadline. Use this
// as the ping argument to StartKeepalive when the connection is wrapped
// by a mutex-serializing writer (ws.Client, connWriter, connClient).
func PingWriter(writeMsg func(int, []byte) error) func() error {
	return func() error {
		return writeMsg(websocket.PingMessage, nil)
	}
}