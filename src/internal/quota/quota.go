package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

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

	// 3. OAuth Callback handler
	mux.HandleFunc("/api/quotas/oauth2callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}

		proto := "http"
		if r.TLS != nil {
			proto = "https"
		}
		redirectURI := fmt.Sprintf("%s://%s/api/quotas/oauth2callback", proto, r.Host)

		data := url.Values{}
		data.Set("client_id", "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com")
		data.Set("client_secret", "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")
		data.Set("code", code)
		data.Set("redirect_uri", redirectURI)
		data.Set("grant_type", "authorization_code")

		resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to exchange code: %v", err), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			http.Error(w, fmt.Sprintf("token exchange returned status %d: %s", resp.StatusCode, string(body)), http.StatusBadRequest)
			return
		}

		var tokenResp struct {
			RefreshToken string `json:"refresh_token"`
			AccessToken  string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
			http.Error(w, fmt.Sprintf("failed to decode response: %v", err), http.StatusInternalServerError)
			return
		}

		tokenToSave := tokenResp.RefreshToken
		if tokenToSave == "" {
			tokenToSave = tokenResp.AccessToken
		}

		if tokenToSave != "" {
			settings, err := store.GetQuotaSettings()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if settings == nil {
				settings = make(map[string]map[string]string)
			}
			if settings["antigravity"] == nil {
				settings["antigravity"] = make(map[string]string)
			}
			settings["antigravity"]["apiKey"] = tokenToSave

			if err := store.SaveQuotaSettings(settings); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<head>
				<title>Authentication Successful</title>
				<style>
					body {
						font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
						display: flex;
						align-items: center;
						justify-content: center;
						height: 100vh;
						margin: 0;
						background-color: #0d0e12;
						color: #ffffff;
					}
					.card {
						background: rgba(255, 255, 255, 0.03);
						border: 1px solid rgba(255, 255, 255, 0.08);
						border-radius: 12px;
						padding: 32px;
						text-align: center;
						max-width: 400px;
						box-shadow: 0 4px 30px rgba(0, 0, 0, 0.5);
					}
					h1 {
						color: #10b981;
						margin-top: 0;
						font-size: 20px;
					}
					p {
						color: #9ca3af;
						font-size: 13px;
						line-height: 1.5;
					}
					.icon {
						font-size: 40px;
						margin-bottom: 12px;
					}
				</style>
			</head>
			<body>
				<div class="card">
					<div class="icon">🔒</div>
					<h1>Authentication Successful</h1>
					<p>Antigravity limits configured successfully! You can now close this browser tab and return to the application.</p>
				</div>
			</body>
			</html>
		`))
	})
}
