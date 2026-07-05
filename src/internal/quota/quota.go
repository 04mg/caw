package quota

import (
	"encoding/json"
	"net/http"

	"github.com/04mg/caw/internal/state"
)

type Quota struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

type QuotaResponse struct {
	FiveHour Quota `json:"fiveHour"`
	Weekly   Quota `json:"weekly"`
	Monthly  Quota `json:"monthly"`
}

type ProviderResponse struct {
	Data  *QuotaResponse `json:"data,omitempty"`
	Error string         `json:"error,omitempty"`
}

// QuotaProvider is the Strategy interface.
type QuotaProvider interface {
	GetQuotas(config map[string]string) (*QuotaResponse, error)
}

var registry = make(map[string]QuotaProvider)

// RegisterProvider allows strategies to register themselves.
func RegisterProvider(name string, provider QuotaProvider) {
	registry[name] = provider
}

func Register(mux *http.ServeMux, store *state.Store) {
	// 1. Quota fetch endpoint
	mux.HandleFunc("/api/quotas", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		settings, err := store.GetQuotaSettings()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		res := make(map[string]ProviderResponse)

		for name, provider := range registry {
			config, ok := settings[name]
			if !ok {
				continue
			}

			// Call the strategy implementation
			data, err := provider.GetQuotas(config)
			if err != nil {
				res[name] = ProviderResponse{Error: err.Error()}
			} else {
				res[name] = ProviderResponse{Data: data}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})

	// 2. Settings manage endpoint
	mux.HandleFunc("/api/quotas/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			settings, err := store.GetQuotaSettings()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if settings == nil {
				settings = make(map[string]map[string]string)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(settings)

		case http.MethodPost:
			var req map[string]map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if err := store.SaveQuotaSettings(req); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
