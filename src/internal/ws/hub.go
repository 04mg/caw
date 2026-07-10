package ws

import (
	"encoding/json"
	"net/http"
	"sync"

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