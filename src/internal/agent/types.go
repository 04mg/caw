package agent

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