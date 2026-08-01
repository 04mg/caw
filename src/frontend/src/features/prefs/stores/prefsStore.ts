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
  })
}

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

async function persistAndBroadcast(next: PrefsState) {
  cache = next
  loaded = true
  // Persist to backend
  try {
    await fetch('/api/prefs', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(next),
    })
  } catch {
    // ignore
  }
  // Broadcast via WS for live sync
  wsMux.send('prefs', next)
  for (const l of listeners) l()
  window.dispatchEvent(new CustomEvent('caw:settings-updated'))
}

export async function setDefaultNewAgent(v: string) {
  await persistAndBroadcast({ ...cache, defaultNewAgent: v })
}

export async function setDisabledAgents(list: string[]) {
  await persistAndBroadcast({ ...cache, disabledAgents: list })
}

export async function toggleAgent(agentId: string) {
  const list = cache.disabledAgents.includes(agentId)
    ? cache.disabledAgents.filter((id) => id !== agentId)
    : [...cache.disabledAgents, agentId]
  await setDisabledAgents(list)
}

export async function setAgentCmdOverride(agentId: string, cmd: string[] | null) {
  const cmds = { ...cache.agentCmds }
  if (cmd && cmd.length > 0) {
    cmds[agentId] = cmd
  } else {
    delete cmds[agentId]
  }
  await persistAndBroadcast({ ...cache, agentCmds: cmds })
}

export async function setDefaultShell(v: string) {
  await persistAndBroadcast({ ...cache, defaultShell: v })
}
