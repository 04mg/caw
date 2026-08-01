import { type Workspace, type BackendState } from '../types'
import { normalizeLayout } from '@/features/shared/utils/layout'
import { wsMux } from '@/features/shared/services/wsMultiplexer'

function fixWorkspace(ws: Workspace): Workspace {
  if (!Array.isArray(ws.layouts)) {
    ws.layouts = []
    ws.activeTabIndex = 0
    ws.activePaneId = ''
  } else {
    ws.layouts = ws.layouts.map((t) => ({ ...t, layout: normalizeLayout(t.layout) }))
  }
  if (ws.enableWorktrees === undefined) {
    ws.enableWorktrees = false
  }
  if (!Array.isArray(ws.copyToWorktrees)) {
    ws.copyToWorktrees = []
  }
  return ws
}



const empty: BackendState = { workspaces: [], activeWorkspaceId: null }

type RemoteListener = (state: BackendState) => void
const listeners = new Set<RemoteListener>()
let unsubMux: (() => void) | null = null
let lastRemoteState: BackendState | null = null
let pendingSend: BackendState | null = null
let sendTimer: ReturnType<typeof setTimeout> | null = null

function ensureMux() {
  if (unsubMux) return
  unsubMux = wsMux.subscribe('state', (data) => {
    try {
      const stateData = data as BackendState
      if (!stateData || !Array.isArray(stateData.workspaces)) return
      for (const ws of stateData.workspaces) {
        fixWorkspace(ws)
      }
      // Skip if identical to the last state we processed, so listeners
      // don't re-render on no-op broadcasts (e.g. our own echo filtered
      // by the backend dedup, or a duplicate from a second tab).
      if (lastRemoteState && remoteStatesEqual(lastRemoteState, stateData)) return
      lastRemoteState = stateData
      for (const l of listeners) l(stateData)
    } catch { /* ignore */ }
  })
}

function remoteStatesEqual(a: BackendState, b: BackendState): boolean {
  if (a.activeWorkspaceId !== b.activeWorkspaceId) return false
  if (a.workspaces.length !== b.workspaces.length) return false
  for (let i = 0; i < a.workspaces.length; i++) {
    const aw = a.workspaces[i]
    const bw = b.workspaces[i]
    if (aw.id !== bw.id || aw.activeTabIndex !== bw.activeTabIndex ||
        aw.activePaneId !== bw.activePaneId) return false
    if ((aw.copyToWorktrees || []).join('\u0000') !== (bw.copyToWorktrees || []).join('\u0000')) return false
    if (aw.layouts.length !== bw.layouts.length) return false
    for (let j = 0; j < aw.layouts.length; j++) {
      if (aw.layouts[j].id !== bw.layouts[j].id) return false
    }
  }
  return true
}

export function subscribeRemoteState(cb: RemoteListener): () => void {
  listeners.add(cb)
  ensureMux()
  return () => { listeners.delete(cb) }
}

export async function loadState(): Promise<BackendState> {
  try {
    const res = await fetch('/api/workspaces')
    if (!res.ok) return { ...empty }
    const data = (await res.json())?.data as BackendState
    if (!data || !Array.isArray(data.workspaces)) return { ...empty }
    for (const ws of data.workspaces) {
      fixWorkspace(ws)
    }
    return {
      workspaces: data.workspaces,
      activeWorkspaceId: data.activeWorkspaceId ?? (data.workspaces[0]?.id ?? null),
    }
  } catch {
    return { ...empty }
  }
}

export function persistWorkspaces(workspaces: Workspace[], activeWorkspaceId: string | null) {
  pendingSend = { workspaces, activeWorkspaceId }
  if (sendTimer) clearTimeout(sendTimer)
  sendTimer = setTimeout(() => {
    if (!pendingSend) return
    const payload = pendingSend
    pendingSend = null
    wsMux.send('state', payload)
  }, 150)
}