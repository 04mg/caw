package desktop

import (
	"log"
	"net/http"

	"github.com/04mg/caw/internal/httpx"
)

// defaultManager is the process-wide session manager, initialized by
// Register. It mirrors the terminal package's defaultManagerMgr pattern so
// the package-level ReconcileOrphans can find it.
var defaultManager *SessionManager

// Register wires the desktop subsystem's HTTP routes into the given mux
// and creates the singleton session manager. The proxied /ws/desktop/{id}
// route is registered on the root mux (not under /api) because it forwards
// the raw xpra WebSocket display stream to the bundled HTML5 client (the
// client itself is bundled into Caw's frontend, so there is no longer a
// separate /desktop/{id} HTTP route serving xpra's built-in web client).
func Register(apiMux *http.ServeMux, rootMux *http.ServeMux) {
	defaultManager = NewSessionManager()
	h := NewHandler(defaultManager)

	// API routes (JSON, under /api).
	apiMux.HandleFunc("POST /desktop", h.Create)
	apiMux.HandleFunc("DELETE /desktop/{id}", h.Delete)
	apiMux.HandleFunc("GET /desktop/{id}", h.Get)
	apiMux.HandleFunc("GET /desktop/status", h.Status)

	// WebSocket upgrade to the xpra server, proxied to the bundled client.
	rootMux.HandleFunc("GET /ws/desktop/{id}", h.ProxyWS)
}

// ReconcileOrphans schedules a debounced reconciliation pass on the default
// manager. Called by the state package after a layout-state save. It is a
// no-op if Register has not been called.
func ReconcileOrphans(knownLeafIDs map[string]bool) {
	if defaultManager == nil {
		return
	}
	defaultManager.ReconcileOrphans(knownLeafIDs)
}

// Handler implements the desktop HTTP routes.
type Handler struct {
	mgr *SessionManager
}

func NewHandler(mgr *SessionManager) *Handler {
	return &Handler{mgr: mgr}
}

// Create handles POST /api/desktop. Spawns an xpra server for the leaf.
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

// Delete handles DELETE /api/desktop/{id}. Stops the xpra server.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.mgr.Delete(id) {
		httpx.RespondNotFound(w, "not found")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Status handles GET /api/desktop/status. Reports whether xpra is
// installed on the host (and its version) so the Desktop settings section
// can show install guidance when it's missing.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	installed := xpraAvailable()
	resp := map[string]any{"xpraInstalled": installed}
	if installed {
		if v := xpraVersion(); v != "" {
			resp["xpraVersion"] = v
		}
	} else {
		// Debug aid: report the running exe and every location that was
		// checked, both to the server log and in the response, so a
		// stale binary or missed install path is immediately visible.
		debug := xpraDebugInfo()
		log.Printf("[desktop] xpra not detected: %s", debug)
		resp["xpraDebug"] = debug
	}
	httpx.RespondJSON(w, resp)
}

// Get handles GET /api/desktop/{id}. Reports whether the session is alive.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := h.mgr.Get(id)
	if !ok {
		httpx.RespondNotFound(w, "not found")
		return
	}
	httpx.RespondJSON(w, map[string]any{
		"exists": true,
		"healthy": sess.healthCheck(),
		"port":   sess.Port,
	})
}

// ProxyWS forwards /ws/desktop/{id} to the xpra WebSocket endpoint. The
// gorilla/httputil ReverseProxy transparently handles the WS upgrade when
// the request carries the Upgrade: websocket header.
func (h *Handler) ProxyWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := h.mgr.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p := newProxy(sess)
	if p == nil {
		http.Error(w, "xpra upstream unavailable", http.StatusBadGateway)
		return
	}
	// Rewrite the path to what xpra expects. The xpra WS server accepts
	// connections at the root path (/); we map our namespaced
	// /ws/desktop/{id} to / so the bundled client's WebSocket URL lines
	// up. The proxy then forwards / to xpra.
	r.URL.Path = "/"
	r2 := r.Clone(r.Context())
	p.ServeHTTP(w, r2)
}