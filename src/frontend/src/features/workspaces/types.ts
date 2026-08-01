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
}

export interface TabLayout {
  id: string
  name: string
  layout: LayoutNode
}

export interface BackendState {
  workspaces: Workspace[]
  activeWorkspaceId: string | null
}
