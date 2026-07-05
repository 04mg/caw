package agents

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/04mg/caw/internal/quota"
)

type AntigravityProvider struct{}

func init() {
	quota.RegisterProvider("antigravity", &AntigravityProvider{})
}

func (p *AntigravityProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	apiKey := config["apiKey"]
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	req, err := http.NewRequest("GET", "https://api.antigravity.google/v1/quota", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

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

	var qResp quota.QuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &qResp, nil
}
