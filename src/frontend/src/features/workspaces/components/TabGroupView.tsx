import { type ReactNode } from 'react'
import { Settings, Folder, PanelRight } from 'lucide-react'
import { Button } from '@/components/button'
import { DraggableTabBar } from './DraggableTabBar'
import { TerminalGrid } from '@/features/terminal/components/TerminalGrid'
import { findAgentId, countLeaves } from '@/features/shared/utils/layout'
import { type Workspace, type TabGroupsNode } from '../types'

interface TabGroupViewProps {
  workspace: Workspace
  group: Extract<TabGroupsNode, { type: 'group' }>
  isActive: boolean
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
  onOpenDiff?: (filePath?: string) => void
  onOpenSettings?: () => void
  onToggleFolderSidebar?: () => void
}

export function TabGroupView({
  workspace,
  group,
  isActive,
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
  onOpenSettings,
  onToggleFolderSidebar,
}: TabGroupViewProps): ReactNode {
  const activeTabId = group.tabs[group.activeTabIndex]
  const activeTab = workspace.layouts.find((l) => l.id === activeTabId) ?? null
  const leafCount = activeTab ? countLeaves(activeTab.layout) : 0

  const groupTabs = group.tabs.map((tabId) => {
    const tab = workspace.layouts.find((l) => l.id === tabId)
    return {
      id: tabId,
      name: tab?.name || 'Terminal',
      agentId: tab ? findAgentId(tab.layout) : undefined,
      filePath: tab && tab.layout.type === 'leaf' ? tab.layout.filePath : undefined,
      isDiff: tab && tab.layout.type === 'leaf' ? tab.layout.isDiff : undefined,
    }
  })

  return (
    <div
      className={`flex flex-col h-full overflow-hidden transition-all bg-background border border-border/40 ${
        isActive ? 'ring-1 ring-inset ring-primary/60 border-primary/50' : ''
      }`}
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
            onAdd={(cmd, agentId, label) => onAddTab(cmd, agentId, label, group.id)}
            enableWorktrees={workspace.enableWorktrees}
            onToggleWorktrees={onToggleWorktrees}
            onDragStart={onDragStart}
          />
        </div>
        {isActive && (
          <div className="flex items-center shrink-0 h-full border-l border-border bg-background">
            {/* Settings Button */}
            <div className="flex items-center justify-center border-r border-border h-full select-none" style={{ width: 36 }}>
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
            leafCount={leafCount}
            cwd={workspace.path}
            onSizesChange={onSizesChange}
            gitStatuses={gitStatuses}
            onOpenDiff={onOpenDiff}
          />
        ) : workspace.layouts.length > 0 ? (
          <div className="flex flex-col h-full w-full items-center justify-center text-center gap-2 select-none text-muted-foreground text-xs p-6">
            <span>No active terminal in this group</span>
          </div>
        ) : null}

        {/* 5-zone drop overlay overlay */}
        {draggedTabId && (
          <div className="absolute inset-0 z-30 grid grid-cols-3 grid-rows-3 p-4 gap-2 bg-black/45 backdrop-blur-[1px]">
            {/* Top Drop Zone */}
            <div
              onPointerUp={(e) => {
                e.stopPropagation()
                onSplitGroup(group.id, draggedTabId, 'vertical', 'top')
              }}
              className="col-start-2 row-start-1 rounded border border-dashed border-primary bg-primary/10 hover:bg-primary/30 hover:border-primary transition-all flex items-center justify-center text-xs font-semibold text-primary cursor-pointer select-none text-center"
            >
              Split Top
            </div>

            {/* Left Drop Zone */}
            <div
              onPointerUp={(e) => {
                e.stopPropagation()
                onSplitGroup(group.id, draggedTabId, 'horizontal', 'left')
              }}
              className="col-start-1 row-start-2 rounded border border-dashed border-primary bg-primary/10 hover:bg-primary/30 hover:border-primary transition-all flex items-center justify-center text-xs font-semibold text-primary cursor-pointer select-none text-center"
            >
              Split Left
            </div>

            {/* Center / Move Here Drop Zone */}
            <div
              onPointerUp={(e) => {
                e.stopPropagation()
                onMoveTabToGroup(draggedTabId, group.id)
              }}
              className="col-start-2 row-start-2 rounded border border-dashed border-primary bg-primary/10 hover:bg-primary/35 hover:border-primary transition-all flex items-center justify-center text-xs font-semibold text-primary cursor-pointer select-none text-center"
            >
              Move Here
            </div>

            {/* Right Drop Zone */}
            <div
              onPointerUp={(e) => {
                e.stopPropagation()
                onSplitGroup(group.id, draggedTabId, 'horizontal', 'right')
              }}
              className="col-start-3 row-start-2 rounded border border-dashed border-primary bg-primary/10 hover:bg-primary/30 hover:border-primary transition-all flex items-center justify-center text-xs font-semibold text-primary cursor-pointer select-none text-center"
            >
              Split Right
            </div>

            {/* Bottom Drop Zone */}
            <div
              onPointerUp={(e) => {
                e.stopPropagation()
                onSplitGroup(group.id, draggedTabId, 'vertical', 'bottom')
              }}
              className="col-start-2 row-start-3 rounded border border-dashed border-primary bg-primary/10 hover:bg-primary/30 hover:border-primary transition-all flex items-center justify-center text-xs font-semibold text-primary cursor-pointer select-none text-center"
            >
              Split Bottom
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
