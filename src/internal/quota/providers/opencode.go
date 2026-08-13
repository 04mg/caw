package providers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/04mg/caw/internal/quota"
)

type OpenCodeProvider struct{}

func init() {
	quota.RegisterProvider("opencode", &OpenCodeProvider{})
}

var reComments = regexp.MustCompile(`<!--.*?-->`)

// Regex patterns matching the SolidJS SSR hydration output.
// Field order may vary, so we try both orderings.
var scrapedNumber = `(-?\d+(?:\.\d+)?)`

var reRollingPctFirst = regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{[^}]*usagePercent:` + scrapedNumber + `[^}]*resetInSec:` + scrapedNumber + `[^}]*\}`)
var reRollingResetFirst = regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{[^}]*resetInSec:` + scrapedNumber + `[^}]*usagePercent:` + scrapedNumber + `[^}]*\}`)
var reWeeklyPctFirst = regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{[^}]*usagePercent:` + scrapedNumber + `[^}]*resetInSec:` + scrapedNumber + `[^}]*\}`)
var reWeeklyResetFirst = regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{[^}]*resetInSec:` + scrapedNumber + `[^}]*usagePercent:` + scrapedNumber + `[^}]*\}`)
var reMonthlyPctFirst = regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{[^}]*usagePercent:` + scrapedNumber + `[^}]*resetInSec:` + scrapedNumber + `[^}]*\}`)
var reMonthlyResetFirst = regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{[^}]*resetInSec:` + scrapedNumber + `[^}]*usagePercent:` + scrapedNumber + `[^}]*\}`)

// Fallback: match the rendered usage-value spans (original approach).
var reRollingFallback = regexp.MustCompile(`Rolling Usage.*?usage-value[^>]*>(\d+)%`)
var reWeeklyFallback = regexp.MustCompile(`Weekly Usage.*?usage-value[^>]*>(\d+)%`)
var reMonthlyFallback = regexp.MustCompile(`Monthly Usage.*?usage-value[^>]*>(\d+)%`)

type opencodeWindow struct {
	usagePercent int
	resetInSec   float64
}

func parseOpenCodeWindow(html string, rePctFirst, reResetFirst *regexp.Regexp) *opencodeWindow {
	if m := rePctFirst.FindStringSubmatch(html); m != nil {
		pct, _ := strconv.ParseFloat(m[1], 64)
		sec, _ := strconv.ParseFloat(m[2], 64)
		return &opencodeWindow{usagePercent: int(pct), resetInSec: sec}
	}
	if m := reResetFirst.FindStringSubmatch(html); m != nil {
		sec, _ := strconv.ParseFloat(m[1], 64)
		pct, _ := strconv.ParseFloat(m[2], 64)
		return &opencodeWindow{usagePercent: int(pct), resetInSec: sec}
	}
	return nil
}

func resetTimeFromSec(sec float64) string {
	if sec <= 0 {
		return ""
	}
	return time.Now().Add(time.Duration(sec) * time.Second).UTC().Format(time.RFC3339)
}

func (p *OpenCodeProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	cookie := config["cookie"]
	workspaceID := config["workspaceId"]

	if cookie == "" {
		return nil, fmt.Errorf("auth cookie is required")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace ID is required")
	}

	url := fmt.Sprintf("https://opencode.ai/workspace/%s/go", workspaceID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", fmt.Sprintf("auth=%s", cookie))
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

	// Try SolidJS hydration data first (includes resetInSec).
	rolling := parseOpenCodeWindow(cleaned, reRollingPctFirst, reRollingResetFirst)
	weekly := parseOpenCodeWindow(cleaned, reWeeklyPctFirst, reWeeklyResetFirst)
	monthly := parseOpenCodeWindow(cleaned, reMonthlyPctFirst, reMonthlyResetFirst)

	// Fallback to rendered usage-value spans if hydration data not found.
	if rolling == nil {
		if m := reRollingFallback.FindStringSubmatch(cleaned); m != nil {
			val, _ := strconv.Atoi(m[1])
			rolling = &opencodeWindow{usagePercent: val}
		}
	}
	if weekly == nil {
		if m := reWeeklyFallback.FindStringSubmatch(cleaned); m != nil {
			val, _ := strconv.Atoi(m[1])
			weekly = &opencodeWindow{usagePercent: val}
		}
	}
	if monthly == nil {
		if m := reMonthlyFallback.FindStringSubmatch(cleaned); m != nil {
			val, _ := strconv.Atoi(m[1])
			monthly = &opencodeWindow{usagePercent: val}
		}
	}

	if rolling == nil && weekly == nil && monthly == nil {
		return nil, fmt.Errorf("failed to parse quotas from HTML")
	}

	res := &quota.QuotaResponse{
		FiveHour: quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
		Weekly:   quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
		Monthly:  quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
	}
	if rolling != nil {
		res.FiveHour = quota.Quota{
			Used:      float64(rolling.usagePercent),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: resetTimeFromSec(rolling.resetInSec),
		}
	}
	if weekly != nil {
		res.Weekly = quota.Quota{
			Used:      float64(weekly.usagePercent),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: resetTimeFromSec(weekly.resetInSec),
		}
	}
	if monthly != nil {
		res.Monthly = quota.Quota{
			Used:      float64(monthly.usagePercent),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: resetTimeFromSec(monthly.resetInSec),
		}
	}

	return res, nil
}