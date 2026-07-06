import { useState, useCallback, useEffect, useRef } from 'react'
import { Group, Panel, Separator, usePanelRef } from 'react-resizable-panels'
import cawSvg from '@/assets/LOGO.svg'
import { WorkspacePanel } from '@/components/WorkspacePanel'
import { TerminalGrid } from '@/components/TerminalGrid'
import {
  type LayoutNode,
  createLeaf,
  splitLeaf,
  removeLeaf,
  collectLeafIds,
  countLeaves,
  setSplitSizes,
  findAgentId,
  findAgentLeaves,
  getLeafCwd,
  getLeaf,
} from '@/lib/layout'
import {
  type Workspace,
  loadState,
  persistWorkspaces,
  subscribeRemoteState,
} from '@/lib/workspaceStore'
import { DraggableTabBar } from '@/components/DraggableTabBar'
import { destroyTerminal, releaseTerminal, setOnTerminalExit } from '@/lib/terminalRegistry'
import { useHotkeys } from '@/hooks/useHotkeys'
import { Settings, Folder, Workflow, Check, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FolderSidebar } from '@/components/FolderSidebar'
import { SettingsDialog } from '@/components/SettingsDialog'
import { CommandPalette } from '@/components/CommandPalette'
import { StatusBar } from '@/components/StatusBar'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Checkbox } from '@/components/ui/checkbox'

const kbd =
  'px-1.5 py-0.5 text-xs font-semibold bg-muted text-muted-foreground rounded border border-border font-mono'

function Shortcut({ keys, label }: { keys: string; label: string }) {
  return (
    <div className="flex items-center gap-6 text-sm text-muted-foreground">
      <kbd className={kbd}>{keys}</kbd>
      <span>{label}</span>
    </div>
  )
}

