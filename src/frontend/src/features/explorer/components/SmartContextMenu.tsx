import React, { useState, useLayoutEffect } from 'react'

interface SmartContextMenuProps {
  x: number
  y: number
  children: React.ReactNode
  position?: 'absolute' | 'fixed'
  bounds?: { width: number; height: number }
}

export const SmartContextMenu = React.forwardRef<HTMLDivElement, SmartContextMenuProps>(({
  x,
  y,
  children,
  position = 'fixed',
  bounds,
}, ref) => {
  const [pos, setPos] = useState({ left: x, top: y })

  useLayoutEffect(() => {
    const el = (ref as React.RefObject<HTMLDivElement | null>).current
    if (!el) return
    const rect = el.getBoundingClientRect()
    const maxWidth = bounds?.width ?? window.innerWidth
    const maxHeight = bounds?.height ?? window.innerHeight
    let left = x
    let top = y

    if (x + rect.width > maxWidth - 4) {
      left = x - rect.width
    }
    if (top + rect.height > maxHeight - 4) {
      top = y - rect.height
    }
    // Never let the menu spill past the screen edges, even when the anchor
    // point itself sits beyond them (e.g. a row that scrolled under the
    // right edge on mobile). Prefer opening toward the left when there's
    // room, then clamp so the menu always stays fully visible.
    left = Math.min(Math.max(4, left), Math.max(4, maxWidth - rect.width - 4))
    top = Math.min(Math.max(4, top), Math.max(4, maxHeight - rect.height - 4))

    setPos({ left, top })
  }, [x, y, ref, bounds?.width, bounds?.height])

  return (
    <div
      ref={ref as React.Ref<HTMLDivElement>}
      className={`${position} z-[60] w-44 rounded-md border border-border bg-popover shadow-md py-0.5 smart-context-menu`}
      style={{ left: pos.left, top: pos.top }}
      onPointerDownCapture={(event) => event.stopPropagation()}
    >
      {children}
    </div>
  )
})

SmartContextMenu.displayName = 'SmartContextMenu'
