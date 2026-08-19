import { useState, useRef, useCallback, type PointerEvent } from 'react'
import { NewTabMenu } from '@/features/workspaces/components/NewTabMenu'
import { TabButton, type TabItem } from '@/features/workspaces/components/TabButton'


interface DraggableTabBarProps {
  tabs: TabItem[]
  activeIndex: number
  onSwitch: (index: number) => void
  onClose: (index: number) => void
  onReorder: (from: number, to: number) => void
  onAdd: (cmd?: string[], agentId?: string, label?: string, groupId?: string, env?: [string, string][], view?: import('@/features/shared/utils/layout').LeafView) => void
  enableWorktrees?: boolean
  onToggleWorktrees?: () => void
  onDragStart?: (tabId: string) => void
}

export function DraggableTabBar({
  tabs,
  activeIndex,
  onSwitch,
  onClose,
  onReorder,
  onAdd,
  enableWorktrees,
  onToggleWorktrees,
  onDragStart,
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

      const container = e.currentTarget.parentElement
      if (container) {
        const r = container.getBoundingClientRect()
        if (e.clientY < r.top - 20 || e.clientY > r.bottom + 20) {
          setDragIndex(null)
          setDragOverIndex(null)
          setDragOffset(0)
          dragStartXRef.current = 0
          ;(e.target as HTMLElement).releasePointerCapture?.(e.pointerId)
          onDragStart?.(tabs[index].id)
          return
        }
      }

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
    [dragIndex, dragOverIndex, tabs, onDragStart],
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
      {tabs.map((tab, i) => (
        <TabButton
          key={tab.id}
          tab={tab}
          index={i}
          isActive={i === activeIndex}
          isDragging={dragIndex === i}
          isDragOver={dragOverIndex === i && dragIndex !== null && dragIndex !== i}
          dragOffset={dragIndex === i ? dragOffset : 0}
          onSwitch={onSwitch}
          onClose={onClose}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          setTabRef={(el) => { tabRefs.current[i] = el }}
        />
      ))}
      <NewTabMenu
        onAdd={onAdd}
        enableWorktrees={enableWorktrees}
        onToggleWorktrees={onToggleWorktrees}
        triggerClassName="border-r border-border"
      />
    </>
  )
}