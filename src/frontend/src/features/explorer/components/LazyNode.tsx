import { useEffect, useState, useCallback, useRef } from 'react'
import { ChevronRight, ChevronDown, Folder, FolderOpen, Loader2, MoreVertical } from 'lucide-react'
import { type FileNode } from '../types'

async function listDir(path: string): Promise<FileNode[]> {
  const res = await fetch(`/api/workspaces/contents?dirs_only=true&path=${encodeURIComponent(path)}`)
  if (!res.ok) return []
  const arr = (await res.json())?.data
  if (!Array.isArray(arr)) return []
  return (arr as FileNode[]).filter((n) => n.isDir)
}

function samePath(a: string, b: string): boolean {
  return a.replace(/\\/g, '/').toLowerCase() === b.replace(/\\/g, '/').toLowerCase()
}

function isAncestor(parent: string, child: string): boolean {
  const p = parent.replace(/\\/g, '/').toLowerCase().replace(/\/$/, '')
  const c = child.replace(/\\/g, '/').toLowerCase().replace(/\/$/, '')
  return c === p || c.startsWith(p + '/')
}

function getIsMobile() {
  return window.innerWidth < 768
}

export interface LazyNodeProps {
  name: string
  path: string
  depth: number
  startExpanded?: boolean
  selected: string | null
  onSelect: (path: string) => void
  focusPath: string | null
  refreshCounter?: number
  onShowContextMenu?: (path: string, name: string, x: number, y: number) => void
  createTargetPath?: string | null
  onCreateFolder?: (parentPath: string, name: string) => void
  onCreateCancel?: () => void
  editingPath?: string | null
  onRenameFolder?: (path: string, name: string) => void
  onRenameCancel?: () => void
  onHoverPath?: (path: string) => void
  scrollBlock?: 'start' | 'nearest'
}

