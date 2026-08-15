package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
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

// commandCodeWindow mirrors a single usage window returned by the API. All
// numeric fields are floats (used/cap come back with fractional parts) and
// resetAt is a UNIX epoch in milliseconds.
type commandCodeWindow struct {
	Used     float64 `json:"used"`
	Cap      float64 `json:"cap"`
	ResetAt  int64   `json:"resetAt"`
	Exceeded bool    `json:"exceeded"`
}

// commandCodeCredits mirrors the credits payload returned by the API.
type commandCodeCredits struct {
	MonthlyCredits           float64 `json:"monthlyCredits"`
	PurchasedCredits         float64 `json:"purchasedCredits"`
	OpensourceMonthlyCredits float64 `json:"opensourceMonthlyCredits"`
}

// commandCodeCreditsResponse mirrors the top-level response of
// GET /internal/billing/credits.
//
// windowLimits values are window objects for active rate-limit windows but
// may also be a bare boolean (e.g. false) or null for windows the account
// does not track or that are unlimited, so each value is decoded
// defensively via json.RawMessage.
type commandCodeCreditsResponse struct {
	Credits      commandCodeCredits         `json:"credits"`
	WindowLimits map[string]json.RawMessage `json:"windowLimits"`
}

// commandCodeInvoice mirrors a single billing invoice returned by
// GET /internal/billing/customers/invoices. The "credits" field carries the
// number of credits granted by the subscription (e.g. 10 for a paid plan).
type commandCodeInvoice struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	AmountPaid  float64 `json:"amountPaid"`
	Credits     float64 `json:"credits"`
	InvoiceType string  `json:"invoiceType"`
	HasStripe   bool    `json:"hasStripeInvoice"`
	AutoCharged bool    `json:"autoCharged"`
}

// commandCodeInvoicesResponse mirrors the top-level response of
// GET /internal/billing/customers/invoices?limit=10.
type commandCodeInvoicesResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Invoices []commandCodeInvoice `json:"invoices"`
		HasMore  bool                 `json:"hasMore"`
	} `json:"data"`
}

func (r *commandCodeCreditsResponse) window(name string) *commandCodeWindow {
	raw, ok := r.WindowLimits[name]
	if !ok {
		return nil
	}
	// Skip values that are booleans or nulls rather than window objects.
	if string(raw) != "null" && string(raw) != "true" && string(raw) != "false" {
		var win commandCodeWindow
		if err := json.Unmarshal(raw, &win); err == nil {
			return &win
		}
	}
	return nil
}

// commandCodeResetTime converts the API's millisecond epoch to an RFC 3339
// timestamp, or "" when resetAt is absent or zero.
func commandCodeResetTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// roundUsed rounds a fractional used value to two decimal places so small
// usages like 0.0845681984 are displayed as 0.08 instead of truncating to 0.
func roundUsed(v float64) float64 {
	return math.Round(v*100) / 100
}

// roundCredits rounds a fractional credit balance to two decimal places so a
// remaining balance like 8.6855948068 is displayed as 8.69 instead of being
// rounded up to a misleading whole 9.
func roundCredits(v float64) float64 {
	return math.Round(v*100) / 100
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

	if win := parsed.window("fiveHour"); win != nil {
		res.FiveHour = quota.Quota{
			Used:      roundUsed(win.Used),
			Limit:     win.Cap,
			Unit:      "currency",
			ResetTime: commandCodeResetTime(win.ResetAt),
		}
	}
	if win := parsed.window("weekly"); win != nil {
		res.Weekly = quota.Quota{
			Used:      roundUsed(win.Used),
			Limit:     win.Cap,
			Unit:      "currency",
			ResetTime: commandCodeResetTime(win.ResetAt),
		}
	}

	// The /credits endpoint reports remaining balances. The intended display is
	// "consumed / total" (e.g. 1.55$ / 10$), i.e. how much has been used out of
	// the full allowance. The full credit allowance comes from the billing
	// invoices, which carry the subscription's credits (e.g. 10 for a paid plan).
	subTotal, purchasedTotal := fetchCommandCodeInvoiceCredits(cookie)
	total := subTotal
	if total <= 0 {
		// Fall back to the remaining balance when no invoice is available so the
		// limit stays meaningful instead of 0.
		total = parsed.Credits.MonthlyCredits
	}

	res.Monthly = quota.Quota{
		Used:  consumedFrom(parsed.Credits.MonthlyCredits, total),
		Limit: roundCredits(total),
		Unit:  "currency",
	}

	res.Groups = []quota.QuotaGroup{
		{
			Name:        "Credits",
			Description: "Command Code credit balance",
			Items: []quota.QuotaItem{
				{
					Name:  "monthly",
					Label: "Monthly Credits",
					Used:  consumedFrom(parsed.Credits.MonthlyCredits, total),
					Limit: roundCredits(total),
					Unit:  "currency",
				},
				{
					Name:  "purchased",
					Label: "Purchased Credits",
					Used:  consumedFrom(parsed.Credits.PurchasedCredits, purchasedTotal),
					Limit: roundCredits(purchasedTotal),
					Unit:  "currency",
				},
				{
					Name:  "opensource",
					Label: "Open Source Credits",
					Used:  consumedFrom(parsed.Credits.OpensourceMonthlyCredits, total),
					Limit: roundCredits(total),
					Unit:  "currency",
				},
			},
		},
	}

	return res, nil
}

// consumedFrom returns the amount consumed out of a per-pool total given the
// pool's remaining balance, clamped to >= 0.
func consumedFrom(remaining, poolTotal float64) float64 {
	used := poolTotal - remaining
	if used < 0 {
		used = 0
	}
	return roundCredits(used)
}

// fetchCommandCodeInvoiceCredits returns the credits granted by the active
// subscription and purchased add-ons by querying the customer's billing
// invoices. It returns (0, 0) when no matching paid invoice is found or the
// request fails.
func fetchCommandCodeInvoiceCredits(cookie string) (subscription, purchased float64) {
	const endpoint = "https://api.commandcode.ai/internal/billing/customers/invoices?limit=10"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, 0
	}
	req.Header.Set("Cookie", "__Secure-commandcode_prod_.session_token="+cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://commandcode.ai")
	req.Header.Set("Referer", "https://commandcode.ai/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}
	body, _ := io.ReadAll(resp.Body)

	var parsed commandCodeInvoicesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, 0
	}

	for _, inv := range parsed.Data.Invoices {
		if inv.Status != "paid" || inv.Credits <= 0 {
			continue
		}
		if inv.InvoiceType == "subscription_credits" {
			subscription += inv.Credits
		} else if inv.AutoCharged || inv.InvoiceType != "" {
			purchased += inv.Credits
		}
	}
	return subscription, purchased
}
