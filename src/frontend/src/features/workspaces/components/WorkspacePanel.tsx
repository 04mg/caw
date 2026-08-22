import { useState, useCallback, useRef, useEffect, useMemo, type PointerEvent } from 'react'
import { createPortal } from 'react-dom'
import { Plus, PanelLeft, PanelLeftClose, PanelRight, PanelRightClose, Pencil, Trash2, FolderPlus, Settings, MoreVertical, ChevronRight } from 'lucide-react'
import { Button } from '@/components/button'
import { ScrollArea } from '@/components/scroll-area'

import { WorkspacePickerDialog } from './WorkspacePickerDialog'
import { WorkspaceEditDialog } from './WorkspaceEditDialog'
import { WorkspaceFolderDialog } from './WorkspaceFolderDialog'
import { WorkspaceContextMenu } from './WorkspaceContextMenu'
import { FolderMenu } from './FolderMenu'
import { WorkspaceMenu } from './WorkspaceMenu'
import { WorkspacePreview, type PreviewAnchor } from './WorkspacePreview'
import { type Workspace, type WorkspaceFolder } from '@/features/workspaces/types'
import { buildSidebarRows, type SidebarState, moveToFolderEnd, setWorkspaceFolder, moveEntryRelative, placeAdjacentFlat } from '@/features/workspaces/utils/sidebarFolders'
import { collectLeafIds } from '@/features/shared/utils/layout'
import { useAgentStatuses } from '@/features/agents/hooks/useAgentStatuses'
import { getAgentStatusDot, getStrongestStatus } from '@/features/agents/utils/statusDot'
import { type AgentStatus } from '@/features/agents/types'


const commonEmojis = ['🚀', '💻', '⚡', '🎯', '🔥', '🌈', '🌟', '🎨', '💡', '📁', '🔧', '📊', '🎮', '🤖', '🛠️', '📦', '🔬', '🎪', '🏗️', '🧩', '🎭', '📡', '🔍', '💎', '🌿', '🍀', '🎵', '📚', '⚙️', '🧪']

// Hover-preview timing: the first hover waits this long before popping the
// thumbnail; while a preview is already visible, moving to another row swaps
// it instantly (quick browsing). Leaving the rows hides it after a short
// grace period so brief mouse exits don't flicker.
const PREVIEW_HOVER_DELAY_MS = 1000
const PREVIEW_HIDE_GRACE_MS = 150

type DropZone = 'before' | 'after' | 'into'

function getIsMobile() {
  return window.innerWidth < 768
}

interface WorkspacePanelProps {
  workspaces: Workspace[]
  folders?: WorkspaceFolder[]
  sidebarOrder?: string[]
  activeWorkspaceId: string | null
  onSelectWorkspace: (id: string) => void
  onAddWorkspace: (path: string, name: string, emoji: string) => void
  onDeleteWorkspace: (id: string) => void
  onEditWorkspace: (id: string, name: string, emoji: string) => void
  onCreateFolder?: (name: string, emoji: string, workspaceIds?: string[]) => void
  onEditFolder?: (id: string, name: string, emoji: string) => void
  onDeleteFolder?: (id: string) => void
  onSidebarMutation?: (fn: (s: SidebarState) => SidebarState) => void
  collapsed: boolean
  onToggle: () => void
  noHeader?: boolean
  pickerOpen?: boolean
  onPickerOpenChange?: (open: boolean) => void
  onOpenSettings?: () => void
  isRight?: boolean
}

