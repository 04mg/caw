import { type LayoutNode } from '@/features/shared/utils/layout'

export interface Workspace {
  id: string
  path: string
  name: string
  emoji?: string
  layouts: TabLayout[]
  activeTabIndex: number
  activePaneId: string
  enableWorktrees?: boolean
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
