package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/04mg/caw/internal/quota"
)

// CopilotProvider fetches GitHub Copilot usage via the copilot_internal API.
type CopilotProvider struct{}

func init() {
	quota.RegisterProvider("copilot", &CopilotProvider{})
}

func (p *CopilotProvider) IsInstalled() bool {
	if _, err := execLookPath("copilot"); err == nil {
		return true
	}
	return false
}

// CopilotUsageResponse mirrors api.github.com/copilot_internal/user.
type CopilotUsageResponse struct {
	QuotaResetDate     string                      `json:"quota_reset_date"`
	QuotaResetDateUTC  string                      `json:"quota_reset_date_utc"`
	QuotaSnapshots     CopilotQuotaSnapshotsPayload `json:"quota_snapshots"`
	CopilotPlan        string                      `json:"copilot_plan"`
}

type CopilotQuotaSnapshotsPayload struct {
	PremiumInteractions *CopilotQuotaSnapshot `json:"premium_interactions"`
	Chat                *CopilotQuotaSnapshot `json:"chat"`
}

type CopilotQuotaSnapshot struct {
	PercentRemaining      *float64  `json:"percent_remaining"`
	OverQuotaUsedPercent  *float64  `json:"over_quota_used_percent"`
	Unlimited             bool      `json:"unlimited"`
	Entitlement           int       `json:"entitlement"`
	Remaining             int       `json:"remaining"`
}

func (p *CopilotProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	token := config["token"]
	if token == "" {
		token = config["accessToken"]
	}
	if token == "" {
		token = config["apiKey"]
	}
	if token == "" {
		return nil, fmt.Errorf("github oauth token is required")
	}

	usage, err := fetchCopilotUsage(token, config["enterpriseHost"])
	if err != nil {
		return nil, err
	}

	var fiveHour, weekly quota.Quota
	if usage.QuotaSnapshots.PremiumInteractions != nil {
		fiveHour = copilotWindow(usage.QuotaSnapshots.PremiumInteractions)
	}
	if usage.QuotaSnapshots.Chat != nil {
		weekly = copilotWindow(usage.QuotaSnapshots.Chat)
	}

	return &quota.QuotaResponse{
		FiveHour: fiveHour,
		Weekly:   weekly,
		Monthly: quota.Quota{
			Used:      0,
			Limit:     100,
			Unit:      "percentage",
			ResetTime: copilotResetTime(usage),
		},
	}, nil
}

func copilotResetTime(u *CopilotUsageResponse) string {
	if u.QuotaResetDateUTC != "" {
		return u.QuotaResetDateUTC
	}
	if u.QuotaResetDate != "" {
		// "2026-08-01" → ISO 8601
		return u.QuotaResetDate + "T00:00:00.000Z"
	}
	return ""
}

func copilotWindow(s *CopilotQuotaSnapshot) quota.Quota {
	if s.Unlimited {
		return quota.Quota{Used: 0, Limit: 100, Unit: "percentage"}
	}
	// Prefer AI Credits / entitlement counts when available.
	if s.Entitlement > 0 {
		used := s.Entitlement - s.Remaining
		if used < 0 {
			used = 0
		}
		return quota.Quota{Used: float64(used), Limit: float64(s.Entitlement), Unit: "count"}
	}
	if s.PercentRemaining != nil {
		used := 100 - int(*s.PercentRemaining+0.5)
		if used < 0 {
			used = 0
		}
		if used > 100 {
			used = 100
		}
		return quota.Quota{Used: float64(used), Limit: 100, Unit: "percentage"}
	}
	if s.OverQuotaUsedPercent != nil {
		return quota.Quota{Used: float64(clampPercent(*s.OverQuotaUsedPercent)), Limit: 100, Unit: "percentage"}
	}
	return quota.Quota{Used: 0, Limit: 100, Unit: "percentage"}
}

func fetchCopilotUsage(token, enterpriseHost string) (*CopilotUsageResponse, error) {
	host := "api.github.com"
	if enterpriseHost != "" {
		if startsWithAPI(enterpriseHost) {
			host = enterpriseHost
		} else {
			host = "api." + enterpriseHost
		}
	}
	url := fmt.Sprintf("https://%s/copilot_internal/user", host)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.96.2")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.26.7")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.26.7")
	req.Header.Set("X-Github-Api-Version", "2025-04-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("copilot unauthorized (re-authenticate via GitHub device flow)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot usage error: HTTP %d: %s", resp.StatusCode, truncateBody(body, 400))
	}

	var parsed CopilotUsageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode copilot usage: %w", err)
	}
	return &parsed, nil
}

func startsWithAPI(host string) bool {
	return len(host) >= 4 && host[:4] == "api."
}