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

type QuotaItem struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Used        int    `json:"used"`
	Limit       int    `json:"limit"`
	ResetTime   string `json:"resetTime,omitempty"`
}

type QuotaGroup struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Items       []QuotaItem `json:"items"`
}

type QuotaResponse struct {
	FiveHour Quota        `json:"fiveHour"`
	Weekly   Quota        `json:"weekly"`
	Monthly  Quota        `json:"monthly"`
	Groups   []QuotaGroup `json:"groups,omitempty"`
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
			if checker, ok := provider.(interface{ IsInstalled() bool }); ok {
				if !checker.IsInstalled() {
					continue
				}
			}

			config, ok := settings[name]
			if !ok {
				if name == "antigravity" || name == "claude" || name == "codex" || name == "copilot" {
					config = make(map[string]string)
				} else {
					continue
				}
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

			// Inject installed status
			for name, provider := range registry {
				if checker, ok := provider.(interface{ IsInstalled() bool }); ok {
					if settings[name] == nil {
						settings[name] = make(map[string]string)
					}
					if checker.IsInstalled() {
						settings[name]["installed"] = "true"
					} else {
						settings[name]["installed"] = "false"
					}
				}
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

	// 3. Copilot GitHub device login
	mux.HandleFunc("/api/quotas/copilot/device-login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		dc, err := initiateDeviceLogin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dc)
	})

	// 4. Copilot device token poll
	mux.HandleFunc("/api/quotas/copilot/device-poll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req DevicePollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tr, err := pollDeviceToken(req.DeviceCode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tr)
	})
}
