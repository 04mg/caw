import { useEffect, useRef, useState } from 'react'
import { MoreVertical, Trash2, Pencil, FolderInput, FolderMinus, FolderPlus, ChevronRight } from 'lucide-react'
import { type WorkspaceFolder } from '@/features/workspaces/types'

interface WorkspaceMenuProps {
  onDelete: () => void
  onEdit: () => void
  folders?: WorkspaceFolder[]
  currentFolderId?: string | null
  onMoveToFolder?: (folderId: string) => void
  onRemoveFromFolder?: () => void
  onNewFolder?: () => void
}

export function WorkspaceMenu({ onDelete, onEdit, folders = [], currentFolderId = null, onMoveToFolder, onRemoveFromFolder, onNewFolder }: WorkspaceMenuProps) {
  const [open, setOpen] = useState(false)
  const [folderPickerOpen, setFolderPickerOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) {
      setFolderPickerOpen(false)
      return
    }
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  const closeAll = () => {
    setFolderPickerOpen(false)
    setOpen(false)
  }

  return (
    <div ref={ref} className="relative">
      <button
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o) }}
        onPointerDown={(e) => e.stopPropagation()}
        className="h-5 w-5 rounded text-muted-foreground hover:text-foreground hover:bg-accent/40 flex items-center justify-center"
        title="More"
      >
        <MoreVertical className="h-3.5 w-3.5" />
      </button>
      {open && (
        <div className="absolute right-0 top-6 z-30 w-44 rounded-md border border-border bg-popover shadow-md py-0.5">
          <button
            onClick={(e) => {
              e.stopPropagation()
              onEdit()
              closeAll()
            }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
          >
            <Pencil className="h-3.5 w-3.5" />
            Edit workspace
          </button>
          {(folders.length > 0 || onNewFolder) && (
            <>
              <div className="my-0.5 border-t border-border" />
              <button
                onClick={(e) => { e.stopPropagation(); setFolderPickerOpen((o) => !o) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <FolderInput className="h-3.5 w-3.5" />
                <span className="flex-1 text-left">Add to folder</span>
                <ChevronRight className={`h-3 w-3 text-muted-foreground transition-transform ${folderPickerOpen ? 'rotate-90' : ''}`} />
              </button>
              {folderPickerOpen && (
                <div className="max-h-48 overflow-y-auto pb-0.5">
                  {currentFolderId && onRemoveFromFolder && (
                    <button
                      onClick={(e) => { e.stopPropagation(); onRemoveFromFolder(); closeAll() }}
                      className="flex w-full items-center gap-2 pl-5 pr-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
                    >
                      <FolderMinus className="h-3.5 w-3.5" />
                      Remove from folder
                    </button>
                  )}
                  {folders.filter((f) => f.id !== currentFolderId).map((f) => (
                    <button
                      key={f.id}
                      onClick={(e) => { e.stopPropagation(); if (onMoveToFolder) onMoveToFolder(f.id); closeAll() }}
                      className="flex w-full items-center gap-2 pl-5 pr-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
                    >
                      <span className="w-3.5 shrink-0 text-center text-sm leading-none">{f.emoji || '\u{1F4C1}'}</span>
                      <span className="truncate">{f.name}</span>
                    </button>
                  ))}
                  {onNewFolder && (
                    <button
                      onClick={(e) => { e.stopPropagation(); onNewFolder(); closeAll() }}
                      className="flex w-full items-center gap-2 pl-5 pr-2 py-1.5 text-xs text-muted-foreground hover:bg-accent/60"
                    >
                      <FolderPlus className="h-3.5 w-3.5" />
                      New folder…
                    </button>
                  )}
                </div>
              )}
            </>
          )}
          <div className="my-0.5 border-t border-border" />
          <button
            onClick={(e) => {
              e.stopPropagation()
              onDelete()
              closeAll()
            }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
          >
            <Trash2 className="h-3.5 w-3.5" />
            Delete workspace
          </button>
        </div>
      )}
    </div>
  )
}
