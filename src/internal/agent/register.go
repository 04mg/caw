package agent

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/04mg/caw/internal/httputil"
)

type Info struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Cmd   []string `json:"cmd"`
}

type SetupWorkspaceRequest struct {
	ProjectPath     string `json:"projectPath"`
	AgentID         string `json:"agentId"`
	EnableWorktrees bool   `json:"enableWorktrees"`
}

type SetupWorkspaceResponse struct {
	IsGit        bool   `json:"isGit"`
	WorktreePath string `json:"worktreePath"`
	BranchName   string `json:"branchName"`
	BaseBranch   string `json:"baseBranch"`
}

type CheckChangesRequest struct {
	WorktreePath string `json:"worktreePath"`
	BranchName   string `json:"branchName"`
	BaseBranch   string `json:"baseBranch"`
}

type CheckChangesResponse struct {
	HasUncommitted     bool `json:"hasUncommitted"`
	HasUnmergedCommits bool `json:"hasUnmergedCommits"`
}

func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		agentsList := []Info{
			{ID: "claude", Label: "Claude Code", Cmd: []string{"claude", "--dangerously-skip-permissions"}},
			{ID: "codex", Label: "Codex CLI", Cmd: []string{"codex", "--sandbox", "workspace-write", "--ask-for-approval", "never"}},
			{ID: "copilot", Label: "GitHub Copilot", Cmd: []string{"copilot", "--allow-all-tools", "--allow-all-paths"}},
			{ID: "agy", Label: "Antigravity", Cmd: []string{"agy", "--dangerously-skip-permissions"}},
			{ID: "opencode", Label: "OpenCode", Cmd: []string{"opencode", "--dangerously-skip-permissions"}},
			{ID: "pi", Label: "Pi", Cmd: []string{"pi"}},
		}
		available := []Info{}
		for _, a := range agentsList {
			if _, err := exec.LookPath(a.Cmd[0]); err == nil {
				available = append(available, a)
			}
		}
		httputil.WriteJSON(w, available)
	})

	mux.HandleFunc("POST /api/agents", func(w http.ResponseWriter, r *http.Request) {
		var req SetupWorkspaceRequest
		if err := httputil.ReadJSON(r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.ProjectPath == "" {
			http.Error(w, "projectPath required", http.StatusBadRequest)
			return
		}

		if !req.EnableWorktrees {
			httputil.WriteJSON(w, SetupWorkspaceResponse{
				IsGit:        false,
				WorktreePath: req.ProjectPath,
			})
			return
		}

		// Check if projectPath is a git repository
		cmdCheck := exec.Command("git", "rev-parse", "--is-inside-work-tree")
		cmdCheck.Dir = req.ProjectPath
		if err := cmdCheck.Run(); err != nil {
			// Not a git repo, return fallback path
			httputil.WriteJSON(w, SetupWorkspaceResponse{
				IsGit:        false,
				WorktreePath: req.ProjectPath,
			})
			return
		}

		// It is a git repo, find current active branch
		cmdBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmdBranch.Dir = req.ProjectPath
		branchOut, err := cmdBranch.Output()
		baseBranch := "main"
		if err == nil {
			baseBranch = strings.TrimSpace(string(branchOut))
		}

		// Generate unique agent run ID and branch name
		uid := uuid.New().String()
		shortID := uid[:8]
		branchName := fmt.Sprintf("caw/agent-%s", shortID)

		home, err := os.UserHomeDir()
		if err != nil {
			home, _ = os.UserConfigDir()
		}
		projectName := filepath.Base(req.ProjectPath)
		worktreePath := filepath.Join(home, ".caw", "worktrees", projectName, shortID)

		// Create worktree parent directory
		_ = os.MkdirAll(filepath.Dir(worktreePath), 0755)

		// Run: git worktree add -b <branchName> <worktreePath> <baseBranch>
		cmdAdd := exec.Command("git", "worktree", "add", "-b", branchName, worktreePath, baseBranch)
		cmdAdd.Dir = req.ProjectPath
		var stderr bytes.Buffer
		cmdAdd.Stderr = &stderr
		if err := cmdAdd.Run(); err != nil {
			http.Error(w, fmt.Sprintf("failed to create git worktree: %v (stderr: %s)", err, stderr.String()), http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, SetupWorkspaceResponse{
			IsGit:        true,
			WorktreePath: worktreePath,
			BranchName:   branchName,
			BaseBranch:   baseBranch,
		})
	})

	mux.HandleFunc("GET /api/agents/changes", func(w http.ResponseWriter, r *http.Request) {
		worktreePath := r.URL.Query().Get("worktreePath")
		branchName := r.URL.Query().Get("branchName")
		baseBranch := r.URL.Query().Get("baseBranch")

		if worktreePath == "" {
			http.Error(w, "worktreePath required", http.StatusBadRequest)
			return
		}

		// Check for uncommitted changes
		cmdStatus := exec.Command("git", "status", "--porcelain", "-u")
		cmdStatus.Dir = worktreePath
		statusOut, err := cmdStatus.Output()
		hasUncommitted := err == nil && len(strings.TrimSpace(string(statusOut))) > 0

		// Check for unmerged commits
		hasUnmergedCommits := false
		if baseBranch != "" && branchName != "" {
			cmdLog := exec.Command("git", "log", fmt.Sprintf("%s..%s", baseBranch, branchName), "--oneline")
			cmdLog.Dir = worktreePath
			logOut, err := cmdLog.Output()
			hasUnmergedCommits = err == nil && len(strings.TrimSpace(string(logOut))) > 0
		}

		httputil.WriteJSON(w, CheckChangesResponse{
			HasUncommitted:     hasUncommitted,
			HasUnmergedCommits: hasUnmergedCommits,
		})
	})
}
