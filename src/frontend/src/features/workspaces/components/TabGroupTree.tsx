import { type ReactNode } from 'react'
import { type Workspace, type TabGroupsNode } from '../types'
import { TabGroupView } from './TabGroupView'
import { TabSplitGroup } from './TabSplitGroup'

interface TabGroupTreeProps {
  workspace: Workspace
  node: TabGroupsNode
  activeGroupId: string
  topRightGroupId: string
  draggedTabId: string | null
  activePaneId: string
  gitStatuses: Record<string, string>
  folderSidebarCollapsed?: boolean

  onSetActiveGroup: (groupId: string) => void
  onSwitchTab: (tabId: string, groupId: string) => void
  onCloseTab: (tabId: string) => void
  onReorderTabs: (tabId: string, groupId: string, toIndex: number) => void
  onAddTab: (cmd?: string[], agentId?: string, label?: string, groupId?: string, env?: [string, string][]) => void
  onSplitGroup: (
    targetGroupId: string,
    draggedTabId: string,
    orientation: 'horizontal' | 'vertical',
    position: 'left' | 'right' | 'top' | 'bottom',
  ) => void
  onMoveTabToGroup: (tabId: string, groupId: string) => void
  onDragStart: (tabId: string) => void
  onToggleWorktrees?: () => void
  onFocusPane: (paneId: string) => void
  onSplitVert: (paneId: string) => void
  onSplitHoriz: (paneId: string) => void
  onClosePane: (paneId: string) => void
  onSizesChange: (splitId: string, sizes: number[]) => void
  onGroupSizesChange: (splitId: string, sizes: number[]) => void
  onOpenDiff?: (filePath?: string) => void
  onOpenFile?: (filePath: string) => void
  onOpenSettings?: () => void
  onToggleFolderSidebar?: () => void
}

export function TabGroupTree({
  workspace,
  node,
  activeGroupId,
  topRightGroupId,
  draggedTabId,
  activePaneId,
  gitStatuses,
  folderSidebarCollapsed = true,
  onSetActiveGroup,
  onSwitchTab,
  onCloseTab,
  onReorderTabs,
  onAddTab,
  onSplitGroup,
  onMoveTabToGroup,
  onDragStart,
  onToggleWorktrees,
  onFocusPane,
  onSplitVert,
  onSplitHoriz,
  onClosePane,
  onSizesChange,
  onGroupSizesChange,
  onOpenDiff,
  onOpenFile,
  onOpenSettings,
  onToggleFolderSidebar,
}: TabGroupTreeProps): ReactNode {
  if (node.type === 'group') {
    return (
      <TabGroupView
        workspace={workspace}
        group={node}
        isActive={node.id === activeGroupId}
        isTopRightGroup={node.id === topRightGroupId}
        draggedTabId={draggedTabId}
        activePaneId={activePaneId}
        gitStatuses={gitStatuses}
        folderSidebarCollapsed={folderSidebarCollapsed}
        onSetActiveGroup={onSetActiveGroup}
        onSwitchTab={onSwitchTab}
        onCloseTab={onCloseTab}
        onReorderTabs={onReorderTabs}
        onAddTab={onAddTab}
        onSplitGroup={onSplitGroup}
        onMoveTabToGroup={onMoveTabToGroup}
        onDragStart={onDragStart}
        onToggleWorktrees={onToggleWorktrees}
        onFocusPane={onFocusPane}
        onSplitVert={onSplitVert}
        onSplitHoriz={onSplitHoriz}
        onClosePane={onClosePane}
        onSizesChange={onSizesChange}
        onOpenDiff={onOpenDiff}
        onOpenFile={onOpenFile}
        onOpenSettings={onOpenSettings}
        onToggleFolderSidebar={onToggleFolderSidebar}
      />
    )
  }

  const sizes =
    node.sizes && node.sizes.length === node.children.length
      ? node.sizes
      : node.children.map(() => 100 / node.children.length)

  const orientation = node.orientation

  return (
    <TabSplitGroup
      splitId={node.id}
      orientation={orientation}
      sizes={sizes}
      onSizesChange={onGroupSizesChange}
    >
      {node.children.map((child) => {
        const childId = child.id
        return (
          <TabGroupTree
            key={childId}
            workspace={workspace}
            node={child}
            activeGroupId={activeGroupId}
            topRightGroupId={topRightGroupId}
            draggedTabId={draggedTabId}
            activePaneId={activePaneId}
            gitStatuses={gitStatuses}
            folderSidebarCollapsed={folderSidebarCollapsed}
            onSetActiveGroup={onSetActiveGroup}
            onSwitchTab={onSwitchTab}
            onCloseTab={onCloseTab}
            onReorderTabs={onReorderTabs}
            onAddTab={onAddTab}
            onSplitGroup={onSplitGroup}
            onMoveTabToGroup={onMoveTabToGroup}
            onDragStart={onDragStart}
            onToggleWorktrees={onToggleWorktrees}
            onFocusPane={onFocusPane}
            onSplitVert={onSplitVert}
            onSplitHoriz={onSplitHoriz}
            onClosePane={onClosePane}
            onSizesChange={onSizesChange}
            onGroupSizesChange={onGroupSizesChange}
            onOpenDiff={onOpenDiff}
            onOpenFile={onOpenFile}
            onOpenSettings={onOpenSettings}
            onToggleFolderSidebar={onToggleFolderSidebar}
          />
        )
      })}
    </TabSplitGroup>
  )
}
