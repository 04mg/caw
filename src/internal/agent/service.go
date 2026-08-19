package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) ListAgents() []Info {
	agentsList := []Info{
		{ID: "claude", Label: "Claude Code", Cmd: []string{"claude", "--dangerously-skip-permissions"}, Env: [][]string{{"IS_SANDBOX", "1"}}},
		{ID: "codex", Label: "Codex CLI", Cmd: []string{"codex", "--sandbox", "workspace-write", "--ask-for-approval", "never"}},
		{ID: "copilot", Label: "GitHub Copilot", Cmd: []string{"copilot", "--allow-all-tools", "--allow-all-paths"}},
		{ID: "agy", Label: "Antigravity", Cmd: []string{"agy", "--dangerously-skip-permissions"}},
		{ID: "opencode", Label: "OpenCode", Cmd: []string{"opencode", "--auto"}},
		{ID: "pi", Label: "Pi", Cmd: []string{"pi"}},
		{ID: "omp", Label: "Oh My Pi", Cmd: []string{"omp"}},
		{ID: "hermes", Label: "Hermes", Cmd: []string{"hermes", "--yolo"}, Env: [][]string{{"HERMES_TUI_BACKGROUND", "#000000"}}},
		{ID: "commandcode", Label: "Command Code", Cmd: []string{"command-code", "--yolo"}},
	}
	available := []Info{}
	for _, a := range agentsList {
		if _, err := exec.LookPath(a.Cmd[0]); err == nil {
			available = append(available, a)
		}
	}
	return available
}

// ListDesktopApps returns the hardcoded registry of graphical applications
// Caw can launch inside an xpra desktop session, filtered by availability:
// both the app binary and xpra must be on PATH. The set is intentionally
// small and generic (a browser, the ZCode and DeepSeek Harness editors,
// and the Unity game editor); user-defined apps can be added via the
// DesktopApps preference (mirrors AgentCmds).
func (s *Service) ListDesktopApps() []DesktopApp {
	// Try common browsers in order of preference.
	browserCandidates := []struct {
		Bin string
		Arg []string
	}{
		{"firefox-esr", []string{"--new-window"}},
		{"firefox", []string{"--new-window"}},
		{"chromium", []string{}},
		{"chromium-browser", []string{}},
		{"xterm", []string{}},
	}
	var browserCmd []string
	for _, c := range browserCandidates {
		if _, err := exec.LookPath(c.Bin); err == nil {
			browserCmd = append([]string{c.Bin}, c.Arg...)
			break
		}
	}
	apps := []DesktopApp{}
	if browserCmd != nil {
		apps = append(apps, DesktopApp{ID: "browser", Label: "Browser", Cmd: browserCmd})
	}
	// Optional apps — only shown when installed.
	for _, extra := range []DesktopApp{
		{ID: "xterm", Label: "XTerm", Cmd: []string{"xterm"}},
		{ID: "zcode", Label: "ZCode", Cmd: []string{"zcode"}},
		{ID: "deepseek-harness", Label: "DeepSeek Harness", Cmd: []string{"deepseek-harness"}},
		{ID: "unity", Label: "Unity", Cmd: []string{"unity-editor"}},
	} {
		if _, err := exec.LookPath(extra.Cmd[0]); err == nil {
			apps = append(apps, extra)
		}
	}
	available := []DesktopApp{}
	for _, a := range apps {
		if _, err := exec.LookPath(a.Cmd[0]); err == nil {
			available = append(available, a)
		}
	}
	// Only show desktop apps if xpra itself is installed; otherwise the
	// menu entry would launch a desktop session that can never start.
	if len(available) > 0 {
		if _, err := exec.LookPath("xpra"); err != nil {
			return []DesktopApp{}
		}
	}
	return available
}

func (s *Service) SetupWorkspace(req SetupWorkspaceRequest) (*SetupWorkspaceResponse, error) {
	if req.ProjectPath == "" {
		return nil, ErrProjectPathRequired
	}

	if !req.EnableWorktrees {
		return &SetupWorkspaceResponse{
			IsGit:        false,
			WorktreePath: req.ProjectPath,
		}, nil
	}

	cmdCheck := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmdCheck.Dir = req.ProjectPath
	if err := cmdCheck.Run(); err != nil {
		return &SetupWorkspaceResponse{
			IsGit:        false,
			WorktreePath: req.ProjectPath,
		}, nil
	}

	cmdBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmdBranch.Dir = req.ProjectPath
	branchOut, err := cmdBranch.Output()
	baseBranch := "main"
	if err == nil {
		baseBranch = strings.TrimSpace(string(branchOut))
	}

	uid := uuid.New().String()
	shortID := uid[:8]
	branchName := fmt.Sprintf("caw/agent-%s", shortID)

	home, err := os.UserHomeDir()
	if err != nil {
		home, _ = os.UserConfigDir()
	}
	projectName := filepath.Base(req.ProjectPath)
	worktreePath := filepath.Join(home, ".caw", "worktrees", projectName, shortID)

	_ = os.MkdirAll(filepath.Dir(worktreePath), 0755)

	cmdAdd := exec.Command("git", "worktree", "add", "-b", branchName, worktreePath, baseBranch)
	cmdAdd.Dir = req.ProjectPath
	var stderr bytes.Buffer
	cmdAdd.Stderr = &stderr
	if err := cmdAdd.Run(); err != nil {
		return nil, fmt.Errorf("failed to create git worktree: %v (stderr: %s)", err, stderr.String())
	}

	for _, p := range req.CopyToWorktrees {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		rel, err := filepath.Rel(req.ProjectPath, p)
		if err != nil {
			continue
		}
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		src := filepath.Join(req.ProjectPath, rel)
		dst := filepath.Join(worktreePath, rel)
		info, err := os.Stat(src)
		if err != nil {
			continue
		}
		if info.IsDir() {
			_ = copyDirToWorktree(src, dst)
		} else {
			_ = copyFileToWorktree(src, dst)
		}
	}

	return &SetupWorkspaceResponse{
		IsGit:        true,
		WorktreePath: worktreePath,
		BranchName:   branchName,
		BaseBranch:   baseBranch,
	}, nil
}

func copyFileToWorktree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func copyDirToWorktree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFileToWorktree(path, target)
	})
}

func (s *Service) CheckChanges(worktreePath, branchName, baseBranch string) (*CheckChangesResponse, error) {
	if worktreePath == "" {
		return nil, ErrWorktreePathRequired
	}

	cmdStatus := exec.Command("git", "status", "--porcelain", "-u")
	cmdStatus.Dir = worktreePath
	statusOut, err := cmdStatus.Output()
	hasUncommitted := err == nil && len(strings.TrimSpace(string(statusOut))) > 0

	hasUnmergedCommits := false
	if baseBranch != "" && branchName != "" {
		cmdLog := exec.Command("git", "log", fmt.Sprintf("%s..%s", baseBranch, branchName), "--oneline")
		cmdLog.Dir = worktreePath
		logOut, err := cmdLog.Output()
		hasUnmergedCommits = err == nil && len(strings.TrimSpace(string(logOut))) > 0
	}

	return &CheckChangesResponse{
		HasUncommitted:     hasUncommitted,
		HasUnmergedCommits: hasUnmergedCommits,
	}, nil
}