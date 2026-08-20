import React, { useEffect, useState, useCallback, useRef } from 'react'
import {
  RefreshCw, FileCode, FolderPlus, Upload, Pencil, Copy, CopyPlus, CopyCheck,
  ClipboardPaste, Download, Trash2, PanelLeftClose, PanelRightClose, Loader2
} from 'lucide-react'
import { ScrollArea } from '@/components/scroll-area'
import { Button } from '@/components/button'
import { normalizePath } from '@/features/shared/utils/path'

import { subscribeToFileTree, type FileTreeEvent } from '../services/fileTreeWs'
import { SmartContextMenu } from './SmartContextMenu'


import { DeleteDialog } from './DeleteDialog'
import { ConflictDialog, type ConflictTarget } from './ConflictDialog'
import { LazyFileNode } from './LazyFileNode'
import { SearchPanel, type SearchPanelMode } from './SearchPanel'

interface FolderSidebarProps {
  workspacePath: string
  onOpenFile: (path: string, line?: number, column?: number) => void
  gitStatuses: Record<string, string>
  gitIgnored?: Record<string, boolean>
  onRefresh: () => void
  noHeader?: boolean
  mainWorkspacePath?: string
  onClose?: () => void
  copyToWorktrees?: string[]
  onToggleCopyToWorktrees?: (paths: string[]) => void
  searchPanelOpen?: boolean
  searchPanelMode?: SearchPanelMode
  onCloseSearchPanel?: () => void
  isRight?: boolean
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
  copyToWorktrees,
  onToggleCopyToWorktrees,
  searchPanelOpen,
  searchPanelMode = 'find',
  onCloseSearchPanel,
  isRight = false,
}: FolderSidebarProps) {
  const [loading, setLoading] = useState(false)
  const isWorktree = !!(mainWorkspacePath && workspacePath && workspacePath !== mainWorkspacePath)
  const [busy, setBusy] = useState(false)
  const [contextMenu, setContextMenu] = useState<{
    x: number; y: number; path: string; name: string; isDir: boolean; isRoot?: boolean
  } | null>(null)
  const [selectedPaths, setSelectedPaths] = useState<string[]>([])
  const [anchorPath, setAnchorPath] = useState<string | null>(null)
  const sidebarRef = useRef<HTMLDivElement>(null)
  const [clipboard, setClipboard] = useState<{ paths: string[] } | null>(null)
  const [editingPath, setEditingPath] = useState<string | null>(null)
  const [hoveredPath, setHoveredPath] = useState<string | null>(null)
  const [createTarget, setCreateTarget] = useState<{
    parentPath: string; type: 'file' | 'dir'
  } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{
    paths: string[]; name?: string; isDir: boolean
  } | null>(null)
  type ConflictState =
    | { operation: 'rename'; name: string; oldPath: string; newPath: string }
    | { operation: 'create'; name: string; parentPath: string; type: 'file' | 'dir' }
    | { operation: 'paste'; name: string; sourcePath: string; targetDir: string }
    | { operation: 'move'; name: string; oldPath: string; newPath: string; targetDir: string }
    | {
        operation: 'batchMove'
        targetDir: string
        moves: Array<{ name: string; oldPath: string; newPath: string }>
        conflicts: Array<{ name: string; oldPath: string; newPath: string }>
      }
    | {
        operation: 'batchPaste'
        targetDir: string
        pastes: Array<{ name: string; sourcePath: string }>
        conflicts: Array<{ name: string; sourcePath: string }>
      }
  const [conflictState, setConflictState] = useState<ConflictState | null>(null)

  const conflictTarget: ConflictTarget | null = conflictState
    ? {
        name: conflictState.operation === 'batchMove'
          ? `${conflictState.conflicts.length} item(s)`
          : conflictState.operation === 'batchPaste'
            ? `${conflictState.conflicts.length} item(s)`
            : conflictState.name,
        operation: conflictState.operation === 'batchMove' ? 'batchMove' : conflictState.operation === 'batchPaste' ? 'batchPaste' : conflictState.operation,
        conflictNames: conflictState.operation === 'batchMove'
          ? conflictState.conflicts.map((c) => c.name)
          : conflictState.operation === 'batchPaste'
            ? conflictState.conflicts.map((c) => c.name)
            : undefined,
      }
    : null
  const [dragOverPath, setDragOverPath] = useState<string | null>(null)
  const [refreshCounter, setRefreshCounter] = useState(0)
  const [uploadTarget, setUploadTarget] = useState<string | null>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

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

  const clearSelection = useCallback(() => {
    setSelectedPaths([])
    setAnchorPath(null)
  }, [])

  const selectOne = useCallback((path: string) => {
    setSelectedPaths([path])
    setAnchorPath(path)
  }, [])

  const toggleSelect = useCallback((path: string) => {
    const norm = normalizePath(path)
    setSelectedPaths((prev) =>
      prev.some((p) => normalizePath(p) === norm)
        ? prev.filter((p) => normalizePath(p) !== norm)
        : [...prev, path],
    )
    setAnchorPath(path)
  }, [])

  const getVisiblePaths = useCallback((): string[] => {
    const container = sidebarRef.current
    if (!container) return []
    const paths: string[] = []
    container.querySelectorAll('[data-path]').forEach((el) => {
      const p = el.getAttribute('data-path')
      if (p) paths.push(p)
    })
    return paths
  }, [])

  const selectRange = useCallback((path: string) => {
    const visible = getVisiblePaths()
    const anchor = anchorPath ?? path
    const anchorIdx = visible.findIndex((p) => normalizePath(p) === normalizePath(anchor))
    const targetIdx = visible.findIndex((p) => normalizePath(p) === normalizePath(path))
    if (anchorIdx === -1 || targetIdx === -1) {
      selectOne(path)
      return
    }
    const start = Math.min(anchorIdx, targetIdx)
    const end = Math.max(anchorIdx, targetIdx)
    setSelectedPaths(visible.slice(start, end + 1))
  }, [anchorPath, getVisiblePaths, selectOne])

  const handleSelectClick = useCallback((path: string, e: React.MouseEvent) => {
    if (e.shiftKey) {
      selectRange(path)
    } else if (e.ctrlKey || e.metaKey) {
      toggleSelect(path)
    } else {
      clearSelection()
    }
  }, [selectRange, toggleSelect, clearSelection])

  const handleDownload = useCallback(async (paths: string[]) => {
    setContextMenu(null)
    if (paths.length === 1) {
      const a = document.createElement('a')
      a.href = '/api/workspaces/files?download=true&path=' + encodeURIComponent(paths[0])
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      return
    }
    try {
      const res = await fetch('/api/workspaces/files/download', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ paths }),
      })
      if (res.ok) {
        const blob = await res.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = 'selection.zip'
        document.body.appendChild(a)
        a.click()
        document.body.removeChild(a)
        URL.revokeObjectURL(url)
      }
    } catch { /* ignore */ }
  }, [])

  const getActionPaths = useCallback(
    (path: string): string[] => {
      const isInSelection = selectedPaths.some((p) => normalizePath(p) === normalizePath(path))
      const raw = isInSelection ? selectedPaths : [path]
      const filtered = raw.filter((p) => normalizePath(p) !== normalizePath(workspacePath))
      return filtered.length > 0 ? filtered : raw
    },
    [selectedPaths, workspacePath],
  )

  useEffect(() => {
    clearSelection()
  }, [workspacePath, clearSelection])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (target.closest('[data-path]')) return
      if (contextMenuRef.current && contextMenuRef.current.contains(target)) return
      if (target.closest('.explorer-sidebar')) {
        clearSelection()
      }
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [clearSelection])

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
      await Promise.all(target.paths.map((path) =>
        fetch('/api/workspaces/files', {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path }),
        }),
      ))
    } catch { /* ignore */ }
    clearSelection()
    triggerRefresh()
    onRefresh()
    setBusy(false)
  }, [deleteTarget, clearSelection, triggerRefresh, onRefresh])

  const handleCopy = useCallback((paths: string[]) => {
    setClipboard({ paths })
    setContextMenu(null)
  }, [])

  const handlePaste = useCallback(async (targetDir: string) => {
    const srcs = clipboard?.paths
    if (!srcs || srcs.length === 0) return
    setContextMenu(null)

    const sep = targetDir.includes('\\') ? '\\' : '/'
    const pastes: Array<{ name: string; sourcePath: string }> = srcs.map((src) => {
      const srcSep = src.includes('\\') ? '\\' : '/'
      const srcName = src.substring(src.lastIndexOf(srcSep) + 1)
      return { name: srcName, sourcePath: src }
    })

    setBusy(true)
    const checkResults = await Promise.all(
      pastes.map((p) => fetch(`/api/workspaces/files?path=${encodeURIComponent(targetDir + sep + p.name)}`).then((r) => r.ok).catch(() => false)),
    )
    const conflicts = pastes.filter((_, i) => checkResults[i])
    if (conflicts.length > 0) {
      setConflictState({ operation: 'batchPaste', targetDir, pastes, conflicts })
      setBusy(false)
      return
    }
    try {
      for (const p of pastes) {
        await fetch('/api/workspaces/files', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ sourcePath: p.sourcePath, targetDir }),
        })
      }
    } catch { /* ignore */ }
    triggerRefresh()
    onRefresh()
    setBusy(false)
  }, [clipboard, triggerRefresh, onRefresh])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return
      const isInSidebar = sidebarRef.current?.contains(event.target as Node)
      if (!isInSidebar) return
      const mod = event.ctrlKey || event.metaKey
      if (mod && (event.key === 'c' || event.key === 'C')) {
        if (selectedPaths.length > 0) {
          event.preventDefault()
          handleCopy(selectedPaths)
        }
      } else if (mod && (event.key === 'v' || event.key === 'V')) {
        if (clipboard && clipboard.paths.length > 0) {
          event.preventDefault()
          const hoverIsDir = hoveredPath && hoveredPath !== workspacePath
          handlePaste(hoverIsDir ? hoveredPath! : workspacePath)
        }
      } else if (mod && (event.key === 'a' || event.key === 'A')) {
        event.preventDefault()
        const visible = getVisiblePaths()
        if (visible.length > 0) {
          setSelectedPaths(visible)
          setAnchorPath(visible[visible.length - 1])
        }
      } else if (event.key !== 'F2' || !hoveredPath || editingPath) {
        return
      } else {
        event.preventDefault()
        setEditingPath(hoveredPath)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [hoveredPath, editingPath, selectedPaths, clipboard, handleCopy, handlePaste, getVisiblePaths, workspacePath])

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

  const executeBatchMoves = useCallback(async (moves: Array<{ oldPath: string; newPath: string }>) => {
    setBusy(true)
    try {
      await Promise.all(moves.map((m) =>
        fetch('/api/workspaces/files', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ oldPath: m.oldPath, newPath: m.newPath }),
        }),
      ))
    } catch { /* ignore */ }
    setConflictState(null)
    triggerRefresh()
    onRefresh()
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
    const raw = e.dataTransfer.getData('application/x-caw-paths') || e.dataTransfer.getData('application/x-caw-path')
    if (!raw) return
    const srcPaths = raw.split('\n').map((p) => p.trim()).filter(Boolean)
    if (srcPaths.length === 0) return
    const sep = targetDir.includes('\\') ? '\\' : '/'
    // Resolve the full set of moves, skipping invalid targets (self,
    // descendant, or the same destination path).
    const moves: Array<{ name: string; oldPath: string; newPath: string }> = []
    for (const srcPath of srcPaths) {
      const normSrc = srcPath.replace(/\\/g, '/')
      const normTarget = targetDir.replace(/\\/g, '/')
      if (normSrc === normTarget) continue
      if (normTarget.startsWith(normSrc + '/')) continue
      const srcSep = srcPath.includes('\\') ? '\\' : '/'
      const srcName = srcPath.substring(srcPath.lastIndexOf(srcSep) + 1)
      const newPath = targetDir + sep + srcName
      if (newPath === srcPath) continue
      moves.push({ name: srcName, oldPath: srcPath, newPath })
    }
    if (moves.length === 0) return
    setBusy(true)
    Promise.all(moves.map((m) => fetch(`/api/workspaces/files?path=${encodeURIComponent(m.newPath)}`)))
      .then((responses) => {
        const conflicts = moves.filter((_, i) => responses[i].ok)
        if (conflicts.length > 0) {
          setConflictState({ operation: 'batchMove', targetDir, moves, conflicts })
          setBusy(false)
        } else {
          executeBatchMoves(moves)
        }
      })
      .catch(() => executeBatchMoves(moves))
  }, [handleUpload, executeBatchMoves])

  const showContextMenu = useCallback((path: string, name: string, isDir: boolean, x: number, y: number) => {
    setContextMenu({ x, y, path, name, isDir })
    if (!selectedPaths.some((p) => normalizePath(p) === normalizePath(path))) {
      setSelectedPaths([path])
      setAnchorPath(path)
    }
  }, [selectedPaths])

  const isCopyToWorktreePath = useCallback(
    (path: string) => {
      if (!copyToWorktrees || copyToWorktrees.length === 0) return false
      const norm = normalizePath(path)
      return copyToWorktrees.some((p) => normalizePath(p) === norm)
    },
    [copyToWorktrees],
  )

  const handleToggleCopyToWorktrees = useCallback(
    (paths: string[]) => {
      setContextMenu(null)
      onToggleCopyToWorktrees?.(paths)
    },
    [onToggleCopyToWorktrees],
  )

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

  const actionPaths = contextMenu ? getActionPaths(contextMenu.path) : []
  const isMultiAction = actionPaths.length > 1
  const allCopied = actionPaths.length > 0 && actionPaths.every((p) => isCopyToWorktreePath(p))
  const copiedCount = actionPaths.filter((p) => isCopyToWorktreePath(p)).length

  return (
    <div ref={sidebarRef} className="flex h-full flex-col bg-background select-none explorer-sidebar">
      {!noHeader && (
        <div className={`flex items-center gap-2 border-b border-border px-3 h-[33px] shrink-0 bg-secondary/20 ${isRight ? '' : 'flex-row-reverse'}`}>
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
            {isRight ? <PanelRightClose className="h-3.5 w-3.5" /> : <PanelLeftClose className="h-3.5 w-3.5" />}
          </Button>
        </div>
      )}

      {searchPanelOpen && workspacePath && (
        <SearchPanel
          workspacePath={workspacePath}
          mode={searchPanelMode}
          onOpenFile={onOpenFile}
          onRefresh={handleRefresh}
          onClose={() => onCloseSearchPanel?.()}
        />
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
              copyToWorktrees={copyToWorktrees}
              selectedPaths={selectedPaths}
              onSelectClick={handleSelectClick}
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
              <button
                onClick={(e) => { e.stopPropagation(); const p = contextMenu.path; setContextMenu(null); setUploadTarget(p); setTimeout(() => fileInputRef.current?.click(), 0) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Upload className="h-3.5 w-3.5" />
                Upload
              </button>
              {!contextMenu.isRoot && <div className="border-b border-border my-1 mx-1" />}
            </>
          )}
          {!contextMenu.isRoot && !isMultiAction && (
            <>
              <button
                onClick={(e) => { e.stopPropagation(); setContextMenu(null); setEditingPath(contextMenu.path) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Pencil className="h-3.5 w-3.5" />
                Rename
              </button>
              <button
                onClick={(e) => { e.stopPropagation(); handleCopy(actionPaths) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
              >
                <Copy className="h-3.5 w-3.5" />
                Copy{isMultiAction ? ` (${actionPaths.length})` : ''}
              </button>
            </>
          )}
          {!isWorktree && !contextMenu.isRoot && (
            <button
              onClick={(e) => { e.stopPropagation(); handleToggleCopyToWorktrees(actionPaths) }}
              className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
            >
              {allCopied ? (
                <>
                  <CopyCheck className="h-3.5 w-3.5" />
                  Stop copying{isMultiAction ? ` (${actionPaths.length})` : ''}
                </>
              ) : (
                <>
                  <CopyPlus className="h-3.5 w-3.5" />
                  Copy to worktrees
                  {isMultiAction
                    ? ` (${actionPaths.length - copiedCount})`
                    : (copiedCount > 0 ? ' (mixed)' : '')}
                </>
              )}
            </button>
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
            onClick={(e) => { e.stopPropagation(); handleDownload(actionPaths) }}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
          >
            <Download className="h-3.5 w-3.5" />
            Download{isMultiAction ? ` (${actionPaths.length})` : ''}
          </button>
          {!contextMenu.isRoot && (
            <>
              <div className="border-t border-border my-0.5" />
              <button
                onClick={() => { setContextMenu(null); setDeleteTarget({ paths: actionPaths, name: contextMenu.name, isDir: contextMenu.isDir }) }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete{isMultiAction ? ` (${actionPaths.length})` : ''}
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

      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => {
          const files = e.target.files
          if (files && files.length > 0 && uploadTarget) {
            handleUpload(uploadTarget, files)
          }
          e.target.value = ''
        }}
      />
      <ConflictDialog
        target={conflictTarget}
        onConfirm={() => {
          if (!conflictState) return
          if (conflictState.operation === 'rename') {
            executeRename(conflictState.oldPath, conflictState.newPath)
          } else if (conflictState.operation === 'move') {
            executeMove(conflictState.oldPath, conflictState.newPath)
          } else if (conflictState.operation === 'batchMove') {
            executeBatchMoves(conflictState.moves)
          } else if (conflictState.operation === 'batchPaste') {
            setConflictState(null)
            setBusy(true)
            Promise.all(
              conflictState.pastes.map((p) =>
                fetch('/api/workspaces/files', {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({
                    sourcePath: p.sourcePath,
                    targetDir: conflictState.targetDir,
                    overwrite: true,
                  }),
                }),
              ),
            ).catch(() => {}).finally(() => {
              triggerRefresh()
              onRefresh()
              setBusy(false)
            })
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
              body: JSON.stringify({ sourcePath: conflictState.sourcePath, targetDir: conflictState.targetDir, overwrite: true }),
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
