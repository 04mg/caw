import { useState } from 'react'
import { createPortal } from 'react-dom'
import { Pencil, Trash2, ChevronRight, FolderInput, FolderMinus, FolderPlus } from 'lucide-react'
import { type Workspace, type WorkspaceFolder } from '@/features/workspaces/types'

interface WorkspaceContextMenuProps {
  x: number
  y: number
  ws: Workspace
  folders: WorkspaceFolder[]
  onEdit: () => void
  onDelete: () => void
  onMoveToFolder: (folderId: string) => void
  onRemoveFromFolder: () => void
  onNewFolder: () => void
}

export function WorkspaceContextMenu({ x, y, ws, folders, onEdit, onDelete, onMoveToFolder, onRemoveFromFolder, onNewFolder }: WorkspaceContextMenuProps) {
  const [pickerOpen, setPickerOpen] = useState(false)
  const others = folders.filter((f) => f.id !== ws.folderId)

  return createPortal(
    <div
      className="fixed z-50 w-44 rounded-md border border-border bg-popover shadow-md py-0.5 smart-context-menu"
      style={{ left: x, top: y }}
      onMouseDown={(e) => e.stopPropagation()}
    >
      <button
        onClick={(e) => { e.stopPropagation(); onEdit() }}
        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
      >
        <Pencil className="h-3.5 w-3.5" />
        Edit workspace
      </button>
      {(others.length > 0 || !!ws.folderId || folders.length > 0) && (
        <>
          <div className="my-0.5 border-t border-border" />
          <button
            onClick={(e) => { e.stopPropagation(); setPickerOpen((o) => !o) }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
          >
            <FolderInput className="h-3.5 w-3.5" />
            <span className="flex-1 text-left">Add to folder</span>
            <ChevronRight className={`h-3 w-3 text-muted-foreground transition-transform ${pickerOpen ? 'rotate-90' : ''}`} />
          </button>
          {pickerOpen && (
            <div className="max-h-48 overflow-y-auto pb-0.5">
              {ws.folderId && (
                <button
                  onClick={(e) => { e.stopPropagation(); onRemoveFromFolder() }}
                  className="flex w-full items-center gap-2 pl-5 pr-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
                >
                  <FolderMinus className="h-3.5 w-3.5" />
                  Remove from folder
                </button>
              )}
              {others.map((f) => (
                <button
                  key={f.id}
                  onClick={(e) => { e.stopPropagation(); onMoveToFolder(f.id) }}
                  className="flex w-full items-center gap-2 pl-5 pr-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
                >
                  <span className="w-3.5 shrink-0 text-center text-sm leading-none">{f.emoji || '\u{1F4C1}'}</span>
                  <span className="truncate">{f.name}</span>
                </button>
              ))}
              <button
                onClick={(e) => { e.stopPropagation(); onNewFolder() }}
                className="flex w-full items-center gap-2 pl-5 pr-2 py-1.5 text-xs text-muted-foreground hover:bg-accent/60"
              >
                <FolderPlus className="h-3.5 w-3.5" />
                New folder…
              </button>
            </div>
          )}
        </>
      )}
      <div className="my-0.5 border-t border-border" />
      <button
        onClick={(e) => { e.stopPropagation(); onDelete() }}
        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
      >
        <Trash2 className="h-3.5 w-3.5" />
        Delete workspace
      </button>
    </div>,
    document.body,
  )
}
