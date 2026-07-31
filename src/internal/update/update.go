package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

var apiURL = "https://api.github.com/repos/04mg/caw/releases/latest"
var testExePath string

type CheckResult struct {
	LatestVersion  string `json:"latestVersion"`
	CurrentVersion string `json:"currentVersion"`
	UpdateAvailable bool `json:"updateAvailable"`
}

func fetchLatestRelease() (*githubRelease, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "caw-updater")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no release found (the repository might not have any releases yet)")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("github API rate limit exceeded or access forbidden (status 403)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned status %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to parse release JSON: %w", err)
	}
	return &rel, nil
}

func Check(currentVersion string) (*CheckResult, error) {
	rel, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}
	updateAvailable := currentVersion != "dev" && currentVersion != rel.TagName
	return &CheckResult{
		LatestVersion:   rel.TagName,
		CurrentVersion:  currentVersion,
		UpdateAvailable: updateAvailable,
	}, nil
}

// getAssetName returns the binary release asset name based on the current GOOS and GOARCH.
func getAssetName() (string, error) {
	osName := runtime.GOOS
	archName := runtime.GOARCH

	switch osName {
	case "linux":
		if archName == "amd64" {
			return "caw-linux-amd64", nil
		}
	case "darwin":
		if archName == "amd64" {
			return "caw-darwin-amd64.tar.gz", nil
		}
		if archName == "arm64" {
			return "caw-darwin-arm64.tar.gz", nil
		}
	case "windows":
		if archName == "amd64" {
			return "caw-windows-amd64.exe", nil
		}
	}
	return "", fmt.Errorf("unsupported system: %s/%s", osName, archName)
}

// Run executes the self-update logic.
func Run(currentVersion string) error {
	assetName, err := getAssetName()
	if err != nil {
		return err
	}

	rel, err := fetchLatestRelease()
	if err != nil {
		return err
	}

	log.Printf("Current version: %s, latest version: %s", currentVersion, rel.TagName)
	if currentVersion != "dev" && currentVersion == rel.TagName {
		log.Printf("Caw is already up-to-date!")
		return nil
	}

	var downloadURL string
	for _, asset := range rel.Assets {
		if asset.Name == assetName {
			downloadURL = asset.DownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no release asset found for name %s", assetName)
	}

	exePath := testExePath
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		evalPath, err := filepath.EvalSymlinks(exePath)
		if err == nil {
			exePath = evalPath
		}
	}

	log.Printf("Downloading latest binary from %s...", downloadURL)
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download release binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download release binary, status: %s", resp.Status)
	}

	exeDir := filepath.Dir(exePath)
	tmpFile, err := os.CreateTemp(exeDir, "caw-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file in %s: %w", exeDir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if _, err := os.Stat(tmpPath); err == nil {
			os.Remove(tmpPath)
		}
	}()

	if strings.HasSuffix(assetName, ".tar.gz") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		tr := tar.NewReader(gr)
		var extracted bool
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				gr.Close()
				tmpFile.Close()
				return fmt.Errorf("failed to read tar archive: %w", err)
			}
			if hdr.Typeflag == tar.TypeReg {
				if _, err := io.Copy(tmpFile, tr); err != nil {
					gr.Close()
					tmpFile.Close()
					return fmt.Errorf("failed to extract binary from archive: %w", err)
				}
				extracted = true
				break
			}
		}
		gr.Close()
		if !extracted {
			tmpFile.Close()
			return fmt.Errorf("no binary found in release archive")
		}
	} else {
		if _, err := io.Copy(tmpFile, resp.Body); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write binary to temp file: %w", err)
		}
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	var mode os.FileMode = 0755
	if info, err := os.Stat(exePath); err == nil {
		mode = info.Mode()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("failed to make temp file executable: %w", err)
	}

	backupPath := exePath + ".old"
	os.Remove(backupPath)

	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current executable: %w", err)
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		if rollbackErr := os.Rename(backupPath, exePath); rollbackErr != nil {
			return fmt.Errorf("failed to apply update (%w) and rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("failed to apply update, rolled back: %w", err)
	}

	if err := os.Remove(backupPath); err != nil {
		log.Printf("Successfully updated, but could not remove backup file %s: %v", backupPath, err)
		if runtime.GOOS == "windows" {
			log.Printf("On Windows, you can delete this file after exiting.")
		}
	} else {
		log.Printf("Successfully updated caw to %s!", rel.TagName)
	}

	return nil
}
