package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Service struct {
	statusCacheMu   sync.Mutex
	statusCache     map[string]string
	statusCacheRepo string
	statusCacheTime time.Time
	statusCacheTTL  time.Duration
}

func NewService() *Service {
	return &Service{statusCacheTTL: 2 * time.Second}
}

// InvalidateStatusCache clears the cached git status so the next Status call
// re-runs git. Used when file-change events signal that the working tree may
// have changed since the cached snapshot was taken.
func (s *Service) InvalidateStatusCache() {
	s.statusCacheMu.Lock()
	s.statusCache = nil
	s.statusCacheRepo = ""
	s.statusCacheTime = time.Time{}
	s.statusCacheMu.Unlock()
}

// StatusAndIgnored returns both the porcelain status map and the ignored map
// for the repo at path, recomputing them in a single call. The status portion
// is cached per repo with a short TTL; callers that need a guaranteed-fresh
// snapshot should call InvalidateStatusCache first.
func (s *Service) StatusAndIgnored(path string) (map[string]string, map[string]bool, error) {
	statuses, err := s.Status(path)
	if err != nil {
		return nil, nil, err
	}
	ignored, err := s.Ignored(path)
	if err != nil {
		return statuses, nil, err
	}
	return statuses, ignored, nil
}

func (s *Service) Status(path string) (map[string]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	s.statusCacheMu.Lock()
	if abs == s.statusCacheRepo && time.Since(s.statusCacheTime) < s.statusCacheTTL && s.statusCache != nil {
		result := s.statusCache
		s.statusCacheMu.Unlock()
		return result, nil
	}
	s.statusCacheMu.Unlock()

	cmd := exec.Command("git", "status", "--porcelain", "-u")
	cmd.Dir = abs
	output, err := cmd.Output()
	if err != nil {
		return map[string]string{}, nil
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

	s.statusCacheMu.Lock()
	s.statusCache = statuses
	s.statusCacheRepo = abs
	s.statusCacheTime = time.Now()
	s.statusCacheMu.Unlock()

	return statuses, nil
}

func (s *Service) Ignored(path string) (map[string]bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "status", "--porcelain", "--ignored", "-u")
	cmd.Dir = abs
	output, err := cmd.Output()
	if err != nil {
		return map[string]bool{}, nil
	}
	ignored := make(map[string]bool, 64)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		statusXY := line[:2]
		if statusXY != "!!" {
			continue
		}
		filePath := line[3:]
		filePath = strings.Trim(filePath, "\"")
		absFilePath := filepath.Join(abs, filePath)
		ignored[absFilePath] = true
	}
	return ignored, nil
}

func (s *Service) Diff(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = abs
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (s *Service) Original(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
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
		return "", ErrNotGitRepo
	}

	relPath, err := filepath.Rel(gitRoot, abs)
	if err != nil {
		return "", err
	}
	relPath = filepath.ToSlash(relPath)

	cmd := exec.Command("git", "show", "HEAD:"+relPath)
	cmd.Dir = gitRoot
	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return string(output), nil
}