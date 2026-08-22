import { type LayoutNode } from '@/features/shared/utils/layout'

export type TabGroupsNode =
  | {
      type: 'group'
      id: string
      tabs: string[]
      activeTabIndex: number
    }
  | {
      type: 'split'
      id: string
      orientation: 'horizontal' | 'vertical'
      children: TabGroupsNode[]
      sizes: number[]
    }

export interface Workspace {
  id: string
  path: string
  name: string
  emoji?: string
  layouts: TabLayout[]
  activeTabIndex: number
  activePaneId: string
  enableWorktrees?: boolean
  tabGroupsJson?: string
  tabGroups?: TabGroupsNode
  activeGroupId?: string
  copyToWorktrees?: string[]
  folderId?: string
}

// A sidebar folder grouping workspaces. Folders are never nested: a folder
// only ever contains workspaces, and it lives at the root level of the
// sidebar alongside loose workspaces.
export interface WorkspaceFolder {
  id: string
  name: string
  emoji?: string
}

export interface TabLayout {
  id: string
  name: string
  layout: LayoutNode
}

export interface BackendState {
  workspaces: Workspace[]
  activeWorkspaceId: string | null
  workspaceFolders?: WorkspaceFolder[]
  // Root-level display order: ids of folders and loose workspaces,
  // interleaved. Workspaces inside a folder are ordered by their position in
  // the flat workspaces array.
  sidebarOrder?: string[]
}
