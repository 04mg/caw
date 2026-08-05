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
	Code         interface{}   `json:"code"`
	Message      string        `json:"message"`
	QuotaSummary *QuotaSummary `json:"quotaSummary"`
	Response     *QuotaSummary `json:"response"`
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

func (p *AntigravityProvider) GetQuotas(config map[string]string) (*quota.QuotaResponse, error) {
	// 1. Try to find an already running agy process (user-opened or our background one)
	pids, err := findAgyPids()
	if err == nil && len(pids) > 0 {
		ports, err := findPortsForPids(pids)
		if err == nil && len(ports) > 0 {
			if res, err := queryAgyPorts(ports); err == nil {
				return res, nil
			}
		}
	}

	// 2. No running agy found — spawn a temporary background instance for this query
	spawned, err := ensureBgAgy()
	if err != nil {
		// 3. Fallback to Google Cloud OAuth API if apiKey/token is configured in Settings
		token := config["apiKey"]
		if token != "" {
			return fetchQuotaViaOAuth(token)
		}
		return nil, fmt.Errorf("agy is not running")
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

	// 3. Fallback to Google Cloud OAuth API if apiKey/token is configured in Settings
	token := config["apiKey"]
	if token != "" {
		return fetchQuotaViaOAuth(token)
	}
	return nil, fmt.Errorf("agy is not running")
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

func (p *AntigravityProvider) IsInstalled() bool {
	_, err := findAgyPath()
	return err == nil
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
			"/opt/homebrew/bin/agy",
			"/usr/local/bin/agy",
			filepath.Join(home, "bin", "agy"),
		}
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("agy binary not found")
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
		Monthly:  quota.Quota{Used: 0, Limit: 100, Unit: "percentage"},
	}

	var groups []quota.QuotaGroup
	for _, group := range qs.Groups {
		qg := quota.QuotaGroup{
			Name:        group.DisplayName,
			Description: group.Description,
		}
		for _, bucket := range group.Buckets {
			if bucket.Disabled {
				continue
			}
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

		qg.Items = append(qg.Items, quota.QuotaItem{
			Name:        bucket.BucketID,
			Label:       bucket.DisplayName,
			Description: bucket.ResetDescription,
			Used:        used,
			Limit:       100,
			Unit:        "percentage",
			ResetTime:   bucket.ResetTime,
		})
		}
		groups = append(groups, qg)
	}
	res.Groups = groups

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
				res.FiveHour = quota.Quota{Used: used, Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
			} else if isWeekly {
				res.Monthly = quota.Quota{Used: used, Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
			}
		} else if strings.Contains(groupName, "claude") || strings.Contains(groupName, "gpt") {
			if isWeekly {
				res.Weekly = quota.Quota{Used: used, Limit: 100, Unit: "percentage", ResetTime: bucket.ResetTime}
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
				Used:      used,
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
