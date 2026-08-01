package prefs

import (
	"encoding/json"
	"net/http"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
	"github.com/04mg/caw/internal/ws"
)

type Handler struct {
	store *state.Store
	mux   *ws.Multiplexer
}

func RegisterHTTP(mux *http.ServeMux, store *state.Store, muxWS *ws.Multiplexer) {
	h := &Handler{store: store, mux: muxWS}
	mux.HandleFunc("GET /prefs", h.GetPrefs)
	mux.HandleFunc("PUT /prefs", h.PutPrefs)
}

func RegisterMuxChannel(mux *ws.Multiplexer, store *state.Store) {
	mux.HandleChannel("prefs",
		func(c *ws.MuxClient) {
			_ = c.Send("prefs", GetPrefs(store))
		},
		nil,
		func(c *ws.MuxClient, data []byte) {
			var p PrefsState
			if err := json.Unmarshal(data, &p); err != nil {
				return
			}
			if err := SetPrefs(store, p); err != nil {
				return
			}
			mux.BroadcastExcept("prefs", p, c)
		},
	)
}

func (h *Handler) GetPrefs(w http.ResponseWriter, r *http.Request) {
	httpx.RespondJSON(w, GetPrefs(h.store))
}

func (h *Handler) PutPrefs(w http.ResponseWriter, r *http.Request) {
	var p PrefsState
	if !httpx.BindRequest(w, r, &p) {
		return
	}
	if err := SetPrefs(h.store, p); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	// Broadcast to all connected clients
	h.mux.Broadcast("prefs", p)
	httpx.RespondJSON(w, map[string]bool{"ok": true})
}
