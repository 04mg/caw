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
  return ws
}



const empty: BackendState = { workspaces: [], activeWorkspaceId: null }

type RemoteListener = (state: BackendState) => void
const listeners = new Set<RemoteListener>()
let unsubMux: (() => void) | null = null
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
      for (const l of listeners) l(stateData)
    } catch { /* ignore */ }
  })
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