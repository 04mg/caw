package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/04mg/caw/internal/quota"
)

// ClaudeProvider fetches Claude Code usage via the Anthropic OAuth API.
type ClaudeProvider struct{}

func init() {
	quota.RegisterProvider("claude", &ClaudeProvider{})
}

func (p *ClaudeProvider) IsInstalled() bool {
	if _, err := findClaudeCredentialsFile(); err == nil {
		return true
	}
	if _, err := execLookPath("claude"); err == nil {
		return true
	}
	return false
}

// ClaudeOAuthUsageResponse mirrors the Anthropic OAuth usage endpoint.
// We decode only the fields we need; unknown fields are ignored.
type ClaudeOAuthUsageResponse struct {
	FiveHour  *ClaudeOAuthWindow `json:"five_hour"`
	SevenDay  *ClaudeOAuthWindow `json:"seven_day"`
	ExtraUse  *ClaudeExtraUsage   `json:"extra_usage"`
}

type ClaudeOAuthWindow struct {
	UsedPercent float64 `json:"used_percent"`
	ResetAt     string  `json:"reset_at"`
}

type ClaudeExtraUsage struct {
	IsEnabled    bool    `json:"is_enabled"`
	MonthlyLimit float64 `json:"monthly_limit"`
	UsedCredits  float64 `json:"used_credits"`
	Utilization  float64 `json:"utilization"`
}

func (p *ClaudeProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	accessToken, err := resolveClaudeAccessToken(config)
	if err != nil {
		return nil, err
	}

	usage, err := fetchClaudeOAuthUsage(accessToken)
	if err != nil {
		return nil, err
	}

	var fiveHour, weekly quota.Quota
	if usage.FiveHour != nil {
		fiveHour = quota.Quota{
			Used:      clampPercent(usage.FiveHour.UsedPercent),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: usage.FiveHour.ResetAt,
		}
	} else if usage.SevenDay != nil {
		// seven_day fallback when five_hour missing.
		fiveHour = quota.Quota{
			Used:      clampPercent(usage.SevenDay.UsedPercent),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: usage.SevenDay.ResetAt,
		}
	}
	if usage.SevenDay != nil {
		weekly = quota.Quota{
			Used:      clampPercent(usage.SevenDay.UsedPercent),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: usage.SevenDay.ResetAt,
		}
	}

	var monthly quota.Quota
	if usage.ExtraUse != nil && usage.ExtraUse.IsEnabled && usage.ExtraUse.MonthlyLimit > 0 {
		monthly = quota.Quota{
			Used:  clampPercent(usage.ExtraUse.Utilization),
			Limit: 100,
			Unit:  "percentage",
		}
	}

	return &quota.QuotaResponse{
		FiveHour: fiveHour,
		Weekly:   weekly,
		Monthly:  monthly,
	}, nil
}

func resolveClaudeAccessToken(config map[string]string) (string, error) {
	// 1. Manual override from settings
	if token := config["accessToken"]; token != "" {
		return token, nil
	}
	if token := config["apiKey"]; token != "" {
		return token, nil
	}

	// 2. Read from ~/.claude/.credentials.json
	creds, err := findClaudeCredentialsFile()
	if err != nil {
		return "", fmt.Errorf("claude credentials not found: %w", err)
	}

	data, err := os.ReadFile(creds)
	if err != nil {
		return "", fmt.Errorf("failed to read claude credentials: %w", err)
	}

	var parsed struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"access_token"`
		} `json:"claudeAiOauth"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse claude credentials: %w", err)
	}
	if parsed.ClaudeAiOauth.AccessToken != "" {
		return parsed.ClaudeAiOauth.AccessToken, nil
	}
	if parsed.AccessToken != "" {
		return parsed.AccessToken, nil
	}
	return "", fmt.Errorf("no access_token in claude credentials")
}

func findClaudeCredentialsFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(home, ".claude", ".credentials.json"),
		}
	} else {
		candidates = []string{
			filepath.Join(home, ".claude", ".credentials.json"),
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("claude credentials file not found")
}

func fetchClaudeOAuthUsage(accessToken string) (*ClaudeOAuthUsageResponse, error) {
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-code/2.1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude oauth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("claude oauth unauthorized (run `claude login`)")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("claude oauth rate limited, retry shortly")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude oauth error: HTTP %d: %s", resp.StatusCode, truncateBody(body, 400))
	}

	var parsed ClaudeOAuthUsageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode claude usage: %w", err)
	}
	return &parsed, nil
}

func clampPercent(v float64) int {
	n := int(v + 0.5)
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func truncateBody(b []byte, max int) string {
	s := string(b)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}