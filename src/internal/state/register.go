package state

import (
	"net/http"

	"github.com/04mg/caw/internal/httputil"
)

func RegisterHTTP(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("/api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			httputil.WriteJSON(w, store.Get())
		case http.MethodPost:
			var s AppState
			if err := httputil.ReadJSON(r, &s); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if s.Workspaces == nil {
				s.Workspaces = []Workspace{}
			}
			store.Set(s)
			broadcastStateToAll(store)
			httputil.WriteJSON(w, map[string]bool{"ok": true})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
