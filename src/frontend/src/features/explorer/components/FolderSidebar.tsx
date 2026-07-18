import React, { useEffect, useState, useCallback, useRef } from 'react'
import {
  RefreshCw, FileCode, FolderPlus, Pencil, Copy,
  ClipboardPaste, Download, Trash2, PanelRightClose, Loader2
} from 'lucide-react'
import { ScrollArea } from '@/components/scroll-area'
import { Button } from '@/components/button'

import { subscribeToFileTree, type FileTreeEvent } from '../services/fileTreeWs'
import { SmartContextMenu } from './SmartContextMenu'


import { DeleteDialog } from './DeleteDialog'
import { ConflictDialog, type ConflictTarget } from './ConflictDialog'
import { LazyFileNode } from './LazyFileNode'

interface FolderSidebarProps {
  workspacePath: string
  onOpenFile: (path: string) => void
  gitStatuses: Record<string, string>
  gitIgnored?: Record<string, boolean>
  onRefresh: () => void
  noHeader?: boolean
  mainWorkspacePath?: string
  onClose?: () => void
}

export function FolderSidebar({
  workspacePath,
  onOpenFile,
  gitStatuses,
  gitIgnored,
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
  const [hoveredPath, setHoveredPath] = useState<string | null>(null)
  const [createTarget, setCreateTarget] = useState<{
    parentPath: string; type: 'file' | 'dir'
  } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{
    path: string; name: string; isDir: boolean
  } | null>(null)
  type ConflictState =
    | { operation: 'rename'; name: string; oldPath: string; newPath: string }
    | { operation: 'create'; name: string; parentPath: string; type: 'file' | 'dir' }
    | { operation: 'paste'; name: string; sourcePath: string; targetDir: string }
    | { operation: 'move'; name: string; oldPath: string; newPath: string; targetDir: string }
  const [conflictState, setConflictState] = useState<ConflictState | null>(null)

  const conflictTarget: ConflictTarget | null = conflictState
    ? { name: conflictState.name, operation: conflictState.operation }
    : null
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

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'F2' || !hoveredPath || editingPath) return
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return
      event.preventDefault()
      setEditingPath(hoveredPath)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [hoveredPath, editingPath])

  const triggerRefresh = useCallback(() => {
    setRefreshCounter((c) => c + 1)
  }, [])

  const handleRefresh = async () => {
    setLoading(true)
    await onRefresh()
    triggerRefresh()
    setLoading(false)
  }

  const executeRename = useCallback(async (oldPath: string, newPath: string) => {
    setBusy(true)
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ oldPath, newPath }),
      })
      if (res.ok) {
        setEditingPath(null)
        setConflictState(null)
        triggerRefresh()
        onRefresh()
      }
    } catch { /* ignore */ }
    setBusy(false)
  }, [triggerRefresh, onRefresh])

  const handleRenameSubmit = useCallback(async (oldPath: string, newName: string) => {
    const sep = oldPath.includes('\\') ? '\\' : '/'
    const parentDir = oldPath.substring(0, oldPath.lastIndexOf(sep))
    const newPath = parentDir + sep + newName

    // Check if newPath already exists by calling file existence checks or checking the UI or files API.
    // If it exists, show a destructive confirmation modal.
    // Let's call /api/workspaces/files?path=<newPath> first.
    setBusy(true)
    try {
      const checkRes = await fetch(`/api/workspaces/files?path=${encodeURIComponent(newPath)}`)
      if (checkRes.ok) {
        setConflictState({ operation: 'rename', name: newName, oldPath, newPath })
        setBusy(false)
        return
      }
    } catch { /* ignore */ }
    setBusy(false)

    await executeRename(oldPath, newPath)
  }, [executeRename])

  const handleCreateSubmit = useCallback(async (parentPath: string, name: string, type: 'file' | 'dir') => {
    const sep = parentPath.includes('\\') ? '\\' : '/'
    const newPath = parentPath + sep + name

    setBusy(true)
    try {
      const checkRes = await fetch(`/api/workspaces/files?path=${encodeURIComponent(newPath)}`)
      if (checkRes.ok) {
        setConflictState({ operation: 'create', name, parentPath, type })
        setCreateTarget(null)
        setBusy(false)
        return
      }
    } catch { /* ignore */ }

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

    const sep = src.includes('\\') ? '\\' : '/'
    const srcName = src.substring(src.lastIndexOf(sep) + 1)
    const destPath = targetDir + sep + srcName

    setBusy(true)
    try {
      const checkRes = await fetch(`/api/workspaces/files?path=${encodeURIComponent(destPath)}`)
      if (checkRes.ok) {
        setConflictState({ operation: 'paste', name: srcName, sourcePath: src, targetDir })
        setBusy(false)
        return
      }
    } catch { /* ignore */ }

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

  const executeMove = useCallback(async (oldPath: string, newPath: string) => {
    setBusy(true)
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ oldPath, newPath }),
      })
      if (res.ok) {
        setConflictState(null)
        triggerRefresh()
        onRefresh()
      }
    } catch { /* ignore */ }
    setBusy(false)
  }, [triggerRefresh, onRefresh])

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
      return
    }
    const srcPath = e.dataTransfer.getData('application/x-caw-path')
    if (!srcPath) return
    // Do not allow dropping a path into itself or its descendant
    const normSrc = srcPath.replace(/\\/g, '/')
    const normTarget = targetDir.replace(/\\/g, '/')
    if (normSrc === normTarget) return
    if (normTarget.startsWith(normSrc + '/')) return
    const sep = srcPath.includes('\\') ? '\\' : '/'
    const srcName = srcPath.substring(srcPath.lastIndexOf(sep) + 1)
    const newPath = targetDir + sep + srcName
    if (newPath === srcPath) return
    setBusy(true)
    fetch(`/api/workspaces/files?path=${encodeURIComponent(newPath)}`)
      .then((checkRes) => {
        if (checkRes.ok) {
          setConflictState({ operation: 'move', name: srcName, oldPath: srcPath, newPath, targetDir })
          setBusy(false)
        } else {
          executeMove(srcPath, newPath)
        }
      })
      .catch(() => executeMove(srcPath, newPath))
  }, [handleUpload, executeMove])

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
    <div className="flex h-full flex-col bg-background select-none border-l border-border explorer-sidebar">
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
        horizontal
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
              key={workspacePath}
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
              gitIgnored={gitIgnored}
              editingPath={editingPath}
              createTarget={createTarget}
              dragOverPath={dragOverPath}
              refreshCounter={refreshCounter}
              onShowContextMenu={showContextMenu}
              onRenameSubmit={handleRenameSubmit}
              onCancelRename={() => setEditingPath(null)}
              onCreateSubmit={handleCreateSubmit}
              onCreateCancel={() => setCreateTarget(null)}
              onHoverPath={setHoveredPath}
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDropFiles={handleDrop}
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
              {!contextMenu.isRoot && <div className="border-b border-border my-1 mx-1" />}
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
          <button
            onClick={(e) => { e.stopPropagation(); setContextMenu(null); const a = document.createElement('a'); a.href = '/api/workspaces/files?download=true&path=' + encodeURIComponent(contextMenu.path); a.download = contextMenu.name; document.body.appendChild(a); a.click(); document.body.removeChild(a) }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
          >
            <Download className="h-3.5 w-3.5" />
            Download
          </button>
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

      <ConflictDialog
        target={conflictTarget}
        onConfirm={() => {
          if (!conflictState) return
          if (conflictState.operation === 'rename') {
            executeRename(conflictState.oldPath, conflictState.newPath)
          } else if (conflictState.operation === 'move') {
            executeMove(conflictState.oldPath, conflictState.newPath)
          } else if (conflictState.operation === 'create') {
            const sep = conflictState.parentPath.includes('\\') ? '\\' : '/'
            const newPath = conflictState.parentPath + sep + conflictState.name
            setConflictState(null)
            setCreateTarget(null)
            fetch('/api/workspaces/files', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ path: newPath, type: conflictState.type }),
            }).catch(() => {}).finally(() => {
              triggerRefresh()
              onRefresh()
              setBusy(false)
            })
          } else if (conflictState.operation === 'paste') {
            setConflictState(null)
            fetch('/api/workspaces/files', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ sourcePath: conflictState.sourcePath, targetDir: conflictState.targetDir }),
            }).catch(() => {}).finally(() => {
              triggerRefresh()
              onRefresh()
              setBusy(false)
            })
          }
        }}
        onCancel={() => {
          setConflictState(null)
          setCreateTarget(null)
          setEditingPath(null)
        }}
      />
    </div>
  )
}
