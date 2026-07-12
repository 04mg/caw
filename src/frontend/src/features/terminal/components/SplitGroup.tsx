import { useRef, useCallback, type ReactNode } from 'react'
import { Group } from 'react-resizable-panels'

interface SplitGroupProps {
  splitId: string
  orientation: 'horizontal' | 'vertical'
  onSizesChange: (splitId: string, sizes: number[]) => void
  children: ReactNode
}

export function SplitGroup({
  splitId,
  orientation,
  onSizesChange,
  children,
}: SplitGroupProps): ReactNode {
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
