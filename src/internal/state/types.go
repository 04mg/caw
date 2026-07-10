package state

type LayoutNode struct {
	Type        string       `json:"type"`
	ID          string       `json:"id,omitempty"`
	Cwd         string       `json:"cwd,omitempty"`
	Cmd         []string     `json:"cmd,omitempty"`
	AgentID     string       `json:"agentId,omitempty"`
	Orientation string       `json:"orientation,omitempty"`
	Children    []LayoutNode `json:"children,omitempty"`
	Sizes       []float64    `json:"sizes,omitempty"`
	FilePath    string       `json:"filePath,omitempty"`
	IsDiff      bool         `json:"isDiff,omitempty"`
	AgentBranch string       `json:"agentBranch,omitempty"`
	BaseBranch  string       `json:"baseBranch,omitempty"`
}

type TabLayout struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Layout LayoutNode `json:"layout"`
}

type Workspace struct {
	ID              string      `json:"id"`
	Path            string      `json:"path"`
	Name            string      `json:"name"`
	Emoji           string      `json:"emoji,omitempty"`
	Layouts         []TabLayout `json:"layouts"`
	ActiveTabIndex  int         `json:"activeTabIndex"`
	ActivePaneID    string      `json:"activePaneId"`
	EnableWorktrees bool        `json:"enableWorktrees"`
}

type AppState struct {
	Workspaces        []Workspace `json:"workspaces"`
	ActiveWorkspaceID string      `json:"activeWorkspaceId"`
}
