package ws

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Multiplexer fans out WebSocket messages across named channels over a
// single upgraded connection. It replaces the previous pattern of opening
// one WebSocket per concern (state, agent statuses, file tree) with a
// single /ws endpoint that multiplexes them.
//
// Inbound messages from the client are routed to per-channel handlers
// registered via HandleChannel. Outbound messages are sent via the
// per-channel Broadcast helpers, which wrap the payload in an envelope
// { "channel": "<name>", "data": <payload> } so the frontend mux can
// dispatch on the channel field.
type Multiplexer struct {
	mu       sync.RWMutex
	clients  map[*MuxClient]bool
	channels map[string]chanHandler
	nextID   uint64
}

type chanHandler struct {
	onSubscribe   func(c *MuxClient)
	onUnsubscribe func(c *MuxClient)
	onMessage     func(c *MuxClient, data []byte)
}

// MuxClient is a single WebSocket connection multiplexed across channels.
type MuxClient struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	id       uint64
	mux      *Multiplexer
	subs     map[string]bool
}

// Envelope is the wire format for multiplexed messages. Outbound
// messages wrap the channel payload in Data. Inbound messages use
// Channel to select the target handler and Data for the payload.
type Envelope struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (m *MuxClient) WriteMessage(msgType int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conn.WriteMessage(msgType, data)
}

// Send wraps payload in an Envelope with the given channel and writes it
// to the connection. payload must be JSON-serializable.
func (m *MuxClient) Send(channel string, payload any) error {
	env := Envelope{Channel: channel}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	env.Data = raw
	wrapped, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return m.WriteMessage(websocket.TextMessage, wrapped)
}

// SendRaw sends a pre-serialized envelope (already containing the channel
// wrapper) to the connection. Used by Broadcast when the envelope was
// built once for all clients.
func (m *MuxClient) SendRaw(data []byte) error {
	return m.WriteMessage(websocket.TextMessage, data)
}

func NewMultiplexer() *Multiplexer {
	return &Multiplexer{
		clients:  make(map[*MuxClient]bool),
		channels: make(map[string]chanHandler),
	}
}

