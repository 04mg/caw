package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/04mg/caw/internal/quota"
)

// OpenRouterProvider fetches OpenRouter key limits and credit usage via
// https://openrouter.ai/api/v1/key.
type OpenRouterProvider struct{}

func init() {
	quota.RegisterProvider("openrouter", &OpenRouterProvider{})
}

type OpenRouterKeyResponse struct {
	Data OpenRouterKey `json:"data"`
}

type OpenRouterKey struct {
	Label           string  `json:"label"`
	Limit           *float64 `json:"limit"`
	LimitRemaining  *float64 `json:"limit_remaining"`
	LimitReset      *string  `json:"limit_reset"`
	Usage           float64 `json:"usage"`
	UsageDaily      float64 `json:"usage_daily"`
	UsageWeekly     float64 `json:"usage_weekly"`
	UsageMonthly    float64 `json:"usage_monthly"`
	IsFreeTier      bool    `json:"is_free_tier"`
}

func (p *OpenRouterProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	apiKey := config["apiKey"]
	if apiKey == "" {
		return nil, fmt.Errorf("openrouter api key is required")
	}

	key, err := fetchOpenRouterKey(apiKey)
	if err != nil {
		return nil, err
	}

	// Credit limit: null means unlimited. Use 0 as sentinel and leave
	// per-window Limit at 0 so the UI can render "unlimited" appropriately.
	limit := 0
	if key.Limit != nil {
		limit = int(*key.Limit + 0.5)
	}

	res := &quota.QuotaResponse{
		FiveHour: quota.Quota{
			Used:  int(key.UsageDaily + 0.5),
			Limit: limit,
			Unit:  "credits",
		},
		Weekly: quota.Quota{
			Used:  int(key.UsageWeekly + 0.5),
			Limit: limit,
			Unit:  "credits",
		},
		Monthly: quota.Quota{
			Used:  int(key.UsageMonthly + 0.5),
			Limit: limit,
			Unit:  "credits",
		},
	}

	// Detailed breakdown as a group, mirroring the Antigravity approach.
	totalUsed := int(key.Usage + 0.5)
	resetTime := ""
	if key.LimitReset != nil {
		resetTime = *key.LimitReset
	}

	items := []quota.QuotaItem{
		{
			Name:  "total",
			Label: "Total Used",
			Used:  totalUsed,
			Limit: limit,
			Unit:  "credits",
		},
	}
	if resetTime != "" {
		items = append(items, quota.QuotaItem{
			Name:      "reset",
			Label:     "Limit Reset",
			Used:      0,
			Limit:     0,
			Unit:      "info",
			ResetTime: resetTime,
		})
	}

	res.Groups = []quota.QuotaGroup{
		{
			Name:        "Credits",
			Description: fmt.Sprintf("Key: %s", key.Label),
			Items:       items,
		},
	}

	if key.IsFreeTier {
		res.Groups = append(res.Groups, quota.QuotaGroup{
			Name:        "Free Tier",
			Description: "Free model rate limits: 20 RPM, up to 1000 RPD with credits",
			Items: []quota.QuotaItem{
				{
					Name:  "free_rpm",
					Label: "Free Model RPM",
					Used:  0,
					Limit: 20,
					Unit:  "count",
				},
				{
					Name:  "free_rpd",
					Label: "Free Model RPD",
					Used:  0,
					Limit: 1000,
					Unit:  "count",
				},
			},
		})
	}

	return res, nil
}

func fetchOpenRouterKey(apiKey string) (*OpenRouterKey, error) {
	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/key", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("openrouter unauthorized (check api key)")
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		return nil, fmt.Errorf("openrouter insufficient credits")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter error: HTTP %d: %s", resp.StatusCode, truncateBody(body, 400))
	}

	var parsed OpenRouterKeyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode openrouter key response: %w", err)
	}
	return &parsed.Data, nil
}