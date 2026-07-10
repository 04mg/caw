package terminal

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
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

var defaultManagerMgr *SessionManager

func Register(rg *gin.RouterGroup, store *state.Store, upgrader *websocket.Upgrader) {
	defaultManagerMgr = NewSessionManager(store, upgrader)
	h := NewHandler(defaultManagerMgr)
	rg.POST("/terminals", h.Create)
	rg.DELETE("/terminals/:id", h.Delete)
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

	sess.mu.Lock()
	sess.conns[c] = true
	scrollback := make([]byte, len(sess.scrollback))
	copy(scrollback, sess.scrollback)
	syncSeq := sess.syncMessage()
	if sess.cols > 0 && sess.rows > 0 {
		msg, _ := json.Marshal(map[string]any{
			"type": "resize",
			"cols": sess.cols,
			"rows": sess.rows,
		})
		c.WriteMessage(websocket.TextMessage, msg)
	}
	sess.mu.Unlock()

	if len(scrollback) > 0 {
		stripped := stripAlternateScreen(scrollback)
		if len(stripped) > 0 {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": string(stripped),
			})
			c.WriteMessage(websocket.TextMessage, msg)
		}
	}

	if syncSeq != "" {
		msg, _ := json.Marshal(map[string]any{
			"type": "output",
			"data": syncSeq,
		})
		c.WriteMessage(websocket.TextMessage, msg)
	}

	defer func() {
		sess.mu.Lock()
		delete(sess.conns, c)
		sess.mu.Unlock()
		c.Close()
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
			sess.resizePTY(cols, rows)
			sess.mu.Unlock()
		}
	}
}