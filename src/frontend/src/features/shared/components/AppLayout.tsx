import { useState, useCallback, useEffect, useRef } from 'react'
import { Group, Panel, Separator, usePanelRef } from 'react-resizable-panels'
import { Toaster, toast } from 'sonner'
import cawSvg from '@/assets/LOGO.svg'
import { WorkspacePanel } from '@/features/workspaces/components/WorkspacePanel'
import { TerminalGrid } from '@/features/terminal/components/TerminalGrid'
import { KanbanBoard } from '@/features/kanban/components/KanbanBoard'
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
  cyclePane,
} from '@/features/shared/utils/layout'
import {
  loadState,
  persistWorkspaces,
  subscribeRemoteState,
} from '@/features/workspaces/stores/workspaceStore'
import { type Workspace, type TabGroupsNode } from '@/features/workspaces/types'
import { TabGroupTree } from '@/features/workspaces/components/TabGroupTree'
import { ensureTabGroups, findGroupById, collectGroups, collectTabIds, moveTabToGroup, removeTabFromTree, splitGroup, getTopRightGroupId, findGroupWithTab } from '@/features/workspaces/utils/tabGroups'
import { destroyTerminal, releaseTerminal, setOnTerminalExit, sendTerminalInput } from '@/features/terminal/services/terminalRegistry'
import { useHotkeys } from '@/hooks/useHotkeys'
import { Folder, Menu, Plus, SquareTerminal, GitBranch, FileCode, Terminal, Settings, PanelRight, X } from 'lucide-react'
import { Button } from '@/components/button'
import { FolderSidebar } from '@/features/explorer/components/FolderSidebar'
import { SettingsDialog } from '@/features/settings/components/SettingsDialog'
import { CommandPalette } from '@/features/command-palette/components/CommandPalette'
import { StatusBar } from '@/features/status-bar/components/StatusBar'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Checkbox } from '@/components/checkbox'
import { TerminalPanel } from '@/features/terminal/components/TerminalPanel'
import { EditorPanel } from '@/features/editor/components/EditorPanel'
import { isFileDirty, discardFileEdits, saveFileFromCache } from '@/features/editor/services/editorDirtyStore'
import { MobileControlBar } from '@/features/terminal/components/MobileControlBar'
import { NewTabMenu } from '@/features/workspaces/components/NewTabMenu'

import { subscribeAgentStatuses } from '@/features/agents/stores/agentStatusStore'
import { subscribeToGitStatus, type GitStatusEvent } from '@/features/git/services/gitStatusWs'
import { type AgentStatus } from '@/features/agents/types'
import { agentTypes } from '@/features/agents/services/agentTypes'
import { Shortcut } from './Shortcut'
import { Sounds } from '@/features/shared/utils/sounds'
import { workspacesEqual } from '@/features/shared/utils/utils'

function findActiveLeaf(node: LayoutNode, activeId: string): any | null {
  if (node.type === 'leaf' && node.id === activeId) {
    return node
  }
  if (node.type === 'split') {
    for (const child of node.children) {
      const found = findActiveLeaf(child, activeId)
      if (found) return found
    }
  }
  return null
}

function findFirstLeaf(node: LayoutNode): any | null {
  if (node.type === 'leaf') return node
  if (node.type === 'split') {
    for (const child of node.children) {
      const found = findFirstLeaf(child)
      if (found) return found
    }
  }
  return null
}