export function LazyNode({
  name,
  path,
  depth,
  startExpanded,
  selected,
  onSelect,
  focusPath,
  refreshCounter,
  onShowContextMenu,
  createTargetPath,
  onCreateFolder,
  onCreateCancel,
  editingPath,
  onRenameFolder,
  onRenameCancel,
  onHoverPath,
  scrollBlock = 'nearest',
}: LazyNodeProps) {
  const [expanded, setExpanded] = useState(!!startExpanded)
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [children, setChildren] = useState<FileNode[]>([])
  const [createValue, setCreateValue] = useState('')
  const [renameValue, setRenameValue] = useState(name)
  const [isMobile, setIsMobile] = useState(getIsMobile)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const createInputRef = useRef<HTMLInputElement>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    const onResize = () => setIsMobile(getIsMobile())
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  const load = useCallback(async (force = false) => {
    if (!force && (loaded || loading)) return
    setLoading(true)
    const kids = await listDir(path)
    setChildren(kids)
    setLoaded(true)
    setLoading(false)
  }, [path, loaded, loading])

  useEffect(() => {
    if (startExpanded) load()
  }, [startExpanded, load])

  // Re-fetch children in place when refreshCounter changes, preserving
  // expand/scroll state. Only expanded nodes reload; collapsed nodes
  // drop their cache so the next expand fetches fresh data.
  const refreshRef = useRef(refreshCounter)
  useEffect(() => {
    if (refreshRef.current === refreshCounter) return
    refreshRef.current = refreshCounter
    if (expanded) {
      load(true)
    } else {
      setLoaded(false)
      setChildren([])
    }
  }, [refreshCounter, expanded, load])

  useEffect(() => {
    if (!focusPath) return
    if (samePath(path, focusPath)) {
      setExpanded(true)
      if (!loaded) load()
      return
    }
    if (isAncestor(path, focusPath)) {
      setExpanded(true)
      if (!loaded) load()
    }
  }, [focusPath, path, loaded, load])

  // Scroll into view when this node becomes the selected/focused one.
  useEffect(() => {
    if (focusPath && samePath(focusPath, path) && buttonRef.current) {
      buttonRef.current.scrollIntoView({ block: scrollBlock, behavior: 'smooth' })
    }
  }, [focusPath, path, scrollBlock])

  const isCreating = createTargetPath === path

  useEffect(() => {
    if (isCreating) {
      setExpanded(true)
      if (!loaded) load()
      createInputRef.current?.focus()
    } else {
      setCreateValue('')
    }
  }, [isCreating, expanded, loaded, load])

  useEffect(() => {
    if (editingPath === path) {
      setRenameValue(name)
      renameInputRef.current?.focus()
      renameInputRef.current?.select()
    }
  }, [editingPath, path, name])

  const submitCreate = () => {
    const value = createValue.trim()
    if (!value || !onCreateFolder) return
    onCreateFolder(path, value)
    setCreateValue('')
  }

  const handleCreateKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      submitCreate()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onCreateCancel?.()
    }
  }

  const handleRenameKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (renameValue.trim() && renameValue.trim() !== name) {
        onRenameFolder?.(path, renameValue.trim())
      } else {
        onRenameCancel?.()
      }
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onRenameCancel?.()
    }
  }

  const toggle = () => {
    setExpanded((e) => {
      const next = !e
      if (next && !loaded) load()
      return next
    })
  }

  const isSelected = selected && samePath(selected, path)

  return (
    <div>
      <button
        ref={buttonRef}
        onMouseEnter={() => onHoverPath?.(path)}
        onContextMenu={(e) => {
          e.preventDefault()
          e.stopPropagation()
          onShowContextMenu?.(path, name, e.clientX, e.clientY)
        }}
        onClick={() => {
          onSelect(path)
          toggle()
        }}
        className={`flex w-full items-center gap-1.5 px-2 py-0.5 text-sm hover:bg-accent/50 text-left ${
          isSelected ? 'bg-accent/70 text-accent-foreground' : ''
        }`}
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {expanded ? (
          <FolderOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {editingPath === path ? (
          <input
            ref={renameInputRef}
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            onKeyDown={handleRenameKeyDown}
            onBlur={() => {
              if (renameValue.trim() && renameValue.trim() !== name) onRenameFolder?.(path, renameValue.trim())
              else onRenameCancel?.()
            }}
            onClick={(e) => e.stopPropagation()}
            className="min-w-0 flex-1 rounded border border-border bg-background px-1 py-0 text-xs text-foreground outline-none focus:ring-1 focus:ring-ring"
          />
        ) : (
          <span className="truncate text-muted-foreground">{name}</span>
        )}
        {loading && <Loader2 className="h-3 w-3 animate-spin ml-auto" />}
        {isMobile && (
          <span
            role="button"
            onClick={(e) => {
              e.stopPropagation()
              const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
              onShowContextMenu?.(path, name, rect.right, rect.bottom + 2)
            }}
            className="h-5 w-5 rounded text-muted-foreground/40 hover:text-foreground hover:bg-accent/40 flex items-center justify-center shrink-0"
          >
            <MoreVertical className="h-3 w-3" />
          </span>
        )}
      </button>
      {expanded && loaded && (
        <div>
          {children.map((child) => (
            <LazyNode
              key={child.path}
              name={child.name}
              path={child.path}
              depth={depth + 1}
              selected={selected}
              onSelect={onSelect}
              focusPath={focusPath}
              refreshCounter={refreshCounter}
              onShowContextMenu={onShowContextMenu}
              createTargetPath={createTargetPath}
              onCreateFolder={onCreateFolder}
              onCreateCancel={onCreateCancel}
              editingPath={editingPath}
              onRenameFolder={onRenameFolder}
              onRenameCancel={onRenameCancel}
              onHoverPath={onHoverPath}
              scrollBlock={scrollBlock}
            />
          ))}
          {isCreating && (
            <div className="flex items-center gap-1.5 px-2 py-0.5" style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}>
              <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <input
                ref={createInputRef}
                value={createValue}
                onChange={(e) => setCreateValue(e.target.value)}
                onKeyDown={handleCreateKeyDown}
                onBlur={() => {
                  if (createValue.trim()) submitCreate()
                  else onCreateCancel?.()
                }}
                placeholder="folder name"
                className="min-w-0 flex-1 rounded border border-border bg-background px-1 py-0 text-xs outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
          )}
          {children.length === 0 && !isCreating && (
            <p className="text-xs text-muted-foreground px-2 py-1 italic" style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}>
              empty
            </p>
          )}
        </div>
      )}
    </div>
  )
}
