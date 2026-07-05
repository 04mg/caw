import { useEffect, useRef, useState } from 'react'
import { MoreVertical, Trash2, Pencil } from 'lucide-react'

interface WorkspaceMenuProps {
  onDelete: () => void
  onEdit: () => void
}

export function WorkspaceMenu({ onDelete, onEdit }: WorkspaceMenuProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o) }}
        className="h-5 w-5 rounded text-muted-foreground hover:text-foreground hover:bg-accent/40 flex items-center justify-center"
        title="More"
      >
        <MoreVertical className="h-3.5 w-3.5" />
      </button>
      {open && (
        <div className="absolute right-0 top-6 z-30 w-40 rounded-md border border-border bg-popover shadow-md py-0.5">
          <button
            onClick={(e) => {
              e.stopPropagation()
              setOpen(false)
              onEdit()
            }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
          >
            <Pencil className="h-3.5 w-3.5" />
            Edit workspace
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              setOpen(false)
              onDelete()
            }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-destructive hover:bg-destructive hover:text-destructive-foreground"
          >
            <Trash2 className="h-3.5 w-3.5" />
            Delete workspace
          </button>
        </div>
      )}
    </div>
  )
}
