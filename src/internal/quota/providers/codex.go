package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// codexAuthFile mirrors ~/.codex/auth.json written by the Codex CLI.
type codexAuthFile struct {
	Tokens struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id"`
}

func (p *CodexProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	accessToken, accountID, err := resolveCodexCredentials(config)
	if err != nil {
		return nil, err
	}

	usage, err := fetchCodexUsage(accessToken, accountID)
	if err != nil && isCodexAuthError(err) && config["credentialsJson"] != "" {
		// An imported/stale accessToken fails with 401 even when the CLI is
		// authenticated (disk holds a refreshed token). Retry once with the
		// live ~/.codex/auth.json before surfacing "auth expired".
		if tok, id, derr := readCodexAuthFile(); derr == nil && tok != "" && tok != accessToken {
			usage, err = fetchCodexUsage(tok, id)
		}
	}
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

func (p *CodexProvider) ImportLogin() (map[string]string, error) {
	authFile, err := findCodexAuthFile()
	if err != nil {
		return nil, fmt.Errorf("no active Codex auth file found on disk (~/.codex/auth.json); log in with Codex in your terminal first")
	}

	data, err := os.ReadFile(authFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read Codex auth file: %w", err)
	}

	var parsed codexAuthFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse Codex auth file: %w", err)
	}
	token := parsed.Tokens.AccessToken
	if token == "" {
		token = parsed.AccessToken
	}
	if token == "" {
		return nil, fmt.Errorf("no access token found in Codex auth file; log in with Codex in your terminal first")
	}
	accountID := parsed.Tokens.AccountID
	if accountID == "" {
		accountID = parsed.AccountID
	}

	cfg := map[string]string{
		"accessToken":     token,
		"credentialsJson": string(data),
		"importedAt":      time.Now().UTC().Format(time.RFC3339),
	}
	if accountID != "" {
		cfg["accountId"] = accountID
	}
	return cfg, nil
}

func codexAccountIDFromConfig(config map[string]string) string {
	for _, k := range []string{"accountId", "account_id", "chatgptAccountId", "chatgpt_account_id", "ChatGPT-Account-Id"} {
		if v := config[k]; v != "" {
			return v
		}
	}
	return ""
}

func resolveCodexCredentials(config map[string]string) (accessToken, accountID string, err error) {
	accountID = codexAccountIDFromConfig(config)

	// 1. Direct token override from account config.
	if token := config["accessToken"]; token != "" {
		if accountID == "" {
			// Pair a legacy token-only config with the account ID from
			// credentialsJson/disk — wham/usage 401s without it.
			if raw := config["credentialsJson"]; raw != "" {
				var parsed codexAuthFile
				if jerr := json.Unmarshal([]byte(raw), &parsed); jerr == nil {
					if id := parsed.Tokens.AccountID; id != "" {
						accountID = id
					} else if id := parsed.AccountID; id != "" {
						accountID = id
					}
				}
			}
			if accountID == "" {
				if _, id, derr := readCodexAuthFile(); derr == nil {
					accountID = id
				}
			}
		}
		return token, accountID, nil
	}
	if token := config["apiKey"]; token != "" {
		return token, accountID, nil
	}
	if raw := config["credentialsJson"]; raw != "" {
		var parsed codexAuthFile
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			tok := parsed.Tokens.AccessToken
			if tok == "" {
				tok = parsed.AccessToken
			}
			id := accountID
			if id == "" {
				id = parsed.Tokens.AccountID
			}
			if id == "" {
				id = parsed.AccountID
			}
			if tok != "" {
				return tok, id, nil
			}
		}
	}

	// 2. Read from ~/.codex/auth.json
	tok, id, err := readCodexAuthFile()
	if err != nil {
		return "", "", err
	}
	return tok, id, nil
}

func readCodexAuthFile() (accessToken, accountID string, err error) {
	authFile, err := findCodexAuthFile()
	if err != nil {
		return "", "", fmt.Errorf("codex auth.json not found: %w", err)
	}
	data, err := os.ReadFile(authFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read codex auth.json: %w", err)
	}
	var parsed codexAuthFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", "", fmt.Errorf("failed to parse codex auth.json: %w", err)
	}
	if parsed.Tokens.AccessToken != "" {
		return parsed.Tokens.AccessToken, parsed.Tokens.AccountID, nil
	}
	if parsed.AccessToken != "" {
		return parsed.AccessToken, parsed.AccountID, nil
	}
	return "", "", fmt.Errorf("no access_token in codex auth.json")
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

func fetchCodexUsage(accessToken, accountID string) (*CodexUsageResponse, error) {
	req, err := http.NewRequest("GET", "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if accountID != "" {
		// The wham/usage endpoint rejects valid ChatGPT OAuth tokens with
		// 401 unless the account is pinned via this header — the Codex CLI
		// sends it on every backend-api call.
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}
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

func isCodexAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "auth expired") || strings.Contains(s, "Unauthorized") || strings.Contains(s, "401")
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