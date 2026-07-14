package workspace

import (
	"encoding/json"
	"net/http"

	"github.com/04mg/caw/internal/ws"
)

func HandleFilesWS(w http.ResponseWriter, r *http.Request) {
	c, err := ws.DefaultUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &connClient{conn: c}
	stopPing := ws.StartKeepalive(c, ws.PingWriter(client.WriteMessage))
	defer stopPing()

	hub := getHub()
	defer hub.UnsubscribeAll(client)

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
		case "subscribe":
			if path, ok := msg["path"].(string); ok {
				hub.Subscribe(client, path)
			}
		case "unsubscribe":
			if path, ok := msg["path"].(string); ok {
				hub.Unsubscribe(client, path)
			}
		}
	}
}