export function AppLayout() {
  const [loaded, setLoaded] = useState(false)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [activeWorkspaceId, setActiveWorkspaceId] = useState<string | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const sidebarRef = usePanelRef()
  const skipPersistRef = useRef(false)
  const localFocusRef = useRef<Record<string, { tabIndex: number; paneId: string }>>({})

  const [folderSidebarCollapsed, setFolderSidebarCollapsed] = useState(true)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsSection, setSettingsSection] = useState<string | undefined>(undefined)
  const [gitStatuses, setGitStatuses] = useState<Record<string, string>>({})
  const [pickerOpen, setPickerOpen] = useState(false)
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false)
  const [closeConfirm, setCloseConfirm] = useState<{
    type: 'tab' | 'pane';
    targetId: string;
    index?: number;
    agentBranch: string;
    hasUncommitted: boolean;
    hasUnmergedCommits: boolean;
  } | null>(null)
  const [deleteBranchChecked, setDeleteBranchChecked] = useState(false)

  useEffect(() => {
    const savedTheme = (localStorage.getItem('caw:theme') as 'light' | 'dark' | 'system') || 'system'
    const root = window.document.documentElement
    if (savedTheme === 'system') {
      const systemTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
      if (systemTheme === 'light') root.classList.add('light')
      else root.classList.remove('light')
    } else if (savedTheme === 'light') {
      root.classList.add('light')
    } else {
      root.classList.remove('light')
    }
  }, [])

  useEffect(() => {
    const savedCollapsed = localStorage.getItem('caw:sidebarCollapsed')
    if (savedCollapsed === '1') setSidebarCollapsed(true)

    const savedFolderCollapsed = localStorage.getItem('caw:folderSidebarCollapsed')
    if (savedFolderCollapsed === '0') setFolderSidebarCollapsed(false)
  }, [])

  const sidebarDefaultSize = (() => {
    const saved = localStorage.getItem('caw:sidebarSize')
    if (saved) {
      const n = parseFloat(saved)
      if (n >= 15 && n <= 50) return `${n}%`
    }
    return '25%'
  })()

  useEffect(() => {
    let done = false
    loadState().then((s) => {
      if (done) return
      setWorkspaces(s.workspaces)
      setActiveWorkspaceId(s.activeWorkspaceId)
      setLoaded(true)
    })
    return () => { done = true }
  }, [])

  useEffect(() => {
    const unsub = subscribeRemoteState((remote) => {
      skipPersistRef.current = true
      setWorkspaces((prev) => {
        const prevLeafIds = new Set<string>()
        for (const w of prev) for (const t of w.layouts) for (const id of collectLeafIds(t.layout)) prevLeafIds.add(id)
        const nextLeafIds = new Set<string>()
        for (const w of remote.workspaces) for (const t of w.layouts) for (const id of collectLeafIds(t.layout)) nextLeafIds.add(id)
        // A leaf disappearing from the shared workspace state usually means
        // another browser closed or reshaped a tab. Only release this
        // client's local hold on the terminal (dispose xterm.js, drop the
        // WebSocket) — do NOT kill the shared backend PTY, since another
        // client may still be viewing it. Killing is the job of the client
        // that actually closed the pane (forceClosePane/Tab), which calls
        // destroyTerminal. Without this distinction, opening a second
        // browser would kill OpenCode running in the first browser
        // whenever the two clients' views of the workspace differ.
        for (const id of prevLeafIds) if (!nextLeafIds.has(id)) releaseTerminal(id)

        return remote.workspaces.map((rw) => {
          const focus = localFocusRef.current[rw.id]
          if (focus) {
            return { ...rw, activeTabIndex: focus.tabIndex, activePaneId: focus.paneId }
          }
          return rw
        })
      })
      setActiveWorkspaceId(remote.activeWorkspaceId)
    })
    return unsub
  }, [])

  const activeWorkspace = workspaces.find((w) => w.id === activeWorkspaceId) ?? workspaces[0] ?? null
  const layouts = activeWorkspace?.layouts ?? []
  const activeTab = layouts[activeWorkspace?.activeTabIndex ?? 0] ?? null
  const activePaneId = activeWorkspace?.activePaneId ?? ''
  const leafCount = activeTab ? countLeaves(activeTab.layout) : 0
  const currentWorkspacePath = (activeTab && activePaneId && getLeafCwd(activeTab.layout, activePaneId)) || activeWorkspace?.path || ''
  const activeLeaf = activeTab && activePaneId ? getLeaf(activeTab.layout, activePaneId) : null
  const activeWorktreeBranch = activeLeaf?.agentBranch ?? undefined

  const fetchGitStatus = useCallback(async () => {
    if (!currentWorkspacePath) {
      setGitStatuses({})
      return
    }
    try {
      const res = await fetch(`/api/git/status?path=${encodeURIComponent(currentWorkspacePath)}`)
      if (res.ok) {
        const data = await res.json()
        setGitStatuses(data)
      } else {
        setGitStatuses({})
      }
    } catch {
      setGitStatuses({})
    }
  }, [currentWorkspacePath])

  useEffect(() => {
    fetchGitStatus()
  }, [fetchGitStatus])


  const toggleFolderSidebar = useCallback(() => {
    setFolderSidebarCollapsed((v) => {
      const next = !v
      localStorage.setItem('caw:folderSidebarCollapsed', next ? '0' : '1') // 0 = false, 1 = true
      return next
    })
  }, [])

  useEffect(() => {
    if (activeWorkspace) {
      localFocusRef.current[activeWorkspace.id] = {
        tabIndex: activeWorkspace.activeTabIndex,
        paneId: activeWorkspace.activePaneId,
      }
    }
  }, [activeWorkspace?.id, activeWorkspace?.activeTabIndex, activeWorkspace?.activePaneId])

  useEffect(() => {
    if (skipPersistRef.current) {
      skipPersistRef.current = false
      return
    }
    persistWorkspaces(workspaces, activeWorkspaceId)
  }, [workspaces, activeWorkspaceId])

  useEffect(() => {
    if (activeWorkspace && workspaces.length > 0) {
      const e = activeWorkspace.emoji || '🐄'
      document.title = `Caw – ${e} ${activeWorkspace.name}`
    } else {
      document.title = 'Caw – 🐄 no workspace'
    }
  }, [activeWorkspace, workspaces.length])

  const patchWorkspace = useCallback(
    (id: string, fn: (ws: Workspace) => Workspace) => {
      setWorkspaces((prev) => prev.map((w) => (w.id === id ? fn(w) : w)))
    },
    [],
  )

  const updateActiveLayout = useCallback(
    (fn: (layout: LayoutNode) => LayoutNode) => {
      if (!activeWorkspace || !activeTab) return
      patchWorkspace(activeWorkspace.id, (ws) => ({
        ...ws,
        layouts: ws.layouts.map((t) =>
          t.id === activeTab.id ? { ...t, layout: fn(t.layout) } : t,
        ),
      }))
    },
    [activeWorkspace, activeTab, patchWorkspace],
  )

  const reorderTabs = useCallback(
    (from: number, to: number) => {
      if (!activeWorkspace || from === to || from < 0 || to < 0) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const layouts = ws.layouts.slice()
        if (from < 0 || from >= layouts.length || to < 0 || to >= layouts.length) return ws
        const [moved] = layouts.splice(from, 1)
        layouts.splice(to, 0, moved)
        return { ...ws, layouts, activeTabIndex: to }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const setActivePane = useCallback(
    (paneId: string) => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => ({ ...ws, activePaneId: paneId }))
    },
    [activeWorkspace, patchWorkspace],
  )

  const switchTab = useCallback(
    (index: number) => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const tab = ws.layouts[index]
        return {
          ...ws,
          activeTabIndex: index,
          activePaneId: tab ? collectLeafIds(tab.layout)[0] : '',
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const addTab = useCallback(async (cmd?: string[], agentId?: string, label?: string) => {
    if (!activeWorkspace) return
    let cwd = activeWorkspace.path || ''
    let agentBranch: string | undefined = undefined
    let baseBranch: string | undefined = undefined

    if (agentId) {
      try {
        const res = await fetch('/api/agents/setup-workspace', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            projectPath: cwd,
            agentId,
            enableWorktrees: activeWorkspace.enableWorktrees !== false,
          }),
        })
        if (res.ok) {
          const data = await res.json()
          if (data.isGit) {
            cwd = data.worktreePath
            agentBranch = data.branchName
            baseBranch = data.baseBranch
          }
        }
      } catch (err) {
        console.error('Failed to setup agent workspace:', err)
      }
    }

    const leafId = crypto.randomUUID()
    const newTab = {
      id: crypto.randomUUID(),
      name: label || 'Terminal',
      layout: {
        type: 'leaf' as const,
        id: leafId,
        cwd,
        cmd,
        agentId,
        agentBranch,
        baseBranch,
      },
    }
    patchWorkspace(activeWorkspace.id, (ws) => ({
      ...ws,
      layouts: [...ws.layouts, newTab],
      activeTabIndex: ws.layouts.length,
      activePaneId: leafId,
    }))
  }, [activeWorkspace, patchWorkspace])

  const forceCloseTab = useCallback(
    (index: number, deleteBranch?: boolean) => {
      if (!activeWorkspace) return
      const tab = activeWorkspace.layouts[index]
      if (tab) {
        for (const leafId of collectLeafIds(tab.layout)) destroyTerminal(leafId, deleteBranch)
      }
      patchWorkspace(activeWorkspace.id, (ws) => {
        const next = ws.layouts.filter((_, i) => i !== index)
        if (next.length === 0) {
          return {
            ...ws,
            layouts: [],
            activeTabIndex: 0,
            activePaneId: '',
          }
        }
        const newIdx = Math.min(ws.activeTabIndex, next.length - 1)
        return {
          ...ws,
          layouts: next,
          activeTabIndex: newIdx,
          activePaneId: collectLeafIds(next[newIdx].layout)[0] || '',
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const closeTab = useCallback(
    async (index: number) => {
      if (!activeWorkspace) return
      const tab = activeWorkspace.layouts[index]
      if (!tab) return

      const agentLeaves = findAgentLeaves(tab.layout)
      if (agentLeaves.length > 0) {
        const firstLeaf = agentLeaves[0]
        let uncommitted = false
        let unmerged = false
        try {
          const res = await fetch('/api/agents/check-changes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              worktreePath: firstLeaf.cwd,
              branchName: firstLeaf.agentBranch,
              baseBranch: firstLeaf.baseBranch,
            }),
          })
          if (res.ok) {
            const data = await res.json()
            uncommitted = !!data.hasUncommitted
            unmerged = !!data.hasUnmergedCommits
          }
        } catch (err) {
          console.error('Failed to check agent changes:', err)
        }

        setCloseConfirm({
          type: 'tab',
          targetId: tab.id,
          index,
          agentBranch: firstLeaf.agentBranch || '',
          hasUncommitted: uncommitted,
          hasUnmergedCommits: unmerged,
        })
        return
      }

      forceCloseTab(index)
    },
    [activeWorkspace, forceCloseTab],
  )

  const openFile = useCallback((filePath: string) => {
    if (!activeWorkspace) return
    const name = filePath.split(/[\\/]/).pop() || filePath
    
    const existingIndex = activeWorkspace.layouts.findIndex(
      (t) => t.layout.type === 'leaf' && t.layout.filePath === filePath
    )
    if (existingIndex >= 0) {
      switchTab(existingIndex)
      return
    }

    const newTab = {
      id: crypto.randomUUID(),
      name,
      layout: {
        type: 'leaf' as const,
        id: crypto.randomUUID(),
        cwd: activeWorkspace.path || '',
        filePath,
      },
    }

    patchWorkspace(activeWorkspace.id, (ws) => ({
      ...ws,
      layouts: [...ws.layouts, newTab],
      activeTabIndex: ws.layouts.length,
      activePaneId: newTab.layout.id,
    }))
  }, [activeWorkspace, patchWorkspace, switchTab])

  const openDiff = useCallback((filePath?: string) => {
    if (!activeWorkspace) return
    const name = filePath ? `Diff: ${filePath.split(/[\\/]/).pop()}` : 'Git Diff'

    const existingIndex = activeWorkspace.layouts.findIndex(
      (t) => t.layout.type === 'leaf' && t.layout.isDiff === true && t.layout.filePath === filePath
    )
    if (existingIndex >= 0) {
      switchTab(existingIndex)
      return
    }

    const newTab = {
      id: crypto.randomUUID(),
      name,
      layout: {
        type: 'leaf' as const,
        id: crypto.randomUUID(),
        cwd: activeWorkspace.path || '',
        filePath,
        isDiff: true,
      },
    }

    patchWorkspace(activeWorkspace.id, (ws) => ({
      ...ws,
      layouts: [...ws.layouts, newTab],
      activeTabIndex: ws.layouts.length,
      activePaneId: newTab.layout.id,
    }))
  }, [activeWorkspace, patchWorkspace, switchTab])

  const handleSplitVert = useCallback(
    (id: string) => {
      if (!activeWorkspace || !activeTab) return
      const paneCwd = getLeafCwd(activeTab.layout, id) || activeWorkspace.path || ''
      const { node, newLeafId } = splitLeaf(activeTab.layout, id, 'vertical', paneCwd)
      patchWorkspace(activeWorkspace.id, (ws) => ({
        ...ws,
        layouts: ws.layouts.map((t) =>
          t.id === activeTab.id ? { ...t, layout: node } : t,
        ),
        activePaneId: newLeafId,
      }))
    },
    [activeWorkspace, activeTab, patchWorkspace],
  )

  const handleSizesChange = useCallback(
    (splitId: string, sizes: number[]) => {
      if (!activeWorkspace || !activeTab) return
      updateActiveLayout((layout) => setSplitSizes(layout, splitId, sizes))
    },
    [activeWorkspace, activeTab, updateActiveLayout],
  )

  const handleSplitHoriz = useCallback(
    (id: string) => {
      if (!activeWorkspace || !activeTab) return
      const paneCwd = getLeafCwd(activeTab.layout, id) || activeWorkspace.path || ''
      const { node, newLeafId } = splitLeaf(activeTab.layout, id, 'horizontal', paneCwd)
      patchWorkspace(activeWorkspace.id, (ws) => ({
        ...ws,
        layouts: ws.layouts.map((t) =>
          t.id === activeTab.id ? { ...t, layout: node } : t,
        ),
        activePaneId: newLeafId,
      }))
    },
    [activeWorkspace, activeTab, patchWorkspace],
  )

  const forceClosePane = useCallback(
    (id: string, deleteBranch?: boolean) => {
      destroyTerminal(id, deleteBranch)
      if (!activeWorkspace || !activeTab) return
      const newLayout = removeLeaf(activeTab.layout, id)
      const remaining = collectLeafIds(newLayout)
      if (remaining.length === 0) {
        const tabIndex = activeWorkspace.layouts.findIndex((t) => t.id === activeTab.id)
        if (tabIndex >= 0) forceCloseTab(tabIndex, deleteBranch)
        return
      }
      updateActiveLayout(() => newLayout)
      patchWorkspace(activeWorkspace.id, (ws) => {
        if (remaining.includes(ws.activePaneId)) return ws
        return { ...ws, activePaneId: remaining[0] }
      })
    },
    [activeWorkspace, activeTab, updateActiveLayout, patchWorkspace, forceCloseTab],
  )

  const handleClosePane = useCallback(
    async (id: string) => {
      if (!activeWorkspace || !activeTab) return

      const findLeafById = (node: LayoutNode, targetId: string): LayoutNode | null => {
        if (node.type === 'leaf' && node.id === targetId) return node
        if (node.type === 'split') {
          for (const child of node.children) {
            const res = findLeafById(child, targetId)
            if (res) return res
          }
        }
        return null
      }

      const leaf = findLeafById(activeTab.layout, id)
      if (leaf && leaf.type === 'leaf' && leaf.agentBranch && leaf.cwd) {
        let uncommitted = false
        let unmerged = false
        try {
          const res = await fetch('/api/agents/check-changes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              worktreePath: leaf.cwd,
              branchName: leaf.agentBranch,
              baseBranch: leaf.baseBranch,
            }),
          })
          if (res.ok) {
            const data = await res.json()
            uncommitted = !!data.hasUncommitted
            unmerged = !!data.hasUnmergedCommits
          }
        } catch (err) {
          console.error('Failed to check agent changes:', err)
        }

        setCloseConfirm({
          type: 'pane',
          targetId: id,
          agentBranch: leaf.agentBranch,
          hasUncommitted: uncommitted,
          hasUnmergedCommits: unmerged,
        })
        return
      }

      forceClosePane(id)
    },
    [activeWorkspace, activeTab, forceClosePane],
  )

  const handleAddWorkspace = useCallback(
    async (path: string, name: string, emoji: string) => {
      let absPath = path
      try {
        const res = await fetch(`/api/workspace/open?path=${encodeURIComponent(path)}`)
        if (res.ok) {
          const data = await res.json()
          absPath = data.path || path
        }
      } catch { /* fall back to raw path */ }

      const layout = createLeaf(absPath)
      const ws: Workspace = {
        id: crypto.randomUUID(),
        path: absPath,
        name: name || absPath.split(/[\\/]/).filter(Boolean).pop() || absPath || 'Workspace',
        emoji: emoji || undefined,
        layouts: [{ id: crypto.randomUUID(), name: 'Terminal', layout }],
        activeTabIndex: 0,
        activePaneId: collectLeafIds(layout)[0],
        enableWorktrees: true,
      }
      setWorkspaces((prev) => [...prev, ws])
      setActiveWorkspaceId(ws.id)
    },
    [],
  )

  const handleEditWorkspace = useCallback(
    (id: string, name: string, emoji: string) => {
      patchWorkspace(id, (ws) => ({
        ...ws,
        name,
        emoji: emoji || undefined,
      }))
    },
    [patchWorkspace],
  )

  const handleDeleteWorkspace = useCallback(
    (id: string) => {
      setWorkspaces((prev) => {
        const target = prev.find((w) => w.id === id)
        if (target) {
          for (const tab of target.layouts) {
            for (const leafId of collectLeafIds(tab.layout)) destroyTerminal(leafId)
          }
        }
        const next = prev.filter((w) => w.id !== id)
        if (id === activeWorkspaceId) {
          setActiveWorkspaceId(next[0]?.id ?? null)
        }
        return next
      })
    },
    [activeWorkspaceId],
  )

  const toggleWorktrees = useCallback(() => {
    if (!activeWorkspace) return
    patchWorkspace(activeWorkspace.id, (ws) => ({
      ...ws,
      enableWorktrees: !ws.enableWorktrees,
    }))
  }, [activeWorkspace, patchWorkspace])

  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed((v) => {
      const next = !v
      localStorage.setItem('caw:sidebarCollapsed', next ? '1' : '0')
      return next
    })
  }, [])

  const handleReorderWorkspaces = useCallback((from: number, to: number) => {
    if (from === to || from < 0 || to < 0) return
    setWorkspaces((prev) => {
      const next = prev.slice()
      const [moved] = next.splice(from, 1)
      next.splice(to, 0, moved)
      return next
    })
  }, [])

  useHotkeys({
    'Alt+W': () => setPickerOpen(true),
    'Alt+T': () => addTab(),
    'Alt+H': () => { if (activePaneId) handleSplitHoriz(activePaneId) },
    'Alt+V': () => { if (activePaneId) handleSplitVert(activePaneId) },
    'Alt+C': () => { if (activePaneId) handleClosePane(activePaneId) },
    'Alt+P': () => setCommandPaletteOpen(true),
  })

  useEffect(() => {
    setOnTerminalExit((leafId) => handleClosePane(leafId))
    return () => setOnTerminalExit(null)
  }, [handleClosePane])

  if (!loaded) {
    return <div className="flex h-full w-full items-center justify-center text-sm text-muted-foreground">Loading…</div>
  }

  const tabs = activeWorkspace ? (
    <DraggableTabBar
      tabs={layouts.map((t) => ({
        id: t.id,
        name: t.name,
        agentId: findAgentId(t.layout),
        filePath: t.layout.type === 'leaf' ? t.layout.filePath : undefined,
        isDiff: t.layout.type === 'leaf' ? t.layout.isDiff : undefined,
      }))}
      activeIndex={activeWorkspace.activeTabIndex}
      onSwitch={switchTab}
      onClose={closeTab}
      onReorder={reorderTabs}
      onAdd={addTab}
    />
  ) : null

  const terminalBody = activeTab && activeWorkspace && leafCount > 0 ? (
    <div className="relative flex-1 min-h-0">
      <TerminalGrid
        key={activeTab.id}
        node={activeTab.layout}
        activePaneId={activePaneId}
        onFocus={setActivePane}
        onSplitVert={handleSplitVert}
        onSplitHoriz={handleSplitHoriz}
        onClose={handleClosePane}
        leafCount={leafCount}
        cwd={activeWorkspace.path}
        onSizesChange={handleSizesChange}
        gitStatuses={gitStatuses}
        onOpenDiff={openDiff}
      />
    </div>
  ) : activeTab && activeWorkspace && leafCount === 0 ? (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
      <img src={cawSvg} alt="" className="w-[35%] h-auto max-w-[300px]" style={{ filter: 'brightness(0) invert(0.55) opacity(0.2)' }} />
      <div className="grid grid-cols-2 gap-x-10 gap-y-3 mt-4">
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+W" label="New workspace" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+C" label="Close pane" />
        </div>
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+H" label="Horizontal split" />
          <Shortcut keys="Alt+V" label="Vertical split" />
          <Shortcut keys="Alt+P" label="Command palette" />
        </div>
      </div>
    </div>
  ) : activeWorkspace && layouts.length === 0 ? (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <img src={cawSvg} alt="" className="w-[35%] h-auto max-w-[300px]" style={{ filter: 'brightness(0) invert(0.55) opacity(0.2)' }} />
      <div className="grid grid-cols-2 gap-x-10 gap-y-3 mt-4">
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+W" label="New workspace" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+C" label="Close pane" />
        </div>
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+H" label="Horizontal split" />
          <Shortcut keys="Alt+V" label="Vertical split" />
          <Shortcut keys="Alt+P" label="Command palette" />
        </div>
      </div>
    </div>
  ) : (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <img src={cawSvg} alt="" className="w-[35%] h-auto max-w-[300px]" style={{ filter: 'brightness(0) invert(0.55) opacity(0.2)' }} />
      <div className="grid grid-cols-2 gap-x-10 gap-y-3 mt-4">
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+W" label="New workspace" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+C" label="Close pane" />
        </div>
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+H" label="Horizontal split" />
          <Shortcut keys="Alt+V" label="Vertical split" />
          <Shortcut keys="Alt+P" label="Command palette" />
        </div>
      </div>
    </div>
  )

  return (
    <div className="flex flex-col h-full w-full bg-background select-none">
      <div className="flex-1 min-h-0">
        <div className="flex h-full w-full">
          <Group orientation="horizontal" className="flex-1">
            {/* Left Workspace Panel */}
            {sidebarCollapsed ? (
              <WorkspacePanel
                workspaces={workspaces}
                activeWorkspaceId={activeWorkspaceId}
                onSelectWorkspace={setActiveWorkspaceId}
                onAddWorkspace={handleAddWorkspace}
                onDeleteWorkspace={handleDeleteWorkspace}
                onEditWorkspace={handleEditWorkspace}
                onReorderWorkspaces={handleReorderWorkspaces}
                collapsed={true}
                onToggle={toggleSidebar}
                pickerOpen={pickerOpen}
                onPickerOpenChange={setPickerOpen}
              />
            ) : (
              <>
                <Panel
                  id="sidebar"
                  panelRef={sidebarRef}
                  defaultSize={sidebarDefaultSize}
                  minSize="15%"
                  maxSize="50%"
                  onResize={(size) => {
                    if (size.asPercentage >= 15) {
                      localStorage.setItem('caw:sidebarSize', String(size.asPercentage))
                    }
                  }}
                >
                  <WorkspacePanel
                    workspaces={workspaces}
                    activeWorkspaceId={activeWorkspaceId}
                    onSelectWorkspace={setActiveWorkspaceId}
                    onAddWorkspace={handleAddWorkspace}
                    onDeleteWorkspace={handleDeleteWorkspace}
                    onEditWorkspace={handleEditWorkspace}
                    onReorderWorkspaces={handleReorderWorkspaces}
                    collapsed={false}
                    onToggle={toggleSidebar}
                    pickerOpen={pickerOpen}
                    onPickerOpenChange={setPickerOpen}
                  />
                </Panel>
                <Separator className="w-px bg-border hover:bg-ring hover:w-[3px] transition-all cursor-col-resize" />
              </>
            )}

            {/* Main Terminals / Editors Content */}
            <Panel>
              <div className="flex flex-col h-full">
                <div className="flex items-center border-b border-border bg-secondary/20 h-[33px] shrink-0">
                  <div className="flex flex-1 overflow-x-auto h-full" style={{ scrollbarWidth: 'thin', scrollbarColor: 'hsl(var(--border)) transparent' }}>
                    {tabs}
                  </div>
                  <div className="flex items-center shrink-0 h-full">
                    {/* Settings Button */}
                    <div className="flex items-center justify-center border-l border-border h-full bg-background select-none" style={{ width: 44 }}>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground"
                        onClick={() => setSettingsOpen(true)}
                        title="Settings"
                      >
                        <Settings className="h-3.5 w-3.5" />
                      </Button>
                    </div>

                    {/* Workflow Button (Always Visible) */}
                    <div className="flex items-center justify-center border-l border-border h-full bg-background select-none" style={{ width: 44 }}>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground"
                            onClick={toggleWorktrees}
                          >
                            <Workflow className={activeWorkspace?.enableWorktrees ? 'lava-lamp-icon h-3.5 w-3.5' : 'h-3.5 w-3.5 opacity-50'} />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent className="flex items-center select-none">
                          {activeWorkspace?.enableWorktrees ? (
                            <>
                              <Check className="h-3.5 w-3.5 text-green-500 mr-1.5 shrink-0 animate-bounce" />
                              <span>Worktrees</span>
                            </>
                          ) : (
                            <>
                              <X className="h-3.5 w-3.5 text-red-500 mr-1.5 shrink-0" />
                              <span>Worktrees</span>
                            </>
                          )}
                        </TooltipContent>
                      </Tooltip>
                    </div>

                    {/* Folder Button (Only Visible when Sidebar is Collapsed) */}
                    {folderSidebarCollapsed && (
                      <div className="flex items-center justify-center border-l bg-background border-border h-full select-none" style={{ width: 44 }}>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground"
                          onClick={toggleFolderSidebar}
                          title="Workspace Files"
                        >
                          <Folder className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    )}
                  </div>
                </div>
                {terminalBody}
              </div>
            </Panel>

            {/* Right Folder Explorer Panel (only if expanded) */}
            {!folderSidebarCollapsed && (
              <>
                <Separator className="w-px bg-border hover:bg-ring hover:w-[3px] transition-all cursor-col-resize" />
                <Panel
                  id="folder-sidebar"
                  defaultSize="20%"
                  minSize="15%"
                  maxSize="50%"
                >
                  <FolderSidebar
                    workspacePath={currentWorkspacePath}
                    mainWorkspacePath={activeWorkspace?.path || ''}
                    onOpenFile={openFile}
                    gitStatuses={gitStatuses}
                    onRefresh={fetchGitStatus}
                    onClose={() => setFolderSidebarCollapsed(true)}
                  />
                </Panel>
              </>
            )}
          </Group>
        </div>
      </div>

      <StatusBar
        workspaceName={activeWorkspace?.name}
        worktreeBranch={activeWorktreeBranch}
        onOpenSettings={(section) => {
          setSettingsSection(section)
          setSettingsOpen(true)
        }}
      />

      <SettingsDialog open={settingsOpen} onOpenChange={(open) => { setSettingsOpen(open); if (!open) setSettingsSection(undefined) }} initialSection={settingsSection} />

      <CommandPalette
        open={commandPaletteOpen}
        onOpenChange={setCommandPaletteOpen}
        workspacePath={activeWorkspace?.path || ''}
        onOpenFile={openFile}
        onAddTerminal={addTab}
        onAddAgent={(cmd, agentId, label) => addTab(cmd, agentId, label)}
        onOpenWorkspacePicker={() => setPickerOpen(true)}
      />

      <Dialog
        open={closeConfirm !== null}
        onOpenChange={(open) => {
          if (!open) {
            setCloseConfirm(null)
            setDeleteBranchChecked(false)
          }
        }}
      >
        <DialogContent className="max-w-md p-6">
          <DialogHeader>
            <DialogTitle className="text-base font-semibold text-foreground">
              Close workspace
            </DialogTitle>
            <DialogDescription className="text-xs text-muted-foreground mt-2 space-y-2" asChild>
              <div>
                <div>
                  Closing this {closeConfirm?.type === 'tab' ? 'tab' : 'pane'} will delete the temporary worktree directory.
                </div>
                
                {closeConfirm && (closeConfirm.hasUncommitted || closeConfirm.hasUnmergedCommits) ? (
                  <div className="text-red-400">
                    ⚠️ Branch {closeConfirm.agentBranch} has unmerged changes.
                  </div>
                ) : (
                  <div className="text-muted-foreground">
                    ✓ Branch {closeConfirm?.agentBranch} is clean with no unmerged changes.
                  </div>
                )}

                <div className="flex items-center gap-2 pt-2">
                  <Checkbox
                    id="delete-branch-cb"
                    checked={deleteBranchChecked}
                    onChange={(e) => setDeleteBranchChecked(e.target.checked)}
                  />
                  <label
                    htmlFor="delete-branch-cb"
                    className="cursor-pointer select-none text-xs text-muted-foreground hover:text-foreground"
                  >
                    Delete branch {closeConfirm?.agentBranch}
                  </label>
                </div>
              </div>
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2 mt-4">
            <Button variant="ghost" onClick={() => { setCloseConfirm(null); setDeleteBranchChecked(false) }}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (closeConfirm) {
                  if (closeConfirm.type === 'tab') {
                    if (closeConfirm.index !== undefined) forceCloseTab(closeConfirm.index, deleteBranchChecked)
                  } else {
                    forceClosePane(closeConfirm.targetId, deleteBranchChecked)
                  }
                  setCloseConfirm(null)
                  setDeleteBranchChecked(false)
                }
              }}
            >
              Close
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <svg style={{ width: 0, height: 0, position: 'absolute' }}>
        <linearGradient id="lava-gradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" className="lava-stop-1" />
          <stop offset="50%" className="lava-stop-2" />
          <stop offset="100%" className="lava-stop-3" />
        </linearGradient>
      </svg>
    </div>
  )
}