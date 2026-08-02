import { wsMux } from '@/features/shared/services/wsMultiplexer'

export interface PrefsState {
  defaultNewAgent: string
  disabledAgents: string[]
  agentCmds: Record<string, string[]>
  defaultShell: string
}

let cache: PrefsState = {
  defaultNewAgent: 'none',
  disabledAgents: [],
  agentCmds: {},
  defaultShell: '',
}

let loaded = false
const listeners = new Set<() => void>()
let unsubMux: (() => void) | null = null

function ensureMux() {
  if (unsubMux) return
  unsubMux = wsMux.subscribe('prefs', (data) => {
    const p = data as PrefsState
    if (!p || typeof p.defaultNewAgent !== 'string') return
    cache = p
    loaded = true
    for (const l of listeners) l()
    window.dispatchEvent(new CustomEvent('caw:settings-updated'))
  })
}

// Subscribe on module load so the prefs cache stays live across devices:
// the backend pushes current prefs on subscribe and broadcasts any update
// made by another device, keeping this tab's cache in sync.
ensureMux()

export function subscribePrefs(cb: () => void): () => void {
  listeners.add(cb)
  ensureMux()
  return () => { listeners.delete(cb) }
}

export async function loadPrefs(): Promise<PrefsState> {
  if (loaded) return cache
  try {
    const res = await fetch('/api/prefs')
    if (res.ok) {
      const data = (await res.json())?.data as PrefsState
      if (data && typeof data.defaultNewAgent === 'string') {
        cache = data
        loaded = true
      }
    }
  } catch {
    // use defaults
  }
  return cache
}

export function getPrefs(): PrefsState {
  return cache
}

export function getDefaultNewAgent(): string {
  return cache.defaultNewAgent
}

export function getDisabledAgents(): string[] {
  return cache.disabledAgents
}

export function getAgentCmdOverrides(): Record<string, string[]> {
  return cache.agentCmds
}

export function getEffectiveAgentCmd(agentId: string, defaultCmd: string[]): string[] {
  const override = cache.agentCmds[agentId]
  if (override && Array.isArray(override) && override.length > 0) return override
  return defaultCmd
}

export function getDefaultShell(): string {
  return cache.defaultShell
}

async function persistAndBroadcast(next: PrefsState): Promise<boolean> {
  cache = next
  loaded = true
  // Persist to backend
  let ok = false
  try {
    const res = await fetch('/api/prefs', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(next),
    })
    ok = res.ok
  } catch {
    // ignore
  }
  // Broadcast via WS for live sync
  wsMux.send('prefs', next)
  for (const l of listeners) l()
  window.dispatchEvent(new CustomEvent('caw:settings-updated'))
  return ok
}

export async function setDefaultNewAgent(v: string): Promise<boolean> {
  return persistAndBroadcast({ ...cache, defaultNewAgent: v })
}

export async function setDisabledAgents(list: string[]): Promise<boolean> {
  return persistAndBroadcast({ ...cache, disabledAgents: list })
}

export async function toggleAgent(agentId: string): Promise<boolean> {
  const list = cache.disabledAgents.includes(agentId)
    ? cache.disabledAgents.filter((id) => id !== agentId)
    : [...cache.disabledAgents, agentId]
  return setDisabledAgents(list)
}

export async function setAgentCmdOverride(agentId: string, cmd: string[] | null): Promise<boolean> {
  const cmds = { ...cache.agentCmds }
  if (cmd && cmd.length > 0) {
    cmds[agentId] = cmd
  } else {
    delete cmds[agentId]
  }
  return persistAndBroadcast({ ...cache, agentCmds: cmds })
}

export async function setDefaultShell(v: string): Promise<boolean> {
  return persistAndBroadcast({ ...cache, defaultShell: v })
}
