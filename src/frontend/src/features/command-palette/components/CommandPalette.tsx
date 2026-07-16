import { useEffect, useState, useCallback, useRef, useMemo } from 'react'
import { Search, Terminal, FolderPlus, Bot, File, Loader2 } from 'lucide-react'
import { Dialog, DialogContent } from '@/components/dialog'
import { Input } from '@/components/input'

import { cn } from '@/features/shared/utils/utils'
import { agentTypes, getEffectiveAgentCmd } from '@/features/agents/services/agentTypes'

interface AgentInfo {
  id: string
  label: string
  cmd: string[]
}

interface FileResult {
  name: string
  path: string
  isDir: boolean
}

interface PaletteItem {
  id: string
  label: string
  description?: string
  type: 'command' | 'file'
  icon: React.ReactNode
  action: () => void
}

interface CommandPaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  workspacePath: string
  onOpenFile: (path: string) => void
  onAddTerminal: () => void
  onAddAgent: (cmd: string[], agentId: string, label: string, env?: [string, string][]) => void
  onOpenWorkspacePicker: () => void
}

export function CommandPalette({
  open,
  onOpenChange,
  workspacePath,
  onOpenFile,
  onAddTerminal,
  onAddAgent,
  onOpenWorkspacePicker,
}: CommandPaletteProps) {
  const [query, setQuery] = useState('')
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [disabledAgents, setDisabledAgents] = useState<string[]>([])
  const [fileResults, setFileResults] = useState<FileResult[]>([])
  const [searching, setSearching] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const searchTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    if (!open) return
    fetch('/api/agents')
      .then((r) => r.ok ? r.json() : Promise.resolve({ data: [] }))
      .then((json) => setAgents(json?.data ?? []))
      .catch(() => setAgents([]))

    const savedDisabled = localStorage.getItem('caw:disabledAgents')
    if (savedDisabled) {
      try {
        setDisabledAgents(JSON.parse(savedDisabled))
      } catch {
        setDisabledAgents([])
      }
    } else {
      setDisabledAgents([])
    }
  }, [open])

  useEffect(() => {
    if (!open) {
      setQuery('')
      setFileResults([])
      setSelectedIndex(0)
      setSearching(false)
    }
  }, [open])

  const commandMode = query.startsWith('>')
  const commandQuery = commandMode ? query.slice(1).trim() : query

  useEffect(() => {
    if (!open || !workspacePath || !query.trim() || commandMode) {
      setFileResults([])
      setSearching(false)
      return
    }
    setSearching(true)
    clearTimeout(searchTimerRef.current)
    searchTimerRef.current = setTimeout(async () => {
      try {
        const res = await fetch(`/api/workspaces/files?q=${encodeURIComponent(query)}&root=${encodeURIComponent(workspacePath)}`)
        if (res.ok) {
          const json = await res.json()
          setFileResults(json?.data ?? [])
        } else {
          setFileResults([])
        }
      } catch {
        setFileResults([])
      }
      setSearching(false)
    }, 200)
    return () => clearTimeout(searchTimerRef.current)
  }, [query, open, workspacePath, commandMode])

  const items: PaletteItem[] = useMemo(() => {
    const result: PaletteItem[] = []

    result.push({
      id: 'new-terminal',
      label: '> New Terminal',
      type: 'command',
      icon: <Terminal className="h-4 w-4" />,
      action: () => { onAddTerminal(); onOpenChange(false) },
    })

    result.push({
      id: 'new-workspace',
      label: '> New Workspace',
      type: 'command',
      icon: <FolderPlus className="h-4 w-4" />,
      action: () => { onOpenWorkspacePicker(); onOpenChange(false) },
    })

    for (const agent of agents) {
      if (disabledAgents.includes(agent.id)) continue
      const agentMeta = agentTypes[agent.id]
      const IconComponent = agentMeta?.icon || Bot
      result.push({
        id: `agent-${agent.id}`,
        label: `> New ${agent.label}`,
        type: 'command',
        icon: <IconComponent className="h-4 w-4" />,
        action: () => { onAddAgent(getEffectiveAgentCmd(agent.id, agent.cmd), agent.id, agent.label, agentMeta?.env); onOpenChange(false) },
      })
    }

    for (const file of fileResults) {
      if (file.isDir) continue
      result.push({
        id: `file-${file.path}`,
        label: file.name,
        description: file.path,
        type: 'file',
        icon: <File className="h-4 w-4" />,
        action: () => { onOpenFile(file.path); onOpenChange(false) },
      })
    }

    return result
  }, [agents, disabledAgents, fileResults, onAddTerminal, onOpenChange, onOpenWorkspacePicker, onAddAgent, onOpenFile])

  const filtered = useMemo(() => {
    if (!query.trim()) return items
    const q = (commandMode ? commandQuery : query).toLowerCase()
    if (!q) return commandMode ? items.filter((item) => item.type === 'command') : items
    return items.filter((item) => {
      if (commandMode && item.type !== 'command') return false
      if (item.label.toLowerCase().includes(q)) return true
      if (item.description?.toLowerCase().includes(q)) return true
      return false
    })
  }, [items, query, commandMode, commandQuery])

  const commandItems = filtered.filter((item) => item.type === 'command')
  const fileItems = filtered.filter((item) => item.type === 'file')
  const hasCommands = commandItems.length > 0
  const hasFiles = fileItems.length > 0
  const safeSelectedIndex = Math.min(selectedIndex, Math.max(filtered.length - 1, 0))

  const getGlobalIndex = useCallback(
    (localIdx: number, section: 'command' | 'file') =>
      section === 'command' ? localIdx : commandItems.length + localIdx,
    [commandItems.length],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex((prev) => Math.min(prev + 1, filtered.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex((prev) => Math.max(prev - 1, 0))
      } else if (e.key === 'Enter' && filtered[safeSelectedIndex]) {
        e.preventDefault()
        filtered[safeSelectedIndex].action()
      }
    },
    [filtered, safeSelectedIndex],
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-0 gap-0 overflow-hidden" data-testid="command-palette">
        <div className="flex items-center border-b border-border px-3">
          <Search className="h-4 w-4 text-muted-foreground shrink-0" />
          <Input
            ref={inputRef}
            autoFocus
            value={query}
            onChange={(e) => { setQuery(e.target.value); setSelectedIndex(0) }}
            onKeyDown={handleKeyDown}
            placeholder={commandMode ? "Type to filter commands..." : "Type to search... (prefix with > for commands only)"}
            className="border-0 shadow-none focus-visible:ring-0 pl-2"
            data-testid="command-palette-input"
          />
        </div>

        <div className="max-h-[50vh] overflow-y-auto">
          {searching && (
            <div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
              <Loader2 className="h-3 w-3 animate-spin" />
              Searching files...
            </div>
          )}

          {hasCommands && (
            <div>
              {query.trim() && (
                <div className="px-3 py-1 text-xs text-muted-foreground font-medium">Commands</div>
              )}
              {commandItems.map((item, idx) => {
                const globalIdx = getGlobalIndex(idx, 'command')
                return (
                  <button
                    key={item.id}
                    onClick={item.action}
                    onMouseEnter={() => setSelectedIndex(globalIdx)}
                    data-testid={`command-palette-item-${item.id}`}
                    className={cn(
                      'flex w-full items-center gap-2 px-3 py-1.5 text-sm text-left',
                      globalIdx === safeSelectedIndex ? 'bg-accent text-accent-foreground' : 'text-foreground',
                    )}
                  >
                    <span className="text-muted-foreground shrink-0">{item.icon}</span>
                    <span className="truncate">{item.label}</span>
                  </button>
                )
              })}
            </div>
          )}

          {hasFiles && (
            <div>
              {hasCommands && <div className="border-t border-border my-1" />}
              <div className="px-3 py-1 text-xs text-muted-foreground font-medium">Files</div>
              {fileItems.map((item, idx) => {
                const globalIdx = getGlobalIndex(idx, 'file')
                return (
                  <button
                    key={item.id}
                    onClick={item.action}
                    onMouseEnter={() => setSelectedIndex(globalIdx)}
                    data-testid={`command-palette-item-${item.id}`}
                    className={cn(
                      'flex w-full items-center gap-2 px-3 py-1.5 text-sm text-left',
                      globalIdx === safeSelectedIndex ? 'bg-accent text-accent-foreground' : 'text-foreground',
                    )}
                  >
                    <span className="text-muted-foreground shrink-0">{item.icon}</span>
                    <span className="truncate flex-1">{item.label}</span>
                    {item.description && (
                      <span className="text-xs text-muted-foreground truncate max-w-[200px]">{item.description}</span>
                    )}
                  </button>
                )
              })}
            </div>
          )}

          {!hasCommands && !hasFiles && !searching && query.trim() && (
            <div className="px-3 py-4 text-xs text-muted-foreground text-center">
              No results for &quot;{query}&quot;
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
