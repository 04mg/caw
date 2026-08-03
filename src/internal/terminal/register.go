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

// ReconcileOrphans schedules a debounced reconciliation pass on the default
// session manager. It kills PTY sessions whose leaf id is no longer in any
// workspace's layout and that have no connected WebSocket viewers. Called
// by the state package after a layout-state save. It is a no-op if Register
// has not been called yet.
func ReconcileOrphans(knownLeafIDs map[string]bool) {
	if defaultManagerMgr == nil {
		return
	}
	defaultManagerMgr.ReconcileOrphans(knownLeafIDs)
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

	sess.mu.Lock()
	sess.pendingResizes++
	sess.mu.Unlock()

	defer func() {
		sess.mu.Lock()
		if _, ok := sess.conns[wc]; ok {
			delete(sess.conns, wc)
			sess.recomputeResize()
		} else {
			sess.pendingResizes--
		}
		sess.mu.Unlock()
		wc.Close()
	}()

	firstResize := make(chan struct{}, 1)
	scrolled := false

	sendScrollback := func() {
		if scrolled {
			return
		}
		scrolled = true

		// Build and deliver the replay while holding s.mu. ReadLoop also
		// broadcasts live output while holding s.mu, so keeping this write
		// under the same lock serializes the snapshot strictly before any
		// subsequent live chunk — a freshly attached client can never
		// receive newer output before the (older) scrollback replay, which
		// is what caused stale bytes to be re-rendered over fresh output
		// (visible as artifacts) when the lock was dropped between
		// snapshotting and writing.
		sess.mu.Lock()
		scrollback := sess.scrollbackBytes()
		syncSeq := sess.syncMessage()
		onAltScreen := sess.altScreen
		sess.pendingResizes--
		sess.conns[wc] = true

		// Build the replay payload. When the running program is on the
		// alternate screen, drive the reattaching client onto the alt
		// screen too and replay only the current frame (the bytes since
		// the last enter-sequence). Otherwise replay the full scrollback
		// into the normal buffer so shell history stays visible. The
		// alt-screen toggles are stripped from the replayed bytes in both
		// cases: in the alt-screen case we emit a single enter-sequence
		// up front so nested toggles from earlier in-session exits/re-
		// entries don't bounce the client back to the normal buffer
		// mid-replay.
		var data []byte
		if len(scrollback) > 0 {
			payload := scrollback
			if onAltScreen {
				if frame := currentAltScreenFrame(scrollback); frame != nil {
					payload = frame
				}
				data = append(data, "\x1b[?1049h"...)
			}
			stripped := stripAlternateScreen(payload)
			data = append(data, stripped...)
		}
		if len(data) > 0 {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": string(data),
			})
			wc.WriteMessage(websocket.TextMessage, msg)
		}
		if syncSeq != "" {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": syncSeq,
			})
			wc.WriteMessage(websocket.TextMessage, msg)
		}
		sess.mu.Unlock()
	}

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
			select {
			case firstResize <- struct{}{}:
			default:
			}
		case "focus":
			focused, _ := msg["focused"].(bool)
			if OnPtyFocus != nil {
				OnPtyFocus(id, focused)
			}
		}
	}
}
