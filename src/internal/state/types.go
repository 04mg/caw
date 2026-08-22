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
	PetSlug     string       `json:"petSlug,omitempty"`
	// View discriminates how the frontend renders a leaf: "terminal"
	// (default; xterm.js over the PTY WS), "editor" (Monaco diff/file
	// view), or "desktop" (an xpra-forwarded graphical app in an iframe).
	// Existing leaves with a filePath/isDiff normalize to "editor"; all
	// others default to "terminal". "desktop" leaves spawn an xpra
	// server per pane via the desktop package instead of a PTY.
	View string `json:"view,omitempty"`
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
	TabGroupsJSON   string      `json:"tabGroupsJson,omitempty"`
	CopyToWorktrees []string    `json:"copyToWorktrees,omitempty"`
	FolderID        string      `json:"folderId,omitempty"`
}

// Folder is a sidebar grouping for workspaces. Folders are never nested:
// they only contain workspaces and live at the root level of the sidebar.
type Folder struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji,omitempty"`
}

type AppState struct {
	Workspaces        []Workspace `json:"workspaces"`
	ActiveWorkspaceID string      `json:"activeWorkspaceId"`
	WorkspaceFolders  []Folder    `json:"workspaceFolders,omitempty"`
	// SidebarOrder holds the root-level display order: ids of folders and
	// loose workspaces, interleaved. Workspaces inside a folder are ordered
	// by their position in Workspaces.
	SidebarOrder []string `json:"sidebarOrder,omitempty"`
}

// CollectLeafIDs walks every workspace/tab/layout tree in the AppState and
// returns the set of leaf node ids. Used by the orphan-session reconciler
// to know which PTY sessions are still referenced by the persisted layout.
func (as AppState) CollectLeafIDs() map[string]bool {
	out := make(map[string]bool)
	for _, w := range as.Workspaces {
		for _, tl := range w.Layouts {
			collectLeafIDs(tl.Layout, out)
		}
	}
	return out
}

func collectLeafIDs(node LayoutNode, out map[string]bool) {
	if node.Type == "leaf" && node.ID != "" {
		out[node.ID] = true
		return
	}
	for _, child := range node.Children {
		collectLeafIDs(child, out)
	}
}
