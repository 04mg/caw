package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetAssetName(t *testing.T) {
	name, err := getAssetName()
	if err != nil {
		if name != "" {
			t.Errorf("expected empty name on error, got %s", name)
		}
		return
	}

	expected := map[string]bool{
		"caw-linux-amd64":       true,
		"caw-darwin-amd64":      true,
		"caw-darwin-arm64":      true,
		"caw-windows-amd64.exe": true,
	}
	if !expected[name] {
		t.Errorf("unexpected asset name %q for OS %s and Arch %s", name, runtime.GOOS, runtime.GOARCH)
	}
}

func TestRun_UpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rel := githubRelease{
			TagName: "v1.2.3",
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer server.Close()

	origAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = origAPIURL }()

	err := Run("v1.2.3")
	if err != nil {
		t.Fatalf("expected nil error when up-to-date, got: %v", err)
	}
}

func TestRun_UpdateSuccess(t *testing.T) {
	assetName, err := getAssetName()
	if err != nil {
		t.Skipf("Skipping integration update test since OS/Arch %s/%s is unsupported", runtime.GOOS, runtime.GOARCH)
	}

	tempDir, err := os.MkdirTemp("", "caw-update-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockExePath := filepath.Join(tempDir, "caw")
	if runtime.GOOS == "windows" {
		mockExePath += ".exe"
	}

	err = os.WriteFile(mockExePath, []byte("original binary content"), 0755)
	if err != nil {
		t.Fatalf("failed to write mock exe: %v", err)
	}

	mockBinaryContent := "updated binary content"
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mockBinaryContent))
	}))
	defer fileServer.Close()

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rel := githubRelease{
			TagName: "v2.0.0",
			Assets: []struct {
				Name        string `json:"name"`
				DownloadURL string `json:"browser_download_url"`
			}{
				{
					Name:        assetName,
					DownloadURL: fileServer.URL,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer githubServer.Close()

	origAPIURL := apiURL
	apiURL = githubServer.URL
	defer func() { apiURL = origAPIURL }()

	origTestExePath := testExePath
	testExePath = mockExePath
	defer func() { testExePath = origTestExePath }()

	err = Run("v1.0.0")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updatedContent, err := os.ReadFile(mockExePath)
	if err != nil {
		t.Fatalf("failed to read mock executable: %v", err)
	}

	if string(updatedContent) != mockBinaryContent {
		t.Errorf("expected updated content %q, got %q", mockBinaryContent, string(updatedContent))
	}
}
