package quota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

const githubClientID = "Ov23liFkTGbDgDFsQPC8"

type DeviceLoginClient struct {
	ClientID   string
	Scope      string
	CodeURL    string
	TokenURL   string
	HTTPClient *http.Client
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type DevicePollRequest struct {
	DeviceCode string `json:"device_code"`
}

type DeviceTokenResponse struct {
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func initiateDeviceLogin() (*DeviceCodeResponse, error) {
	return defaultGitHubDeviceLoginClient().Initiate()
}

func pollDeviceToken(deviceCode string) (*DeviceTokenResponse, error) {
	return defaultGitHubDeviceLoginClient().Poll(deviceCode)
}

func defaultGitHubDeviceLoginClient() DeviceLoginClient {
	return DeviceLoginClient{
		ClientID:   githubClientID,
		Scope:      "read:user",
		CodeURL:    "https://github.com/login/device/code",
		TokenURL:   "https://github.com/login/oauth/access_token",
		HTTPClient: http.DefaultClient,
	}
}

func (c DeviceLoginClient) Initiate() (*DeviceCodeResponse, error) {
	body := fmt.Sprintf("client_id=%s&scope=%s", c.ClientID, c.Scope)
	req, err := http.NewRequest("POST", c.CodeURL, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	var dc DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("failed to decode device code response: %w", err)
	}
	return &dc, nil
}

func (c DeviceLoginClient) Poll(deviceCode string) (*DeviceTokenResponse, error) {
	body := fmt.Sprintf("client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code", c.ClientID, deviceCode)
	req, err := http.NewRequest("POST", c.TokenURL, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("token poll request failed: %w", err)
	}
	defer resp.Body.Close()

	var tr DeviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	return &tr, nil
}

func (c DeviceLoginClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}
