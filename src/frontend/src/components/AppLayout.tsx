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
} from '@/lib/layout'
import {
  type Workspace,
  loadState,
  persistWorkspaces,
  subscribeRemoteState,
} from '@/lib/workspaceStore'
import { DraggableTabBar } from '@/components/DraggableTabBar'
import { destroyTerminal } from '@/lib/terminalRegistry'
import { useHotkeys } from '@/hooks/useHotkeys'

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

  useEffect(() => {
    const savedCollapsed = localStorage.getItem('caw:sidebarCollapsed')
    if (savedCollapsed === '1') setSidebarCollapsed(true)
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
        for (const id of prevLeafIds) if (!nextLeafIds.has(id)) destroyTerminal(id)

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

  const addTab = useCallback(() => {
    if (!activeWorkspace) return
    const newTab = {
      id: crypto.randomUUID(),
      name: 'Terminal',
      layout: createLeaf(activeWorkspace.path || ''),
    }
    patchWorkspace(activeWorkspace.id, (ws) => ({
      ...ws,
      layouts: [...ws.layouts, newTab],
      activeTabIndex: ws.layouts.length,
      activePaneId: collectLeafIds(newTab.layout)[0],
    }))
  }, [activeWorkspace, patchWorkspace])

  const closeTab = useCallback(
    (index: number) => {
      if (!activeWorkspace) return
      const tab = activeWorkspace.layouts[index]
      if (tab) {
        for (const leafId of collectLeafIds(tab.layout)) destroyTerminal(leafId)
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

  const handleSplitVert = useCallback(
    (id: string) => {
      if (!activeWorkspace) return
      updateActiveLayout((layout) =>
        splitLeaf(layout, id, 'vertical', activeWorkspace.path || ''),
      )
    },
    [activeWorkspace, updateActiveLayout],
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
      if (!activeWorkspace) return
      updateActiveLayout((layout) =>
        splitLeaf(layout, id, 'horizontal', activeWorkspace.path || ''),
      )
    },
    [activeWorkspace, updateActiveLayout],
  )

  const handleClosePane = useCallback(
    (id: string) => {
      if (!activeWorkspace || !activeTab) return
      const newLayout = removeLeaf(activeTab.layout, id)
      const remaining = collectLeafIds(newLayout)
      if (remaining.length === 0) {
        const tabIndex = activeWorkspace.layouts.findIndex((t) => t.id === activeTab.id)
        if (tabIndex >= 0) closeTab(tabIndex)
        return
      }
      updateActiveLayout(() => newLayout)
      patchWorkspace(activeWorkspace.id, (ws) => {
        if (remaining.includes(ws.activePaneId)) return ws
        return { ...ws, activePaneId: remaining[0] }
      })
    },
    [activeWorkspace, activeTab, updateActiveLayout, patchWorkspace, closeTab],
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

  const [pickerTrigger, setPickerTrigger] = useState(0)

  useHotkeys({
    'Alt+W': () => setPickerTrigger((v) => v + 1),
    'Alt+T': () => addTab(),
    'Alt+H': () => { if (activePaneId) handleSplitHoriz(activePaneId) },
    'Alt+V': () => { if (activePaneId) handleSplitVert(activePaneId) },
    'Alt+C': () => { if (activePaneId) handleClosePane(activePaneId) },
  })

  if (!loaded) {
    return <div className="flex h-full w-full items-center justify-center text-sm text-muted-foreground">Loading…</div>
  }

  const tabs = activeWorkspace ? (
    <DraggableTabBar
      tabs={layouts.map((t) => ({ id: t.id, name: t.name }))}
      activeIndex={activeWorkspace.activeTabIndex}
      onSwitch={switchTab}
      onClose={closeTab}
      onReorder={reorderTabs}
      onAdd={addTab}
    />
  ) : null

  const tabBar = (
    <div className="flex items-center border-b border-border bg-secondary/20 h-[33px] shrink-0">
      <div className="flex flex-1 overflow-x-auto h-full">
        {tabs}
      </div>
    </div>
  )

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
        </div>
      </div>
    </div>
  )

  const mainContent = (
    <div className="flex h-full flex-col">
      {tabBar}
      {terminalBody}
    </div>
  )

  if (sidebarCollapsed) {
    return (
      <div key="collapsed" className="flex flex-col h-full w-full">
        <div className="flex h-[33px] shrink-0">
          <WorkspacePanel
            workspaces={workspaces}
            activeWorkspaceId={activeWorkspaceId}
            onSelectWorkspace={setActiveWorkspaceId}
            onAddWorkspace={handleAddWorkspace}
            onDeleteWorkspace={handleDeleteWorkspace}
            onEditWorkspace={handleEditWorkspace}
            onReorderWorkspaces={handleReorderWorkspaces}
            pickerTrigger={pickerTrigger}
            collapsed={true}
            onToggle={toggleSidebar}
          />
          <div className="flex-1 min-w-0">{tabBar}</div>
        </div>
        <div className="flex-1 min-h-0 flex flex-col">
          {terminalBody}
        </div>
      </div>
    )
  }

  return (
    <Group key="expanded" orientation="horizontal" className="h-full w-full">
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
          pickerTrigger={pickerTrigger}
          collapsed={false}
          onToggle={toggleSidebar}
        />
      </Panel>

      <Separator className="w-px bg-border hover:bg-ring hover:w-[3px] transition-all cursor-col-resize" />

      <Panel>
        {mainContent}
      </Panel>
    </Group>
  )
}