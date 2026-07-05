import { useEffect, useState, useCallback } from 'react'
import { ChevronRight, ChevronDown, Folder, FolderOpen, Loader2, GitBranch, RefreshCw, FileCode } from 'lucide-react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'

interface FileNode {
  name: string
  path: string
  isDir: boolean
}

interface FolderSidebarProps {
  workspacePath: string
  onOpenFile: (path: string) => void
  onOpenDiff: () => void
  gitStatuses: Record<string, string>
  onRefresh: () => void
  noHeader?: boolean
}

export function FolderSidebar({
  workspacePath,
  onOpenFile,
  onOpenDiff,
  gitStatuses,
  onRefresh,
  noHeader,
}: FolderSidebarProps) {
  const [loading, setLoading] = useState(false)

  const handleRefresh = async () => {
    setLoading(true)
    await onRefresh()
    setLoading(false)
  }

  const isGitRepo = Object.keys(gitStatuses).length > 0

  return (
    <div className="flex h-full flex-col bg-background select-none border-l border-border">
      {/* Sidebar Header */}
      {!noHeader && (
      <div className="flex items-center gap-2 border-b border-border px-3 h-[33px] shrink-0 bg-secondary/20">
        <span className="flex-1 text-xs font-medium text-muted-foreground truncate">
          Explorer
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5"
          onClick={handleRefresh}
          disabled={loading}
          title="Refresh files"
        >
          <RefreshCw className={`h-3 w-3 ${loading ? 'animate-spin' : ''}`} />
        </Button>
      </div>
      )}

      {/* Git Diff Bar */}
      {isGitRepo && (
        <div className="p-2 border-b border-border bg-muted/10 shrink-0">
          <Button
            variant="outline"
            size="sm"
            onClick={onOpenDiff}
            className="w-full h-7 text-xs flex items-center justify-center gap-1.5 border-dashed border-primary/40 hover:border-primary/80 transition-colors"
          >
            <GitBranch className="h-3.5 w-3.5 text-primary" />
            See Git Diff
          </Button>
        </div>
      )}

      {/* File Tree */}
      <ScrollArea className="flex-1">
        <div>
          {workspacePath ? (
            <LazyFileNode
              name={workspacePath.split(/[\\/]/).filter(Boolean).pop() || workspacePath}
              path={workspacePath}
              isDir={true}
              depth={0}
              startExpanded
              onOpenFile={onOpenFile}
              gitStatuses={gitStatuses}
            />
          ) : (
            <p className="text-xs text-muted-foreground italic text-center mt-4">
              No workspace open.
            </p>
          )}
        </div>
      </ScrollArea>
    </div>
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
}

function LazyFileNode({
  name,
  path,
  isDir,
  depth,
  startExpanded = false,
  onOpenFile,
  gitStatuses,
}: LazyFileNodeProps) {
  const [expanded, setExpanded] = useState(startExpanded)
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [children, setChildren] = useState<FileNode[]>([])

  const load = useCallback(async () => {
    if (loaded || loading) return
    setLoading(true)
    try {
      const res = await fetch(`/api/workspace/list-all?path=${encodeURIComponent(path)}`)
      if (res.ok) {
        const arr = (await res.json()) as FileNode[]
        // Sort folders first, then files alphabetically
        const sorted = arr.sort((a, b) => {
          if (a.isDir && !b.isDir) return -1
          if (!a.isDir && b.isDir) return 1
          return a.name.localeCompare(b.name)
        })
        setChildren(sorted)
      }
    } catch {
      setChildren([])
    } finally {
      setLoaded(true)
      setLoading(false)
    }
  }, [path, loaded, loading])

  useEffect(() => {
    if (startExpanded) load()
  }, [startExpanded, load])

  const toggle = () => {
    if (loading) return
    setExpanded((e) => {
      const next = !e
      if (next && !loaded) load()
      return next
    })
  }

  // Get git status code for this node
  const statusXY = gitStatuses[path] || ''
  const isModified = statusXY.includes('M')
  const isUntracked = statusXY.includes('?')
  const isAdded = statusXY.includes('A')

  let statusBadge = null
  let textClass = ''
  if (isModified) {
    statusBadge = (
      <span className="text-[9px] font-bold text-muted-foreground bg-muted border border-border px-1 rounded shrink-0" title="Modified">
        M
      </span>
    )
  } else if (isAdded) {
    statusBadge = (
      <span className="text-[9px] font-bold text-muted-foreground bg-muted border border-border px-1 rounded shrink-0" title="Added">
        A
      </span>
    )
  } else if (isUntracked) {
    statusBadge = (
      <span className="text-[9px] font-bold text-muted-foreground bg-muted border border-border px-1 rounded shrink-0" title="Untracked">
        U
      </span>
    )
  }

  return (
    <div className="w-full">
      <button
        onClick={() => {
          if (isDir) {
            toggle()
          } else {
            onOpenFile(path)
          }
        }}
        className={`flex w-full items-center gap-1.5 px-2.5 py-1 text-xs hover:bg-accent/40 text-left select-none border-b border-transparent hover:border-accent/10 transition-colors ${textClass}`}
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
        <span className="truncate flex-1 font-medium">{name}</span>
        {loading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground/50 shrink-0" />}
        {statusBadge}
      </button>

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
            />
          ))}
          {children.length === 0 && (
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
