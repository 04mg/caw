import React, { useEffect, useState, useCallback, useRef, useLayoutEffect } from 'react'
import {
  ChevronRight, ChevronDown, Folder, FolderOpen, Loader2,
  RefreshCw, FileCode, Pencil, Trash2, Copy, Download,
  ClipboardPaste, FolderPlus, MoreVertical, PanelRightClose
} from 'lucide-react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { subscribeToFileTree, type FileTreeEvent } from '@/lib/fileTreeWs'

interface FileNode {
  name: string
  path: string
  isDir: boolean
}

interface FolderSidebarProps {
  workspacePath: string
  onOpenFile: (path: string) => void
  gitStatuses: Record<string, string>
  onRefresh: () => void
  noHeader?: boolean
  mainWorkspacePath?: string
  onClose?: () => void
}

export function FolderSidebar({
  workspacePath,
  onOpenFile,
  gitStatuses,
  onRefresh,
  noHeader,
  mainWorkspacePath,
  onClose,
}: FolderSidebarProps) {
  const [loading, setLoading] = useState(false)
  const isWorktree = !!(mainWorkspacePath && workspacePath && workspacePath !== mainWorkspacePath)
  const [busy, setBusy] = useState(false)
  const [contextMenu, setContextMenu] = useState<{
    x: number; y: number; path: string; name: string; isDir: boolean; isRoot?: boolean
  } | null>(null)
  const [clipboard, setClipboard] = useState<{ path: string } | null>(null)
  const [editingPath, setEditingPath] = useState<string | null>(null)
  const [createTarget, setCreateTarget] = useState<{
    parentPath: string; type: 'file' | 'dir'
  } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{
    path: string; name: string; isDir: boolean
  } | null>(null)
  const [dragOverPath, setDragOverPath] = useState<string | null>(null)
  const [refreshCounter, setRefreshCounter] = useState(0)
  const contextMenuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!contextMenu) return
    const onDown = (e: MouseEvent) => {
      if (contextMenuRef.current && !contextMenuRef.current.contains(e.target as Node)) {
        setContextMenu(null)
      }
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [contextMenu])

  const triggerRefresh = useCallback(() => {
    setRefreshCounter((c) => c + 1)
  }, [])

  const handleRefresh = async () => {
    setLoading(true)
    await onRefresh()
    triggerRefresh()
    setLoading(false)
  }

  const handleRenameSubmit = useCallback(async (oldPath: string, newName: string) => {
    setBusy(true)
    const sep = oldPath.includes('\\') ? '\\' : '/'
    const parentDir = oldPath.substring(0, oldPath.lastIndexOf(sep))
    const newPath = parentDir + sep + newName
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ oldPath, newPath }),
      })
      if (res.ok) {
        setEditingPath(null)
        triggerRefresh()
        onRefresh()
      }
    } catch { /* ignore */ }
    setBusy(false)
  }, [triggerRefresh, onRefresh])

  const handleCreateSubmit = useCallback(async (parentPath: string, name: string, type: 'file' | 'dir') => {
    setBusy(true)
    const sep = parentPath.includes('\\') ? '\\' : '/'
    const newPath = parentPath + sep + name
    setCreateTarget(null)
    try {
      await fetch('/api/workspaces/files', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: newPath, type }),
      })
    } catch { /* ignore */ }
    triggerRefresh()
    onRefresh()
    setBusy(false)
  }, [triggerRefresh, onRefresh])

  const handleDeleteConfirm = useCallback(async () => {
    const target = deleteTarget
    if (!target) return
    setDeleteTarget(null)
    setBusy(true)
    try {
      await fetch('/api/workspaces/files', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: target.path }),
      })
    } catch { /* ignore */ }
    triggerRefresh()
    onRefresh()
    setBusy(false)
  }, [deleteTarget, triggerRefresh, onRefresh])

  const handleCopy = useCallback((path: string) => {
    setClipboard({ path })
    setContextMenu(null)
  }, [])

  const handlePaste = useCallback(async (targetDir: string) => {
    const src = clipboard?.path
    if (!src) return
    setContextMenu(null)
    setBusy(true)
    try {
      await fetch('/api/workspaces/files', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sourcePath: src, targetDir }),
      })
    } catch { /* ignore */ }
    triggerRefresh()
    onRefresh()
    setBusy(false)
  }, [clipboard, triggerRefresh, onRefresh])

  const handleUpload = useCallback(async (targetDir: string, files: FileList) => {
    setBusy(true)
    for (let i = 0; i < files.length; i++) {
      const file = files[i]
      const formData = new FormData()
      formData.append('targetDir', targetDir)
      formData.append('file', file)
      try {
        await fetch('/api/workspaces/files', { method: 'POST', body: formData })
      } catch { /* ignore */ }
    }
    triggerRefresh()
    setBusy(false)
  }, [triggerRefresh])

  const handleDragOver = useCallback((e: React.DragEvent, path: string) => {
    e.preventDefault()
    e.stopPropagation()
    setDragOverPath(path)
  }, [])

  const handleDragLeave = useCallback(() => {
    setDragOverPath(null)
  }, [])

  const handleDrop = useCallback((e: React.DragEvent, targetDir: string) => {
    e.preventDefault()
    e.stopPropagation()
    setDragOverPath(null)
    if (e.dataTransfer.files.length > 0) {
      handleUpload(targetDir, e.dataTransfer.files)
    }
  }, [handleUpload])

  const showContextMenu = useCallback((path: string, name: string, isDir: boolean, x: number, y: number) => {
    setContextMenu({ x, y, path, name, isDir })
  }, [])

  // Debounce file tree re-fetch to avoid cascading refreshes
  // when many files change rapidly (e.g. SQLite writes).
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (!workspacePath) return

    const handleEvent = (_event: FileTreeEvent) => {
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
      debounceTimerRef.current = setTimeout(() => {
        triggerRefresh()
        onRefresh?.()
      }, 400)
    }

    const unsub = subscribeToFileTree(workspacePath, handleEvent)
    return () => {
      unsub()
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    }
  }, [workspacePath, triggerRefresh, onRefresh])

  return (
    <div className="flex h-full flex-col bg-background select-none border-l border-border">
      {!noHeader && (
      <div className="flex items-center gap-2 border-b border-border px-3 h-[33px] shrink-0 bg-secondary/20">
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 hover:text-foreground"
          onClick={handleRefresh}
          disabled={loading}
          title="Refresh files"
        >
          <RefreshCw className={`h-3 w-3 ${loading ? 'animate-spin' : ''}`} />
        </Button>
        <span className="flex-1 text-xs font-medium text-muted-foreground truncate">
          Explorer
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 hover:text-foreground"
          onClick={onClose}
          title="Close Sidebar"
        >
          <PanelRightClose className="h-3.5 w-3.5" />
        </Button>
      </div>
      )}

      <ScrollArea
        className="relative flex-1"
        onContextMenu={(e) => {
          if (!workspacePath) return
          e.preventDefault()
          const rootName = workspacePath.split(/[\\/]/).filter(Boolean).pop() || workspacePath
          setContextMenu({ x: e.clientX, y: e.clientY, path: workspacePath, name: rootName, isDir: true, isRoot: true })
        }}
        onDragOver={(e) => {
          if (workspacePath) handleDragOver(e, workspacePath)
        }}
        onDragLeave={handleDragLeave}
        onDrop={(e) => {
          if (workspacePath) handleDrop(e, workspacePath)
        }}
      >
        {busy && (
          <div className="absolute inset-0 z-20 flex items-center justify-center bg-background/60">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        )}
        <div className={dragOverPath === workspacePath ? 'ring-1 ring-primary rounded-sm' : ''}>
          {workspacePath ? (
            <LazyFileNode
              name={
                isWorktree
                  ? `Worktree: ${workspacePath.split(/[\\/]/).filter(Boolean).pop() || workspacePath}`
                  : (workspacePath.split(/[\\/]/).filter(Boolean).pop() || workspacePath)
              }
              path={workspacePath}
              isDir={true}
              depth={0}
              startExpanded
              onOpenFile={onOpenFile}
              gitStatuses={gitStatuses}
              editingPath={editingPath}
              createTarget={createTarget}
              clipboard={clipboard}
              dragOverPath={dragOverPath}
              refreshCounter={refreshCounter}
              onShowContextMenu={showContextMenu}
              onRenameSubmit={handleRenameSubmit}
              onCancelRename={() => setEditingPath(null)}
              onStartRename={(p) => setEditingPath(p)}
              onCreateSubmit={handleCreateSubmit}
              onCreateCancel={() => setCreateTarget(null)}
              onStartCreate={(parentPath, type) => setCreateTarget({ parentPath, type })}
              onCopy={handleCopy}
              onPaste={handlePaste}
              onDelete={(path, name, isDir) => { setContextMenu(null); setDeleteTarget({ path, name, isDir }) }}
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDropFiles={handleDrop}
              onUpload={handleUpload}
            />
          ) : (
            <p className="text-xs text-muted-foreground italic text-center mt-4">
              No workspace open.
            </p>
          )}
        </div>
      </ScrollArea>

      {contextMenu && (
        <SmartContextMenu x={contextMenu.x} y={contextMenu.y} ref={contextMenuRef}>
          {contextMenu.isDir && (
            <>
              <button
                onClick={(e) => { e.stopPropagation(); const p = contextMenu.path; setContextMenu(null); setCreateTarget({ parentPath: p, type: 'file' }) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <FileCode className="h-3.5 w-3.5" />
                New File
              </button>
              <button
                onClick={(e) => { e.stopPropagation(); const p = contextMenu.path; setContextMenu(null); setCreateTarget({ parentPath: p, type: 'dir' }) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <FolderPlus className="h-3.5 w-3.5" />
                New Folder
              </button>
              <div className="border-t border-border my-0.5" />
            </>
          )}
          {!contextMenu.isRoot && (
            <>
              <button
                onClick={(e) => { e.stopPropagation(); setContextMenu(null); setEditingPath(contextMenu.path) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Pencil className="h-3.5 w-3.5" />
                Rename
              </button>
              <button
                onClick={(e) => { e.stopPropagation(); handleCopy(contextMenu.path) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Copy className="h-3.5 w-3.5" />
                Copy
              </button>
            </>
          )}
          {clipboard && contextMenu.isDir && (
            <button
              onClick={(e) => { e.stopPropagation(); handlePaste(contextMenu.path) }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
            >
              <ClipboardPaste className="h-3.5 w-3.5" />
              Paste
            </button>
          )}
          {!contextMenu.isDir && (
            <>
              <button
                onClick={(e) => { e.stopPropagation(); setContextMenu(null); const a = document.createElement('a'); a.href = '/api/workspaces/files?download=true&path=' + encodeURIComponent(contextMenu.path); a.download = contextMenu.name; document.body.appendChild(a); a.click(); document.body.removeChild(a) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Download className="h-3.5 w-3.5" />
                Download
              </button>
            </>
          )}
          {!contextMenu.isRoot && (
            <>
              <div className="border-t border-border my-0.5" />
              <button
                onClick={() => { setContextMenu(null); setDeleteTarget({ path: contextMenu.path, name: contextMenu.name, isDir: contextMenu.isDir }) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete
              </button>
            </>
          )}
        </SmartContextMenu>
      )}

      <DeleteDialog
        target={deleteTarget}
        onConfirm={handleDeleteConfirm}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}

const SmartContextMenu = React.forwardRef<HTMLDivElement, { x: number; y: number; children: React.ReactNode }>(({ x, y, children }, ref) => {
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
      className="fixed z-50 w-44 rounded-md border border-border bg-popover shadow-md py-0.5"
      style={{ left: pos.left, top: pos.top }}
    >
      {children}
    </div>
  )
})

function DeleteDialog({
  target,
  onConfirm,
  onCancel,
}: {
  target: { path: string; name: string; isDir: boolean } | null
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <Dialog open={!!target} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete {target?.isDir ? 'Folder' : 'File'}</DialogTitle>
          <DialogDescription>
            Are you sure you want to delete <span className="font-medium text-foreground">{target?.name}</span>?
            {target?.isDir && <span className="block mt-1">All contents inside will be permanently removed.</span>}
          </DialogDescription>
        </DialogHeader>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            Delete
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface LazyFileNodeProps {
  name: string
  path: string
  isDir: boolean
  depth: number
  startExpanded?: boolean
  onOpenFile: (path: string) => void
  gitStatuses: Record<string, string>
  editingPath: string | null
  createTarget: { parentPath: string; type: 'file' | 'dir' } | null
  clipboard: { path: string } | null
  dragOverPath: string | null
  refreshCounter: number
  onShowContextMenu: (path: string, name: string, isDir: boolean, x: number, y: number) => void
  onRenameSubmit: (oldPath: string, newName: string) => void
  onCancelRename: () => void
  onStartRename: (path: string) => void
  onCreateSubmit: (parentPath: string, name: string, type: 'file' | 'dir') => void
  onCreateCancel: () => void
  onStartCreate: (parentPath: string, type: 'file' | 'dir') => void
  onCopy: (path: string) => void
  onPaste: (targetDir: string) => void
  onDelete: (path: string, name: string, isDir: boolean) => void
  onDragOver: (e: React.DragEvent, path: string) => void
  onDragLeave: () => void
  onDropFiles: (e: React.DragEvent, targetDir: string) => void
  onUpload: (targetDir: string, files: FileList) => void
}

function LazyFileNode({
  name,
  path,
  isDir,
  depth,
  startExpanded = false,
  onOpenFile,
  gitStatuses,
  editingPath,
  createTarget,
  clipboard,
  dragOverPath,
  refreshCounter,
  onShowContextMenu,
  onRenameSubmit,
  onCancelRename,
  onStartRename,
  onCreateSubmit,
  onCreateCancel,
  onStartCreate,
  onCopy,
  onPaste,
  onDelete,
  onDragOver,
  onDragLeave,
  onDropFiles,
  onUpload,
}: LazyFileNodeProps) {
  const [expanded, setExpanded] = useState(startExpanded)
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [children, setChildren] = useState<FileNode[]>([])
  const [localEditValue, setLocalEditValue] = useState(name)
  const [localCreateValue, setLocalCreateValue] = useState('')
  const editInputRef = useRef<HTMLInputElement>(null)
  const createInputRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await fetch(`/api/workspaces/contents?path=${encodeURIComponent(path)}`)
      if (res.ok) {
        const arr = await res.json()
        if (Array.isArray(arr)) {
          const sorted = (arr as FileNode[]).sort((a, b) => {
            if (a.isDir && !b.isDir) return -1
            if (!a.isDir && b.isDir) return 1
            return a.name.localeCompare(b.name)
          })
          setChildren(sorted)
        } else {
          setChildren([])
        }
      }
    } catch {
      setChildren([])
    } finally {
      setLoaded(true)
      setLoading(false)
    }
  }, [path])

  // Load on first mount if startExpanded
  const initialLoadDone = useRef(false)
  useEffect(() => {
    if (startExpanded && !initialLoadDone.current) {
      initialLoadDone.current = true
      load()
    }
  }, [startExpanded, load])

  // Re-fetch children when refreshCounter changes.
  // Only fires on refreshCounter change (not on toggle), captures current expanded state.
  useEffect(() => {
    if (expanded) {
      load()
    } else {
      setLoaded(false)
      setChildren([])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshCounter])

  const isCreating = createTarget?.parentPath === path
  useEffect(() => {
    if (isCreating && !expanded) {
      setExpanded(true)
      if (!loaded) load()
    }
  }, [isCreating, expanded, loaded, load])

  useEffect(() => {
    if (editingPath === path && editInputRef.current) {
      editInputRef.current.focus()
      editInputRef.current.select()
    }
  }, [editingPath, path])

  useEffect(() => {
    if (isCreating && createInputRef.current) {
      createInputRef.current.focus()
    }
  }, [isCreating])

  useEffect(() => {
    if (editingPath === path) {
      setLocalEditValue(name)
    }
  }, [editingPath, path, name])

  const toggle = () => {
    if (loading) return
    setExpanded((e) => {
      const next = !e
      if (next && !loaded) load()
      return next
    })
  }

  const handleEditKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      onRenameSubmit(path, localEditValue)
    } else if (e.key === 'Escape') {
      onCancelRename()
    }
  }

  const handleCreateKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (localCreateValue.trim()) {
        onCreateSubmit(path, localCreateValue.trim(), createTarget!.type)
        setLocalCreateValue('')
      }
    } else if (e.key === 'Escape') {
      onCreateCancel()
      setLocalCreateValue('')
    }
  }

  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    onShowContextMenu(path, name, isDir, e.clientX, e.clientY)
  }

  const statusXY = gitStatuses[path] || ''
  const isModified = statusXY.includes('M')
  const isUntracked = statusXY.includes('?')
  const isAdded = statusXY.includes('A')

  let statusBadge = null
  let textClass = ''
  if (isModified) {
    textClass = 'text-yellow-500'
    statusBadge = (
      <span className="text-[9px] font-bold text-yellow-600 bg-yellow-500/15 border border-yellow-500/30 px-1 rounded shrink-0" title="Modified">
        M
      </span>
    )
  } else if (isAdded) {
    textClass = 'text-green-500'
    statusBadge = (
      <span className="text-[9px] font-bold text-green-600 bg-green-500/15 border border-green-500/30 px-1 rounded shrink-0" title="Added">
        A
      </span>
    )
  } else if (isUntracked) {
    textClass = 'text-red-400'
    statusBadge = (
      <span className="text-[9px] font-bold text-red-500 bg-red-500/15 border border-red-500/30 px-1 rounded shrink-0" title="Untracked">
        U
      </span>
    )
  }

  const isDragOver = dragOverPath === path && isDir

  return (
    <div className="w-full">
      <div
        className={`flex items-center ${isDragOver ? 'bg-accent/30 ring-1 ring-primary rounded-sm' : ''}`}
        onContextMenu={handleContextMenu}
        onDragOver={(e) => { if (isDir) onDragOver(e, path) }}
        onDragLeave={onDragLeave}
        onDrop={(e) => { if (isDir) onDropFiles(e, path) }}
      >
        <button
          onClick={() => {
            if (isDir) {
              toggle()
            } else {
              onOpenFile(path)
            }
          }}
          className={`group flex w-full items-center gap-1.5 px-2.5 py-1 text-xs hover:bg-accent/40 text-left select-none border-b border-transparent hover:border-accent/10 transition-colors ${textClass} ${isDragOver ? 'bg-accent/20' : ''}`}
          style={{ paddingLeft: `${depth * 12 + 10}px` }}
        >
          {isDir ? (
            <>
              {expanded ? (
                <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/75" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/75" />
              )}
              {expanded ? (
                <FolderOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
              ) : (
                <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
              )}
            </>
          ) : (
            <>
              <span className="w-3.5" />
              <FileCode className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
            </>
          )}

          {editingPath === path ? (
            <input
              ref={editInputRef}
              value={localEditValue}
              onChange={(e) => setLocalEditValue(e.target.value)}
              onKeyDown={handleEditKeyDown}
              onBlur={() => { if (localEditValue.trim() && localEditValue !== name) onRenameSubmit(path, localEditValue); else onCancelRename() }}
              className="flex-1 min-w-0 bg-background border border-border rounded px-1 py-0 text-xs outline-none focus:ring-1 focus:ring-ring"
              onClick={(e) => e.stopPropagation()}
            />
          ) : (
            <span className="truncate flex-1 font-medium text-muted-foreground">{name}</span>
          )}

          {loading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground/50 shrink-0" />}
          {statusBadge}

          <button
            onClick={(e) => {
              e.stopPropagation()
              onShowContextMenu(path, name, isDir, e.currentTarget.getBoundingClientRect().right - 140, e.currentTarget.getBoundingClientRect().bottom + 2)
            }}
            className="h-5 w-5 rounded text-muted-foreground/40 hover:text-foreground hover:bg-accent/40 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
            title="More"
          >
            <MoreVertical className="h-3 w-3" />
          </button>
        </button>
      </div>

      {isDir && expanded && loaded && (
        <div className="w-full">
          {children.map((child) => (
            <LazyFileNode
              key={child.path}
              name={child.name}
              path={child.path}
              isDir={child.isDir}
              depth={depth + 1}
              onOpenFile={onOpenFile}
              gitStatuses={gitStatuses}
              editingPath={editingPath}
              createTarget={createTarget}
              clipboard={clipboard}
              dragOverPath={dragOverPath}
              refreshCounter={refreshCounter}
              onShowContextMenu={onShowContextMenu}
              onRenameSubmit={onRenameSubmit}
              onCancelRename={onCancelRename}
              onStartRename={onStartRename}
              onCreateSubmit={onCreateSubmit}
              onCreateCancel={onCreateCancel}
              onStartCreate={onStartCreate}
              onCopy={onCopy}
              onPaste={onPaste}
              onDelete={onDelete}
              onDragOver={onDragOver}
              onDragLeave={onDragLeave}
              onDropFiles={onDropFiles}
              onUpload={onUpload}
            />
          ))}

          {isCreating && (
            <div className="flex items-center gap-1.5 px-2.5 py-1 text-xs" style={{ paddingLeft: `${(depth + 1) * 12 + 10}px` }}>
              {createTarget?.type === 'dir' ? (
                <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
              ) : (
                <FileCode className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
              )}
              <input
                ref={createInputRef}
                value={localCreateValue}
                onChange={(e) => setLocalCreateValue(e.target.value)}
                onKeyDown={handleCreateKeyDown}
                onBlur={() => { if (localCreateValue.trim()) onCreateSubmit(path, localCreateValue.trim(), createTarget!.type); else onCreateCancel() }}
                placeholder={createTarget?.type === 'dir' ? 'folder name' : 'file name'}
                className="flex-1 min-w-0 bg-background border border-border rounded px-1 py-0 text-xs outline-none focus:ring-1 focus:ring-ring"
                onClick={(e) => e.stopPropagation()}
              />
            </div>
          )}

          {children.length === 0 && !isCreating && (
            <p
              className="text-[10px] text-muted-foreground/50 px-2 py-0.5 italic"
              style={{ paddingLeft: `${(depth + 1) * 12 + 10}px` }}
            >
              (empty folder)
            </p>
          )}
        </div>
      )}
    </div>
  )
}
