package terminal

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
	"github.com/04mg/caw/internal/ws"
)

type Handler struct {
	mgr *SessionManager
}

func NewHandler(mgr *SessionManager) *Handler {
	return &Handler{mgr: mgr}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	id, err := h.mgr.Create(req)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, map[string]string{"id": id})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleteBranch := r.URL.Query().Get("deleteBranch") == "true"
	if !h.mgr.Delete(id, deleteBranch) {
		httpx.RespondNotFound(w, "not found")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.mgr.Get(id)
	if !ok {
		httpx.RespondNotFound(w, "not found")
		return
	}
	httpx.RespondJSON(w, map[string]bool{"exists": true})
}

var defaultManagerMgr *SessionManager

func Register(mux *http.ServeMux, store *state.Store, upgrader *websocket.Upgrader) {
	defaultManagerMgr = NewSessionManager(store, upgrader)
	h := NewHandler(defaultManagerMgr)
	mux.HandleFunc("POST /terminals", h.Create)
	mux.HandleFunc("DELETE /terminals/{id}", h.Delete)
	mux.HandleFunc("GET /terminals/{id}", h.Get)
}

func HandleTerminalWS(w http.ResponseWriter, r *http.Request, id string, upgrader *websocket.Upgrader) {
	sess, ok := defaultManagerMgr.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	wc := &connWriter{conn: c}
	stopPing := ws.StartKeepalive(c, ws.PingWriter(wc.WriteMessage))
	defer stopPing()

	// Track this connection as pending until sendScrollback registers it
	// in sess.conns. This prevents resizePTY from treating this connection
	// as the only viewer while another connection is still connecting.
	sess.mu.Lock()
	sess.pendingResizes++
	sess.mu.Unlock()

	defer func() {
		sess.mu.Lock()
		if _, ok := sess.conns[wc]; ok {
			delete(sess.conns, wc)
			// A viewer left — the PTY may now be able to grow to the
			// remaining smallest viewer (or the single remaining viewer's
			// full size). Recompute before broadcasting so larger viewers
			// stop receiving downscaled output and the released mobile
			// viewer no longer pins the PTY small.
			sess.recomputeResize()
		} else {
			// sendScrollback hasn't run yet — undo the pending count.
			sess.pendingResizes--
		}
		sess.mu.Unlock()
		wc.Close()
	}()

	// firstResize is signaled when the client sends its first resize
	// message (sent in ws.onopen on the frontend). We wait for it (with
	// a 2s timeout) before sending scrollback so the replay renders with
	// the correct cols/rows, eliminating the garbled-characters-on-load
	// bug that required a manual resize to fix.
	firstResize := make(chan struct{}, 1)
	scrolled := false

	// sendScrollback sends the buffered scrollback and sync-mode sequences
	// to this client and registers it in sess.conns so it starts receiving
	// live output. Must be called at most once. The client is registered
	// in sess.conns AFTER the scrollback is written, so ReadLoop's live
	// output cannot interleave with the replay. The connWriter mutex still
	// serializes any concurrent writes, but the ordering guarantee ensures
	// the client sees scrollback before new output.
	sendScrollback := func() {
		if scrolled {
			return
		}
		scrolled = true

		sess.mu.Lock()
		scrollback := make([]byte, len(sess.scrollback))
		copy(scrollback, sess.scrollback)
		syncSeq := sess.syncMessage()

		var scrollbackMsg []byte
		if len(scrollback) > 0 {
			stripped := stripAlternateScreen(scrollback)
			if len(stripped) > 0 {
				scrollbackMsg, _ = json.Marshal(map[string]any{
					"type": "output",
					"data": string(stripped),
				})
			}
		}

		// Register the client only after scrollback is fully written so
		// that live output from ReadLoop doesn't interleave with the
		// replay. Decrement pendingResizes now that this connection is
		// fully established.
		sess.pendingResizes--
		sess.conns[wc] = true
		sess.mu.Unlock()

		// Send messages outside the lock to avoid holding it during I/O.
		if len(scrollbackMsg) > 0 {
			wc.WriteMessage(websocket.TextMessage, scrollbackMsg)
		}
		if syncSeq != "" {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": syncSeq,
			})
			wc.WriteMessage(websocket.TextMessage, msg)
		}
	}

	// Wait for the first resize from the client, with a timeout fallback.
	// If the client doesn't send a resize within 2s (e.g. older frontend
	// or a headless client), fall back to sending scrollback with the
	// session's current dimensions.
	go func() {
		select {
		case <-firstResize:
		case <-time.After(2 * time.Second):
		}
		sendScrollback()
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
				if OnPtyInput != nil {
					OnPtyInput(id, s)
				}
				sess.Pty.ptmx.Write([]byte(s))
			}
		case "resize":
			colsF, okCols := msg["cols"].(float64)
			rowsF, okRows := msg["rows"].(float64)
			if !okCols || !okRows {
				continue
			}
			cols := int(colsF)
			rows := int(rowsF)
			if cols <= 0 || rows <= 0 {
				continue
			}
			sess.mu.Lock()
			sess.resizePTY(cols, rows, wc)
			sess.mu.Unlock()
			// Signal the first resize so sendScrollback runs with the
			// client's actual dimensions.
			select {
			case firstResize <- struct{}{}:
			default:
			}
		case "focus":
			// Frontend reports that this pane gained or lost the user's
			// focus. Forward to the agent status package via OnPtyFocus so
			// the idle-timeout and re-bind heuristics can account for which
			// terminal the user is currently driving.
			focused, _ := msg["focused"].(bool)
			if OnPtyFocus != nil {
				OnPtyFocus(id, focused)
			}
		}
	}
}