export function WorkspacePanel({
  workspaces,
  folders,
  sidebarOrder,
  activeWorkspaceId,
  onSelectWorkspace,
  onAddWorkspace,
  onDeleteWorkspace,
  onEditWorkspace,
  onCreateFolder,
  onEditFolder,
  onDeleteFolder,
  onSidebarMutation,
  collapsed,
  onToggle,
  noHeader,
  pickerOpen: externalPickerOpen,
  onPickerOpenChange,
  onOpenSettings,
  isRight = false,
}: WorkspacePanelProps) {
  const [internalPickerOpen, setInternalPickerOpen] = useState(false)
  const pickerOpen = externalPickerOpen ?? internalPickerOpen
  const setPickerOpen = onPickerOpenChange ?? setInternalPickerOpen
  const [editTarget, setEditTarget] = useState<Workspace | null>(null)
  const [collapsedFolderIds, setCollapsedFolderIds] = useState<Set<string>>(new Set())
  const [folderDialog, setFolderDialog] = useState<
    { mode: 'create'; assignWsId?: string } | { mode: 'edit'; folder: WorkspaceFolder } | null
  >(null)
  const allFolders = useMemo(() => folders ?? [], [folders])
  const order = useMemo(() => sidebarOrder ?? [], [sidebarOrder])

  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [dropTarget, setDropTarget] = useState<{ index: number; zone: DropZone } | null>(null)
  const [dragOffset, setDragOffset] = useState(0)
  const dragStartYRef = useRef(0)
  const itemRefs = useRef<(HTMLDivElement | null)[]>([])
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; workspaceId: string } | null>(null)
  const [folderContextMenu, setFolderContextMenu] = useState<{ x: number; y: number; folderId: string } | null>(null)
  const [generalContextMenu, setGeneralContextMenu] = useState<{ x: number; y: number } | null>(null)
  const [isMobile, setIsMobile] = useState(getIsMobile)

  // Visible sidebar rows: folders interleaved with loose workspaces at the
  // root level, children indented right below their folder. While a folder
  // is being dragged its children are hidden so the whole group moves as
  // one visual unit.
  const baseRows = useMemo(
    () => buildSidebarRows(workspaces, allFolders, order, collapsedFolderIds),
    [workspaces, allFolders, order, collapsedFolderIds],
  )
  const rows = useMemo(() => {
    if (dragIndex === null || dragIndex >= baseRows.length) return baseRows
    const dragged = baseRows[dragIndex]
    if (dragged.kind !== 'folder') return baseRows
    const eff = new Set(collapsedFolderIds)
    eff.add(dragged.folder.id)
    return buildSidebarRows(workspaces, allFolders, order, eff)
  }, [baseRows, dragIndex, collapsedFolderIds, workspaces, allFolders, order])

  // Hover-preview state: which workspace's thumbnail is showing and where it
  // is anchored. previewVisibleRef mirrors previewWsId for synchronous checks
  // inside the hover handlers (state updates are async).
  const [previewWsId, setPreviewWsId] = useState<string | null>(null)
  const [previewAnchor, setPreviewAnchor] = useState<PreviewAnchor | null>(null)
  const previewVisibleRef = useRef(false)
  const previewShowTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const previewHideTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Subscribe to live agent statuses so the per-workspace roll-up status dot
  // in the sidebar stays in sync with the tab-panes dots.
  const statuses = useAgentStatuses()

  useEffect(() => {
    const onResize = () => setIsMobile(getIsMobile())
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useEffect(() => {
    if (!contextMenu) return
    const onDown = () => setContextMenu(null)
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [contextMenu])

  useEffect(() => {
    if (!folderContextMenu) return
    const onDown = () => setFolderContextMenu(null)
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [folderContextMenu])

  useEffect(() => {
    if (!generalContextMenu) return
    const onDown = () => setGeneralContextMenu(null)
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [generalContextMenu])

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

  const handleFolderSave = useCallback(
    (name: string, emoji: string) => {
      if (!folderDialog) return
      if (folderDialog.mode === 'edit') {
        onEditFolder?.(folderDialog.folder.id, name, emoji)
      } else {
        onCreateFolder?.(name, emoji, folderDialog.assignWsId ? [folderDialog.assignWsId] : undefined)
      }
      setFolderDialog(null)
    },
    [folderDialog, onCreateFolder, onEditFolder],
  )

  const toggleFolder = useCallback((folderId: string) => {
    setCollapsedFolderIds((prev) => {
      const next = new Set(prev)
      if (next.has(folderId)) next.delete(folderId)
      else next.add(folderId)
      return next
    })
  }, [])

  const moveToFolderCb = useCallback(
    (wsId: string, folderId: string | null) => {
      onSidebarMutation?.((s) =>
        folderId ? moveToFolderEnd(s, wsId, folderId) : setWorkspaceFolder(s, wsId, null),
      )
    },
    [onSidebarMutation],
  )

  const onPointerDown = useCallback(
    (e: PointerEvent<HTMLDivElement>, index: number) => {
      if (e.button !== 0) return
      dragStartYRef.current = e.clientY
      setDragIndex(index)
      setDropTarget(null)
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
      const draggingIsFolder = baseRows[index]?.kind === 'folder'
      let hit: { index: number; zone: DropZone } | null = null
      for (let i = 0; i < rows.length; i++) {
        if (i === dragIndex) continue
        const r = itemRefs.current[i]?.getBoundingClientRect()
        if (!r || r.height === 0) continue
        if (myY >= r.top && myY <= r.bottom) {
          const rel = (myY - r.top) / r.height
          const row = rows[i]
          let zone: DropZone
          if (row.kind === 'folder') {
            if (draggingIsFolder) zone = rel < 0.5 ? 'before' : 'after'
            else zone = rel < 0.3 ? 'before' : rel > 0.7 ? 'after' : 'into'
          } else {
            zone = rel < 0.5 ? 'before' : 'after'
          }
          hit = { index: i, zone }
          break
        }
      }

      const nextKey = hit ? `${hit.index}:${hit.zone}` : ''
      const prevKey = dropTarget ? `${dropTarget.index}:${dropTarget.zone}` : ''
      if (nextKey !== prevKey) setDropTarget(hit)
    },
    [dragIndex, baseRows, rows, dropTarget],
  )

  const performDrop = useCallback(() => {
    if (dragIndex === null || !dropTarget || dropTarget.index === dragIndex) return
    const dragRow = rows[dragIndex]
    const targetRow = rows[dropTarget.index]
    const zone = dropTarget.zone
    if (!dragRow || !targetRow) return

    if (!onSidebarMutation) return

    if (dragRow.kind === 'folder') {
      if (zone === 'into') return
      let anchorId: string | null = null
      if (targetRow.kind === 'folder') anchorId = targetRow.folder.id
      else if (targetRow.depth === 0) anchorId = targetRow.ws.id
      else anchorId = targetRow.ws.folderId ?? null
      if (!anchorId || anchorId === dragRow.folder.id) return
      onSidebarMutation((s) => moveEntryRelative(s, dragRow.folder.id, anchorId!, zone))
      return
    }

    const wsId = dragRow.ws.id
    if (zone === 'into') {
      if (targetRow.kind !== 'folder') return
      onSidebarMutation((s) => moveToFolderEnd(s, wsId, targetRow.folder.id))
      return
    }
    if (targetRow.kind === 'folder') {
      onSidebarMutation((s) => moveEntryRelative(setWorkspaceFolder(s, wsId, null), wsId, targetRow.folder.id, zone))
      return
    }
    if (targetRow.depth === 0) {
      onSidebarMutation((s) => {
        const out = setWorkspaceFolder(s, wsId, null)
        const flat = placeAdjacentFlat(out, wsId, targetRow.ws.id, zone)
        return moveEntryRelative(flat, wsId, targetRow.ws.id, zone)
      })
      return
    }
    const fid = targetRow.ws.folderId ?? null
    onSidebarMutation((s) => placeAdjacentFlat(setWorkspaceFolder(s, wsId, fid), wsId, targetRow.ws.id, zone))
  }, [dragIndex, dropTarget, rows, onSidebarMutation])

  const onPointerUp = useCallback(
    (e: PointerEvent<HTMLDivElement>) => {
      ;(e.target as HTMLElement).releasePointerCapture?.(e.pointerId)
      performDrop()
      setDragIndex(null)
      setDropTarget(null)
      setDragOffset(0)
      dragStartYRef.current = 0
    },
    [performDrop],
  )

  const clearPreviewTimers = useCallback(() => {
    if (previewShowTimer.current) {
      clearTimeout(previewShowTimer.current)
      previewShowTimer.current = null
    }
    if (previewHideTimer.current) {
      clearTimeout(previewHideTimer.current)
      previewHideTimer.current = null
    }
  }, [])

  useEffect(() => clearPreviewTimers, [clearPreviewTimers])

  const hidePreview = useCallback((immediate = false) => {
    if (previewShowTimer.current) {
      clearTimeout(previewShowTimer.current)
      previewShowTimer.current = null
    }
    if (!previewVisibleRef.current) return
    if (immediate) {
      if (previewHideTimer.current) {
        clearTimeout(previewHideTimer.current)
        previewHideTimer.current = null
      }
      previewVisibleRef.current = false
      setPreviewWsId(null)
      setPreviewAnchor(null)
      return
    }
    if (previewHideTimer.current) return
    previewHideTimer.current = setTimeout(() => {
      previewHideTimer.current = null
      previewVisibleRef.current = false
      setPreviewWsId(null)
      setPreviewAnchor(null)
    }, PREVIEW_HIDE_GRACE_MS)
  }, [])

  const showPreview = useCallback((wsId: string, rowEl: HTMLElement | null) => {
    if (previewHideTimer.current) {
      clearTimeout(previewHideTimer.current)
      previewHideTimer.current = null
    }
    const rect = rowEl?.getBoundingClientRect()
    setPreviewAnchor({
      top: rect?.top ?? 0,
      edge: rect ? (isRight ? rect.left : rect.right) : 0,
      side: isRight ? 'left' : 'right',
    })
    setPreviewWsId(wsId)
    previewVisibleRef.current = true
  }, [isRight])

  // Rows the preview must not trigger for: mobile (no hover) and while a
  // drag-reorder or context menu is active. The active workspace previews
  // too — as a static image snapshot instead of a second live terminal grid.
  const previewSuppressed = isMobile || dragIndex !== null || !!contextMenu || !!folderContextMenu || !!generalContextMenu

  const handleRowMouseEnter = useCallback(
    (wsId: string, rowEl: HTMLElement | null) => {
      if (previewSuppressed) return
      if (previewVisibleRef.current) {
        // A thumbnail is already up — browsing across rows swaps it
        // instantly instead of waiting for the hover delay again.
        if (previewShowTimer.current) {
          clearTimeout(previewShowTimer.current)
          previewShowTimer.current = null
        }
        showPreview(wsId, rowEl)
        return
      }
      if (previewShowTimer.current) clearTimeout(previewShowTimer.current)
      previewShowTimer.current = setTimeout(() => {
        previewShowTimer.current = null
        showPreview(wsId, rowEl)
      }, PREVIEW_HOVER_DELAY_MS)
    },
    [previewSuppressed, showPreview],
  )

  const handleRowsMouseLeave = useCallback(() => {
    hidePreview()
  }, [hidePreview])

  const handleRowSelect = useCallback(
    (wsId: string) => {
      hidePreview(true)
      onSelectWorkspace(wsId)
    },
    [hidePreview, onSelectWorkspace],
  )

  const renderContextMenuPortal = (menu: { x: number; y: number; workspaceId: string } | null) => {
    if (!menu) return null
    const ws = workspaces.find((w) => w.id === menu.workspaceId)
    if (!ws) return null
    return (
      <WorkspaceContextMenu
        x={menu.x}
        y={menu.y}
        ws={ws}
        folders={allFolders}
        onEdit={() => { setContextMenu(null); setEditTarget(ws) }}
        onDelete={() => { setContextMenu(null); onDeleteWorkspace(ws.id) }}
        onMoveToFolder={(fid) => { setContextMenu(null); moveToFolderCb(ws.id, fid) }}
        onRemoveFromFolder={() => { setContextMenu(null); moveToFolderCb(ws.id, null) }}
        onNewFolder={() => { setContextMenu(null); setFolderDialog({ mode: 'create', assignWsId: ws.id }) }}
      />
    )
  }

  const renderFolderContextMenuPortal = () => {
    if (!folderContextMenu) return null
    const folder = allFolders.find((f) => f.id === folderContextMenu.folderId)
    if (!folder) return null
    return createPortal(
      <div
        className="fixed z-50 w-40 rounded-md border border-border bg-popover shadow-md py-0.5 smart-context-menu"
        style={{ left: folderContextMenu.x, top: folderContextMenu.y }}
        onMouseDown={(e) => e.stopPropagation()}
        onContextMenu={(e) => e.preventDefault()}
      >
        <button
          onClick={(e) => { e.stopPropagation(); setFolderContextMenu(null); setFolderDialog({ mode: 'edit', folder }) }}
          className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
        >
          <Pencil className="h-3.5 w-3.5" />
          Edit folder
        </button>
        <button
          onClick={(e) => { e.stopPropagation(); setFolderContextMenu(null); onDeleteFolder?.(folder.id) }}
          className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
        >
          <Trash2 className="h-3.5 w-3.5" />
          Delete folder
        </button>
      </div>,
      document.body,
    )
  }

  const renderFolderDialog = () => {
    if (!folderDialog) return null
    return (
      <WorkspaceFolderDialog
        open={true}
        onOpenChange={() => setFolderDialog(null)}
        initialName={folderDialog.mode === 'edit' ? folderDialog.folder.name : 'New Folder'}
        initialEmoji={folderDialog.mode === 'edit' ? folderDialog.folder.emoji || '' : ''}
        onSave={handleFolderSave}
      />
    )
  }

  if (collapsed) {
    return (
      <div
        className={`flex h-full w-full flex-col bg-background overflow-hidden workspace-panel ${isRight ? 'border-l border-border' : 'border-r border-border'}`}
        onContextMenu={(e) => {
          e.preventDefault()
          setGeneralContextMenu({ x: e.clientX, y: e.clientY })
        }}
      >
        <div className="flex items-center justify-center border-b border-border h-[33px] select-none bg-background">
          <Button
            variant="ghost"
            size="icon"
            className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={onToggle}
            title="Show sidebar"
          >
            {isRight ? <PanelRight className="h-3.5 w-3.5" /> : <PanelLeft className="h-3.5 w-3.5" />}
          </Button>
        </div>
        <div className="flex flex-col items-center flex-1 overflow-y-auto" onMouseLeave={handleRowsMouseLeave}>
          {workspaces.map((ws, i) => (
            <div
              key={ws.id}
              className={`flex items-center ${isRight ? 'flex-row-reverse' : ''} h-[33px] w-full transition-colors ${
                ws.id === activeWorkspaceId ? 'bg-accent/70' : 'hover:bg-accent/40'
              }`}
              onMouseEnter={(e) => handleRowMouseEnter(ws.id, e.currentTarget)}
            >
              <button
                onClick={() => handleRowSelect(ws.id)}
                onContextMenu={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  setContextMenu({ x: e.clientX, y: e.clientY, workspaceId: ws.id })
                }}
                className="flex items-center justify-center flex-1 text-base"
              >
                {ws.emoji || commonEmojis[i % commonEmojis.length]}
              </button>
              {isMobile && (
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
                    setContextMenu({ x: rect.left, y: rect.bottom + 2, workspaceId: ws.id })
                  }}
                  className="flex items-center justify-center h-5 w-5 shrink-0 text-muted-foreground/40 hover:text-foreground"
                >
                  <MoreVertical className="h-3 w-3" />
                </button>
              )}
            </div>
          ))}
        </div>
        {renderContextMenuPortal(contextMenu)}
        {previewWsId && previewAnchor && (() => {
          const ws = workspaces.find((w) => w.id === previewWsId)
          // No thumbnail for the workspace that's already open in the main area.
          if (!ws || ws.id === activeWorkspaceId) return null
          const idx = workspaces.indexOf(ws)
          return (
            <WorkspacePreview
              workspace={ws}
              emoji={ws.emoji || commonEmojis[idx % commonEmojis.length]}
              title={ws.name || ws.path || 'Workspace'}
              anchor={previewAnchor}
            />
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
        {renderFolderDialog()}
      </div>
    )
  }

  return (
    <div
      className="flex h-full flex-col bg-background select-none workspace-panel"
      onContextMenu={(e) => {
        e.preventDefault()
        setGeneralContextMenu({ x: e.clientX, y: e.clientY })
      }}
    >
      {!noHeader && (
        <div className={`flex items-center gap-2 border-b border-border h-[33px] shrink-0 bg-secondary/20 ${isRight ? 'flex-row-reverse px-3' : 'px-3'}`}>
          <Button
            variant="ghost"
            size="icon"
            className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={onToggle}
            title="Hide sidebar"
          >
            {isRight ? <PanelRightClose className="h-3.5 w-3.5" /> : <PanelLeftClose className="h-3.5 w-3.5" />}
          </Button>
          <span className="flex-1 text-xs font-semibold text-muted-foreground truncate">
            Workspaces
          </span>
          <Button
            variant="ghost"
            size="icon"
            className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => setPickerOpen(true)}
            title="Add workspace"
          >
            <Plus className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}

      {baseRows.length === 0 ? (
        <div className="flex-1 flex items-center justify-center">
          <p className="text-xs text-muted-foreground italic">No workspaces.</p>
        </div>
      ) : (
        <ScrollArea className="flex-1">
          <div onMouseLeave={handleRowsMouseLeave}>
            {rows.map((row, i) => {
              const isDragging = dragIndex === i
              const isDropTarget = dropTarget !== null && dropTarget.index === i && dragIndex !== null && dragIndex !== i
              const isBefore = isDropTarget && dropTarget!.zone === 'before'
              const isAfter = isDropTarget && dropTarget!.zone === 'after'
              const isInto = isDropTarget && dropTarget!.zone === 'into'
              const baseClasses = `group flex items-center gap-1.5 pr-3 py-1.5 text-sm select-none transition-transform duration-150 border-t border-border ${
                i === 0 ? 'border-t-0' : ''
              } ${isBefore ? 'border-t-2 border-t-primary' : ''} ${isAfter ? 'border-b-2 border-b-primary' : ''} ${
                isDragging ? 'opacity-60 z-10' : ''
              }`
              const dragStyle = {
                userSelect: 'none' as const,
                ...(isDragging ? { transform: `translateY(${dragOffset}px)`, transition: 'none' } : {}),
              }
              const pointerHandlers = {
                onPointerDown: (e: PointerEvent<HTMLDivElement>) => { hidePreview(true); onPointerDown(e, i) },
                onPointerMove: (e: PointerEvent<HTMLDivElement>) => onPointerMove(e, i),
                onPointerUp: (e: PointerEvent<HTMLDivElement>) => onPointerUp(e),
              }

              if (row.kind === 'folder') {
                return (
                  <div
                    key={`folder-${row.folder.id}`}
                    ref={(el) => { itemRefs.current[i] = el }}
                    onClick={() => toggleFolder(row.folder.id)}
                    onContextMenu={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      setFolderContextMenu({ x: e.clientX, y: e.clientY, folderId: row.folder.id })
                    }}
                    {...pointerHandlers}
                    className={`${baseClasses} pl-2 cursor-pointer ${
                      isInto ? 'bg-accent/70 ring-1 ring-inset ring-primary' : 'hover:bg-accent/40 text-muted-foreground'
                    }`}
                    style={dragStyle}
                  >
                    <ChevronRight
                      className={`h-3 w-3 shrink-0 text-muted-foreground transition-transform ${row.collapsed ? '' : 'rotate-90'}`}
                    />
                    <span className="text-base leading-none shrink-0">{row.folder.emoji || '\u{1F4C1}'}</span>
                    <span className="truncate flex-1 font-medium">{row.folder.name}</span>
                    <div className="relative flex h-5 w-5 shrink-0 items-center justify-center">
                      <div className={`absolute inset-0 transition-opacity ${isMobile ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}`}>
                        <FolderMenu
                          onEdit={() => setFolderDialog({ mode: 'edit', folder: row.folder })}
                          onDelete={() => onDeleteFolder?.(row.folder.id)}
                        />
                      </div>
                    </div>
                  </div>
                )
              }

              const ws = row.ws
              const isActive = ws.id === activeWorkspaceId
              const label = ws.name || ws.path || 'Workspace'
              const flatIdx = workspaces.indexOf(ws)
              // Aggregate the strongest agent status across all panes in this
              // workspace (leaf ids = session ids). Used for the roll-up dot.
              const wsStatuses: AgentStatus[] = []
              for (const tab of ws.layouts) {
                for (const leafId of collectLeafIds(tab.layout)) {
                  const s = statuses[leafId]
                  if (s) wsStatuses.push(s)
                }
              }
              const strongest = getStrongestStatus(wsStatuses)
              const hasAnyAgent = wsStatuses.length > 0
              const dotColors = strongest ? getAgentStatusDot(strongest) : null
              return (
                <div
                  key={ws.id}
                  ref={(el) => { itemRefs.current[i] = el }}
                  onClick={() => handleRowSelect(ws.id)}
                  onMouseEnter={(e) => handleRowMouseEnter(ws.id, e.currentTarget)}
                  onContextMenu={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    setContextMenu({ x: e.clientX, y: e.clientY, workspaceId: ws.id })
                  }}
                  {...pointerHandlers}
                  className={`${baseClasses} ${row.depth > 0 ? 'pl-7' : 'pl-2'} ${
                    isActive ? 'bg-accent/70 text-accent-foreground' : 'hover:bg-accent/40 text-muted-foreground'
                  }`}
                  style={dragStyle}
                >
                  <span className="text-base leading-none shrink-0">{ws.emoji || commonEmojis[flatIdx % commonEmojis.length]}</span>
                  <span className="truncate flex-1">{label}</span>
                  {/* Status dot + three-dots share the exact same 20x20 slot.
                      On hover the dot fades out and the kebab fades in, both
                      pinned to the right edge so they stay aligned with the
                      "Add workspace" + button in the header (px-3). */}
                  <div className="relative flex h-5 w-5 shrink-0 items-center justify-center">
                    <span
                      className={`absolute inset-0 flex items-center justify-center transition-opacity duration-150 ${
                        isMobile ? 'opacity-0' : 'opacity-100 group-hover:opacity-0'
                      }`}
                    >
                      {dotColors ? (
                        <span className="relative flex" style={{ height: 7, width: 7 }}>
                          {dotColors.ring && (
                            <span className={`animate-ping absolute inline-flex h-full w-full rounded-full ${dotColors.ring} opacity-75`} />
                          )}
                          <span className={`relative inline-flex rounded-full ${dotColors.dot}`} style={{ height: 7, width: 7 }} />
                        </span>
                      ) : hasAnyAgent ? (
                        <span className="relative inline-flex rounded-full bg-slate-400" style={{ height: 7, width: 7 }} />
                      ) : (
                        <span className="relative inline-flex rounded-full bg-slate-400 opacity-25" style={{ height: 7, width: 7 }} />
                      )}
                    </span>
                    <div className={`absolute inset-0 transition-opacity ${isMobile ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}`}>
                      <WorkspaceMenu
                        onDelete={() => onDeleteWorkspace(ws.id)}
                        onEdit={() => setEditTarget(ws)}
                        folders={allFolders}
                        currentFolderId={ws.folderId ?? null}
                        onMoveToFolder={(fid) => moveToFolderCb(ws.id, fid)}
                        onRemoveFromFolder={() => moveToFolderCb(ws.id, null)}
                        onNewFolder={() => setFolderDialog({ mode: 'create', assignWsId: ws.id })}
                      />
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        </ScrollArea>
      )}

      {renderContextMenuPortal(contextMenu)}

      {renderFolderContextMenuPortal()}

      {generalContextMenu && (() => {
        return createPortal(
          <div
            className="fixed z-50 w-40 rounded-md border border-border bg-popover shadow-md py-0.5 smart-context-menu"
            style={{ left: generalContextMenu.x, top: generalContextMenu.y }}
            onMouseDown={(e) => e.stopPropagation()}
            onContextMenu={(e) => e.preventDefault()}
          >
            <button
              onClick={(e) => { e.stopPropagation(); setGeneralContextMenu(null); setPickerOpen(true) }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
            >
              <Plus className="h-3.5 w-3.5" />
              New Workspace
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); setGeneralContextMenu(null); setFolderDialog({ mode: 'create' }) }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
            >
              <FolderPlus className="h-3.5 w-3.5" />
              New Folder
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); setGeneralContextMenu(null); onOpenSettings?.() }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
            >
              <Settings className="h-3.5 w-3.5" />
              Settings
            </button>
          </div>,
          document.body
        )
      })()}

      {previewWsId && previewAnchor && (() => {
        const ws = workspaces.find((w) => w.id === previewWsId)
        // No thumbnail for the workspace that's already open in the main area.
        if (!ws || ws.id === activeWorkspaceId) return null
        const idx = workspaces.indexOf(ws)
        return (
          <WorkspacePreview
            workspace={ws}
            emoji={ws.emoji || commonEmojis[idx % commonEmojis.length]}
            title={ws.name || ws.path || 'Workspace'}
            anchor={previewAnchor}
          />
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

      {renderFolderDialog()}
    </div>
  )
}
