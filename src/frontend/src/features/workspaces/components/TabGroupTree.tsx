import { type ReactNode, Fragment, useRef, useCallback } from 'react'
import { Group, Panel, Separator } from 'react-resizable-panels'
import { type Workspace, type TabGroupsNode } from '../types'
import { TabGroupView } from './TabGroupView'

interface TabSplitGroupProps {
  splitId: string
  orientation: 'horizontal' | 'vertical'
  onSizesChange: (splitId: string, sizes: number[]) => void
  children: ReactNode
  sizes: number[]
}

export function TabSplitGroup({
  splitId,
  orientation,
  onSizesChange,
  children,
}: TabSplitGroupProps): ReactNode {
  const childIdsRef = useRef<string[]>([])
  childIdsRef.current = []

  const collectIds = (kids: ReactNode) => {
    const arr = Array.isArray(kids) ? kids : [kids]
    for (const k of arr) {
      if (k && typeof k === 'object' && 'props' in (k as any)) {
        const p = (k as any).props
        const id = p?.id
        if (typeof id === 'string') childIdsRef.current.push(id)
        if (p?.children) collectIds(p.children)
      }
    }
  }
  collectIds(children)

  const handleLayoutChanged = useCallback(
    (layout: Record<string, number>) => {
      const ids = childIdsRef.current
      if (ids.length === 0) return
      const ordered = ids.map((id) => layout[id]).filter((v) => typeof v === 'number')
      if (ordered.length === ids.length) {
        const total = ordered.reduce((a, b) => a + b, 0) || 1
        const normalized = ordered.map((v) => (v / total) * 100)
        onSizesChange(splitId, normalized)
      }
    },
    [splitId, onSizesChange],
  )

  return (
    <Group
      key={splitId}
      orientation={orientation}
      className="h-full w-full"
      onLayoutChanged={handleLayoutChanged}
    >
      {children}
    </Group>
  )
}

interface TabGroupTreeProps {
  workspace: Workspace
  node: TabGroupsNode
  activeGroupId: string
  draggedTabId: string | null
  activePaneId: string
  gitStatuses: Record<string, string>
  folderSidebarCollapsed?: boolean

  onSetActiveGroup: (groupId: string) => void
  onSwitchTab: (tabId: string, groupId: string) => void
  onCloseTab: (tabId: string) => void
  onReorderTabs: (tabId: string, groupId: string, toIndex: number) => void
  onAddTab: (cmd?: string[], agentId?: string, label?: string, groupId?: string) => void
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
  onOpenSettings?: () => void
  onToggleFolderSidebar?: () => void
}

export function TabGroupTree({
  workspace,
  node,
  activeGroupId,
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
  onOpenSettings,
  onToggleFolderSidebar,
}: TabGroupTreeProps): ReactNode {
  if (node.type === 'group') {
    return (
      <TabGroupView
        workspace={workspace}
        group={node}
        isActive={node.id === activeGroupId}
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
      {node.children.map((child, i) => {
        const childId = child.id
        return (
          <Fragment key={childId}>
            {i > 0 && (
              <Separator
                className={
                  orientation === 'horizontal'
                    ? 'w-[3px] bg-border hover:bg-ring transition-colors cursor-col-resize shrink-0'
                    : 'h-[3px] bg-border hover:bg-ring transition-colors cursor-row-resize shrink-0'
                }
              />
            )}
            <Panel id={childId} defaultSize={`${sizes[i]}%`}>
              <TabGroupTree
                workspace={workspace}
                node={child}
                activeGroupId={activeGroupId}
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
                onOpenSettings={onOpenSettings}
                onToggleFolderSidebar={onToggleFolderSidebar}
              />
            </Panel>
          </Fragment>
        )
      })}
    </TabSplitGroup>
  )
}
