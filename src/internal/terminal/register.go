package terminal

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if !httpx.Bind(c, &req) {
		return
	}
	id, err := h.mgr.Create(req)
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"id": id})
}

func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	deleteBranch := c.Query("deleteBranch") == "true"
	if !h.mgr.Delete(id, deleteBranch) {
		httpx.NotFound(c, "not found")
		return
	}
	c.Status(http.StatusOK)
}

func (h *Handler) Get(c *gin.Context) {
	id := c.Param("id")
	_, ok := h.mgr.Get(id)
	if !ok {
		httpx.NotFound(c, "not found")
		return
	}
	httpx.OK(c, map[string]bool{"exists": true})
}

var defaultManagerMgr *SessionManager

func Register(rg *gin.RouterGroup, store *state.Store, upgrader *websocket.Upgrader) {
	defaultManagerMgr = NewSessionManager(store, upgrader)
	h := NewHandler(defaultManagerMgr)
	rg.POST("/terminals", h.Create)
	rg.DELETE("/terminals/:id", h.Delete)
	rg.GET("/terminals/:id", h.Get)
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
		if wc.vs != nil {
			wc.vs.Dispose()
			wc.vs = nil
		}
		if _, ok := sess.conns[wc]; ok {
			delete(sess.conns, wc)
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

		// Ensure VirtualScreen exists if viewer dimensions differ from
		// the PTY. All vs operations must be under sess.mu to avoid
		// races with resizePTY running in the message handler.
		if wc.cols > 0 && wc.rows > 0 && (wc.cols != sess.cols || wc.rows != sess.rows) {
			if wc.vs == nil {
				wc.vs = NewVirtualScreen(sess.cols, sess.rows, wc.cols, wc.rows)
			}
		}

		var scrollbackMsg []byte
		if len(scrollback) > 0 {
			if wc.vs != nil {
				// VirtualScreen: process scrollback through the terminal
				// emulator and serialize adapted output for this viewer.
				adapted := wc.vs.Process(scrollback)
				if len(adapted) > 0 {
					scrollbackMsg, _ = json.Marshal(map[string]any{
						"type": "output",
						"data": string(adapted),
					})
				}
			} else {
				stripped := stripAlternateScreen(scrollback)
				if len(stripped) > 0 {
					scrollbackMsg, _ = json.Marshal(map[string]any{
						"type": "output",
						"data": string(stripped),
					})
				}
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
