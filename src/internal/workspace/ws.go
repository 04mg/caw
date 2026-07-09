package workspace

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func RegisterWS(mux *http.ServeMux) {
	mux.HandleFunc("/ws/workspaces/files", handleWS)
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	hub := getHub()

	defer hub.UnsubscribeAll(c)

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
				hub.Subscribe(c, path)
			}
		case "unsubscribe":
			if path, ok := msg["path"].(string); ok {
				hub.Unsubscribe(c, path)
			}
		}
	}
}
