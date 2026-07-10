import { type Workspace, type BackendState } from '../types'



const empty: BackendState = { workspaces: [], activeWorkspaceId: null }

type RemoteListener = (state: BackendState) => void
const listeners = new Set<RemoteListener>()
let stateWs: WebSocket | null = null
let pendingSend: BackendState | null = null
let sendTimer: ReturnType<typeof setTimeout> | null = null

function ensureWs() {
  if (stateWs) return
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  stateWs = new WebSocket(`${protocol}//${location.host}/ws/state`)
  stateWs.onmessage = (e) => {
    try {
      const data = JSON.parse(e.data) as BackendState
      if (!data || !Array.isArray(data.workspaces)) return
      for (const ws of data.workspaces) {
        if (!Array.isArray(ws.layouts)) {
          ws.layouts = []
          ws.activeTabIndex = 0
          ws.activePaneId = ''
        }
        if (ws.enableWorktrees === undefined) {
          ws.enableWorktrees = false
        }
      }
      for (const l of listeners) l(data)
    } catch { /* ignore */ }
  }
  stateWs.onclose = () => {
    stateWs = null
    setTimeout(() => ensureWs(), 1000)
  }
}

export function subscribeRemoteState(cb: RemoteListener): () => void {
  listeners.add(cb)
  ensureWs()
  return () => { listeners.delete(cb) }
}

export async function loadState(): Promise<BackendState> {
  try {
    const res = await fetch('/api/workspaces')
    if (!res.ok) return { ...empty }
    const data = (await res.json())?.data as BackendState
    if (!data || !Array.isArray(data.workspaces)) return { ...empty }
    for (const ws of data.workspaces) {
      if (!Array.isArray(ws.layouts)) {
        ws.layouts = []
        ws.activeTabIndex = 0
        ws.activePaneId = ''
      }
      if (ws.enableWorktrees === undefined) {
        ws.enableWorktrees = false
      }
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
    if (stateWs && stateWs.readyState === WebSocket.OPEN) {
      stateWs.send(JSON.stringify(payload))
    } else {
      fetch('/api/workspaces', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }).catch(() => { /* ignore */ })
    }
  }, 150)
}