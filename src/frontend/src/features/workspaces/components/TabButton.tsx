import { type PointerEvent } from 'react'
import { Terminal, X, GitBranch, FileCode } from 'lucide-react'
import { agentTypes } from '@/features/agents/services/agentTypes'
import { useFileDirty } from '@/features/editor/hooks/useFileDirty'
import { type AgentStatus } from '@/features/agents/types'
import { getAgentStatusDot } from '@/features/agents/utils/statusDot'

export interface TabItem {
  id: string
  name: string
  agentId?: string
  filePath?: string
  isDiff?: boolean
  agentStatus?: AgentStatus
}

function renderTabIcon(tab: TabItem) {
  if (tab.isDiff) {
    return <GitBranch className="h-3.5 w-3.5 text-primary shrink-0" />
  }
  if (tab.filePath) {
    return <FileCode className="h-3.5 w-3.5 text-blue-400 shrink-0" />
  }
  const agent = tab.agentId ? agentTypes[tab.agentId] : null
  if (agent && agent.icon) {
    const IconComponent = agent.icon
    return <IconComponent size={14} className="h-3.5 w-3.5 shrink-0" />
  }
  return <Terminal className="h-3 w-3 shrink-0" />
}

export interface TabButtonProps {
  tab: TabItem
  index: number
  isActive: boolean
  isDragging: boolean
  isDragOver: boolean
  dragOffset: number
  onSwitch: (index: number) => void
  onClose: (index: number) => void
  onPointerDown: (e: PointerEvent<HTMLButtonElement>, index: number) => void
  onPointerMove: (e: PointerEvent<HTMLButtonElement>, index: number) => void
  onPointerUp: (e: PointerEvent<HTMLButtonElement>) => void
  setTabRef: (el: HTMLButtonElement | null) => void
}

export function TabButton({
  tab,
  index,
  isActive,
  isDragging,
  isDragOver,
  dragOffset,
  onSwitch,
  onClose,
  onPointerDown,
  onPointerMove,
  onPointerUp,
  setTabRef,
}: TabButtonProps) {
  const dirty = useFileDirty(tab.filePath)
  const displayName = dirty ? `${tab.name}*` : tab.name
  return (
    <button
      ref={setTabRef}
      onClick={() => onSwitch(index)}
      onPointerDown={(e) => {
        if (e.button === 1) onClose(index)
        else onPointerDown(e, index)
      }}
      onPointerMove={(e) => onPointerMove(e, index)}
      onPointerUp={onPointerUp}
      className={`group flex items-center gap-1.5 px-3 text-xs border-r border-border transition-colors h-full select-none ${
        isActive
          ? 'bg-background text-foreground'
          : 'bg-secondary/10 text-muted-foreground hover:bg-secondary/30 hover:text-foreground'
      } ${isDragOver ? 'border-t-2 border-t-primary' : ''} ${
        isDragging ? 'opacity-60 z-10' : ''
      }`}
      style={{
        userSelect: 'none',
        ...(isDragging
          ? { transform: `translateX(${dragOffset}px)`, transition: 'none' }
          : { transition: 'transform 0.15s ease-out' }),
      }}
    >
      {renderTabIcon(tab)}
      <span className="truncate max-w-28">{displayName}</span>
      <span className="relative ml-1 flex h-3 w-3 shrink-0 items-center justify-center">
        {tab.agentId && tab.agentStatus && (() => {
          const colors = getAgentStatusDot(tab.agentStatus)
          return (
            <span className="absolute inset-0 flex items-center justify-center transition-opacity duration-150 opacity-100 group-hover:opacity-0">
              <span className="relative flex" style={{ height: 7, width: 7 }}>
                {colors.ring && (
                  <span className={`animate-ping absolute inline-flex h-full w-full rounded-full ${colors.ring} opacity-75`} />
                )}
                <span className={`relative inline-flex rounded-full ${colors.dot}`} style={{ height: 7, width: 7 }} />
              </span>
            </span>
          )
        })()}
        <X
          onPointerDown={(e) => { e.stopPropagation() }}
          onClick={(e) => { e.stopPropagation(); onClose(index) }}
          className="absolute inset-0 flex items-center justify-center h-3 w-3 opacity-0 group-hover:opacity-100 hover:text-red-400 active:text-red-300 transition-opacity duration-150"
        />
      </span>
    </button>
  )
}