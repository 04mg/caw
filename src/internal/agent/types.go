package agent

type Info struct {
	ID    string     `json:"id"`
	Label string     `json:"label"`
	Cmd   []string   `json:"cmd"`
	Env   [][]string `json:"env,omitempty"` // [key, value] pairs to inject into the PTY environment
}

type SetupWorkspaceRequest struct {
	ProjectPath     string   `json:"projectPath"`
	AgentID         string   `json:"agentId"`
	EnableWorktrees bool     `json:"enableWorktrees"`
	CopyToWorktrees []string `json:"copyToWorktrees,omitempty"`
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

// ExplainStatus is the diagnostic view of a single tracked agent session for
// the "agent explain" endpoint. It mirrors AgentStatus but adds the evidence
// that produced the current status so misclassifications (wrong session bound
// to a leaf, false idle, stale state) can be investigated from concrete data
// rather than log archaeology.
type ExplainStatus struct {
	SessionID   string `json:"sessionId"`
	AgentID     string `json:"agentId"`
	Cwd         string `json:"cwd,omitempty"`
	Status      string `json:"status"`
	Tool        string `json:"tool,omitempty"`
	Details     string `json:"details,omitempty"`
	Title       string `json:"title,omitempty"`
	Sequence    int64  `json:"sequence"`
	// ExternalSessionID is the agent's own internal session id/path that the
	// watcher is bound to (OpenCode session row, Codex rollout UUID, ...).
	// Empty means the watcher has not bound to a native session yet.
	ExternalSessionID string `json:"externalSessionId,omitempty"`
	// Source records the status authority behind this session: "watcher" for
	// the per-agent transcript/DB watchers, "watchdog" for the stale-state
	// revert, or "" when no source has produced a status yet.
	Source string `json:"source,omitempty"`
	// Timestamp is the wall-clock time of the last status update.
	Timestamp string `json:"timestamp"`
	// LastPtyActivity is the most recent time the leaf's PTY produced output,
	// or "" if none has been recorded. Used to judge whether a "working" or
	// "unknown" state has supporting evidence.
	LastPtyActivity string `json:"lastPtyActivity,omitempty"`
	// Focused reports whether the leaf's PTY currently has the user's focus.
	Focused bool `json:"focused"`
	// LastPtyInterrupt is the most recent time the user sent the interrupt key
	// sequence into the leaf's PTY, or "" if none.
	LastPtyInterrupt string `json:"lastPtyInterrupt,omitempty"`
}