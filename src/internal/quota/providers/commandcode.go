package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/04mg/caw/internal/quota"
)

// CommandCodeProvider reports Command Code (commandcode.ai) limits by calling
// Langbase's internal billing endpoint authenticated with the user's session
// cookie (__Secure-commandcode_prod_.session_token) fetched from the browser.
type CommandCodeProvider struct{}

func init() {
	quota.RegisterProvider("commandcode", &CommandCodeProvider{})
}

// commandCodeWindow mirrors a single usage window returned by the API.
type commandCodeWindow struct {
	Used     int    `json:"used"`
	Cap      int    `json:"cap"`
	ResetAt  string `json:"resetAt"`
	Exceeded bool   `json:"exceeded"`
}

// commandCodeCredits mirrors the credits payload returned by the API.
type commandCodeCredits struct {
	MonthlyCredits         int `json:"monthlyCredits"`
	PurchasedCredits       int `json:"purchasedCredits"`
	OpensourceMonthlyCredits int `json:"opensourceMonthlyCredits"`
}

// commandCodeCreditsResponse mirrors the top-level response of
// GET /internal/billing/credits.
type commandCodeCreditsResponse struct {
	Credits      commandCodeCredits                     `json:"credits"`
	WindowLimits map[string]*commandCodeWindow          `json:"windowLimits"`
}

func (p *CommandCodeProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	cookie := config["cookie"]
	if cookie == "" {
		return nil, fmt.Errorf("session cookie is required")
	}

	const endpoint = "https://api.commandcode.ai/internal/billing/credits"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "__Secure-commandcode_prod_.session_token="+cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://commandcode.ai")
	req.Header.Set("Referer", "https://commandcode.ai/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("commandcode request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("commandcode unauthorized (re-paste a fresh session cookie)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("commandcode error: HTTP %d: %s", resp.StatusCode, truncateBody(body, 400))
	}

	var parsed commandCodeCreditsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode commandcode credits response: %w", err)
	}

	res := &quota.QuotaResponse{}

	if win, ok := parsed.WindowLimits["fiveHour"]; ok && win != nil {
		res.FiveHour = quota.Quota{
			Used:      win.Used,
			Limit:     win.Cap,
			Unit:      "count",
			ResetTime: win.ResetAt,
		}
	}
	if win, ok := parsed.WindowLimits["weekly"]; ok && win != nil {
		res.Weekly = quota.Quota{
			Used:      win.Used,
			Limit:     win.Cap,
			Unit:      "count",
			ResetTime: win.ResetAt,
		}
	}

	res.Monthly = quota.Quota{
		Used:  0,
		Limit: parsed.Credits.MonthlyCredits,
		Unit:  "credits",
	}

	res.Groups = []quota.QuotaGroup{
		{
			Name:        "Credits",
			Description: "Command Code credit balance",
			Items: []quota.QuotaItem{
				{
					Name:  "monthly",
					Label: "Monthly Credits",
					Used:  0,
					Limit: parsed.Credits.MonthlyCredits,
					Unit:  "credits",
				},
				{
					Name:  "purchased",
					Label: "Purchased Credits",
					Used:  0,
					Limit: parsed.Credits.PurchasedCredits,
					Unit:  "credits",
				},
				{
					Name:  "opensource",
					Label: "Open Source Credits",
					Used:  0,
					Limit: parsed.Credits.OpensourceMonthlyCredits,
					Unit:  "credits",
				},
			},
		},
	}

	return res, nil
}