package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/04mg/caw/internal/quota"
)

// ZedProvider reports Zed (zed.dev) usage limits by calling Zed's billing
// page, authenticated with the user's zed.session cookie fetched from the
// browser.
type ZedProvider struct{}

func init() {
	quota.RegisterProvider("zed", &ZedProvider{})
}

// zedUsageResponse mirrors the JSON payload embedded in
// https://cloud.zed.dev/frontend/billing/usage. The edit predictions limit
// is nullable because accounts with an unlimited allowance return null.
type zedUsageResponse struct {
	Plan              string          `json:"plan"`
	IsAccountTooYoung bool            `json:"is_account_too_young"`
	CurrentUsage      zedCurrentUsage `json:"current_usage"`
	PortalURL         string          `json:"portal_url"`
}

type zedCurrentUsage struct {
	TokenSpendInCents float64            `json:"token_spend_in_cents"`
	TokenSpend        zedTokenSpend      `json:"token_spend"`
	EditPredictions   zedEditPredictions `json:"edit_predictions"`
}

type zedTokenSpend struct {
	SpendInCents float64 `json:"spend_in_cents"`
	LimitInCents float64 `json:"limit_in_cents"`
	UpdatedAt    string  `json:"updated_at"`
}

type zedEditPredictions struct {
	Used      float64  `json:"used"`
	Limit     *float64 `json:"limit"`
	Remaining *float64 `json:"remaining"`
}

func (p *ZedProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	cookie := config["cookie"]
	if cookie == "" {
		return nil, fmt.Errorf("zed.session cookie is required")
	}

	const endpoint = "https://cloud.zed.dev/frontend/billing/usage"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "zed.session="+cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://cloud.zed.dev")
	req.Header.Set("Referer", "https://cloud.zed.dev/frontend/billing/usage")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zed usage request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("zed unauthorized (re-paste a fresh zed.session cookie)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zed usage error: HTTP %d: %s", resp.StatusCode, truncateBody(body, 400))
	}

	var parsed zedUsageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode zed usage response: %w", err)
	}

	// Zed bills token spend in cents; surface it as dollars so the shared
	// currency formatter renders "$ used / $ limit".
	spend := roundCredits(parsed.CurrentUsage.TokenSpend.SpendInCents / 100)
	limit := roundCredits(parsed.CurrentUsage.TokenSpend.LimitInCents / 100)

	res := &quota.QuotaResponse{
		Monthly: quota.Quota{
			Used:  spend,
			Limit: limit,
			Unit:  "currency",
		},
	}

	groups := []quota.QuotaGroup{
		{
			Name:        "Token Spend",
			Description: "Zed token spend against your monthly allowance",
			Items: []quota.QuotaItem{
				{
					Name:  "token_spend",
					Label: "Token Spend",
					Used:  spend,
					Limit: limit,
					Unit:  "currency",
				},
			},
		},
	}

	// Only surface edit predictions when the plan reports a concrete limit;
	// a null limit means the allowance is unlimited.
	if preds := parsed.CurrentUsage.EditPredictions; preds.Limit != nil && *preds.Limit > 0 {
		groups = append(groups, quota.QuotaGroup{
			Name:        "Edit Predictions",
			Description: "Zed edit predictions usage",
			Items: []quota.QuotaItem{
				{
					Name:  "edit_predictions",
					Label: "Edit Predictions",
					Used:  preds.Used,
					Limit: *preds.Limit,
					Unit:  "count",
				},
			},
		})
	}
	res.Groups = groups

	return res, nil
}
