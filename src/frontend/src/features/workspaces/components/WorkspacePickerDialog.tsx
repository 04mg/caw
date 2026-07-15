import { useEffect, useState, useCallback, useRef } from 'react'
import { Search, Folder, Check, ArrowLeft, FolderPlus, Pencil, Trash2 } from 'lucide-react'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Input } from '@/components/input'

import { LazyTree } from '@/features/explorer/components/LazyTree'
import { SmartContextMenu } from '@/features/explorer/components/SmartContextMenu'
import { DeleteDialog } from '@/features/explorer/components/DeleteDialog'
import { type FileNode } from '@/features/explorer/types'
import { EmojiPicker } from 'frimousse'

const commonEmojis = ['🚀', '💻', '⚡', '🎯', '🔥', '🌈', '🌟', '🎨', '💡', '📁', '🔧', '📊', '🎮', '🤖', '🛠️', '📦', '🔬', '🎪', '🏗️', '🧩', '🎭', '📡', '🔍', '💎', '🌿', '🍀', '🎵', '📚', '⚙️', '🧪']

function randomEmoji(): string {
  return commonEmojis[Math.floor(Math.random() * commonEmojis.length)]
}


interface WorkspacePickerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onChoose: (path: string, name: string, emoji: string) => void
}

export function WorkspacePickerDialog({ open, onOpenChange, onChoose }: WorkspacePickerDialogProps) {
  const [step, setStep] = useState(1)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [focusPath, setFocusPath] = useState<string | null>(null)
  const [results, setResults] = useState<FileNode[]>([])
  const [searching, setSearching] = useState(false)
  const [browseRoot, setBrowseRoot] = useState('/')
  const treeScrollRef = useRef<HTMLDivElement>(null)
  const treeContainerRef = useRef<HTMLDivElement>(null)
  const [refreshCounter, setRefreshCounter] = useState(0)
  const [createFolderParent, setCreateFolderParent] = useState<string | null>(null)
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; path: string; name: string } | null>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const [editingPath, setEditingPath] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ path: string; name: string } | null>(null)
  const [hoveredPath, setHoveredPath] = useState<string | null>(null)

  useEffect(() => {
    if (!contextMenu) return
    const onDown = (event: MouseEvent) => {
      if (contextMenuRef.current?.contains(event.target as Node)) return
      setContextMenu(null)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [contextMenu])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!open || event.key !== 'F2' || !hoveredPath || hoveredPath === '/' || editingPath) return
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return
      event.preventDefault()
      setEditingPath(hoveredPath)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [open, hoveredPath, editingPath])

  const [wsName, setWsName] = useState('')
  const [emoji, setEmoji] = useState('')

  const runSearch = useCallback(async (q: string, root: string) => {
    if (!q.trim()) {
      setResults([])
      setSearching(false)
      return
    }
    setSearching(true)
    try {
      const res = await fetch(`/api/workspaces/directories?q=${encodeURIComponent(q)}&root=${encodeURIComponent(root)}`)
      if (res.ok) {
        const json = await res.json()
        setResults(json?.data ?? [])
      } else {
        setResults([])
      }
    } catch { setResults([]) }
    setSearching(false)
  }, [])

  useEffect(() => {
    if (!open) {
      setStep(1)
      setQuery('')
      setSelected(null)
      setFocusPath(null)
      setResults([])
      setSearching(false)
      setBrowseRoot('/')
      setWsName('')
      setEmoji('')
      setCreateFolderParent(null)
      setContextMenu(null)
      setEditingPath(null)
      setDeleteTarget(null)
      setHoveredPath(null)
    }
  }, [open])

  const selectAndExpand = useCallback((path: string) => {
    setSelected(path)
    setBrowseRoot(path)
    setFocusPath(path)
    setQuery('')
    setResults([])
  }, [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const q = query.trim()
    if (!q) return
    if (results.length > 0) {
      const match = results.find((r) => r.name.toLowerCase() === q.toLowerCase()) ?? results[0]
      selectAndExpand(match.path)
    }
  }

  const handleSelect = useCallback((path: string) => {
    setSelected(path)
    setBrowseRoot(path)
  }, [])

  const goToStep2 = () => {
    if (!selected) return
    const defaultName = selected.split(/[\\/]/).filter(Boolean).pop() || 'Workspace'
    setWsName(defaultName)
    setEmoji(randomEmoji())
    setStep(2)
  }

  const confirm = () => {
    if (selected && wsName) onChoose(selected, wsName, emoji)
  }

  const createFolder = useCallback(async (parentPath: string, name: string) => {
    const sep = parentPath.includes('\\') ? '\\' : '/'
    const path = parentPath.endsWith(sep) ? parentPath + name : parentPath + sep + name
    setCreateFolderParent(null)
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path, type: 'dir' }),
      })
      if (res.ok) {
        setFocusPath(path)
        setRefreshCounter((c) => c + 1)
      }
    } catch { /* ignore */ }
  }, [])

  const renameFolder = useCallback(async (oldPath: string, name: string) => {
    const sep = oldPath.includes('\\') ? '\\' : '/'
    const parentPath = oldPath.substring(0, oldPath.lastIndexOf(sep)) || sep
    const newPath = parentPath + (parentPath.endsWith(sep) ? '' : sep) + name
    setEditingPath(null)
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ oldPath, newPath }),
      })
      if (res.ok) {
        setFocusPath(newPath)
        setRefreshCounter((c) => c + 1)
      }
    } catch { /* ignore */ }
  }, [])

  const deleteFolder = useCallback(async () => {
    const target = deleteTarget
    if (!target) return
    setDeleteTarget(null)
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: target.path }),
      })
      if (res.ok) {
        if (selected === target.path) setSelected(null)
        setRefreshCounter((c) => c + 1)
      }
    } catch { /* ignore */ }
  }, [deleteTarget, selected])

  const showDropdown = query.trim().length > 0 && (searching || results.length > 0)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        {step === 1 ? (
          <>
            <DialogHeader>
              <DialogTitle>Create workspace</DialogTitle>
              <DialogDescription>
                Search for a directory by name.
              </DialogDescription>
            </DialogHeader>

            <form onSubmit={handleSubmit} className="relative shrink-0">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                autoFocus
                value={query}
                placeholder="Search directory..."
                onChange={(e) => {
                  setQuery(e.target.value)
                  runSearch(e.target.value, browseRoot)
                }}
                className="pl-8"
              />
            </form>

            <div ref={treeContainerRef} className="relative h-[50vh] shrink-0 border border-border rounded-md overflow-visible">
              <div ref={treeScrollRef} className="h-full overflow-auto">
                <LazyTree
                  rootPath="/"
                  selected={selected}
                  onSelect={handleSelect}
                  focusPath={focusPath}
                  refreshCounter={refreshCounter}
                  onShowContextMenu={(path, name, x, y) => {
                    const rect = treeContainerRef.current?.getBoundingClientRect()
                    if (!rect) return
                    setContextMenu({ path, name, x: x - rect.left, y: y - rect.top })
                  }}
                  createTargetPath={createFolderParent}
                  onCreateFolder={createFolder}
                  onCreateCancel={() => setCreateFolderParent(null)}
                  editingPath={editingPath}
                  onRenameFolder={renameFolder}
                  onRenameCancel={() => setEditingPath(null)}
                  onHoverPath={setHoveredPath}
                />
              </div>

              {contextMenu && (
                <SmartContextMenu
                  x={contextMenu.x}
                  y={contextMenu.y}
                  ref={contextMenuRef}
                  position="absolute"
                  bounds={{ width: treeContainerRef.current?.clientWidth ?? 0, height: treeContainerRef.current?.clientHeight ?? 0 }}
                >
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      setContextMenu(null)
                      setCreateFolderParent(contextMenu.path)
                    }}
                    className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
                  >
                    <FolderPlus className="h-3.5 w-3.5" />
                    New Folder
                  </button>
                  {contextMenu.path !== '/' && (
                    <>
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          setContextMenu(null)
                          setEditingPath(contextMenu.path)
                        }}
                        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-foreground hover:bg-accent/60"
                      >
                        <Pencil className="h-3.5 w-3.5" />
                        Rename
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          const target = contextMenu
                          setContextMenu(null)
                          setDeleteTarget({ path: target.path, name: target.name })
                        }}
                        className="flex w-full items-center gap-2 px-2 py-1.5 text-xs text-red-400 hover:bg-destructive hover:text-destructive-foreground"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                        Delete
                      </button>
                    </>
                  )}
                </SmartContextMenu>
              )}

              {showDropdown && (
                <div className="absolute inset-0 z-10 bg-background overflow-auto py-1">
                  {searching && <p className="px-3 py-2 text-xs text-muted-foreground">Searching…</p>}
                  {results.map((r) => (
                    <button
                      key={r.path}
                      onClick={() => selectAndExpand(r.path)}
                      className={`flex w-full items-center gap-1.5 px-3 py-1 text-sm hover:bg-accent/50 text-left ${
                        selected && selected === r.path ? 'bg-accent/70' : ''
                      }`}
                    >
                      <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      <span className="truncate">{r.name}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            <DeleteDialog
              target={deleteTarget ? { ...deleteTarget, isDir: true } : null}
              onConfirm={deleteFolder}
              onCancel={() => setDeleteTarget(null)}
            />

            <div className="flex items-center justify-between gap-2 shrink-0">
              <span className="text-xs text-muted-foreground truncate">
                {selected ? selected : 'No directory selected'}
              </span>
              <div className="flex gap-2 shrink-0">
                <button
                  onClick={() => onOpenChange(false)}
                  className="px-3 py-1.5 text-xs rounded-md border border-border hover:bg-accent/40"
                >
                  Cancel
                </button>
                <button
                  onClick={goToStep2}
                  disabled={!selected}
                  className="px-3 py-1.5 text-xs rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 flex items-center gap-1"
                >
                  Next
                </button>
              </div>
            </div>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Create workspace</DialogTitle>
              <DialogDescription>
                Give your workspace a name and pick an emoji.
              </DialogDescription>
            </DialogHeader>

            <div className="flex flex-col gap-4 py-2">
              <div className="flex flex-col gap-1.5">
                <label className="text-xs text-muted-foreground font-medium">Name</label>
                <Input
                  autoFocus
                  value={wsName}
                  onChange={(e) => setWsName(e.target.value)}
                  placeholder="Workspace name"
                  className="w-full"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-xs text-muted-foreground font-medium">Emoji</label>
                <div className="flex items-center gap-2">
                  <div className={`h-8 w-8 rounded-md border border-border flex items-center justify-center text-lg ${emoji ? '' : 'text-muted-foreground'}`}>
                    {emoji || '○'}
                  </div>
                  <span className="text-xs text-muted-foreground">{emoji ? 'Click an emoji below to change' : 'Select an emoji'}</span>
                </div>
                <div className="mt-1 w-full rounded-md border border-border overflow-hidden">
                  <EmojiPicker.Root
                    onEmojiSelect={({ emoji }) => setEmoji(emoji)}
                    className="isolate flex h-[240px] w-full flex-col bg-background"
                  >
                    <EmojiPicker.Search className="z-10 mx-2 mt-2 mb-1 appearance-none rounded-md bg-secondary px-2.5 py-1.5 text-sm text-foreground placeholder-muted-foreground outline-hidden" />
                    <EmojiPicker.Viewport className="relative flex-1 w-full outline-hidden">
                      <EmojiPicker.Loading className="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">
                        Loading…
                      </EmojiPicker.Loading>
                      <EmojiPicker.Empty className="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">
                        No emoji found.
                      </EmojiPicker.Empty>
                      <EmojiPicker.List
                        className="select-none pb-1 w-full"
                        components={{
                          CategoryHeader: ({ category, ...props }) => (
                            <div
                              className="bg-background px-3 pt-2 pb-1 text-xs font-medium text-muted-foreground"
                              {...props}
                            >
                              {category.label}
                            </div>
                          ),
                          Row: ({ children, ...props }) => (
                            <div className="scroll-my-1 px-1 w-full flex" {...props}>{children}</div>
                          ),
                          Emoji: ({ emoji, ...props }) => (
                            <button
                              className="flex flex-1 items-center justify-center rounded-md text-lg data-[active]:bg-accent aspect-square min-w-0"
                              {...props}
                            >
                              {emoji.emoji}
                            </button>
                          ),
                        }}
                      />
                    </EmojiPicker.Viewport>
                  </EmojiPicker.Root>
                </div>
              </div>
            </div>

            <div className="flex items-center justify-between gap-2 shrink-0">
              <button
                onClick={() => setStep(1)}
                className="flex items-center gap-1 px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground rounded-md hover:bg-accent/40"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                Back
              </button>
              <div className="flex gap-2 shrink-0">
                <button
                  onClick={() => onOpenChange(false)}
                  className="px-3 py-1.5 text-xs rounded-md border border-border hover:bg-accent/40"
                >
                  Cancel
                </button>
                <button
                  onClick={confirm}
                  disabled={!wsName.trim()}
                  className="px-3 py-1.5 text-xs rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 flex items-center gap-1"
                >
                  <Check className="h-3.5 w-3.5" />
                  Create
                </button>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
