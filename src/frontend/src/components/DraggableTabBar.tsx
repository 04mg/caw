import { useState, useRef, useCallback, type PointerEvent } from 'react'
import { Terminal, Plus, X } from 'lucide-react'

interface TabItem {
  id: string
  name: string
}

interface DraggableTabBarProps {
  tabs: TabItem[]
  activeIndex: number
  onSwitch: (index: number) => void
  onClose: (index: number) => void
  onReorder: (from: number, to: number) => void
  onAdd: () => void
}

export function DraggableTabBar({
  tabs,
  activeIndex,
  onSwitch,
  onClose,
  onReorder,
  onAdd,
}: DraggableTabBarProps) {
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
            <Terminal className="h-3 w-3 shrink-0" />
            <span className="truncate max-w-28">{tab.name}</span>
            <X
              onClick={(e) => { e.stopPropagation(); onClose(i) }}
              className="h-3 w-3 ml-1 shrink-0 opacity-0 group-hover:opacity-100 hover:text-destructive transition-opacity"
            />
          </button>
        )
      })}
      <button
        onClick={onAdd}
        className="flex items-center justify-center px-2 text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors h-full shrink-0 border-r border-border"
        title="New tab"
      >
        <Plus className="h-3.5 w-3.5" />
      </button>
    </>
  )
}