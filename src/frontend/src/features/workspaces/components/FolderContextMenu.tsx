import { useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Plus, Pencil, Trash2 } from 'lucide-react'

export interface FolderContextMenuProps {
  x: number
  y: number
  onNewWorkspace: () => void
  onEdit: () => void
  onDelete: () => void
}

export function FolderContextMenu({
  x,
  y,
  onNewWorkspace,
  onEdit,
  onDelete,
}: FolderContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ left: x, top: y })

  useLayoutEffect(() => {
    const el = menuRef.current
    if (!el) return
    const rect = el.getBoundingClientRect()
    const maxWidth = window.innerWidth
    const maxHeight = window.innerHeight
    let left = x
    let top = y

    if (x + rect.width > maxWidth - 4) {
      left = x - rect.width
    }
    if (top + rect.height > maxHeight - 4) {
      top = y - rect.height
    }
    left = Math.min(Math.max(4, left), Math.max(4, maxWidth - rect.width - 4))
    top = Math.min(Math.max(4, top), Math.max(4, maxHeight - rect.height - 4))
    setPos({ left, top })
  }, [x, y])

  return createPortal(
    <div
      ref={menuRef}
      className="fixed z-50 w-40 rounded-md border border-border bg-popover shadow-md py-0.5 smart-context-menu"
      style={{ left: pos.left, top: pos.top }}
      onMouseDown={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.preventDefault()}
    >
      <button
        onClick={(e) => {
          e.stopPropagation()
          onNewWorkspace()
        }}
        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
      >
        <Plus className="h-3.5 w-3.5" />
        New workspace
      </button>
      <div className="my-0.5 border-t border-border" />
      <button
        onClick={(e) => {
          e.stopPropagation()
          onEdit()
        }}
        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
      >
        <Pencil className="h-3.5 w-3.5" />
        Edit folder
      </button>
      <div className="my-0.5 border-t border-border" />
      <button
        onClick={(e) => {
          e.stopPropagation()
          onDelete()
        }}
        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
      >
        <Trash2 className="h-3.5 w-3.5" />
        Delete folder
      </button>
    </div>,
    document.body,
  )
}
