package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/04mg/caw/internal/quota"
)

type GoogleModelQuota struct {
	QuotaInfo *struct {
		RemainingFraction *float64 `json:"remainingFraction"`
		ResetTime         string   `json:"resetTime"`
	} `json:"quotaInfo"`
}

type GoogleAvailableModelsResponse struct {
	Models map[string]GoogleModelQuota `json:"models"`
}

type AntigravityProvider struct{}

func init() {
	quota.RegisterProvider("antigravity", &AntigravityProvider{})
}

func (p *AntigravityProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	token := config["apiKey"]
	if token == "" {
		return nil, fmt.Errorf("API key / Token is required")
	}

	accessToken, err := getAccessToken(token)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	modelsResponse, err := fetchAvailableModels(accessToken)
	if err != nil {
		return nil, fmt.Errorf("api error: %w", err)
	}

	fiveHourQuota, err := getModelQuota(modelsResponse, "gemini-3-pro-high", "gemini-3-pro-low")
	if err != nil {
		return nil, err
	}

	weeklyQuota, err := getModelQuota(modelsResponse, "claude-opus-4-5-thinking", "claude-opus-4-5")
	if err != nil {
		return nil, err
	}

	monthlyQuota, err := getModelQuota(modelsResponse, "gemini-3-flash", "gemini-3-pro-image")
	if err != nil {
		return nil, err
	}

	return &quota.QuotaResponse{
		FiveHour: fiveHourQuota,
		Weekly:   weeklyQuota,
		Monthly:  monthlyQuota,
	}, nil
}

func getAccessToken(token string) (string, error) {
	// If it starts with ya29., it is already an access token
	if len(token) > 5 && token[:5] == "ya29." {
		return token, nil
	}

	// Exchange refresh token for access token using standard client credentials
	data := url.Values{}
	data.Set("client_id", "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com")
	data.Set("client_secret", "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")
	data.Set("refresh_token", token)
	data.Set("grant_type", "refresh_token")

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return tokenResp.AccessToken, nil
}

func fetchAvailableModels(accessToken string) (*GoogleAvailableModelsResponse, error) {
	bodyData := map[string]string{
		"ideName":       "antigravity",
		"extensionName": "antigravity",
		"locale":        "en",
		"ideVersion":    "unknown",
	}
	bodyBytes, err := json.Marshal(bodyData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "antigravity/1.11.9 windows/amd64")

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

	var respData GoogleAvailableModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &respData, nil
}

func getModelQuota(modelsResponse *GoogleAvailableModelsResponse, keys ...string) (quota.Quota, error) {
	for _, key := range keys {
		if m, ok := modelsResponse.Models[key]; ok {
			fraction := 1.0
			if m.QuotaInfo != nil && m.QuotaInfo.RemainingFraction != nil {
				fraction = *m.QuotaInfo.RemainingFraction
			}
			// Map remaining fraction to used percentage
			used := 100 - int(fraction*100)
			if used < 0 {
				used = 0
			}
			if used > 100 {
				used = 100
			}
			return quota.Quota{
				Used:  used,
				Limit: 100,
			}, nil
		}
	}
	return quota.Quota{
		Used:  0,
		Limit: 100,
	}, nil
}
