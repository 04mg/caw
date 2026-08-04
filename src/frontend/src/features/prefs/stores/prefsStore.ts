import { wsMux } from '@/features/shared/services/wsMultiplexer'

export interface PetsConfig {
  enabled: boolean
  roster: string[]
  agentPins: Record<string, string>
  // Persistent per-agent assignments (agentId -> pet slug), kept in sync
  // automatically so a pet survives its terminal being closed and reopened.
  assignments?: Record<string, string>
}

export interface PrefsState {
  defaultNewAgent: string
  disabledAgents: string[]
  agentCmds: Record<string, string[]>
  defaultShell: string
  parkedTerminals: number
  hotkeys: Record<string, string>
  pets: PetsConfig
}

export const DEFAULT_PARKED_TERMINALS = 6

export const DEFAULT_PETS: PetsConfig = {
  enabled: false,
  roster: [],
  agentPins: {},
  assignments: {},
}

export const DEFAULT_HOTKEYS: Record<string, string> = {
  closePane: 'Alt+W',
  switchPaneLeft: 'Alt+ArrowLeft',
  switchPaneRight: 'Alt+ArrowRight',
  newTerminal: 'Alt+T',
  splitHorizontal: 'Alt+H',
  splitVertical: 'Alt+V',
  commandPalette: 'Alt+P',
  commandPaletteCmd: 'Alt+Shift+P',
  toggleKanban: 'Alt+C',
}

export const HOTKEY_LABELS: Record<string, string> = {
  closePane: 'Close pane',
  switchPaneLeft: 'Switch pane left',
  switchPaneRight: 'Switch pane right',
  newTerminal: 'New terminal',
  splitHorizontal: 'Horizontal split',
  splitVertical: 'Vertical split',
  commandPalette: 'Command palette',
  commandPaletteCmd: 'Command palette (commands)',
  toggleKanban: 'Toggle Command Center',
}

let cache: PrefsState = {
  defaultNewAgent: 'none',
  disabledAgents: [],
  agentCmds: {},
  defaultShell: '',
  parkedTerminals: DEFAULT_PARKED_TERMINALS,
  hotkeys: { ...DEFAULT_HOTKEYS },
  pets: { ...DEFAULT_PETS, agentPins: {} },
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

export function getParkedTerminalLimit(): number {
  const v = cache.parkedTerminals
  return Number.isFinite(v) ? Math.max(0, Math.min(16, Math.floor(v))) : DEFAULT_PARKED_TERMINALS
}

export async function setParkedTerminals(v: number): Promise<boolean> {
  return persistAndBroadcast({ ...cache, parkedTerminals: v })
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
}export function getHotkey(action: string): string {
  return cache.hotkeys[action] || DEFAULT_HOTKEYS[action] || ''
}

export async function setHotkey(action: string, combo: string): Promise<boolean> {
  return persistAndBroadcast({
    ...cache,
    hotkeys: { ...cache.hotkeys, [action]: combo },
  })
}

export async function resetHotkey(action: string): Promise<boolean> {
  const next = { ...cache.hotkeys }
  delete next[action]
  return persistAndBroadcast({ ...cache, hotkeys: next })
}

export async function resetAllHotkeys(): Promise<boolean> {
  return persistAndBroadcast({ ...cache, hotkeys: { ...DEFAULT_HOTKEYS } })
}

export function getPetsConfig(): PetsConfig {
  return cache.pets
}

export async function setPetsConfig(pets: PetsConfig): Promise<boolean> {
  return persistAndBroadcast({ ...cache, pets })
}

export async function setPetsEnabled(enabled: boolean): Promise<boolean> {
  return persistAndBroadcast({ ...cache, pets: { ...cache.pets, enabled } })
}

export async function setPetRoster(roster: string[]): Promise<boolean> {
  const agentPins = { ...cache.pets.agentPins }
  // Drop pins for slugs no longer in the roster so they fall back to rotation.
  for (const [agentId, slug] of Object.entries(agentPins)) {
    if (!roster.includes(slug)) delete agentPins[agentId]
  }
  const assignments = { ...(cache.pets.assignments ?? {}) }
  for (const [agentId, slug] of Object.entries(assignments)) {
    if (!roster.includes(slug)) delete assignments[agentId]
  }
  return persistAndBroadcast({
    ...cache,
    pets: { ...cache.pets, roster, agentPins, assignments },
  })
}

export async function setAgentPetAssignment(agentId: string, slug: string | null): Promise<boolean> {
  const assignments = { ...(cache.pets.assignments ?? {}) }
  if (slug && cache.pets.roster.includes(slug)) {
    assignments[agentId] = slug
  } else {
    delete assignments[agentId]
  }
  return persistAndBroadcast({ ...cache, pets: { ...cache.pets, assignments } })
}

export async function setAgentPetAssignments(all: Record<string, string>): Promise<boolean> {
  return persistAndBroadcast({ ...cache, pets: { ...cache.pets, assignments: all } })
}

export async function setAgentPetPin(agentId: string, slug: string | null): Promise<boolean> {
  const agentPins = { ...cache.pets.agentPins }
  if (slug && cache.pets.roster.includes(slug)) {
    agentPins[agentId] = slug
  } else {
    delete agentPins[agentId]
  }
  return persistAndBroadcast({ ...cache, pets: { ...cache.pets, agentPins } })
}
