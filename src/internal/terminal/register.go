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
	if (!ok) {
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

	// lastReported holds the most recent cols/rows the client sent via a
	// "resize" message before it is registered in sess.conns (which happens
	// in sendScrollback). Once registered, the viewer struct carries the
	// dimensions and lastReported is no longer used.
	var lastReportedCols, lastReportedRows int

	defer func() {
		sess.mu.Lock()
		delete(sess.conns, wc)
		sess.recomputeResize()
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
		sess.mu.Unlock()

		if len(scrollback) > 0 {
			stripped := stripAlternateScreen(scrollback)
			if len(stripped) > 0 {
				msg, _ := json.Marshal(map[string]any{
					"type": "output",
					"data": string(stripped),
				})
				wc.WriteMessage(websocket.TextMessage, msg)
			}
		}

		if syncSeq != "" {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": syncSeq,
			})
			wc.WriteMessage(websocket.TextMessage, msg)
		}

		// Register the client only after scrollback is fully written so
		// that live output from ReadLoop doesn't interleave with the
		// replay. Seed the viewer with the dimensions the client already
		// reported (if any) and recompute the PTY size so the new viewer
		// participates in the "smallest" calculation immediately.
		sess.mu.Lock()
		sess.conns[wc] = &viewer{cols: lastReportedCols, rows: lastReportedRows}
		sess.recomputeResize()
		sess.mu.Unlock()
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
			if v, ok := sess.conns[wc]; ok {
				// Viewer is already registered — update its size and
				// recompute the PTY to the new smallest.
				v.cols = cols
				v.rows = rows
				sess.recomputeResize()
			} else {
				// Viewer is not yet registered (sendScrollback hasn't
				// run). Stash the reported size so it can be seeded into
				// the viewer struct at registration time.
				lastReportedCols = cols
				lastReportedRows = rows
			}
			sess.mu.Unlock()
			// Signal the first resize so sendScrollback runs with the
			// client's actual dimensions.
			select {
			case firstResize <- struct{}{}:
			default:
			}
		}
	}
}