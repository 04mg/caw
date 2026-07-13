package workspace

import (
	"encoding/json"
	"sync"

	"github.com/04mg/caw/internal/ws"
)

// muxSubscriber adapts a ws.MuxClient to the Subscriber interface
// expected by FileEventHub. It wraps the raw FileEvent JSON in a mux
// Envelope with channel "files" before writing.
type muxSubscriber struct {
	client *ws.MuxClient
}

func (m *muxSubscriber) WriteMessage(msgType int, data []byte) error {
	env := ws.Envelope{
		Channel: "files",
		Data:    json.RawMessage(data),
	}
	wrapped, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return m.client.WriteMessage(msgType, wrapped)
}

var (
	muxSubs   sync.Map // map[*ws.MuxClient]*muxSubscriber
)

func getMuxSub(c *ws.MuxClient) *muxSubscriber {
	if v, ok := muxSubs.Load(c); ok {
		return v.(*muxSubscriber)
	}
	sub := &muxSubscriber{client: c}
	actual, _ := muxSubs.LoadOrStore(c, sub)
	return actual.(*muxSubscriber)
}

// RegisterMuxChannel wires the "files" channel into the multiplexer.
// Inbound messages with type "subscribe"/"unsubscribe" and a "path"
// field are forwarded to the FileEventHub. The MuxClient is wrapped in
// a muxSubscriber (one per client, cached in muxSubs) so that the
// FileEventHub sees a stable Subscriber identity for broadcasts and
// unsubscription cleanup.
func RegisterMuxChannel(mux *ws.Multiplexer) {
	hub := getHub()

	mux.HandleChannel("files",
		nil,
		func(c *ws.MuxClient) {
			sub := getMuxSub(c)
			hub.UnsubscribeAll(sub)
			muxSubs.Delete(c)
		},
		func(c *ws.MuxClient, data []byte) {
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			sub := getMuxSub(c)
			switch msg["type"] {
			case "subscribe":
				if path, ok := msg["path"].(string); ok {
					hub.Subscribe(sub, path)
				}
			case "unsubscribe":
				if path, ok := msg["path"].(string); ok {
					hub.Unsubscribe(sub, path)
				}
			}
		},
	)
}