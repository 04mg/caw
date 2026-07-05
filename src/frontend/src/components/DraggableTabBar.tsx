import { useState, useRef, useCallback, useEffect, type PointerEvent } from 'react'
import { Terminal, Plus, X, GitBranch, FileCode } from 'lucide-react'
import { agentTypes } from '@/lib/agentTypes'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'

interface TabItem {
  id: string
  name: string
  agentId?: string
  filePath?: string
  isDiff?: boolean
}

interface DraggableTabBarProps {
  tabs: TabItem[]
  activeIndex: number
  onSwitch: (index: number) => void
  onClose: (index: number) => void
  onReorder: (from: number, to: number) => void
  onAdd: (cmd?: string[], agentId?: string, label?: string) => void
}

export function DraggableTabBar({
  tabs,
  activeIndex,
  onSwitch,
  onClose,
  onReorder,
  onAdd,
}: DraggableTabBarProps) {
  const [availableAgents, setAvailableAgents] = useState<any[]>([])

  useEffect(() => {
    fetch('/api/agents/available')
      .then((res) => res.json())
      .then((data) => {
        if (Array.isArray(data)) {
          setAvailableAgents(data)
        }
      })
      .catch(() => {})
  }, [])
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)
  const [dragOffset, setDragOffset] = useState(0)
  const dragStartXRef = useRef(0)
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([])

  const onPointerDown = useCallback(
    (e: PointerEvent<HTMLButtonElement>, index: number) => {
      if (e.button !== 0) return
      dragStartXRef.current = e.clientX
      setDragIndex(index)
      setDragOverIndex(null)
      setDragOffset(0)
      ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
    },
    [],
  )

  const onPointerMove = useCallback(
    (e: PointerEvent<HTMLButtonElement>, index: number) => {
      if (dragIndex !== index) return
      const delta = e.clientX - dragStartXRef.current
      setDragOffset(delta)

      const myX = e.clientX
      let hoverIdx = -1
      for (let i = 0; i < tabs.length; i++) {
        if (i === dragIndex) continue
        const r = tabRefs.current[i]?.getBoundingClientRect()
        if (!r) continue
        if (myX >= r.left && myX <= r.right) {
          hoverIdx = i
          break
        }
      }

      if (hoverIdx >= 0 && hoverIdx !== dragOverIndex) {
        setDragOverIndex(hoverIdx)
      }
    },
    [dragIndex, dragOverIndex, tabs.length],
  )

  const onPointerUp = useCallback(
    (e: PointerEvent<HTMLButtonElement>) => {
      ;(e.target as HTMLElement).releasePointerCapture?.(e.pointerId)
      if (dragIndex !== null && dragOverIndex !== null && dragOverIndex !== dragIndex) {
        onReorder(dragIndex, dragOverIndex)
      }
      setDragIndex(null)
      setDragOverIndex(null)
      setDragOffset(0)
      dragStartXRef.current = 0
    },
    [dragIndex, dragOverIndex, onReorder],
  )

  return (
    <>
      {tabs.map((tab, i) => {
        const isActive = i === activeIndex
        const isDragging = dragIndex === i
        const isDragOver = dragOverIndex === i && dragIndex !== null && dragIndex !== i
        return (
          <button
            key={tab.id}
            ref={(el) => { tabRefs.current[i] = el }}
            onClick={() => onSwitch(i)}
            onPointerDown={(e) => {
              if (e.button === 1) onClose(i)
              else onPointerDown(e, i)
            }}
            onPointerMove={(e) => onPointerMove(e, i)}
            onPointerUp={(e) => onPointerUp(e)}
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
            {(() => {
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
            })()}
            <span className="truncate max-w-28">{tab.name}</span>
            <X
              onPointerDown={(e) => { e.stopPropagation() }}
              onClick={(e) => { e.stopPropagation(); onClose(i) }}
              className="h-3 w-3 ml-1 shrink-0 opacity-0 group-hover:opacity-100 hover:text-red-400 active:text-red-300 transition-opacity"
            />
          </button>
        )
      })}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            className="flex items-center justify-center px-2 text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors h-full shrink-0 border-r border-border"
            title="New tab/agent"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onClick={() => onAdd()}>
            <Terminal className="h-4 w-4" />
            <span>New Terminal</span>
          </DropdownMenuItem>
          {(() => {
            const savedDisabled = localStorage.getItem('caw:disabledAgents')
            let disabledList: string[] = []
            if (savedDisabled) {
              try {
                disabledList = JSON.parse(savedDisabled)
              } catch {}
            }

            const visibleAgents = availableAgents.filter((a) => !disabledList.includes(a.id))
            if (visibleAgents.length === 0) return null

            return (
              <>
                <DropdownMenuSeparator />
                {visibleAgents.map((agentInfo) => {
                  const agent = agentTypes[agentInfo.id]
                  const IconComponent = agent?.icon || Terminal
                  return (
                    <DropdownMenuItem
                      key={agentInfo.id}
                      onClick={() => onAdd(agentInfo.cmd, agentInfo.id, agentInfo.label)}
                    >
                      <IconComponent size={16} className="h-4 w-4" />
                      <span>{agentInfo.label}</span>
                    </DropdownMenuItem>
                  )
                })}
              </>
            )
          })()}
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  )
}