export function AppLayout() {
  const [loaded, setLoaded] = useState(false)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [activeWorkspaceId, setActiveWorkspaceId] = useState<string | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const sidebarRef = usePanelRef()
  const folderSidebarRef = usePanelRef()
  // Tracks the user's last chosen sidebar width (in %) so the imperative
  // expand() call restores it. Updated from the sidebar Panel's onResize.
  const sidebarSizeRef = useRef(15)
  const folderSidebarSizeRef = useRef(20)
  const skipPersistRef = useRef(false)
  const loadedRef = useRef(false)
  const localFocusRef = useRef<Record<string, { tabIndex: number; paneId: string }>>({})

  const [folderSidebarCollapsed, setFolderSidebarCollapsed] = useState(true)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsSection, setSettingsSection] = useState<string | undefined>(undefined)
  const [gitStatuses, setGitStatuses] = useState<Record<string, string>>({})
  const [gitIgnored, setGitIgnored] = useState<Record<string, boolean>>({})
  const [pickerOpen, setPickerOpen] = useState(false)
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false)
  const [agentBoardOpen, setAgentBoardOpen] = useState(false)
  const [kanbanClosing, setKanbanClosing] = useState(false)
  // Ref to the StatusBar control-center button so we can blur it when the
  // board closes (otherwise it retains focus and keyboard focus is stuck on
  // the toolbar instead of returning to the terminal the user was editing).
  const controlCenterBtnRef = useRef<HTMLButtonElement | null>(null)
  const [draggedTabId, setDraggedTabId] = useState<string | null>(null)
  const [dragMousePos, setDragMousePos] = useState<{ x: number; y: number } | null>(null)

  useEffect(() => {
    if (!draggedTabId) {
      setDragMousePos(null)
      return
    }
    const handleGlobalPointerMove = (e: PointerEvent) => {
      setDragMousePos({ x: e.clientX, y: e.clientY })
    }
    const handleGlobalPointerUp = () => {
      setTimeout(() => {
        setDraggedTabId(null)
        setDragMousePos(null)
      }, 50)
    }
    window.addEventListener('pointermove', handleGlobalPointerMove)
    window.addEventListener('pointerup', handleGlobalPointerUp)
    return () => {
      window.removeEventListener('pointermove', handleGlobalPointerMove)
      window.removeEventListener('pointerup', handleGlobalPointerUp)
    }
  }, [draggedTabId])

  // Mobile layout state variables
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768)
  const [mobileView, setMobileView] = useState<'control_center' | 'terminals'>('control_center')
  const [workspacesDrawerOpen, setWorkspacesDrawerOpen] = useState(false)
  const [explorerDrawerOpen, setExplorerDrawerOpen] = useState(false)

  // Touch Swipe Gesture Variables
  const touchStartRef = useRef<{ x: number; y: number } | null>(null)
  const controlBarZoneRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < 768)
    handleResize()
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])
  const [closeConfirm, setCloseConfirm] = useState<{
    type: 'tab' | 'pane';
    targetId: string;
    index?: number;
    agentBranch: string;
    hasUncommitted: boolean;
    hasUnmergedCommits: boolean;
    // Unsaved-file confirmation flow (Guardar / Descartar / Cancelar)
    unsavedFilePath?: string;
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

  const sidebarDefaultSize = '15%'

  useEffect(() => {
    let done = false
    loadState().then((s) => {
      if (done) return
      skipPersistRef.current = true
      loadedRef.current = true
      const parsedWorkspaces = s.workspaces.map((w) => {
        // Seed a local focus entry for every workspace on initial load so
        // each client keeps its own selection independent of the shared
        // backend state. The backend's activeTabIndex/activePaneId are only
        // used as the fresh-load default here; afterwards selection is
        // driven entirely by local user interaction.
        localFocusRef.current[w.id] = {
          tabIndex: Math.max(0, Math.min(w.activeTabIndex, w.layouts.length - 1)),
          paneId: w.activePaneId,
        }
        const { tree, activeGroupId } = ensureTabGroups(w)
        return { ...w, tabGroups: tree, activeGroupId }
      })
      setWorkspaces(parsedWorkspaces)
      // Selection is per-client: prefer the local focus we just seeded,
      // falling back to the backend's last-writer value only to pick the
      // initial workspace. Other devices switching workspaces must never
      // clobber this client's active workspace.
      const initialWs = s.activeWorkspaceId && parsedWorkspaces.some((w) => w.id === s.activeWorkspaceId)
        ? s.activeWorkspaceId
        : (parsedWorkspaces[0]?.id ?? null)
      setActiveWorkspaceId(initialWs)
      setLoaded(true)
    })
    return () => { done = true }
  }, [])

  useEffect(() => {
    const unsub = subscribeRemoteState((remote) => {
      skipPersistRef.current = true
      setWorkspaces((prev) => {
        if (workspacesEqual(prev, remote.workspaces)) {
          return prev
        }

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
          // Selection is per-client. Seed a local focus entry for
          // workspaces this client has never visited (e.g. one created on
          // another device), so it doesn't inherit the other device's
          // selection. For workspaces already known, keep this client's
          // own tab/pane focus.
          if (!localFocusRef.current[rw.id]) {
            localFocusRef.current[rw.id] = {
              tabIndex: Math.max(0, Math.min(rw.activeTabIndex, rw.layouts.length - 1)),
              paneId: rw.activePaneId,
            }
          }
          const { tree, activeGroupId } = ensureTabGroups(rw)
          const focus = localFocusRef.current[rw.id]
          if (focus) {
            return { ...rw, tabGroups: tree, activeGroupId, activeTabIndex: focus.tabIndex, activePaneId: focus.paneId }
          }
          return { ...rw, tabGroups: tree, activeGroupId }
        })
      })
      // Intentionally do NOT call setActiveWorkspaceId here. Each client
      // keeps its own active workspace; adopting the remote value would
      // make one device's workspace switch ripple to every other device.
      // If the client's current workspace was removed remotely, fall back
      // to the first remaining one below.
      setActiveWorkspaceId((cur) => {
        if (cur && remote.workspaces.some((w) => w.id === cur)) return cur
        return remote.workspaces[0]?.id ?? null
      })
    })
    return unsub
  }, [])

  const activeWorkspace = workspaces.find((w) => w.id === activeWorkspaceId) ?? workspaces[0] ?? null

  // Top-level Group panel sizing strategy
  // --------------------------------------
  // The left workspace sidebar, main content, and right folder sidebar Panels
  // are ALWAYS mounted inside the top-level <Group>. Collapse is driven through
  // the imperative panelRef.resize() API rather than unmounting the Panels.
  //
  // This is required because react-resizable-panels throws
  // "Invalid N panel layout: ..." from its ResizeObserver whenever the Panel
  // count changes mid-layout (the cached layout has a different arity than the
  // current panel constraints). The throw originates inside a ResizeObserver
  // callback so it bypasses React's ErrorBoundary, leaves the library's
  // internal layout store un-updated, and recurs on every sidebar toggle.
  // See https://github.com/bvaughn/react-resizable-panels/issues/691.
  //
  // `collapsible` is left OFF. The 15% drag floor is enforced by the library's
  // own minSize clamping (Z() in the lib hard-clamps to minSize), so dragging
  // the separator can never push the sidebar below 15% or above 50%. When
  // collapsed, minSize and maxSize are both pinned to the collapsed size in
  // PIXELS so the panel can't be dragged at all (no "gap" the user can grab to
  // shrink the rail further). Pixels are used (rather than a percentage) so
  // the pinned width matches the 44px rail exactly regardless of window width.
  // The lib re-registers a Panel whenever its min/max props change, so the
  // constraints update live without unmounting.
  const SIDEBAR_COLLAPSED_PX = '44px'
  const folderVisible = activeWorkspace && !folderSidebarCollapsed
  const sidebarMinSize = sidebarCollapsed ? SIDEBAR_COLLAPSED_PX : '15%'
  const sidebarMaxSize = sidebarCollapsed ? SIDEBAR_COLLAPSED_PX : '50%'
  const folderMinSize = folderVisible ? '15%' : '0%'
  const folderMaxSize = folderVisible ? '50%' : '0%'

  // Guard so onResize doesn't persist sizes during programmatic resize calls.
  const programmaticLayoutRef = useRef(false)

  // Drive the sidebar Panel's size imperatively. The Panel itself is always
  // mounted (see the desktop Group below); only its size and content change.
  // panelRef.resize() accepts px and % strings; the lib clamps to the Panel's
  // current minSize/maxSize and redistributes the remainder to the main panel.
  useEffect(() => {
    const ref = sidebarRef.current
    if (!ref) return
    programmaticLayoutRef.current = true
    if (sidebarCollapsed) {
      ref.resize(SIDEBAR_COLLAPSED_PX)
    } else {
      ref.resize(`${sidebarSizeRef.current}%`)
    }
    setTimeout(() => { programmaticLayoutRef.current = false }, 0)
  }, [sidebarCollapsed, sidebarRef, SIDEBAR_COLLAPSED_PX])

  // Drive the folder sidebar Panel's size imperatively. It collapses to 0%
  // when hidden (no workspace or toggled off) and restores to its saved size.
  // When collapsing, the workspace sidebar is also re-resized to its current
  // size so the freed space goes to the main (terminals) panel instead of
  // being absorbed proportionally by the workspace sidebar.
  useEffect(() => {
    const ref = folderSidebarRef.current
    if (!ref) return
    programmaticLayoutRef.current = true
    if (folderVisible) {
      ref.resize(`${folderSidebarSizeRef.current}%`)
    } else {
      ref.resize('0%')
      const sidebarPanel = sidebarRef.current
      if (sidebarPanel && !sidebarCollapsed) {
        sidebarPanel.resize(`${sidebarSizeRef.current}%`)
      }
    }
    setTimeout(() => { programmaticLayoutRef.current = false }, 0)
  }, [folderVisible, folderSidebarRef, sidebarRef, sidebarCollapsed])

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
      setGitIgnored({})
      return
    }
    try {
      const [statusRes, ignoredRes] = await Promise.all([
        fetch(`/api/git/statuses?path=${encodeURIComponent(currentWorkspacePath)}`),
        fetch(`/api/git/ignored?path=${encodeURIComponent(currentWorkspacePath)}`),
      ])
      if (statusRes.ok) {
        const json = await statusRes.json()
        setGitStatuses(json?.data || {})
      } else {
        setGitStatuses({})
      }
      if (ignoredRes.ok) {
        const json = await ignoredRes.json()
        setGitIgnored(json?.data || {})
      } else {
        setGitIgnored({})
      }
    } catch {
      setGitStatuses({})
      setGitIgnored({})
    }
  }, [currentWorkspacePath])

  useEffect(() => {
    fetchGitStatus()
  }, [fetchGitStatus])

  // Subscribe to the "git" WebSocket channel so the file explorer's git
  // status badges update automatically whenever the working tree changes
  // (file edits, git add/commit, branch switch, etc.) — independent of the
  // FolderSidebar being mounted.
  useEffect(() => {
    if (!currentWorkspacePath) {
      setGitStatuses({})
      setGitIgnored({})
      return
    }
    const handle = (event: GitStatusEvent) => {
      // Only apply snapshots for the repo we currently have open.
      if (event.path !== currentWorkspacePath) return
      setGitStatuses(event.statuses || {})
      setGitIgnored(event.ignored || {})
    }
    const unsub = subscribeToGitStatus(currentWorkspacePath, handle)
    return unsub
  }, [currentWorkspacePath])


  const toggleFolderSidebar = useCallback(() => {
    setFolderSidebarCollapsed((v) => !v)
  }, [])

  useEffect(() => {
    if (activeWorkspace) {
      localFocusRef.current[activeWorkspace.id] = {
        tabIndex: activeWorkspace.activeTabIndex,
        paneId: activeWorkspace.activePaneId,
      }
    }
  }, [activeWorkspace])

  useEffect(() => {
    if (!loadedRef.current) return
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

  // Global Undo/Redo key listener for File Explorer operations
  useEffect(() => {
    const handleKeyDown = async (e: KeyboardEvent) => {
      const active = document.activeElement
      const isInputActive = active && (
        active.tagName === 'INPUT' ||
        active.tagName === 'TEXTAREA' ||
        active.hasAttribute('contenteditable') ||
        active.getAttribute('contenteditable') === 'true' ||
        active.classList.contains('inputarea') ||
        active.closest('.monaco-editor') !== null
      )

      if (isInputActive) {
        return
      }

      const isCtrl = e.ctrlKey || e.metaKey
      const key = e.key.toLowerCase()

      if (isCtrl && key === 'z' && !e.shiftKey) {
        e.preventDefault()
        e.stopPropagation()
        try {
          const res = await fetch('/api/workspaces/history', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'undo' }),
          })
          if (res.ok) {
            fetchGitStatus()
          }
        } catch { /* ignore */ }
      } else if ((isCtrl && key === 'y') || (isCtrl && key === 'z' && e.shiftKey)) {
        e.preventDefault()
        e.stopPropagation()
        try {
          const res = await fetch('/api/workspaces/history', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'redo' }),
          })
          if (res.ok) {
            fetchGitStatus()
          }
        } catch { /* ignore */ }
      }
    }

    window.addEventListener('keydown', handleKeyDown, true)
    return () => {
      window.removeEventListener('keydown', handleKeyDown, true)
    }
  }, [fetchGitStatus])

  const patchWorkspace = useCallback(
    (id: string, fn: (ws: Workspace) => Workspace) => {
      setWorkspaces((prev) =>
        prev.map((w) => {
          if (w.id !== id) return w
          const next = fn(w)
          if (next.tabGroups) {
            next.tabGroupsJson = JSON.stringify(next.tabGroups)
          }
          return next
        }),
      )
    },
    [],
  )

  const navigateToAgent = useCallback(
    (workspaceId: string, tabIndex: number, paneId: string) => {
      setActiveWorkspaceId(workspaceId)
      localFocusRef.current[workspaceId] = {
        tabIndex,
        paneId,
      }
      patchWorkspace(workspaceId, (ws) => {
        const { tree } = ensureTabGroups(ws)
        let nextTree = tree
        let nextActiveGroupId = ws.activeGroupId
        const tabId = ws.layouts[tabIndex]?.id
        if (tabId) {
          const targetGroup = findGroupWithTab(tree, tabId)
          if (targetGroup) {
            nextActiveGroupId = targetGroup.id
            const idx = targetGroup.tabs.indexOf(tabId)
            
            const updateGroupActive = (n: TabGroupsNode): TabGroupsNode => {
              if (n.type === 'group' && n.id === targetGroup.id) {
                return { ...n, activeTabIndex: idx >= 0 ? idx : n.activeTabIndex }
              }
              if (n.type === 'split') {
                return { ...n, children: n.children.map(updateGroupActive) }
              }
              return n
            }
            nextTree = updateGroupActive(tree)
          }
        }
        return {
          ...ws,
          tabGroups: nextTree,
          activeGroupId: nextActiveGroupId,
          activeTabIndex: tabIndex,
          activePaneId: paneId,
        }
      })
    },
    [patchWorkspace, setActiveWorkspaceId],
  )

  // Closing the Command Center should return keyboard focus to the terminal
  // pane the user was working in, not leave it parked on the toolbar button.
  // We blur the control-center button (so it doesn't keep an active focus
  // ring) and dispatch a focus request for the last active terminal pane,
  // which TerminalPanel listens for and forwards to its xterm instance.
  // The Kanban overlay plays a fade-out animation before unmounting; we keep
  // it mounted with a `closing` flag for the duration of that animation.
  const kanbanCloseTimer = useRef<number | null>(null)
  const closeAgentBoard = useCallback(() => {
    if (kanbanCloseTimer.current) window.clearTimeout(kanbanCloseTimer.current)
    setKanbanClosing(true)
    setAgentBoardOpen(false)
    controlCenterBtnRef.current?.blur()
    if (activePaneId) {
      window.dispatchEvent(new CustomEvent('caw:focus-terminal', { detail: { paneId: activePaneId } }))
    }
    kanbanCloseTimer.current = window.setTimeout(() => {
      setKanbanClosing(false)
      kanbanCloseTimer.current = null
    }, 170)
  }, [activePaneId])

  // ─── Agent Status Notifications ──────────────────────────────────────────
  // Keep a ref to the latest workspaces so the notification callback can
  // look up workspace/worktree data without capturing stale closure state.
  const workspacesRef = useRef<Workspace[]>([])
  useEffect(() => { workspacesRef.current = workspaces }, [workspaces])

  // Ref that tracks the last known AgentStatus per sessionId so we can
  // detect meaningful transitions (e.g. thinking → waiting_input).
  const prevStatusesRef = useRef<Record<string, AgentStatus>>({})
  // Pending "finished" notifications: keyed by sessionId, each with a timer
  // that fires after a short delay. If the agent goes back to working before
  // the timer elapses, the notification is cancelled.
  const pendingFinishedRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({})

  // Sound enabled preference — read from localStorage, updated on
  // 'caw:settings-updated' so toggling in Settings takes effect immediately.
  const soundEnabledRef = useRef(true)
  useEffect(() => {
    const readSoundPref = () => { soundEnabledRef.current = localStorage.getItem('caw:soundEnabled') !== '0' }
    readSoundPref()
    window.addEventListener('caw:settings-updated', readSoundPref)
    return () => window.removeEventListener('caw:settings-updated', readSoundPref)
  }, [])

  // Refs for the navigate-on-click callback — avoids capturing stale closures
  // inside the toast custom render function.
  const setActiveWorkspaceIdRef = useRef(setActiveWorkspaceId)
  const patchWorkspaceRef = useRef(patchWorkspace)
  const setAgentBoardOpenRef = useRef(setAgentBoardOpen)
  const navigateToAgentRef = useRef(navigateToAgent)
  useEffect(() => { setActiveWorkspaceIdRef.current = setActiveWorkspaceId }, [setActiveWorkspaceId])
  useEffect(() => { patchWorkspaceRef.current = patchWorkspace }, [patchWorkspace])
  useEffect(() => { setAgentBoardOpenRef.current = setAgentBoardOpen }, [setAgentBoardOpen])
  useEffect(() => { navigateToAgentRef.current = navigateToAgent }, [navigateToAgent])

  const triggerAgentNotification = useCallback(
    (agentStatus: AgentStatus, type: 'needs_input' | 'finished') => {
      const agentDef = agentTypes[agentStatus.agentId]
      // AgentIcon is a React component — rendered inside the toast custom fn
      const AgentIcon = agentDef?.icon

      // Find the workspace + tab + pane that owns this sessionId
      const findDetails = (sessionId: string) => {
        for (const ws of workspacesRef.current) {
          for (let tabIdx = 0; tabIdx < ws.layouts.length; tabIdx++) {
            const tab = ws.layouts[tabIdx]
            const leafIds = collectLeafIds(tab.layout)
            if (leafIds.includes(sessionId)) {
              const leaf = getLeaf(tab.layout, sessionId)
              return {
                workspaceId: ws.id,
                workspaceName: ws.name || ws.path || 'Workspace',
                workspaceEmoji: ws.emoji || '💼',
                tabIndex: tabIdx,
                paneId: sessionId,
                agentBranch: leaf?.agentBranch,
              }
            }
          }
        }
        return null
      }

      const wsDetails = findDetails(agentStatus.sessionId)
      const raw = agentStatus.title || ''
      const truncatedTitle = raw.length > 60 ? raw.substring(0, 57) + '…' : raw

      // Play notification sound
      if (soundEnabledRef.current) {
        if (type === 'needs_input') {
          Sounds.waitingInput()
        } else {
          Sounds.finished()
        }
      }

      toast.custom(
        (t) => (
          <div
            onClick={() => {
              if (wsDetails) {
                navigateToAgentRef.current(wsDetails.workspaceId, wsDetails.tabIndex, wsDetails.paneId)
                setAgentBoardOpenRef.current(false)
              }
              toast.dismiss(t)
            }}
            style={{ fontFamily: 'inherit' }}
            className={`flex items-center gap-3 p-3 rounded-xl border bg-background/95 backdrop-blur-md shadow-lg shadow-black/20 cursor-pointer transition-all duration-200 select-none w-[340px] text-foreground relative ${
              type === 'needs_input'
                ? 'border-amber-500/30 hover:border-amber-500/50'
                : 'border-emerald-500/20 hover:border-emerald-500/40'
            }`}
          >
            {/* Close button — top-right aligned */}
            <button
              type="button"
              aria-label="Close"
              onClick={(e) => {
                e.stopPropagation()
                toast.dismiss(t)
              }}
              className="absolute top-1.5 right-1.5 flex items-center justify-center w-5 h-5 rounded-md text-muted-foreground/60 hover:text-foreground cursor-pointer transition-colors"
            >
              <X className="w-3.5 h-3.5" />
            </button>

            {/* Large agent icon */}
            <div className="shrink-0 flex items-center justify-center w-12 h-12 pointer-events-none">
              {AgentIcon
                ? <AgentIcon className="w-12 h-12" />
                : <span className="block w-12 h-12" />
              }
            </div>

            {/* Text content */}
            <div className="flex flex-col gap-0.5 min-w-0 flex-1 pointer-events-none">
              {/* Headline: state */}
              <span className={`font-bold text-[11px] leading-tight ${
                type === 'needs_input' ? 'text-amber-400' : 'text-foreground'
              }`}>
                {type === 'needs_input' ? (
                  <span className="flex items-center gap-1.5">
                    <span className="relative flex h-1.5 w-1.5 shrink-0">
                      <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75" />
                      <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-amber-500" />
                    </span>
                    Needs Input
                  </span>
                ) : 'Finished'}
              </span>

              {/* Chat title subtext — only rendered when a title exists */}
              {truncatedTitle && (
                <span className="text-[11px] text-foreground/60 truncate leading-snug">
                  {truncatedTitle}
                </span>
              )}

              {/* Footnote: workspace • branch */}
              <span className="text-[9px] text-muted-foreground/50 truncate mt-0.5 leading-tight">
                {wsDetails ? (
                  <>
                    {wsDetails.workspaceEmoji} {wsDetails.workspaceName}
                    {wsDetails.agentBranch && (
                      <span className="text-violet-400/60"> · {wsDetails.agentBranch}</span>
                    )}
                  </>
                ) : (
                  <span className="italic">Unknown workspace</span>
                )}
              </span>
            </div>
          </div>
        ),
        { duration: 7000 },
      )
    },
    [],
  )

  // Subscribe to real-time agent status changes and fire toasts on transitions
  useEffect(() => {
    const unsub = subscribeAgentStatuses((nextStatuses) => {
      for (const [sessionId, next] of Object.entries(nextStatuses)) {
        const prev = prevStatusesRef.current[sessionId]
        if (prev && prev.status !== next.status) {
          const prevS = prev.status
          const nextS = next.status

          // If the agent goes back to a working state, cancel any pending
          // "finished" notification — it was a transient idle blip, not a
          // real completion.
          if (nextS === 'thinking' || nextS === 'executing') {
            const timer = pendingFinishedRef.current[sessionId]
            if (timer) {
              clearTimeout(timer)
              delete pendingFinishedRef.current[sessionId]
            }
          }

          if (nextS === 'waiting_input') {
            triggerAgentNotification(next, 'needs_input')
          } else if (
            (nextS === 'idle' || nextS === 'stopped') &&
            (prevS === 'thinking' || prevS === 'executing')
          ) {
            // Debounce the "finished" notification: wait 3s before firing.
            // If the agent resumes working within that window (e.g. a brief
            // status glitch), the pending timer is cancelled above.
            const timer = setTimeout(() => {
              delete pendingFinishedRef.current[sessionId]
              triggerAgentNotification(next, 'finished')
            }, 3000)
            pendingFinishedRef.current[sessionId] = timer
          }
        }
      }
      prevStatusesRef.current = nextStatuses
    })
    return () => {
      unsub()
      for (const timer of Object.values(pendingFinishedRef.current)) {
        clearTimeout(timer)
      }
      pendingFinishedRef.current = {}
    }
  }, [triggerAgentNotification])

  // Listen for service worker notification-click messages to navigate to the
  // relevant agent session when a push notification is clicked.
  useEffect(() => {
    if (!navigator.serviceWorker) return
    const handler = (event: MessageEvent) => {
      const data = event.data
      if (!data || data.type !== 'notification-click' || !data.sessionId) return
      for (const ws of workspacesRef.current) {
        for (let tabIdx = 0; tabIdx < ws.layouts.length; tabIdx++) {
          const tab = ws.layouts[tabIdx]
          const leafIds = collectLeafIds(tab.layout)
          if (leafIds.includes(data.sessionId)) {
            setActiveWorkspaceId(ws.id)
            patchWorkspace(ws.id, (w) => ({
              ...w,
              activeTabIndex: tabIdx,
              activePaneId: data.sessionId,
            }))
            setAgentBoardOpen(false)
            return
          }
        }
      }
    }
    navigator.serviceWorker.addEventListener('message', handler)
    return () => navigator.serviceWorker.removeEventListener('message', handler)
  }, [patchWorkspace])

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
    (tabId: string, targetGroupId: string, targetIndex: number) => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const { tree } = ensureTabGroups(ws)
        const nextTree = moveTabToGroup(tree, tabId, targetGroupId, targetIndex)
        const tabIdsInTree = collectTabIds(nextTree)
        const layouts = ws.layouts.slice().sort((a, b) => tabIdsInTree.indexOf(a.id) - tabIdsInTree.indexOf(b.id))

        const activeGroup = findGroupById(nextTree, targetGroupId)
        const activeTabId = activeGroup ? activeGroup.tabs[activeGroup.activeTabIndex] : undefined
        const activeTab = layouts.find((l) => l.id === activeTabId)
        const activeTabIndex = activeTab ? layouts.indexOf(activeTab) : ws.activeTabIndex

        return {
          ...ws,
          tabGroups: nextTree,
          layouts,
          activeGroupId: targetGroupId,
          activeTabIndex,
        }
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
    (tabId: string, groupId?: string) => {
      if (!activeWorkspace) return
      const tabIndex = activeWorkspace.layouts.findIndex((l) => l.id === tabId)
      if (tabIndex < 0) return
      const tab = activeWorkspace.layouts[tabIndex]
      const leafIds = collectLeafIds(tab.layout)
      const activePaneId = leafIds.includes(activeWorkspace.activePaneId)
        ? activeWorkspace.activePaneId
        : leafIds[0] || ''

      patchWorkspace(activeWorkspace.id, (ws) => {
        const { tree, activeGroupId } = ensureTabGroups(ws)
        const targetGroupId = groupId || activeGroupId

        function updateGroupActive(n: TabGroupsNode): TabGroupsNode {
          if (n.type === 'group' && n.id === targetGroupId) {
            const idx = n.tabs.indexOf(tabId)
            return { ...n, activeTabIndex: idx >= 0 ? idx : n.activeTabIndex }
          }
          if (n.type === 'split') {
            return { ...n, children: n.children.map(updateGroupActive) }
          }
          return n
        }

        const nextTree = updateGroupActive(tree)
        return {
          ...ws,
          tabGroups: nextTree,
          activeGroupId: targetGroupId,
          activeTabIndex: tabIndex,
          activePaneId,
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const addTab = useCallback(
    async (cmd?: string[], agentId?: string, label?: string, groupId?: string, env?: [string, string][]) => {
      if (!activeWorkspace) return
      let cwd = activeWorkspace.path || ''
      let agentBranch: string | undefined = undefined
      let baseBranch: string | undefined = undefined

      if (agentId) {
        try {
          const res = await fetch('/api/agents', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              projectPath: cwd,
              agentId,
              enableWorktrees: activeWorkspace.enableWorktrees !== false,
            }),
          })
          if (res.ok) {
            const data = (await res.json())?.data
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
          env,
          agentId,
          agentBranch,
          baseBranch,
        },
      }
      patchWorkspace(activeWorkspace.id, (ws) => {
        const layouts = [...ws.layouts, newTab]
        const { tree, activeGroupId } = ensureTabGroups({ ...ws, layouts })
        const targetGroupId = groupId || activeGroupId
        const nextTree = moveTabToGroup(tree, newTab.id, targetGroupId)
        return {
          ...ws,
          layouts,
          tabGroups: nextTree,
          activeGroupId: targetGroupId,
          activeTabIndex: layouts.length - 1,
          activePaneId: leafId,
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const forceCloseTab = useCallback(
    (tabId: string, deleteBranch?: boolean) => {
      if (!activeWorkspace) return
      const tabIndex = activeWorkspace.layouts.findIndex((l) => l.id === tabId)
      if (tabIndex < 0) return
      const tab = activeWorkspace.layouts[tabIndex]
      for (const leafId of collectLeafIds(tab.layout)) destroyTerminal(leafId, deleteBranch)

      patchWorkspace(activeWorkspace.id, (ws) => {
        const layouts = ws.layouts.filter((l) => l.id !== tabId)
        if (layouts.length === 0) {
          return {
            ...ws,
            layouts: [],
            tabGroups: undefined,
            activeTabIndex: 0,
            activePaneId: '',
          }
        }

        const { tree, activeGroupId } = ensureTabGroups(ws)
        const nextTree = removeTabFromTree(tree, tabId, layouts.map((l) => l.id))

        const activeGroup = findGroupById(nextTree, activeGroupId) || collectGroups(nextTree)[0]
        const nextActiveGroupId = activeGroup ? activeGroup.id : activeGroupId
        const nextActiveTabId = activeGroup && activeGroup.tabs[activeGroup.activeTabIndex]
        const nextActiveTab = layouts.find((l) => l.id === nextActiveTabId)

        const nextActiveIndex = nextActiveTab ? layouts.indexOf(nextActiveTab) : 0
        const nextActivePaneId = nextActiveTab ? collectLeafIds(nextActiveTab.layout)[0] || '' : ''

        return {
          ...ws,
          layouts,
          tabGroups: nextTree,
          activeGroupId: nextActiveGroupId,
          activeTabIndex: nextActiveIndex,
          activePaneId: nextActivePaneId,
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const closeTab = useCallback(
    async (tabId: string) => {
      if (!activeWorkspace) return
      const tab = activeWorkspace.layouts.find((l) => l.id === tabId)
      if (!tab) return

      // Check for unsaved editor files in this tab before closing.
      const unsavedFiles: string[] = []
      for (const leafId of collectLeafIds(tab.layout)) {
        const l = getLeaf(tab.layout, leafId)
        if (l && !l.isDiff && l.filePath && isFileDirty(l.filePath)) {
          unsavedFiles.push(l.filePath)
        }
      }
      if (unsavedFiles.length > 0) {
        setCloseConfirm({
          type: 'tab',
          targetId: tab.id,
          agentBranch: '',
          hasUncommitted: false,
          hasUnmergedCommits: false,
          unsavedFilePath: unsavedFiles[0],
        })
        return
      }

      const agentLeaves = findAgentLeaves(tab.layout)
      if (agentLeaves.length > 0) {
        const firstLeaf = agentLeaves[0]
        let uncommitted = false
        let unmerged = false
        try {
          const res = await fetch(
            `/api/agents/changes?worktreePath=${encodeURIComponent(firstLeaf.cwd || '')}&branchName=${encodeURIComponent(firstLeaf.agentBranch || '')}&baseBranch=${encodeURIComponent(firstLeaf.baseBranch || '')}`,
          )
          if (res.ok) {
            const data = (await res.json())?.data
            uncommitted = !!data.hasUncommitted
            unmerged = !!data.hasUnmergedCommits
          }
        } catch (err) {
          console.error('Failed to check agent changes:', err)
        }

        setCloseConfirm({
          type: 'tab',
          targetId: tab.id,
          agentBranch: firstLeaf.agentBranch || '',
          hasUncommitted: uncommitted,
          hasUnmergedCommits: unmerged,
        })
        return
      }

      forceCloseTab(tabId)
    },
    [activeWorkspace, forceCloseTab],
  )

  const openFile = useCallback(
    (filePath: string, cwd?: string) => {
      if (!activeWorkspace) return
      const name = filePath.split(/[\\/]/).pop() || filePath

      const existing = activeWorkspace.layouts.find(
        (t) => t.layout.type === 'leaf' && t.layout.filePath === filePath,
      )
      if (existing) {
        switchTab(existing.id)
        return
      }

      const newTab = {
        id: crypto.randomUUID(),
        name,
        layout: {
          type: 'leaf' as const,
          id: crypto.randomUUID(),
          cwd: cwd || activeWorkspace.path || '',
          filePath,
        },
      }

      patchWorkspace(activeWorkspace.id, (ws) => {
        const layouts = [...ws.layouts, newTab]
        const { tree, activeGroupId } = ensureTabGroups({ ...ws, layouts })
        const nextTree = moveTabToGroup(tree, newTab.id, activeGroupId)
        return {
          ...ws,
          layouts,
          tabGroups: nextTree,
          activeGroupId,
          activeTabIndex: layouts.length - 1,
          activePaneId: newTab.layout.id,
        }
      })
    },
    [activeWorkspace, patchWorkspace, switchTab],
  )

  const openDiff = useCallback(
    (filePath?: string) => {
      if (!activeWorkspace) return
      const name = filePath ? `Diff: ${filePath.split(/[\\/]/).pop()}` : 'Git Diff'

      const existing = activeWorkspace.layouts.find(
        (t) => t.layout.type === 'leaf' && t.layout.isDiff === true && t.layout.filePath === filePath,
      )
      if (existing) {
        switchTab(existing.id)
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

      patchWorkspace(activeWorkspace.id, (ws) => {
        const layouts = [...ws.layouts, newTab]
        const { tree, activeGroupId } = ensureTabGroups({ ...ws, layouts })
        const nextTree = moveTabToGroup(tree, newTab.id, activeGroupId)
        return {
          ...ws,
          layouts,
          tabGroups: nextTree,
          activeGroupId,
          activeTabIndex: layouts.length - 1,
          activePaneId: newTab.layout.id,
        }
      })
    },
    [activeWorkspace, patchWorkspace, switchTab],
  )

  const handleSetActiveGroup = useCallback(
    (groupId: string) => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const { tree } = ensureTabGroups(ws)
        const group = findGroupById(tree, groupId)
        const tabId = group ? group.tabs[group.activeTabIndex] : undefined
        const tab = ws.layouts.find((l) => l.id === tabId)
        const activeTabIndex = tab ? ws.layouts.indexOf(tab) : ws.activeTabIndex
        const activePaneId = tab ? collectLeafIds(tab.layout)[0] || '' : ws.activePaneId
        return {
          ...ws,
          activeGroupId: groupId,
          activeTabIndex,
          activePaneId,
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const handleGroupSizesChange = useCallback(
    (splitId: string, sizes: number[]) => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const { tree } = ensureTabGroups(ws)
        function updateSizes(n: TabGroupsNode): TabGroupsNode {
          if (n.type === 'split') {
            if (n.id === splitId) {
              return { ...n, sizes }
            }
            return { ...n, children: n.children.map(updateSizes) }
          }
          return n
        }
        return {
          ...ws,
          tabGroups: updateSizes(tree),
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const handleSplitGroup = useCallback(
    (targetGroupId: string, draggedTabId: string, orientation: 'horizontal' | 'vertical', position: 'left' | 'right' | 'top' | 'bottom') => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const { tree } = ensureTabGroups(ws)
        const nextTree = splitGroup(tree, targetGroupId, draggedTabId, orientation, position, ws.layouts.map(l => l.id))
        const tabIdsInTree = collectTabIds(nextTree)
        const layouts = ws.layouts.slice().sort((a, b) => tabIdsInTree.indexOf(a.id) - tabIdsInTree.indexOf(b.id))

        const groups = collectGroups(nextTree)
        const newGroup = groups.find(g => g.tabs.includes(draggedTabId))
        const activeGroupId = newGroup ? newGroup.id : targetGroupId

        const activeTab = layouts.find(l => l.id === draggedTabId)
        const activeTabIndex = activeTab ? layouts.indexOf(activeTab) : ws.activeTabIndex
        const activePaneId = activeTab ? collectLeafIds(activeTab.layout)[0] || '' : ws.activePaneId

        return {
          ...ws,
          tabGroups: nextTree,
          layouts,
          activeGroupId,
          activeTabIndex,
          activePaneId,
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

  const handleMoveTabToGroup = useCallback(
    (tabId: string, groupId: string) => {
      if (!activeWorkspace) return
      patchWorkspace(activeWorkspace.id, (ws) => {
        const { tree } = ensureTabGroups(ws)
        const nextTree = moveTabToGroup(tree, tabId, groupId)
        const tabIdsInTree = collectTabIds(nextTree)
        const layouts = ws.layouts.slice().sort((a, b) => tabIdsInTree.indexOf(a.id) - tabIdsInTree.indexOf(b.id))

        const activeTab = layouts.find(l => l.id === tabId)
        const activeTabIndex = activeTab ? layouts.indexOf(activeTab) : ws.activeTabIndex
        const activePaneId = activeTab ? collectLeafIds(activeTab.layout)[0] || '' : ws.activePaneId

        return {
          ...ws,
          tabGroups: nextTree,
          layouts,
          activeGroupId: groupId,
          activeTabIndex,
          activePaneId,
        }
      })
    },
    [activeWorkspace, patchWorkspace],
  )

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
        if (tabIndex >= 0) forceCloseTab(activeTab.id, deleteBranch)
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
      // Unsaved editor file inside this pane -> confirm before closing.
      if (leaf && leaf.type === 'leaf' && !leaf.isDiff && leaf.filePath && isFileDirty(leaf.filePath)) {
        setCloseConfirm({
          type: 'pane',
          targetId: id,
          agentBranch: '',
          hasUncommitted: false,
          hasUnmergedCommits: false,
          unsavedFilePath: leaf.filePath,
        })
        return
      }
      if (leaf && leaf.type === 'leaf' && leaf.agentBranch && leaf.cwd) {
        let uncommitted = false
        let unmerged = false
        try {
          const res = await fetch(
            `/api/agents/changes?worktreePath=${encodeURIComponent(leaf.cwd || '')}&branchName=${encodeURIComponent(leaf.agentBranch || '')}&baseBranch=${encodeURIComponent(leaf.baseBranch || '')}`,
          )
          if (res.ok) {
            const data = (await res.json())?.data
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
        const res = await fetch(`/api/workspaces/details?path=${encodeURIComponent(path)}`)
        if (res.ok) {
          const data = (await res.json())?.data
          absPath = data.path || path
        }
      } catch { /* fall back to raw path */ }

      const defaultAgentId = localStorage.getItem('caw:defaultNewAgent') || 'terminal'
      const agent = agentTypes[defaultAgentId]
      const cmd = agent && agent.id !== 'terminal' ? agent.cmd : undefined
      const env = agent && agent.id !== 'terminal' ? agent.env : undefined
      const layout = createLeaf(absPath, cmd, agent && agent.id !== 'terminal' ? agent.id : undefined, env)
      const ws: Workspace = {
        id: crypto.randomUUID(),
        path: absPath,
        name: name || absPath.split(/[\\/]/).filter(Boolean).pop() || absPath || 'Workspace',
        emoji: emoji || undefined,
        layouts: [{ id: crypto.randomUUID(), name: agent && agent.id !== 'terminal' ? agent.label : 'Terminal', layout }],
        activeTabIndex: 0,
        activePaneId: collectLeafIds(layout)[0],
        enableWorktrees: false,
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
    setSidebarCollapsed((v) => !v)
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
    'Alt+W': () => { if (activePaneId) handleClosePane(activePaneId) },
    'Alt+ArrowLeft': () => {
      if (!activeWorkspace || !activeTab || !activePaneId) return
      const next = cyclePane(activeWorkspace.layouts, activeTab.id, activePaneId, 'left')
      if (!next) return
      if (next.tabId !== activeTab.id) switchTab(next.tabId)
      setActivePane(next.paneId)
    },
    'Alt+ArrowRight': () => {
      if (!activeWorkspace || !activeTab || !activePaneId) return
      const next = cyclePane(activeWorkspace.layouts, activeTab.id, activePaneId, 'right')
      if (!next) return
      if (next.tabId !== activeTab.id) switchTab(next.tabId)
      setActivePane(next.paneId)
    },
    'Alt+T': () => addTab(),
    'Alt+H': () => { if (activePaneId) handleSplitHoriz(activePaneId) },
    'Alt+V': () => { if (activePaneId) handleSplitVert(activePaneId) },
    'Alt+P': () => setCommandPaletteOpen(true),
  })

  useEffect(() => {
    setOnTerminalExit((leafId) => handleClosePane(leafId))
    return () => setOnTerminalExit(null)
  }, [handleClosePane])

  if (!loaded) {
    return <div className="flex h-full w-full items-center justify-center text-sm text-muted-foreground">Loading…</div>
  }



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
          <Shortcut keys="Alt+→" label="Switch pane" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+W" label="Close pane" />
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
          <Shortcut keys="Alt+→" label="Switch pane" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+W" label="Close pane" />
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
          <Shortcut keys="Alt+→" label="Switch pane" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+W" label="Close pane" />
        </div>
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+H" label="Horizontal split" />
          <Shortcut keys="Alt+V" label="Vertical split" />
          <Shortcut keys="Alt+P" label="Command palette" />
        </div>
      </div>
    </div>
  )

  const currentActiveLeaf = activeTab ? (findActiveLeaf(activeTab.layout, activePaneId) || findFirstLeaf(activeTab.layout)) : null

  // Touch handlers for edge swipes
  const handleTouchStart = (e: React.TouchEvent) => {
    if (controlBarZoneRef.current && e.target instanceof Node && controlBarZoneRef.current.contains(e.target)) return
    const touch = e.touches[0]
    touchStartRef.current = { x: touch.clientX, y: touch.clientY }
  }

  const handleTouchMove = (e: React.TouchEvent) => {
    if (!touchStartRef.current) return
    if (controlBarZoneRef.current && e.target instanceof Node && controlBarZoneRef.current.contains(e.target)) {
      touchStartRef.current = null
      return
    }
    const touch = e.touches[0]
    const diffX = touch.clientX - touchStartRef.current.x
    const diffY = touch.clientY - touchStartRef.current.y

    // Ensure horizontal gesture
    if (Math.abs(diffX) > Math.abs(diffY)) {
      // Swipe from left edge (start x < 50) to open workspaces drawer
      if (touchStartRef.current.x < 50 && diffX > 80) {
        setExplorerDrawerOpen(false)
        setWorkspacesDrawerOpen(true)
        touchStartRef.current = null
      }
      // Swipe from right edge (start x > width - 50) to open explorer drawer
      else if (touchStartRef.current.x > window.innerWidth - 50 && diffX < -80) {
        if (activeWorkspace) {
          setWorkspacesDrawerOpen(false)
          setExplorerDrawerOpen(true)
          touchStartRef.current = null
        }
      }
    }
  }

  const handleTouchEnd = () => {
    touchStartRef.current = null
  }

  return (
    <div className="flex flex-col h-full w-full bg-background select-none">
      {isMobile ? (
        <div 
          className="flex flex-col h-full w-full overflow-hidden relative"
          onTouchStart={handleTouchStart}
          onTouchMove={handleTouchMove}
          onTouchEnd={handleTouchEnd}
        >
          {/* Top Header */}
          <header className="flex items-center justify-between h-[50px] border-b border-border bg-secondary/15 px-3 shrink-0">
            <Button variant="ghost" size="icon" className="animate-none" onClick={() => { setExplorerDrawerOpen(false); setWorkspacesDrawerOpen(true) }}>
              <Menu className="h-5 w-5" />
            </Button>

            {/* Toggle buttons for Home / Terminals views */}
            <div className="flex bg-background/50 rounded-lg p-0.5 border border-border text-[11px] font-medium">
              <button
                className={`px-3 py-1 rounded-md transition-colors ${mobileView === 'control_center' ? 'bg-secondary text-foreground font-semibold shadow-sm animate-none' : 'text-muted-foreground'}`}
                onClick={() => setMobileView('control_center')}
              >
                Control Center
              </button>
              <button
                className={`px-3 py-1 rounded-md transition-colors ${mobileView === 'terminals' ? 'bg-secondary text-foreground font-semibold shadow-sm animate-none' : 'text-muted-foreground'}`}
                onClick={() => setMobileView('terminals')}
              >
                Terminals
              </button>
            </div>

            {activeWorkspace ? (
              <Button variant="ghost" size="icon" className="animate-none" onClick={() => { setWorkspacesDrawerOpen(false); setExplorerDrawerOpen(true) }}>
                <Folder className="h-5 w-5" />
              </Button>
            ) : (
              <div className="w-9" />
            )}
          </header>

          {/* Workspaces Drawer (80% width with backdrop-blur) */}
          <div className={`fixed inset-0 z-50 ${workspacesDrawerOpen ? 'pointer-events-auto' : 'pointer-events-none'}`}>
            <div className={`absolute inset-0 bg-black/60 backdrop-blur-md transition-opacity duration-200 ${workspacesDrawerOpen ? 'opacity-100' : 'opacity-0'}`} onClick={() => setWorkspacesDrawerOpen(false)} />
            <div className={`absolute top-0 bottom-0 left-0 w-[80%] max-w-[320px] bg-background border-r border-border transition-transform duration-300 ease-out ${workspacesDrawerOpen ? 'translate-x-0 delay-150' : '-translate-x-full'}`}>
              <WorkspacePanel
                workspaces={workspaces}
                activeWorkspaceId={activeWorkspaceId}
                onSelectWorkspace={(id) => {
                  setActiveWorkspaceId(id)
                  setWorkspacesDrawerOpen(false)
                }}
                onAddWorkspace={handleAddWorkspace}
                onDeleteWorkspace={handleDeleteWorkspace}
                onEditWorkspace={handleEditWorkspace}
                onReorderWorkspaces={handleReorderWorkspaces}
                collapsed={false}
                onToggle={() => setWorkspacesDrawerOpen(false)}
                pickerOpen={pickerOpen}
                onPickerOpenChange={setPickerOpen}
                onOpenSettings={() => setSettingsOpen(true)}
              />
            </div>
          </div>

          {/* Explorer Drawer (80% width with backdrop-blur) */}
          <div className={`fixed inset-0 z-50 ${explorerDrawerOpen ? 'pointer-events-auto' : 'pointer-events-none'}`}>
            <div className={`absolute inset-0 bg-black/60 backdrop-blur-md transition-opacity duration-200 ${explorerDrawerOpen ? 'opacity-100' : 'opacity-0'}`} onClick={() => setExplorerDrawerOpen(false)} />
            <div className={`absolute top-0 bottom-0 right-0 w-[80%] max-w-[320px] bg-background border-l border-border transition-transform duration-300 ease-out ${explorerDrawerOpen ? 'translate-x-0 delay-150' : 'translate-x-full'}`}>
              <FolderSidebar
                workspacePath={currentWorkspacePath}
                mainWorkspacePath={activeWorkspace?.path || ''}
                onOpenFile={(path) => {
                  openFile(path, currentWorkspacePath)
                  setExplorerDrawerOpen(false)
                  setMobileView('terminals')
                }}
                gitStatuses={gitStatuses}
                gitIgnored={gitIgnored}
                onRefresh={fetchGitStatus}
                onClose={() => setExplorerDrawerOpen(false)}
              />
            </div>
          </div>

          {/* Main Content Area */}
          <div className="flex-1 min-h-0 relative flex flex-col">
            {mobileView === 'control_center' ? (
              <div className="flex-1 min-h-0">
                <KanbanBoard
                  workspaces={workspaces}
                  onNavigateToWorkspace={(workspaceId, tabIndex, paneId) => {
                    navigateToAgent(workspaceId, tabIndex, paneId)
                    setMobileView('terminals')
                  }}
                />
              </div>
            ) : (
              <div className="flex flex-col h-full">
                {/* Scrollable horizontal tab bar below the header */}
                {activeWorkspace && (
                  <div className="flex items-center border-b border-border bg-secondary/10 h-[36px] shrink-0 overflow-x-auto pl-2 select-none scrollbar-none">
                    {layouts.map((t, idx) => {
                      const isActive = idx === activeWorkspace.activeTabIndex
                      return (
                        <button
                          key={t.id}
                          onClick={() => switchTab(t.id)}
                          className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-t-md border-t border-x transition-colors shrink-0 ${
                            isActive 
                              ? 'bg-background border-border text-foreground font-semibold' 
                              : 'border-transparent text-muted-foreground hover:text-foreground'
                          }`}
                        >
                          <span className="truncate max-w-[100px]">{t.name}</span>
                          <span 
                            onClick={(e) => {
                              e.stopPropagation()
                              closeTab(t.id)
                            }}
                            className="p-0.5 rounded-full hover:bg-muted text-muted-foreground/60 hover:text-foreground cursor-pointer text-[10px] ml-1 select-none"
                          >
                            ✕
                          </span>
                        </button>
                      )
                    })}
                    {/* Add button reusing the desktop dropdown menu */}
                    <NewTabMenu
                      onAdd={(cmd, agentId, label, env) => addTab(cmd, agentId, label, undefined, env)}
                      enableWorktrees={activeWorkspace.enableWorktrees}
                      onToggleWorktrees={toggleWorktrees}
                      triggerClassName="h-[36px] px-2 border-r-0"
                      align="start"
                    />
                  </div>
                )}

                {/* Active Terminals/Editor Area */}
                <div className="flex-1 min-h-0 relative">
                  {currentActiveLeaf ? (
                    currentActiveLeaf.filePath || currentActiveLeaf.isDiff ? (
                      <EditorPanel filePath={currentActiveLeaf.filePath} isDiff={currentActiveLeaf.isDiff} cwd={currentActiveLeaf.cwd || activeWorkspace?.path || ''} gitStatuses={gitStatuses} onOpenDiff={openDiff} />
                    ) : (
                      <TerminalPanel terminalId={currentActiveLeaf.id} cwd={currentActiveLeaf.cwd || activeWorkspace?.path || ''} cmd={currentActiveLeaf.cmd} env={currentActiveLeaf.env} isActive={true} />
                    )
                  ) : activeWorkspace ? (
                    <div className="flex flex-col h-full w-full items-center justify-center px-6 text-center gap-4 select-none">
                      <div className="p-4 rounded-full bg-muted/30 border border-border/30">
                        <SquareTerminal className="w-8 h-8 text-muted-foreground" />
                      </div>
                      <div className="flex flex-col gap-1">
                        <span className="text-sm font-semibold text-foreground/80">No terminals open</span>
                        <span className="text-xs text-muted-foreground/60">
                          Create a new terminal to start running commands or agents.
                        </span>
                      </div>
                      <NewTabMenu
                        onAdd={(cmd, agentId, label, env) => addTab(cmd, agentId, label, undefined, env)}
                        enableWorktrees={activeWorkspace?.enableWorktrees}
                        onToggleWorktrees={toggleWorktrees}
                        align="center"
                        triggerTitle="New terminal / agent"
                      >
                        <Button variant="outline" size="sm" className="gap-1.5">
                          <Plus className="h-3.5 w-3.5" />
                          New Terminal / Agent
                        </Button>
                      </NewTabMenu>
                    </div>
                  ) : (
                    <div className="flex flex-col items-center justify-center h-full text-muted-foreground text-xs gap-4 px-6 text-center">
                      <div className="p-4 rounded-full bg-muted/30 border border-border/30">
                        <Plus className="w-7 h-7 text-muted-foreground" />
                      </div>
                      <span>No workspace selected.</span>
                      <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setPickerOpen(true)}>
                        <Plus className="h-3.5 w-3.5" />
                        Create Workspace
                      </Button>
                    </div>
                  )}
                </div>

                {/* Mobile Control Bar - placed at the bottom, rises with keyboard */}
                <div ref={controlBarZoneRef}>
                  {currentActiveLeaf && !currentActiveLeaf.filePath && !currentActiveLeaf.isDiff && (
                    <MobileControlBar terminalId={currentActiveLeaf.id} />
                  )}
                </div>

              </div>
            )}
          </div>

          {/* Global Status Bar - stays mounted across mobile view switches to avoid reloading usage limits */}
          <StatusBar
            workspaceName={activeWorkspace?.name}
            worktreeBranch={activeWorktreeBranch}
            agentBoardOpen={agentBoardOpen}
            onToggleAgentBoard={() => setMobileView(mobileView === 'control_center' ? 'terminals' : 'control_center')}
            onOpenSettings={(section) => {
              setSettingsSection(section)
              setSettingsOpen(true)
            }}
            hideControlCenter
            onSendText={(text) => { if (activePaneId) sendTerminalInput(activePaneId, text) }}
          />
        </div>
      ) : (
        <>
          <div className="relative flex-1 min-h-0">
            <div className="flex h-full w-full">
              <Group orientation="horizontal" className="flex-1">
                {/* Left Workspace Panel — always mounted; collapse/expand is
                    driven imperatively via sidebarRef.resize() (see the effect
                    near the derived-size block above). Only the Panel content
                    swaps between the 44px collapsed rail and the full sidebar.
                    The Panel count never changes, which prevents the
                    "Invalid N panel layout" throw from react-resizable-panels'
                    ResizeObserver when the cached layout arity mismatches the
                    live panel constraints. */}
                <Panel
                  id="sidebar"
                  panelRef={sidebarRef}
                  defaultSize={sidebarCollapsed ? SIDEBAR_COLLAPSED_PX : sidebarDefaultSize}
                  minSize={sidebarMinSize}
                  maxSize={sidebarMaxSize}
                  onResize={(size) => {
                    if (programmaticLayoutRef.current) return
                    if (size.asPercentage >= 15) {
                      sidebarSizeRef.current = size.asPercentage
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
                    collapsed={sidebarCollapsed}
                    onToggle={toggleSidebar}
                    pickerOpen={pickerOpen}
                    onPickerOpenChange={setPickerOpen}
                    onOpenSettings={() => setSettingsOpen(true)}
                  />
                </Panel>
                {!sidebarCollapsed && (
                  <Separator className="w-px bg-border hover:bg-ring hover:w-[3px] transition-all cursor-col-resize" />
                )}

                {/* Main Terminals / Editors Content */}
                <Panel id="main" defaultSize="55%">
                  {activeWorkspace && activeWorkspace.layouts.length > 0 ? (
                    <div className="flex-1 h-full min-h-0 relative">
                      {(() => {
                        const { tree, activeGroupId } = ensureTabGroups(activeWorkspace)
                        const topRightGroupId = getTopRightGroupId(tree)
                        return (
                          <TabGroupTree
                            workspace={activeWorkspace}
                            node={tree}
                            activeGroupId={activeGroupId}
                            topRightGroupId={topRightGroupId}
                            draggedTabId={draggedTabId}
                            activePaneId={activePaneId}
                            gitStatuses={gitStatuses}
                            folderSidebarCollapsed={folderSidebarCollapsed}
                            onSetActiveGroup={handleSetActiveGroup}
                            onSwitchTab={switchTab}
                            onCloseTab={closeTab}
                            onReorderTabs={reorderTabs}
                            onAddTab={addTab}
                            onSplitGroup={handleSplitGroup}
                            onMoveTabToGroup={handleMoveTabToGroup}
                            onDragStart={setDraggedTabId}
                            onToggleWorktrees={toggleWorktrees}
                            onFocusPane={setActivePane}
                            onSplitVert={handleSplitVert}
                            onSplitHoriz={handleSplitHoriz}
                            onClosePane={handleClosePane}
                            onSizesChange={handleSizesChange}
                            onGroupSizesChange={handleGroupSizesChange}
                            onOpenDiff={openDiff}
                            onOpenSettings={() => setSettingsOpen(true)}
                            onToggleFolderSidebar={toggleFolderSidebar}
                          />
                        )
                      })()}
                    </div>
                  ) : activeWorkspace ? (
                    <div className="flex flex-col h-full bg-background">
                      {/* Empty workspace header top bar */}
                      <div className="flex items-center border-b border-border bg-secondary/15 h-[33px] shrink-0 select-none">
                        <div className="flex flex-1 h-full">
                          <NewTabMenu
                            onAdd={(cmd, agentId, label, env) => addTab(cmd, agentId, label, undefined, env)}
                            enableWorktrees={activeWorkspace.enableWorktrees}
                            onToggleWorktrees={toggleWorktrees}
                            triggerClassName="h-[33px] px-2 border-r border-border"
                            align="start"
                          />
                        </div>
                        <div className="flex items-center shrink-0 h-full border-l border-border bg-background">
                          {/* Settings Button */}
                          <div className={`flex items-center justify-center h-full select-none ${folderSidebarCollapsed ? 'border-r border-border' : ''}`} style={{ width: 36 }}>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground animate-none"
                              onClick={() => setSettingsOpen(true)}
                              title="Settings"
                              data-testid="settings-open-button"
                            >
                              <Settings className="h-3.5 w-3.5" />
                            </Button>
                          </div>

                          {/* Folder Button */}
                          {folderSidebarCollapsed && (
                            <div className="group flex items-center justify-center h-full select-none" style={{ width: 36 }}>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-5 w-5 shrink-0 text-muted-foreground hover:text-foreground animate-none"
                                onClick={toggleFolderSidebar}
                                title="Workspace Files"
                              >
                                <Folder className="h-3.5 w-3.5 group-hover:hidden" />
                                <PanelRight className="h-3.5 w-3.5 hidden group-hover:block" />
                              </Button>
                            </div>
                          )}
                        </div>
                      </div>
                      <div className="flex-1 min-h-0 relative flex flex-col">
                        {terminalBody}
                      </div>
                    </div>
                  ) : (
                    <div className="flex flex-col h-full">
                      {terminalBody}
                    </div>
                  )}
                </Panel>

                {/* Right Folder Explorer Panel — always mounted (same reason as
                    the left sidebar above); collapses to 0% imperatively via
                    folderSidebarRef.resize("0%") and renders null content when
                    hidden. The separator is conditionally rendered since its
                    count does not affect the layout validator. */}
                {activeWorkspace && !folderSidebarCollapsed && (
                  <Separator className="w-px bg-border hover:bg-ring hover:w-[3px] transition-all cursor-col-resize" />
                )}
                <Panel
                  id="folder-sidebar"
                  panelRef={folderSidebarRef}
                  defaultSize={folderVisible ? '20%' : '0%'}
                  minSize={folderMinSize}
                  maxSize={folderMaxSize}
                  onResize={(size) => {
                    if (programmaticLayoutRef.current) return
                    if (size.asPercentage >= 15) {
                      folderSidebarSizeRef.current = size.asPercentage
                    }
                  }}
                >
                  {activeWorkspace && !folderSidebarCollapsed ? (
                    <FolderSidebar
                      workspacePath={currentWorkspacePath}
                      mainWorkspacePath={activeWorkspace.path || ''}
                      onOpenFile={(path) => openFile(path, currentWorkspacePath)}
                      gitStatuses={gitStatuses}
                      gitIgnored={gitIgnored}
                      onRefresh={fetchGitStatus}
                      onClose={() => setFolderSidebarCollapsed(true)}
                    />
                  ) : null}
                </Panel>
              </Group>
            </div>
            {(agentBoardOpen || kanbanClosing) && (
              <div
                className={`absolute inset-0 z-40 bg-background/80 backdrop-blur-md backdrop-saturate-150 ${
                  kanbanClosing ? 'kanban-fade-out' : 'kanban-fade-in'
                }`}
              >
                <KanbanBoard
                  workspaces={workspaces}
                  onNavigateToWorkspace={(workspaceId, tabIndex, paneId) => {
                    navigateToAgent(workspaceId, tabIndex, paneId)
                    closeAgentBoard()
                  }}
                />
              </div>
            )}
          </div>

          <StatusBar
            workspaceName={activeWorkspace?.name}
            worktreeBranch={activeWorktreeBranch}
            agentBoardOpen={agentBoardOpen}
            onToggleAgentBoard={() => {
              if (agentBoardOpen) closeAgentBoard()
              else setAgentBoardOpen(true)
            }}
            onOpenSettings={(section) => {
              setSettingsSection(section)
              setSettingsOpen(true)
            }}
            controlCenterButtonRef={controlCenterBtnRef}
            onSendText={(text) => { if (activePaneId) sendTerminalInput(activePaneId, text) }}
          />
        </>
      )}

      <SettingsDialog open={settingsOpen} onOpenChange={(open) => { setSettingsOpen(open); if (!open) setSettingsSection(undefined) }} initialSection={settingsSection} />

      <CommandPalette
        open={commandPaletteOpen}
        onOpenChange={setCommandPaletteOpen}
        workspacePath={activeWorkspace?.path || ''}
        onOpenFile={openFile}
        onAddTerminal={addTab}
        onAddAgent={(cmd, agentId, label, env) => addTab(cmd, agentId, label, undefined, env)}
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
          {closeConfirm?.unsavedFilePath ? (
            <>
              <DialogHeader>
                <DialogTitle className="text-base font-semibold text-foreground">
                  Unsaved changes
                </DialogTitle>
                <DialogDescription className="text-xs text-muted-foreground mt-2" asChild>
                  <div className="space-y-1">
                    <div>
                      <span className="text-foreground/80">{closeConfirm.unsavedFilePath.split(/[\\/]/).pop()}</span> has unsaved changes.
                    </div>
                  </div>
                </DialogDescription>
              </DialogHeader>
              <div className="flex justify-end gap-2 mt-4">
                <Button
                  variant="outline"
                  onClick={async () => {
                    if (closeConfirm) {
                      discardFileEdits(closeConfirm.unsavedFilePath!)
                      if (closeConfirm.type === 'tab') {
                        forceCloseTab(closeConfirm.targetId)
                      } else {
                        forceClosePane(closeConfirm.targetId)
                      }
                      setCloseConfirm(null)
                    }
                  }}
                >
                  Discard
                </Button>
                <Button
                  onClick={async () => {
                    if (closeConfirm) {
                      const ok = await saveFileFromCache(closeConfirm.unsavedFilePath!)
                      if (ok) {
                        if (closeConfirm.type === 'tab') {
                          forceCloseTab(closeConfirm.targetId)
                        } else {
                          forceClosePane(closeConfirm.targetId)
                        }
                        setCloseConfirm(null)
                      }
                    }
                  }}
                >
                  Save
                </Button>
              </div>
            </>
          ) : (
            <>
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
                        onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDeleteBranchChecked(e.target.checked)}
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
                        forceCloseTab(closeConfirm.targetId, deleteBranchChecked)
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
            </>
          )}
        </DialogContent>
      </Dialog>

      <svg style={{ width: 0, height: 0, position: 'absolute' }}>
        <linearGradient id="lava-gradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" className="lava-stop-1" />
          <stop offset="50%" className="lava-stop-2" />
          <stop offset="100%" className="lava-stop-3" />
        </linearGradient>
      </svg>

      {/* Agent status toast notifications — positioned above StatusBar (desktop only) */}
      {!isMobile && (
        <Toaster
          position="bottom-right"
          visibleToasts={5}
          gap={8}
          toastOptions={{ unstyled: true, classNames: { toast: '' } }}
        />
      )}

      {/* VS Code-style floating dragged tab preview */}
      {(() => {
        const draggedTabLayout = activeWorkspace?.layouts.find((l) => l.id === draggedTabId)
        if (!draggedTabId || !dragMousePos || !draggedTabLayout) return null
        return (
          <div
            className="fixed pointer-events-none z-50 opacity-80 shadow-2xl border border-primary/30 bg-background rounded-md px-3 py-1.5 flex items-center gap-1.5 text-xs text-foreground"
            style={{
              top: dragMousePos.y - 15,
              left: dragMousePos.x - 50,
            }}
          >
            {(() => {
              const isDiff = draggedTabLayout.layout.type === 'leaf' && draggedTabLayout.layout.isDiff
              const filePath = draggedTabLayout.layout.type === 'leaf' && draggedTabLayout.layout.filePath
              const agentId = findAgentId(draggedTabLayout.layout)
              if (isDiff) {
                return <GitBranch className="h-3.5 w-3.5 text-primary shrink-0" />
              }
              if (filePath) {
                return <FileCode className="h-3.5 w-3.5 text-blue-400 shrink-0" />
              }
              const agent = agentId ? agentTypes[agentId] : null
              if (agent && agent.icon) {
                const IconComponent = agent.icon
                return <IconComponent size={14} className="h-3.5 w-3.5 shrink-0" />
              }
              return <Terminal className="h-3 w-3 shrink-0" />
            })()}
            <span className="font-medium">{draggedTabLayout.name}</span>
          </div>
        )
      })()}
    </div>
  )
}