package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

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

// QuotaProvider is the interface for different coding plan quota sources.
type QuotaProvider interface {
	GetQuotas() (*QuotaResponse, error)
}

// AntigravityProvider implements QuotaProvider.
type AntigravityProvider struct {
	APIKey string
}

func (p *AntigravityProvider) GetQuotas() (*QuotaResponse, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	req, err := http.NewRequest("GET", "https://api.antigravity.google/v1/quota", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var qResp QuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &qResp, nil
}

// OpenCodeProvider implements QuotaProvider.
type OpenCodeProvider struct {
	Cookie      string
	WorkspaceID string
}

var reComments = regexp.MustCompile(`<!--.*?-->`)
var reRolling = regexp.MustCompile(`Rolling Usage.*?usage-value[^>]*>(\d+)%`)
var reWeekly = regexp.MustCompile(`Weekly Usage.*?usage-value[^>]*>(\d+)%`)
var reMonthly = regexp.MustCompile(`Monthly Usage.*?usage-value[^>]*>(\d+)%`)

func (p *OpenCodeProvider) GetQuotas() (*QuotaResponse, error) {
	if p.Cookie == "" {
		return nil, fmt.Errorf("auth cookie is required")
	}
	if p.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace ID is required")
	}

	url := fmt.Sprintf("https://opencode.ai/workspace/%s/go", p.WorkspaceID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", fmt.Sprintf("auth=%s", p.Cookie))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	htmlStr := string(bodyBytes)

	// Clean comments to simplify regex matching
	cleaned := reComments.ReplaceAllString(htmlStr, "")

	mRolling := reRolling.FindStringSubmatch(cleaned)
	mWeekly := reWeekly.FindStringSubmatch(cleaned)
	mMonthly := reMonthly.FindStringSubmatch(cleaned)

	if mRolling == nil || mWeekly == nil || mMonthly == nil {
		return nil, fmt.Errorf("failed to parse quotas from HTML (rolling=%v, weekly=%v, monthly=%v)", mRolling != nil, mWeekly != nil, mMonthly != nil)
	}

	var rollingVal, weeklyVal, monthlyVal int
	if _, err := fmt.Sscanf(mRolling[1], "%d", &rollingVal); err != nil {
		return nil, fmt.Errorf("invalid rolling value: %s", mRolling[1])
	}
	if _, err := fmt.Sscanf(mWeekly[1], "%d", &weeklyVal); err != nil {
		return nil, fmt.Errorf("invalid weekly value: %s", mWeekly[1])
	}
	if _, err := fmt.Sscanf(mMonthly[1], "%d", &monthlyVal); err != nil {
		return nil, fmt.Errorf("invalid monthly value: %s", mMonthly[1])
	}

	return &QuotaResponse{
		FiveHour: Quota{Used: rollingVal, Limit: 100},
		Weekly:   Quota{Used: weeklyVal, Limit: 100},
		Monthly:  Quota{Used: monthlyVal, Limit: 100},
	}, nil
}

func Register(mux *http.ServeMux, store *state.Store) {
	// 1. Quota fetch endpoint (runs GET, reads settings from SQLite)
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

		// Antigravity Provider
		if antigravity, ok := settings["antigravity"]; ok {
			if apiKey, exists := antigravity["apiKey"]; exists && apiKey != "" {
				provider := &AntigravityProvider{APIKey: apiKey}
				data, err := provider.GetQuotas()
				if err != nil {
					res["antigravity"] = ProviderResponse{Error: err.Error()}
				} else {
					res["antigravity"] = ProviderResponse{Data: data}
				}
			}
		}

		// OpenCode Provider
		if opencode, ok := settings["opencode"]; ok {
			cookie := opencode["cookie"]
			workspaceID := opencode["workspaceId"]
			if cookie != "" && workspaceID != "" {
				provider := &OpenCodeProvider{
					Cookie:      cookie,
					WorkspaceID: workspaceID,
				}
				data, err := provider.GetQuotas()
				if err != nil {
					res["opencode"] = ProviderResponse{Error: err.Error()}
				} else {
					res["opencode"] = ProviderResponse{Data: data}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})

	// 2. Settings manage endpoint (GET loads from DB, POST saves to DB)
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
