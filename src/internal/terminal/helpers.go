package terminal

import (
	"os"
	"path/filepath"
	"strings"
)

func getMainRepoPath(worktreePath string) string {
	gitFile := filepath.Join(worktreePath, ".git")
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	content := string(data)
	if !strings.HasPrefix(content, "gitdir:") {
		return ""
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))

	idx := strings.Index(gitdir, "/.git/worktrees/")
	if idx == -1 {
		idx = strings.Index(gitdir, "\\.git\\worktrees\\")
	}
	if idx != -1 {
		return filepath.Clean(gitdir[:idx])
	}
	return ""
}