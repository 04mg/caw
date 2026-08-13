package providers

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/04mg/caw/internal/quota"
)

type OllamaProvider struct{}

func init() {
	quota.RegisterProvider("ollama", &OllamaProvider{})
}

var reOllamaSession = regexp.MustCompile(`aria-label="Session usage ([0-9]+(?:\.[0-9]+)?)% used"`)
var reOllamaWeekly = regexp.MustCompile(`aria-label="Weekly usage ([0-9]+(?:\.[0-9]+)?)% used"`)
var reOllamaDataTime = regexp.MustCompile(`data-time="([^"]+)"`)

func (p *OllamaProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	cookie := config["cookie"]
	if cookie == "" {
		return nil, fmt.Errorf("cookie (__Secure-session) is required")
	}

	// Make request to ollama.com settings page
	req, err := http.NewRequest("GET", "https://ollama.com/settings", nil)
	if err != nil {
		return nil, err
	}

	// Setup __Secure-session cookie
	req.Header.Set("Cookie", fmt.Sprintf("__Secure-session=%s", cookie))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ollama settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	html := string(bodyBytes)

	// Regex parsing of Session usage. Ollama renders the value as a
	// decimal percentage (e.g. "3.5%"), so match a floating-point number
	// and round to the nearest integer.
	sessionUsed := 0
	if matches := reOllamaSession.FindStringSubmatch(html); len(matches) >= 2 {
		if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
			sessionUsed = int(math.Round(val))
		}
	} else if strings.Contains(html, "Weekly limit reached") {
		// Fallback if weekly limit is reached (blocks sessions)
		sessionUsed = 100
	}

	// Regex parsing of Weekly usage
	weeklyUsed := 0
	if matches := reOllamaWeekly.FindStringSubmatch(html); len(matches) >= 2 {
		if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
			weeklyUsed = int(math.Round(val))
		}
	}

	// Extract reset times from data-time attributes. The HTML renders a
	// .local-time element with data-time="ISO" after each usage block.
	// The first data-time corresponds to Session (5h), the second to Weekly.
	var sessionReset, weeklyReset string
	if dataTimes := reOllamaDataTime.FindAllStringSubmatch(html, -1); len(dataTimes) >= 1 {
		sessionReset = dataTimes[0][1]
		if len(dataTimes) >= 2 {
			weeklyReset = dataTimes[1][1]
		}
	}

	return &quota.QuotaResponse{
		FiveHour: quota.Quota{
			Used:      float64(sessionUsed),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: sessionReset,
		},
		Weekly: quota.Quota{
			Used:      float64(weeklyUsed),
			Limit:     100,
			Unit:      "percentage",
			ResetTime: weeklyReset,
		},
		Monthly: quota.Quota{
			Used:  0,
			Limit: 100,
			Unit:  "percentage",
		},
	}, nil
}