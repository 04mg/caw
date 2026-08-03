import { type ReactNode, useState } from 'react'
import { Settings, Folder, PanelRight } from 'lucide-react'
import { Button } from '@/components/button'
import { DraggableTabBar } from './DraggableTabBar'
import { TerminalGrid } from '@/features/terminal/components/TerminalGrid'
import { findAgentId, countLeaves, collectLeafIds, getLeaf, type LayoutNode } from '@/features/shared/utils/layout'
import { type Workspace, type TabGroupsNode } from '../types'
import { useAgentStatuses } from '@/features/agents/hooks/useAgentStatuses'
import { type AgentStatus } from '@/features/agents/types'

interface TabGroupViewProps {
  workspace: Workspace
  group: Extract<TabGroupsNode, { type: 'group' }>
  isActive: boolean
  isTopRightGroup: boolean
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
  onOpenDiff?: (filePath?: string) => void
  onOpenFile?: (filePath: string) => void
  onOpenSettings?: () => void
  onToggleFolderSidebar?: () => void
}

export function TabGroupView({
  workspace,
  group,
  isActive,
  isTopRightGroup,
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
  onOpenDiff,
  onOpenFile,
  onOpenSettings,
  onToggleFolderSidebar,
}: TabGroupViewProps): ReactNode {
  const [activeZone, setActiveZone] = useState<'left' | 'right' | 'top' | 'bottom' | 'center' | null>(null)
  const statuses = useAgentStatuses()

  const activeTabId = group.tabs[group.activeTabIndex]
  const activeTab = workspace.layouts.find((l) => l.id === activeTabId) ?? null
  const leafCount = activeTab ? countLeaves(activeTab.layout) : 0

  function resolveTabAgentStatus(layout: LayoutNode): AgentStatus | undefined {
    const leafIds = collectLeafIds(layout)
    for (const id of leafIds) {
      const s = statuses[id]
      if (s) return s
    }
    for (const id of leafIds) {
      const leaf = getLeaf(layout, id)
      if (!leaf || !leaf.cwd) continue
      for (const s of Object.values(statuses)) {
        if (s.cwd === leaf.cwd && (!leaf.agentId || s.agentId === leaf.agentId)) {
          return s
        }
      }
    }
    return undefined
  }

  const groupTabs = group.tabs.map((tabId) => {
    const tab = workspace.layouts.find((l) => l.id === tabId)
    return {
      id: tabId,
      name: tab?.name || 'Terminal',
      agentId: tab ? findAgentId(tab.layout) : undefined,
      filePath: tab && tab.layout.type === 'leaf' ? tab.layout.filePath : undefined,
      isDiff: tab && tab.layout.type === 'leaf' ? tab.layout.isDiff : undefined,
      agentStatus: tab ? resolveTabAgentStatus(tab.layout) : undefined,
    }
  })

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const x = (e.clientX - rect.left) / rect.width
    const y = (e.clientY - rect.top) / rect.height
    
    if (x < 0.25) {
      setActiveZone('left')
    } else if (x > 0.75) {
      setActiveZone('right')
    } else if (y < 0.25) {
      setActiveZone('top')
    } else if (y > 0.75) {
      setActiveZone('bottom')
    } else {
      setActiveZone('center')
    }
  }

  const handlePointerUp = () => {
    if (!draggedTabId || !activeZone) return
    if (activeZone === 'center') {
      onMoveTabToGroup(draggedTabId, group.id)
    } else if (activeZone === 'left') {
      onSplitGroup(group.id, draggedTabId, 'horizontal', 'left')
    } else if (activeZone === 'right') {
      onSplitGroup(group.id, draggedTabId, 'horizontal', 'right')
    } else if (activeZone === 'top') {
      onSplitGroup(group.id, draggedTabId, 'vertical', 'top')
    } else if (activeZone === 'bottom') {
      onSplitGroup(group.id, draggedTabId, 'vertical', 'bottom')
    }
    setActiveZone(null)
  }

  return (
    <div
      className="flex flex-col h-full overflow-hidden bg-background"
      onPointerDown={() => {
        if (!isActive) {
          onSetActiveGroup(group.id)
        }
      }}
    >
      {/* Group Tab Bar */}
      <div className="flex items-center border-b border-border bg-secondary/15 h-[33px] shrink-0 select-none">
        <div className="flex flex-1 overflow-x-auto h-full scrollbar-none">
          <DraggableTabBar
            tabs={groupTabs}
            activeIndex={group.activeTabIndex}
            onSwitch={(idx) => onSwitchTab(group.tabs[idx], group.id)}
            onClose={(idx) => onCloseTab(group.tabs[idx])}
            onReorder={(fromIdx, toIdx) => onReorderTabs(group.tabs[fromIdx], group.id, toIdx)}
            onAdd={(cmd, agentId, label, env) => onAddTab(cmd, agentId, label, group.id, env)}
            enableWorktrees={workspace.enableWorktrees}
            onToggleWorktrees={onToggleWorktrees}
            onDragStart={onDragStart}
          />
        </div>
        {isTopRightGroup && (
          <div className="flex items-center shrink-0 h-full border-l border-border bg-background">
            {/* Settings Button */}
            <div className={`flex items-center justify-center h-full select-none ${folderSidebarCollapsed ? 'border-r border-border' : ''}`} style={{ width: 36 }}>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground animate-none"
                onClick={onOpenSettings}
                title="Settings"
              >
                <Settings className="h-3.5 w-3.5" />
              </Button>
            </div>

            {/* Folder Button */}
            {folderSidebarCollapsed && (
              <div className="group flex items-center justify-center h-full select-none" style={{ width: 36 }}>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground animate-none"
                  onClick={onToggleFolderSidebar}
                  title="Workspace Files"
                >
                  <Folder className="h-3.5 w-3.5 group-hover:hidden" />
                  <PanelRight className="h-3.5 w-3.5 hidden group-hover:block" />
                </Button>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Group Content Area */}
      <div className="flex-1 min-h-0 relative flex flex-col">
        <div className="flex-1 min-h-0 relative">
          {activeTab && leafCount > 0 ? (
            <TerminalGrid
              key={activeTab.id}
              node={activeTab.layout}
              activePaneId={activePaneId}
              onFocus={onFocusPane}
              onSplitVert={onSplitVert}
              onSplitHoriz={onSplitHoriz}
              onClose={onClosePane}
              cwd={workspace.path}
              onSizesChange={onSizesChange}
              gitStatuses={gitStatuses}
              onOpenDiff={onOpenDiff}
              onOpenFile={onOpenFile}
            />
          ) : workspace.layouts.length > 0 ? (
            <div className="flex flex-col h-full w-full items-center justify-center text-center gap-2 select-none text-muted-foreground text-xs p-6">
              <span>No active terminal in this group</span>
            </div>
          ) : null}

          {/* VS Code style drop overlay (no text, dynamic highlights) */}
          {draggedTabId && (
            <div
              onPointerMove={handlePointerMove}
              onPointerUp={handlePointerUp}
              onPointerLeave={() => setActiveZone(null)}
              className="absolute inset-0 z-30 bg-black/10 backdrop-blur-[0.5px] cursor-grabbing"
            >
              {activeZone === 'left' && (
                <div className="absolute top-0 bottom-0 left-0 w-1/2 bg-primary/15 border-r-2 border-primary transition-all duration-75 pointer-events-none" />
              )}
              {activeZone === 'right' && (
                <div className="absolute top-0 bottom-0 right-0 w-1/2 bg-primary/15 border-l-2 border-primary transition-all duration-75 pointer-events-none" />
              )}
              {activeZone === 'top' && (
                <div className="absolute left-0 right-0 top-0 h-1/2 bg-primary/15 border-b-2 border-primary transition-all duration-75 pointer-events-none" />
              )}
              {activeZone === 'bottom' && (
                <div className="absolute left-0 right-0 bottom-0 h-1/2 bg-primary/15 border-t-2 border-primary transition-all duration-75 pointer-events-none" />
              )}
              {activeZone === 'center' && (
                <div className="absolute inset-0 bg-primary/10 border-2 border-primary transition-all duration-75 pointer-events-none" />
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
