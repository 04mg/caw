package git

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/04mg/caw/internal/httputil"
)

func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/git/statuses", handleStatus)
	mux.HandleFunc("GET /api/git/diffs", handleDiff)
	mux.HandleFunc("GET /api/git/originals", handleOriginal)
}

var (
	statusCache     map[string]string
	statusCacheRepo string
	statusCacheTime time.Time
	statusCacheMu   sync.Mutex
	statusCacheTTL  = 2 * time.Second
)

func handleStatus(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	statusCacheMu.Lock()
	if abs == statusCacheRepo && time.Since(statusCacheTime) < statusCacheTTL && statusCache != nil {
		result := statusCache
		statusCacheMu.Unlock()
		httputil.WriteJSON(w, result)
		return
	}
	statusCacheMu.Unlock()

	cmd := exec.Command("git", "status", "--porcelain", "-u")
	cmd.Dir = abs
	output, err := cmd.Output()
	if err != nil {
		httputil.WriteJSON(w, map[string]string{})
		return
	}

	statuses := make(map[string]string, 64)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		statusXY := line[:2]
		filePath := line[3:]
		filePath = strings.Trim(filePath, "\"")
		absFilePath := filepath.Join(abs, filePath)
		statuses[absFilePath] = statusXY
	}

	statusCacheMu.Lock()
	statusCache = statuses
	statusCacheRepo = abs
	statusCacheTime = time.Now()
	statusCacheMu.Unlock()

	httputil.WriteJSON(w, statuses)
}

func handleDiff(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = abs
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(output)
}

func handleOriginal(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dir := filepath.Dir(abs)
	var gitRoot string
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			gitRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if gitRoot == "" {
		http.Error(w, "not a git repository", http.StatusBadRequest)
		return
	}

	relPath, err := filepath.Rel(gitRoot, abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	relPath = filepath.ToSlash(relPath)

	cmd := exec.Command("git", "show", "HEAD:"+relPath)
	cmd.Dir = gitRoot
	output, err := cmd.Output()
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(""))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(output)
}