// HandleChannel registers inbound handlers for a channel. Any of the
// callbacks may be nil. onSubscribe fires when a client sends a
// subscribe message for this channel; onUnsubscribe fires on unsubscribe
// or disconnect; onMessage fires for any other inbound message addressed
// to this channel.
func (m *Multiplexer) HandleChannel(name string, onSubscribe, onUnsubscribe func(c *MuxClient), onMessage func(c *MuxClient, data []byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = chanHandler{onSubscribe, onUnsubscribe, onMessage}
}

// Register upgrades the HTTP connection and starts the read loop. The
// returned MuxClient is valid until the connection closes.
func (m *Multiplexer) Register(conn *websocket.Conn) *MuxClient {
	m.mu.Lock()
	m.nextID++
	c := &MuxClient{conn: conn, id: m.nextID, mux: m, subs: make(map[string]bool)}
	m.clients[c] = true
	m.mu.Unlock()
	return c
}

func (m *Multiplexer) Unregister(c *MuxClient) {
	m.mu.Lock()
	if _, ok := m.clients[c]; ok {
		delete(m.clients, c)
		// Snapshot subscribed channels to fire onUnsubscribe outside the lock.
	}
	channelsToUnsub := make([]string, 0, len(c.subs))
	for ch := range c.subs {
		channelsToUnsub = append(channelsToUnsub, ch)
	}
	m.mu.Unlock()

	for _, ch := range channelsToUnsub {
		m.mu.RLock()
		h, ok := m.channels[ch]
		m.mu.RUnlock()
		if ok && h.onUnsubscribe != nil {
			h.onUnsubscribe(c)
		}
	}
}

// Subscribe registers the client on the given channel and fires
// onSubscribe. Safe to call multiple times for the same channel.
func (m *Multiplexer) Subscribe(c *MuxClient, channel string) {
	m.mu.Lock()
	if _, exists := m.channels[channel]; !exists {
		m.mu.Unlock()
		return
	}
	already := c.subs[channel]
	c.subs[channel] = true
	h := m.channels[channel]
	m.mu.Unlock()

	if !already && h.onSubscribe != nil {
		h.onSubscribe(c)
	}
}

// Unsubscribe removes the client from the channel and fires onUnsubscribe.
func (m *Multiplexer) Unsubscribe(c *MuxClient, channel string) {
	m.mu.Lock()
	if _, exists := m.channels[channel]; !exists {
		m.mu.Unlock()
		return
	}
	already := c.subs[channel]
	delete(c.subs, channel)
	h := m.channels[channel]
	m.mu.Unlock()

	if already && h.onUnsubscribe != nil {
		h.onUnsubscribe(c)
	}
}

// Dispatch routes an inbound message to the channel's onMessage handler.
func (m *Multiplexer) Dispatch(c *MuxClient, channel string, data []byte) {
	m.mu.RLock()
	h, ok := m.channels[channel]
	m.mu.RUnlock()
	if ok && h.onMessage != nil {
		h.onMessage(c, data)
	}
}

// Broadcast sends a payload to every client subscribed to the channel.
// The payload is marshaled once into an Envelope and sent to all clients.
func (m *Multiplexer) Broadcast(channel string, payload any) {
	env := Envelope{Channel: channel}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	env.Data = raw
	wrapped, err := json.Marshal(env)
	if err != nil {
		return
	}

	m.mu.RLock()
	clients := make([]*MuxClient, 0, len(m.clients))
	for c := range m.clients {
		if c.subs[channel] {
			clients = append(clients, c)
		}
	}
	m.mu.RUnlock()

	for _, c := range clients {
		_ = c.SendRaw(wrapped)
	}
}

// BroadcastExcept sends a payload to every client subscribed to the
// channel except the given one.
func (m *Multiplexer) BroadcastExcept(channel string, payload any, exclude *MuxClient) {
	env := Envelope{Channel: channel}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	env.Data = raw
	wrapped, err := json.Marshal(env)
	if err != nil {
		return
	}

	m.mu.RLock()
	clients := make([]*MuxClient, 0, len(m.clients))
	for c := range m.clients {
		if c != exclude && c.subs[channel] {
			clients = append(clients, c)
		}
	}
	m.mu.RUnlock()

	for _, c := range clients {
		_ = c.SendRaw(wrapped)
	}
}

// ClientCount returns the total number of connected clients.
func (m *Multiplexer) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// ChannelCount returns the number of clients subscribed to a channel.
func (m *Multiplexer) ChannelCount(channel string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for c := range m.clients {
		if c.subs[channel] {
			n++
		}
	}
	return n
}

// InboundMessage is the client->server envelope for subscribe/unsubscribe
// and channel-specific messages.
type InboundMessage struct {
	Channel string          `json:"channel"`
	Type    string          `json:"type"`          // "subscribe", "unsubscribe", or anything else
	Data    json.RawMessage `json:"data,omitempty"`
}

// HandleMuxWS is the single /ws endpoint. It upgrades the connection,
// starts keepalive, and runs the read loop, dispatching inbound messages
// to the registered channel handlers.
func (m *Multiplexer) HandleMuxWS(w http.ResponseWriter, r *http.Request) {
	conn, err := DefaultUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := m.Register(conn)
	stopPing := StartKeepalive(conn, PingWriter(c.WriteMessage))
	defer func() {
		stopPing()
		m.Unregister(c)
		conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg InboundMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Channel == "" {
			continue
		}
		switch msg.Type {
		case "subscribe":
			m.Subscribe(c, msg.Channel)
		case "unsubscribe":
			m.Unsubscribe(c, msg.Channel)
		default:
			// Pass the raw data bytes to the channel handler.
			payload := data
			if len(msg.Data) > 0 {
				payload = msg.Data
			}
			m.Dispatch(c, msg.Channel, payload)
		}
	}
}