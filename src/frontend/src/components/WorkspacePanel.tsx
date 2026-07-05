import { useState, useCallback, useRef, useEffect, type PointerEvent } from 'react'
import { Plus, PanelLeft, PanelLeftClose, Pencil, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { WorkspacePickerDialog } from '@/components/WorkspacePickerDialog'
import { WorkspaceEditDialog } from '@/components/WorkspaceEditDialog'
import { WorkspaceMenu } from '@/components/WorkspaceMenu'
import { type Workspace } from '@/lib/workspaceStore'

const commonEmojis = ['🚀', '💻', '⚡', '🎯', '🔥', '🌈', '🌟', '🎨', '💡', '📁', '🔧', '📊', '🎮', '🤖', '🛠️', '📦', '🔬', '🎪', '🏗️', '🧩', '🎭', '📡', '🔍', '💎', '🌿', '🍀', '🎵', '📚', '⚙️', '🧪']

interface WorkspacePanelProps {
  workspaces: Workspace[]
  activeWorkspaceId: string | null
  onSelectWorkspace: (id: string) => void
  onAddWorkspace: (path: string, name: string, emoji: string) => void
  onDeleteWorkspace: (id: string) => void
  onEditWorkspace: (id: string, name: string, emoji: string) => void
  onReorderWorkspaces: (from: number, to: number) => void
  collapsed: boolean
  onToggle: () => void
  noHeader?: boolean
  pickerOpen?: boolean
  onPickerOpenChange?: (open: boolean) => void
}

export function WorkspacePanel({
  workspaces,
  activeWorkspaceId,
  onSelectWorkspace,
  onAddWorkspace,
  onDeleteWorkspace,
  onEditWorkspace,
  onReorderWorkspaces,
  collapsed,
  onToggle,
  noHeader,
  pickerOpen: externalPickerOpen,
  onPickerOpenChange,
}: WorkspacePanelProps) {
  const [internalPickerOpen, setInternalPickerOpen] = useState(false)
  const pickerOpen = externalPickerOpen ?? internalPickerOpen
  const setPickerOpen = onPickerOpenChange ?? setInternalPickerOpen
  const [editTarget, setEditTarget] = useState<Workspace | null>(null)
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)
  const [dragOffset, setDragOffset] = useState(0)
  const dragStartYRef = useRef(0)
  const itemRefs = useRef<(HTMLDivElement | null)[]>([])
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; workspaceId: string } | null>(null)

  useEffect(() => {
    if (!contextMenu) return
    const onDown = () => setContextMenu(null)
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [contextMenu])

  const handleChoose = useCallback(
    (path: string, name: string, emoji: string) => {
      onAddWorkspace(path, name, emoji)
      setPickerOpen(false)
    },
    [onAddWorkspace, setPickerOpen],
  )

  const handleEditSave = useCallback(
    (name: string, emoji: string) => {
      if (editTarget) onEditWorkspace(editTarget.id, name, emoji)
      setEditTarget(null)
    },
    [editTarget, onEditWorkspace],
  )

  const onPointerDown = useCallback(
    (e: PointerEvent<HTMLDivElement>, index: number) => {
      if (e.button !== 0) return
      dragStartYRef.current = e.clientY
      setDragIndex(index)
      setDragOverIndex(null)
      setDragOffset(0)
      ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
    },
    [],
  )

  const onPointerMove = useCallback(
    (e: PointerEvent<HTMLDivElement>, index: number) => {
      if (dragIndex !== index) return
      const delta = e.clientY - dragStartYRef.current
      setDragOffset(delta)

      const myY = e.clientY
      let hoverIdx = -1
      for (let i = 0; i < workspaces.length; i++) {
        if (i === dragIndex) continue
        const r = itemRefs.current[i]?.getBoundingClientRect()
        if (!r) continue
        if (myY >= r.top && myY <= r.bottom) {
          hoverIdx = i
          break
        }
      }

      if (hoverIdx >= 0 && hoverIdx !== dragOverIndex) {
        setDragOverIndex(hoverIdx)
      }
    },
    [dragIndex, dragOverIndex, workspaces.length],
  )

  const onPointerUp = useCallback(
    (e: PointerEvent<HTMLDivElement>) => {
      ;(e.target as HTMLElement).releasePointerCapture?.(e.pointerId)
      if (dragIndex !== null && dragOverIndex !== null && dragOverIndex !== dragIndex) {
        onReorderWorkspaces(dragIndex, dragOverIndex)
      }
      setDragIndex(null)
      setDragOverIndex(null)
      setDragOffset(0)
      dragStartYRef.current = 0
    },
    [dragIndex, dragOverIndex, onReorderWorkspaces],
  )

  if (collapsed) {
    return (
      <div className="flex flex-col bg-background border-r border-border overflow-hidden shrink-0" style={{ width: 44 }}>
        <div className="flex items-center justify-center px-3 h-[33px] shrink-0">
          <Button
            variant="ghost"
            size="icon"
            className="h-5 w-5 shrink-0"
            onClick={onToggle}
            title="Show sidebar"
          >
            <PanelLeft className="h-3.5 w-3.5" />
          </Button>
        </div>
        <div className="flex flex-col items-center flex-1 overflow-y-auto">
          {workspaces.map((ws, i) => (
            <button
              key={ws.id}
              onClick={() => onSelectWorkspace(ws.id)}
              onContextMenu={(e) => {
                e.preventDefault()
                e.stopPropagation()
                setContextMenu({ x: e.clientX, y: e.clientY, workspaceId: ws.id })
              }}
              className={`flex items-center justify-center h-[33px] w-full text-base transition-colors ${
                ws.id === activeWorkspaceId ? 'bg-accent/70' : 'hover:bg-accent/40'
              }`}
              title={ws.name || ws.path || 'Workspace'}
            >
              {ws.emoji || commonEmojis[i % commonEmojis.length]}
            </button>
          ))}
        </div>
        {contextMenu && (() => {
          const ws = workspaces.find((w) => w.id === contextMenu.workspaceId)
          if (!ws) return null
          return (
            <div
              className="fixed z-50 w-40 rounded-md border border-border bg-popover shadow-md py-0.5"
              style={{ left: contextMenu.x, top: contextMenu.y }}
            >
              <button
                onClick={(e) => { e.stopPropagation(); setContextMenu(null); setEditTarget(ws) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Pencil className="h-3.5 w-3.5" />
                Edit workspace
              </button>
              <button
                onClick={(e) => { e.stopPropagation(); setContextMenu(null); onDeleteWorkspace(ws.id) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete workspace
              </button>
            </div>
          )
        })()}
        <WorkspacePickerDialog open={pickerOpen} onOpenChange={setPickerOpen} onChoose={handleChoose} />
        {editTarget && (
          <WorkspaceEditDialog
            open={true}
            onOpenChange={() => setEditTarget(null)}
            initialName={editTarget.name}
            initialEmoji={editTarget.emoji || ''}
            onSave={handleEditSave}
          />
        )}
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col bg-background select-none">
      {!noHeader && (
        <div className="flex items-center gap-2 border-b border-border px-3 h-[33px] shrink-0 bg-secondary/20">
          <Button
            variant="ghost"
            size="icon"
            className="h-5 w-5"
            onClick={onToggle}
            title="Hide sidebar"
          >
            <PanelLeftClose className="h-3.5 w-3.5" />
          </Button>
          <span className="flex-1 text-xs font-semibold text-muted-foreground">
            Workspaces
          </span>
          <Button
            variant="ghost"
            size="icon"
            className="h-5 w-5"
            onClick={() => setPickerOpen(true)}
            title="Add workspace"
          >
            <Plus className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}

      {workspaces.length === 0 ? (
        <div className="flex-1 flex items-center justify-center">
          <p className="text-xs text-muted-foreground italic">No workspaces.</p>
        </div>
      ) : (
        <ScrollArea className="flex-1">
          <div>
            {workspaces.map((ws, i) => {
              const isActive = ws.id === activeWorkspaceId
              const label = ws.name || ws.path || 'Workspace'
              const isDragging = dragIndex === i
              const isDragOver = dragOverIndex === i && dragIndex !== null && dragIndex !== i
              return (
                <div
                  key={ws.id}
                  ref={(el) => { itemRefs.current[i] = el }}
                  onClick={() => onSelectWorkspace(ws.id)}
                  onContextMenu={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    setContextMenu({ x: e.clientX, y: e.clientY, workspaceId: ws.id })
                  }}
                  onPointerDown={(e) => onPointerDown(e, i)}
                  onPointerMove={(e) => onPointerMove(e, i)}
                  onPointerUp={(e) => onPointerUp(e)}
                  className={`group flex items-center gap-1.5 px-2 py-1.5 text-sm select-none transition-transform duration-150 border-t border-border ${
                    i === 0 ? 'border-t-0' : ''
                  } ${isDragOver ? 'border-t-2 border-t-primary' : ''} ${
                    isActive ? 'bg-accent/70 text-accent-foreground' : 'hover:bg-accent/40 text-muted-foreground'
                  } ${isDragging ? 'opacity-60 z-10' : ''}`}
                  style={{
                    userSelect: 'none',
                    ...(isDragging
                      ? { transform: `translateY(${dragOffset}px)`, transition: 'none' }
                      : {}),
                  }}
                >
                  <span className="text-base leading-none shrink-0">{ws.emoji || commonEmojis[i % commonEmojis.length]}</span>
                  <span className="truncate flex-1" title={ws.path}>{label}</span>
                  <div className="opacity-0 group-hover:opacity-100 transition-opacity">
                    <WorkspaceMenu
                      onDelete={() => onDeleteWorkspace(ws.id)}
                      onEdit={() => setEditTarget(ws)}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        </ScrollArea>
      )}

      {contextMenu && (() => {
        const ws = workspaces.find((w) => w.id === contextMenu.workspaceId)
        if (!ws) return null
        return (
          <div
            className="fixed z-50 w-40 rounded-md border border-border bg-popover shadow-md py-0.5"
            style={{ left: contextMenu.x, top: contextMenu.y }}
          >
            <button
              onClick={(e) => { e.stopPropagation(); setContextMenu(null); setEditTarget(ws) }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
            >
              <Pencil className="h-3.5 w-3.5" />
              Edit workspace
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); setContextMenu(null); onDeleteWorkspace(ws.id) }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete workspace
            </button>
          </div>
        )
      })()}

      <WorkspacePickerDialog open={pickerOpen} onOpenChange={setPickerOpen} onChoose={handleChoose} />

      {editTarget && (
        <WorkspaceEditDialog
          open={true}
          onOpenChange={() => setEditTarget(null)}
          initialName={editTarget.name}
          initialEmoji={editTarget.emoji || ''}
          onSave={handleEditSave}
        />
      )}
    </div>
  )
}
