package providers

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	ptylib "github.com/aymanbagabas/go-pty"
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

type QuotaSummaryBucket struct {
	BucketID          string   `json:"bucketId"`
	DisplayName       string   `json:"displayName"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
	ResetDescription  string   `json:"resetDescription"`
	Disabled          bool     `json:"disabled"`
}

type QuotaSummaryGroup struct {
	DisplayName string               `json:"displayName"`
	Description string               `json:"description"`
	Buckets     []QuotaSummaryBucket `json:"buckets"`
}

type QuotaSummary struct {
	Description string               `json:"description"`
	Groups      []QuotaSummaryGroup  `json:"groups"`
}

type QuotaSummaryResponse struct {
	Code         interface{}          `json:"code"`
	Message      string               `json:"message"`
	QuotaSummary *QuotaSummary        `json:"quotaSummary"`
	Response     *QuotaSummary        `json:"response"`
	Groups       []QuotaSummaryGroup  `json:"groups"`
	Description  string               `json:"description"`
}

type AntigravityProvider struct{}

// bgAgy holds the state of a background agy PTY instance spawned on demand
// to query the Antigravity quota. It is closed once the quota has been read
// so it does not linger in the background consuming resources.
type bgAgy struct {
	ptmx ptylib.Pty
	cmd  *ptylib.Cmd
	pid  int
}

func init() {
	quota.RegisterProvider("antigravity", &AntigravityProvider{})
}

func (p *AntigravityProvider) ImportLogin() (map[string]string, error) {
	t, err := readAgyStoredToken()
	if err != nil {
		return nil, fmt.Errorf("no active Antigravity login found on disk (~/.gemini/antigravity-cli/antigravity-oauth-token); log in with Antigravity in your terminal first")
	}
	best := t.bestToken()
	if best == "" {
		return nil, fmt.Errorf("no valid token found in Antigravity credential file; log in with Antigravity in your terminal first")
	}
	raw, err := readAgyStoredTokenRaw()
	if err != nil {
		raw = []byte("{}")
	}
	return map[string]string{
		"credentialsJson": string(raw),
		"accessToken":     t.Token.AccessToken,
		"refreshToken":    t.Token.RefreshToken,
		"expiry":          t.Token.Expiry,
		"importedAt":      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (p *AntigravityProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	// If the account has explicit credentials (e.g. imported login or configured token),
	// query Google Cloud quota API directly with those credentials.
	if hasExplicitAntigravityCredentials(config) {
		return fetchQuotaFromExplicitCredentials(config)
	}

	// 1. Try to find an already running agy process (user-opened or our background one).
	// A running instance exposes the richest quota data via its local endpoint.
	pids, err := findAgyPids()
	if err == nil && len(pids) > 0 {
		ports, err := findPortsForPids(pids)
		if err == nil && len(ports) > 0 {
			if res, err := queryAgyPorts(ports); err == nil {
				return res, nil
			}
		}
	}

	// 2. Query the Google Cloud quota API directly with OAuth credentials read
	// from disk (written by the agy CLI at login time) or a manually configured
	// apiKey. This avoids spawning an agy process entirely.
	if res, err := fetchQuotaFromStoredCredentials(config); err == nil {
		return res, nil
	}

	// 3. Last resort: spawn a temporary background instance for this query.
	spawned, err := ensureBgAgy()
	if err != nil {
		return nil, fmt.Errorf("agy is not running: %w", err)
	}
	// Close the spawned instance once the quota has been queried so it does
	// not linger in the background consuming resources. A user-opened agy
	// pane is never touched.
	defer closeBgAgy(spawned)

	// Wait up to 15s for the background agy to bind a port
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		pids, err := findAgyPids()
		if err == nil && len(pids) > 0 {
			ports, err := findPortsForPids(pids)
			if err == nil && len(ports) > 0 {
				if res, err := queryAgyPorts(ports); err == nil {
					return res, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("agy did not expose a quota endpoint")
}

func hasExplicitAntigravityCredentials(config map[string]string) bool {
	if len(config) == 0 {
		return false
	}
	return config["credentialsJson"] != "" || config["refreshToken"] != "" || config["accessToken"] != "" || config["apiKey"] != "" || config["token"] != ""
}

func fetchQuotaFromExplicitCredentials(config map[string]string) (*quota.QuotaResponse, error) {
	token := resolveExplicitAntigravityToken(config)
	if token == "" {
		return nil, fmt.Errorf("no antigravity oauth token configured for account")
	}
	return fetchQuotaViaOAuth(token)
}

func resolveExplicitAntigravityToken(config map[string]string) string {
	if raw := config["credentialsJson"]; raw != "" {
		var t agyStoredToken
		if err := json.Unmarshal([]byte(raw), &t); err == nil {
			if best := t.bestToken(); best != "" {
				return best
			}
		}
	}
	if t := config["refreshToken"]; t != "" {
		return t
	}
	if t := config["accessToken"]; t != "" {
		return t
	}
	if t := config["apiKey"]; t != "" {
		return t
	}
	if t := config["token"]; t != "" {
		return t
	}
	return ""
}

type agyStoredToken struct {
	Token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Expiry       string `json:"expiry"`
	} `json:"token"`
}

// agyTokenPath returns the location of the OAuth credential file the agy CLI
// writes after a successful login.
func agyTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"), nil
}

func readAgyStoredTokenRaw() ([]byte, error) {
	path, err := agyTokenPath()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func readAgyStoredToken() (*agyStoredToken, error) {
	data, err := readAgyStoredTokenRaw()
	if err != nil {
		return nil, err
	}
	var t agyStoredToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// bestToken returns the cached access token while it is still valid and falls
// back to the refresh token otherwise. getAccessToken accepts both forms.
func (t *agyStoredToken) bestToken() string {
	if t.Token.AccessToken == "" {
		return t.Token.RefreshToken
	}
	if exp, err := time.Parse(time.RFC3339Nano, t.Token.Expiry); err == nil && time.Now().Before(exp.Add(-30*time.Second)) {
		return t.Token.AccessToken
	}
	return t.Token.RefreshToken
}

// fetchQuotaFromStoredCredentials queries the quota API directly using locally
// stored OAuth credentials instead of starting an agy process.
func fetchQuotaFromStoredCredentials(config map[string]string) (*quota.QuotaResponse, error) {
	if t, err := readAgyStoredToken(); err == nil {
		if token := t.bestToken(); token != "" {
			return fetchQuotaViaOAuth(token)
		}
	}
	return nil, fmt.Errorf("no antigravity oauth credentials found")
}

// ensureBgAgy starts a temporary background agy PTY instance to query the
// Antigravity quota. The returned handle must be closed with closeBgAgy once
// the quota has been queried.
func ensureBgAgy() (*bgAgy, error) {
	agyPath, err := findAgyPath()
	if err != nil {
		return nil, err
	}

	ptmx, err := ptylib.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create PTY: %w", err)
	}

	ptyCmd := ptmx.Command(agyPath, "--dangerously-skip-permissions")
	ptyCmd.Env = append(os.Environ(), "TERM=xterm-256color")

	if err := ptyCmd.Start(); err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("failed to start agy in PTY: %w", err)
	}

	h := &bgAgy{ptmx: ptmx, cmd: ptyCmd, pid: ptyCmd.Process.Pid}

	// Drain PTY output in background to prevent blocking
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := ptmx.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	// Reap the process in background so it doesn't become a zombie
	go func() {
		ptyCmd.Wait()
	}()

	return h, nil
}

// closeBgAgy kills a background agy instance spawned by ensureBgAgy.
func closeBgAgy(h *bgAgy) {
	if h == nil {
		return
	}
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
	if h.ptmx != nil {
		_ = h.ptmx.Close()
	}
}

func fetchQuotaViaOAuth(token string) (*quota.QuotaResponse, error) {
	accessToken, err := getAccessToken(token)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	// 1. Prefer retrieveUserQuotaSummary directly via Cloud API.
	// This returns the exact same rich quota summary as the local language server,
	// including gemini-weekly, gemini-5h, 3p-weekly, and 3p-5h.
	if qs, err := fetchUserQuotaSummaryAPI(accessToken); err == nil && qs != nil {
		return mapQuotaSummaryToResponse(qs), nil
	}

	// 2. Fallback to fetchAvailableModels
	modelsResponse, err := fetchAvailableModels(accessToken)
	if err != nil {
		return nil, fmt.Errorf("api error: %w", err)
	}
	fiveHourQuota, err := getModelQuota(modelsResponse, "gemini-3.1-pro-high", "gemini-3-pro-high", "gemini-3.1-pro-low", "gemini-3-pro-low")
	if err != nil {
		return nil, err
	}
	weeklyQuota, err := getModelQuota(modelsResponse, "claude-opus-4-6-thinking", "claude-opus-4-5-thinking", "claude-opus-4-6", "claude-opus-4-5")
	if err != nil {
		return nil, err
	}
	return &quota.QuotaResponse{
		FiveHour: fiveHourQuota,
		Weekly:   weeklyQuota,
	}, nil
}

func fetchUserQuotaSummaryAPI(accessToken string) (*QuotaSummary, error) {
	req, err := http.NewRequest("POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "antigravity/1.11.9 windows/amd64")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var qResp QuotaSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return nil, err
	}

	summary := qResp.QuotaSummary
	if summary == nil {
		summary = qResp.Response
	}
	if summary == nil && len(qResp.Groups) > 0 {
		summary = &QuotaSummary{
			Groups:      qResp.Groups,
			Description: qResp.Description,
		}
	}
	if summary == nil {
		return nil, fmt.Errorf("quotaSummary missing in response")
	}
	return summary, nil
}

func (p *AntigravityProvider) IsInstalled() bool {
	// The provider works both with an agy binary and with OAuth credentials
	// left on disk by a previous agy login.
	if _, err := findAgyPath(); err == nil {
		return true
	}
	if t, err := readAgyStoredToken(); err == nil && t.bestToken() != "" {
		return true
	}
	return false
}

func findAgyPids() ([]int, error) {
	var pids []int
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/nh", "/fo", "csv")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		lines := strings.Split(out.String(), "\n")
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), "agy.exe") {
				parts := strings.Split(line, ",")
				if len(parts) >= 2 {
					pidStr := strings.Trim(parts[1], "\" \r\n")
					if pid, err := strconv.Atoi(pidStr); err == nil {
						pids = append(pids, pid)
					}
				}
			}
		}
	} else {
		cmd := exec.Command("ps", "-ax", "-o", "pid=", "-o", "comm=")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		lines := strings.Split(out.String(), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				pidStr := fields[0]
				comm := fields[1]
				if strings.Contains(strings.ToLower(comm), "agy") {
					if pid, err := strconv.Atoi(pidStr); err == nil {
						pids = append(pids, pid)
					}
				}
			}
		}
	}
	return pids, nil
}

func findPortsForPids(pids []int) ([]int, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	var ports []int

	if runtime.GOOS == "windows" {
		cmd := exec.Command("netstat", "-ano")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		lines := strings.Split(out.String(), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 5 && fields[0] == "TCP" && fields[3] == "LISTENING" {
				pidStr := fields[4]
				for _, pid := range pids {
					if pidStr == strconv.Itoa(pid) {
						addr := fields[1]
						idx := strings.LastIndex(addr, ":")
						if idx != -1 {
							portStr := addr[idx+1:]
							if port, err := strconv.Atoi(portStr); err == nil {
								ports = append(ports, port)
							}
						}
					}
				}
			}
		}
	} else {
		// Linux: Try ss first (standard on Linux distributions without requiring lsof)
		ssCmd := exec.Command("ss", "-tlpn", "-H")
		var ssOut bytes.Buffer
		ssCmd.Stdout = &ssOut
		if err := ssCmd.Run(); err == nil {
			for _, line := range strings.Split(ssOut.String(), "\n") {
				for _, pid := range pids {
					pidPattern := fmt.Sprintf("pid=%d,", pid)
					pidPatternEnd := fmt.Sprintf("pid=%d)", pid)
					if strings.Contains(line, pidPattern) || strings.Contains(line, pidPatternEnd) {
						fields := strings.Fields(line)
						if len(fields) >= 4 {
							addr := fields[3]
							idx := strings.LastIndex(addr, ":")
							if idx != -1 {
								portStr := addr[idx+1:]
								if port, err := strconv.Atoi(portStr); err == nil {
									ports = append(ports, port)
								}
							}
						}
					}
				}
			}
		}
		// Fallback to lsof if ss found nothing
		if len(ports) == 0 {
			for _, pid := range pids {
				cmd := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-p", strconv.Itoa(pid))
				var out bytes.Buffer
				cmd.Stdout = &out
				if err := cmd.Run(); err == nil {
					lines := strings.Split(out.String(), "\n")
					for _, line := range lines {
						if strings.Contains(line, "(LISTEN)") {
							fields := strings.Fields(line)
							if len(fields) >= 9 {
								name := fields[8]
								idx := strings.LastIndex(name, ":")
								if idx != -1 {
									portStr := name[idx+1:]
									if port, err := strconv.Atoi(portStr); err == nil {
										ports = append(ports, port)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	portMap := make(map[int]bool)
	var deduped []int
	for _, port := range ports {
		if !portMap[port] {
			portMap[port] = true
			deduped = append(deduped, port)
		}
	}
	return deduped, nil
}

func findAgyPath() (string, error) {
	if path := os.Getenv("ANTIGRAVITY_CLI_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	if path, err := exec.LookPath("agy"); err == nil {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var paths []string
	if runtime.GOOS == "windows" {
		paths = []string{
			filepath.Join(home, "AppData", "Local", "agy", "bin", "agy.exe"),
			filepath.Join(home, ".local", "bin", "agy.exe"),
		}
	} else {
		paths = []string{
			filepath.Join(home, ".local", "bin", "agy"),
			filepath.Join(home, "bin", "agy"),
			"/usr/local/bin/agy",
			"/usr/bin/agy",
		}
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("agy binary not found in PATH or standard locations")
}

func queryAgyPorts(ports []int) (*quota.QuotaResponse, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	var lastErr error
	for _, port := range ports {
		schemes := []string{"https", "http"}
		for _, scheme := range schemes {
			urlStr := fmt.Sprintf("%s://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary", scheme, port)
			
			req, err := http.NewRequest("POST", urlStr, strings.NewReader("{}"))
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connect-Protocol-Version", "1")

			resp, err := client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}

			if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
				continue
			}

			var qResp QuotaSummaryResponse
			if err := json.Unmarshal(body, &qResp); err != nil {
				lastErr = err
				continue
			}

			summary := qResp.QuotaSummary
			if summary == nil {
				summary = qResp.Response
			}
			if summary == nil && len(qResp.Groups) > 0 {
				summary = &QuotaSummary{
					Groups:      qResp.Groups,
					Description: qResp.Description,
				}
			}

			if summary == nil {
				lastErr = fmt.Errorf("quotaSummary missing in response")
				continue
			}

			return mapQuotaSummaryToResponse(summary), nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("failed to query quota from ports %v", ports)
}

func mapQuotaSummaryToResponse(qs *QuotaSummary) *quota.QuotaResponse {
	res := &quota.QuotaResponse{
		FiveHour: quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
		Weekly:   quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
	}

	for _, group := range qs.Groups {
		groupName := strings.ToLower(group.DisplayName)
		for _, bucket := range group.Buckets {
			if bucket.Disabled {
				continue
			}
			bucketID := strings.ToLower(bucket.BucketID)
			bucketName := strings.ToLower(bucket.DisplayName)
			combined := bucketID + " " + bucketName

			fraction := 1.0
			if bucket.RemainingFraction != nil {
				fraction = *bucket.RemainingFraction
			}
			used := 100 - int(fraction*100)
			if used < 0 {
				used = 0
			}
			if used > 100 {
				used = 100
			}

			is5h := strings.Contains(combined, "5h") || strings.Contains(combined, "5-hour") || strings.Contains(combined, "five hour")
			isWeekly := strings.Contains(combined, "weekly")

			if strings.Contains(groupName, "gemini") {
				if is5h {
					res.FiveHour = quota.Quota{Used: float64(used), Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
				} else if isWeekly {
					res.Weekly = quota.Quota{Used: float64(used), Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
				}
			} else {
				// 3p/Claude/GPT or other model groups: override if 5h/Weekly unset or higher usage
				if is5h && (res.FiveHour.ResetTime == "" || float64(used) > res.FiveHour.Used) {
					res.FiveHour = quota.Quota{Used: float64(used), Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
				}
				if isWeekly && (res.Weekly.ResetTime == "" || float64(used) > res.Weekly.Used) {
					res.Weekly = quota.Quota{Used: float64(used), Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
				}
			}
		}
	}
	return res
}

func getAccessToken(token string) (string, error) {
	if len(token) > 5 && token[:5] == "ya29." {
		return token, nil
	}

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
	// The endpoint rejects any request body fields; an empty JSON object is
	// the expected payload. It also requires an Antigravity User-Agent.
	req, err := http.NewRequest("POST", "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", strings.NewReader("{}"))
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
			used := 100 - int(fraction*100)
			if used < 0 {
				used = 0
			}
			if used > 100 {
				used = 100
			}
			resetTime := ""
			if m.QuotaInfo != nil {
				resetTime = m.QuotaInfo.ResetTime
			}
			return quota.Quota{
				Used:      float64(used),
				Limit:     100,
				Unit:      "percentage",
				ResetTime: resetTime,
			}, nil
		}
	}
	return quota.Quota{
		Used:  0,
		Limit: 100,
		Unit:  "percentage",
	}, nil
}
