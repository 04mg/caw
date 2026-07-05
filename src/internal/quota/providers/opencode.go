package providers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/04mg/caw/internal/quota"
)

type OpenCodeProvider struct{}

func init() {
	quota.RegisterProvider("opencode", &OpenCodeProvider{})
}

var reComments = regexp.MustCompile(`<!--.*?-->`)
var reRolling = regexp.MustCompile(`Rolling Usage.*?usage-value[^>]*>(\d+)%`)
var reWeekly = regexp.MustCompile(`Weekly Usage.*?usage-value[^>]*>(\d+)%`)
var reMonthly = regexp.MustCompile(`Monthly Usage.*?usage-value[^>]*>(\d+)%`)

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

	mRolling := reRolling.FindStringSubmatch(cleaned)
	mWeekly := reWeekly.FindStringSubmatch(cleaned)
	mMonthly := reMonthly.FindStringSubmatch(cleaned)

	if mRolling == nil || mWeekly == nil || mMonthly == nil {
		return nil, fmt.Errorf("failed to parse quotas from HTML (rolling=%v, weekly=%v, monthly=%v)", mRolling != nil, mWeekly != nil, mMonthly != nil)
	}

	var rollingVal, weeklyVal, monthlyVal int
	if _, err := fmt.Sscanf(mRolling[1], "%d", &rollingVal); err != nil {
		return nil, fmt.Errorf("invalid rolling value: %s", mRolling[1])
	}
	if _, err := fmt.Sscanf(mWeekly[1], "%d", &weeklyVal); err != nil {
		return nil, fmt.Errorf("invalid weekly value: %s", mWeekly[1])
	}
	if _, err := fmt.Sscanf(mMonthly[1], "%d", &monthlyVal); err != nil {
		return nil, fmt.Errorf("invalid monthly value: %s", mMonthly[1])
	}

	return &quota.QuotaResponse{
		FiveHour: quota.Quota{Used: rollingVal, Limit: 100},
		Weekly:   quota.Quota{Used: weeklyVal, Limit: 100},
		Monthly:  quota.Quota{Used: monthlyVal, Limit: 100},
	}, nil
}
