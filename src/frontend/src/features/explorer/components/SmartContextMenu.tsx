import React, { useState, useLayoutEffect } from 'react'

interface SmartContextMenuProps {
  x: number
  y: number
  children: React.ReactNode
}

export const SmartContextMenu = React.forwardRef<HTMLDivElement, SmartContextMenuProps>(({ x, y, children }, ref) => {
  const [pos, setPos] = useState({ left: x, top: y })

  useLayoutEffect(() => {
    const el = (ref as React.RefObject<HTMLDivElement | null>).current
    if (!el) return
    const rect = el.getBoundingClientRect()
    const winW = window.innerWidth
    const winH = window.innerHeight
    let left = x
    let top = y

    if (x + rect.width > winW - 4) {
      left = x - rect.width
    }
    if (top + rect.height > winH - 4) {
      top = y - rect.height
    }
    if (left < 4) left = 4
    if (top < 4) top = 4

    setPos({ left, top })
  }, [x, y, ref])

  return (
    <div
      ref={ref as React.Ref<HTMLDivElement>}
      className="fixed z-50 w-44 rounded-md border border-border bg-popover shadow-md py-0.5 smart-context-menu"
      style={{ left: pos.left, top: pos.top }}
      onPointerDownCapture={(event) => event.stopPropagation()}
    >
      {children}
    </div>
  )
})

SmartContextMenu.displayName = 'SmartContextMenu'
