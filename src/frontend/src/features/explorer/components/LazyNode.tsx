import { useEffect, useState, useCallback, useRef } from 'react'
import { ChevronRight, ChevronDown, Folder, FolderOpen, Loader2 } from 'lucide-react'
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

export interface LazyNodeProps {
  name: string
  path: string
  depth: number
  startExpanded?: boolean
  selected: string | null
  onSelect: (path: string) => void
  focusPath: string | null
  scrollContainerRef: React.RefObject<HTMLDivElement | null>
}

export function LazyNode({ name, path, depth, startExpanded, selected, onSelect, focusPath, scrollContainerRef }: LazyNodeProps) {
  const [expanded, setExpanded] = useState(!!startExpanded)
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [children, setChildren] = useState<FileNode[]>([])
  const buttonRef = useRef<HTMLButtonElement>(null)

  const load = useCallback(async () => {
    if (loaded || loading) return
    setLoading(true)
    const kids = await listDir(path)
    setChildren(kids)
    setLoaded(true)
    setLoading(false)
  }, [path, loaded, loading])

  useEffect(() => {
    if (startExpanded) load()
  }, [startExpanded, load])

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
    if (selected && samePath(selected, path) && buttonRef.current) {
      buttonRef.current.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }, [selected, path])

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
        <span className="truncate text-muted-foreground">{name}</span>
        {loading && <Loader2 className="h-3 w-3 animate-spin ml-auto" />}
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
              scrollContainerRef={scrollContainerRef}
            />
          ))}
          {children.length === 0 && (
            <p className="text-xs text-muted-foreground px-2 py-1 italic" style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}>
              empty
            </p>
          )}
        </div>
      )}
    </div>
  )
}
