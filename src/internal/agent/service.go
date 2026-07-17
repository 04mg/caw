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
	}
	available := []Info{}
	for _, a := range agentsList {
		if _, err := exec.LookPath(a.Cmd[0]); err == nil {
			available = append(available, a)
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

	return &SetupWorkspaceResponse{
		IsGit:        true,
		WorktreePath: worktreePath,
		BranchName:   branchName,
		BaseBranch:   baseBranch,
	}, nil
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