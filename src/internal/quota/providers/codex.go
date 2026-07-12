package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/04mg/caw/internal/quota"
)

// CodexProvider fetches OpenAI Codex usage via the wham/usage OAuth API.
type CodexProvider struct{}

func init() {
	quota.RegisterProvider("codex", &CodexProvider{})
}

func (p *CodexProvider) IsInstalled() bool {
	if _, err := findCodexAuthFile(); err == nil {
		return true
	}
	if _, err := execLookPath("codex"); err == nil {
		return true
	}
	return false
}

// CodexUsageResponse mirrors chatgpt.com/backend-api/wham/usage.
type CodexUsageResponse struct {
	RateLimit *CodexRateLimit `json:"rate_limit"`
	PlanType  string          `json:"plan_type"`
}

type CodexRateLimit struct {
	PrimaryWindow   *CodexWindow `json:"primary_window"`
	SecondaryWindow *CodexWindow `json:"secondary_window"`
}

type CodexWindow struct {
	UsedPercent       float64 `json:"used_percent"`
	ResetAt           float64 `json:"reset_at"`
	ResetAfterSeconds float64 `json:"reset_after_seconds"`
}

func (p *CodexProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	accessToken, err := resolveCodexAccessToken(config)
	if err != nil {
		return nil, err
	}

	usage, err := fetchCodexUsage(accessToken)
	if err != nil {
		return nil, err
	}

	var fiveHour, weekly quota.Quota
	if usage.RateLimit != nil {
		if usage.RateLimit.PrimaryWindow != nil {
			fiveHour = quota.Quota{
				Used:      clampPercent(usage.RateLimit.PrimaryWindow.UsedPercent),
				Limit:     100,
				Unit:      "percentage",
				ResetTime: codexResetTime(usage.RateLimit.PrimaryWindow),
			}
		}
		if usage.RateLimit.SecondaryWindow != nil {
			weekly = quota.Quota{
				Used:      clampPercent(usage.RateLimit.SecondaryWindow.UsedPercent),
				Limit:     100,
				Unit:      "percentage",
				ResetTime: codexResetTime(usage.RateLimit.SecondaryWindow),
			}
		}
	}

	return &quota.QuotaResponse{
		FiveHour: fiveHour,
		Weekly:   weekly,
		Monthly:  quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
	}, nil
}

func resolveCodexAccessToken(config map[string]string) (string, error) {
	if token := config["accessToken"]; token != "" {
		return token, nil
	}
	if token := config["apiKey"]; token != "" {
		return token, nil
	}

	authFile, err := findCodexAuthFile()
	if err != nil {
		return "", fmt.Errorf("codex auth.json not found: %w", err)
	}
	data, err := os.ReadFile(authFile)
	if err != nil {
		return "", fmt.Errorf("failed to read codex auth.json: %w", err)
	}
	var parsed struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse codex auth.json: %w", err)
	}
	if parsed.Tokens.AccessToken != "" {
		return parsed.Tokens.AccessToken, nil
	}
	if parsed.AccessToken != "" {
		return parsed.AccessToken, nil
	}
	return "", fmt.Errorf("no access_token in codex auth.json")
}

func findCodexAuthFile() (string, error) {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		p := filepath.Join(v, "auth.json")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(home, ".codex", "auth.json")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("codex auth.json not found")
}

func fetchCodexUsage(accessToken string) (*CodexUsageResponse, error) {
	req, err := http.NewRequest("GET", "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex-cli/0.1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex usage request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("codex auth expired (run `codex login`)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex usage error: HTTP %d: %s", resp.StatusCode, truncateBody(body, 400))
	}

	var parsed CodexUsageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode codex usage: %w", err)
	}
	return &parsed, nil
}

func codexResetTime(w *CodexWindow) string {
	if w.ResetAt > 0 {
		return time.Unix(int64(w.ResetAt), 0).UTC().Format(time.RFC3339)
	}
	if w.ResetAfterSeconds > 0 {
		return time.Now().Add(time.Duration(w.ResetAfterSeconds) * time.Second).UTC().Format(time.RFC3339)
	}
	return ""
}