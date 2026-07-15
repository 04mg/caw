import React, { useState, useRef, useEffect, useCallback } from 'react'
import { ChevronDown, ChevronRight, FolderOpen, Folder, FileCode, Loader2, MoreVertical } from 'lucide-react'
import { type FileNode } from '../types'

export interface LazyFileNodeProps {
  name: string
  path: string
  isDir: boolean
  depth: number
  startExpanded?: boolean
  onOpenFile: (path: string) => void
  gitStatuses: Record<string, string>
  gitIgnored?: Record<string, boolean>
  editingPath: string | null
  createTarget: { parentPath: string; type: 'file' | 'dir' } | null
  dragOverPath: string | null
  refreshCounter: number
  onShowContextMenu: (path: string, name: string, isDir: boolean, x: number, y: number) => void
  onRenameSubmit: (oldPath: string, newName: string) => void
  onCancelRename: () => void
  onCreateSubmit: (parentPath: string, name: string, type: 'file' | 'dir') => void
  onCreateCancel: () => void
  onDragOver: (e: React.DragEvent, path: string) => void
  onDragLeave: () => void
  onDropFiles: (e: React.DragEvent, targetDir: string) => void
}

export function LazyFileNode({
  name,
  path,
  isDir,
  depth,
  startExpanded = false,
  onOpenFile,
  gitStatuses,
  gitIgnored,
  editingPath,
  createTarget,
  dragOverPath,
  refreshCounter,
  onShowContextMenu,
  onRenameSubmit,
  onCancelRename,
  onCreateSubmit,
  onCreateCancel,
  onDragOver,
  onDragLeave,
  onDropFiles,
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
        const arr = (await res.json())?.data
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
  const isIgnored = !!(gitIgnored && gitIgnored[path])

  // For files, the status comes directly from gitStatuses[path].
  // For folders, derive the effective status from any descendant that has
  // a git status, so a folder shows a label when it contains a file or
  // subfolder with a diff.
  let effectiveXY = statusXY
  if (isDir && !effectiveXY) {
    const prefix = path.replace(/\\/g, '/').toLowerCase().replace(/\/$/, '') + '/'
    let folderIsModified = false
    let folderIsAdded = false
    let folderIsUntracked = false
    for (const p in gitStatuses) {
      const norm = p.replace(/\\/g, '/').toLowerCase()
      if (norm.startsWith(prefix)) {
        const s = gitStatuses[p]
        if (s.includes('M')) folderIsModified = true
        else if (s.includes('A')) folderIsAdded = true
        else if (s.includes('?')) folderIsUntracked = true
      }
    }
    if (folderIsModified) effectiveXY = 'M'
    else if (folderIsAdded) effectiveXY = 'A'
    else if (folderIsUntracked) effectiveXY = '?'
  }

  const isModified = effectiveXY.includes('M')
  const isUntracked = effectiveXY.includes('?')
  const isAdded = effectiveXY.includes('A')

  let statusBadge = null
  let textClass = ''
  let iconClass = ''
  if (isIgnored) {
    textClass = 'text-muted-foreground/40'
    iconClass = 'text-muted-foreground/40'
  } else if (isModified) {
    textClass = 'text-yellow-500'
    iconClass = 'text-yellow-500'
    statusBadge = (
      <span className="text-[9px] font-bold text-yellow-600 bg-yellow-500/15 border border-yellow-500/30 px-1 rounded shrink-0" title="Modified">
        M
      </span>
    )
  } else if (isAdded) {
    textClass = 'text-green-500'
    iconClass = 'text-green-500'
    statusBadge = (
      <span className="text-[9px] font-bold text-green-600 bg-green-500/15 border border-green-500/30 px-1 rounded shrink-0" title="Added">
        A
      </span>
    )
  } else if (isUntracked) {
    textClass = 'text-red-400'
    iconClass = 'text-red-400'
    statusBadge = (
      <span className="text-[9px] font-bold text-red-500 bg-red-500/15 border border-red-500/30 px-1 rounded shrink-0" title="Untracked">
        U
      </span>
    )
  }

  const isDragOver = dragOverPath === path && isDir

  return (
    <div className="min-w-full">
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
                <FolderOpen className={`h-3.5 w-3.5 shrink-0 ${iconClass || 'text-muted-foreground/70'}`} />
              ) : (
                <Folder className={`h-3.5 w-3.5 shrink-0 ${iconClass || 'text-muted-foreground/70'}`} />
              )}
            </>
          ) : (
            <>
              <span className="w-3.5" />
              <FileCode className={`h-3.5 w-3.5 shrink-0 ${iconClass || 'text-muted-foreground/70'}`} />
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
            <span className={`truncate flex-1 font-medium ${textClass || 'text-muted-foreground'}`}>{name}</span>
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
    <div className="min-w-full">
          {children.map((child) => (
            <LazyFileNode
              key={child.path}
              name={child.name}
              path={child.path}
              isDir={child.isDir}
              depth={depth + 1}
              onOpenFile={onOpenFile}
              gitStatuses={gitStatuses}
              gitIgnored={gitIgnored}
              editingPath={editingPath}
              createTarget={createTarget}
              dragOverPath={dragOverPath}
              refreshCounter={refreshCounter}
              onShowContextMenu={onShowContextMenu}
              onRenameSubmit={onRenameSubmit}
              onCancelRename={onCancelRename}
              onCreateSubmit={onCreateSubmit}
              onCreateCancel={onCreateCancel}
              onDragOver={onDragOver}
              onDragLeave={onDragLeave}
              onDropFiles={onDropFiles}
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
