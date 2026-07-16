import { type AgentStatus, type AgentStatusEvent } from '../types'
import { wsMux } from '@/features/shared/services/wsMultiplexer'


type AgentStatusListener = (statuses: Record<string, AgentStatus>) => void

let activeStatuses: Record<string, AgentStatus> = {}
const listeners = new Set<AgentStatusListener>()
let unsubMux: (() => void) | null = null
// True once the store has received its first data snapshot from either the
// REST seed fetch or the WS subscription. Until then, consumers should treat
// an empty `activeStatuses` as "loading" rather than "no agents".
let hydrated = false

function ensureMux() {
  if (unsubMux) return
  unsubMux = wsMux.subscribe('agents', (data) => {
    try {
      const ev = data as AgentStatusEvent
      if (!ev || !ev.sessionId) return

      if (ev.event === 'agent_stopped') {
        delete activeStatuses[ev.sessionId]
        hydrated = true
      } else if (ev.event === 'agent_started') {
        activeStatuses[ev.sessionId] = {
          sessionId: ev.sessionId,
          agentId: ev.agentId,
          cwd: ev.cwd,
          status: 'idle',
          timestamp: ev.timestamp || new Date().toISOString(),
          sequence: typeof ev.sequence === 'number' ? ev.sequence : undefined,
        }
      } else {
        // agent_status or initial payload
        activeStatuses[ev.sessionId] = {
          sessionId: ev.sessionId,
          agentId: ev.agentId,
          cwd: ev.cwd,
          status: ev.status || 'idle',
          tool: ev.tool,
          details: ev.details,
          title: ev.title,
          timestamp: ev.timestamp || new Date().toISOString(),
          sequence: typeof ev.sequence === 'number' ? ev.sequence : undefined,
        }
      }

      hydrated = true
      notify()
    } catch (err) {
      console.error('Error handling agent status message:', err)
    }
  })
}

// Start the background WS subscription as soon as this module is imported so
// the store stays live for the whole app lifetime, regardless of whether the
// Command Center is open. The multiplexer's onSubscribe handler sends the
// full current status snapshot once the WebSocket handshake completes.
//
// On a cold page load (or after a WS reconnect), the handshake + subscribe +
// server snapshot round-trip takes time, during which the store is empty and
// the Command Center would render "No agents" until the snapshot lands. We
// seed the store with a synchronous REST fetch first so cards render
// instantly; the WS subscription then keeps it live and reconciles deltas.
loadInitialStatuses()
ensureMux()

function notify() {
  const current = { ...activeStatuses }
  for (const l of listeners) l(current)
}

export function subscribeAgentStatuses(cb: AgentStatusListener): () => void {
  listeners.add(cb)
  ensureMux()
  // Trigger initial callback with whatever state we currently have
  cb({ ...activeStatuses })
  return () => {
    listeners.delete(cb)
  }
}

// Synchronous snapshot of the current agent statuses. Use this to seed
// component initial state so the first render already reflects the latest
// store data instead of an empty object that flashes "No agents" placeholders
// before the subscribe effect fires.
export function getAgentStatuses(): Record<string, AgentStatus> {
  return { ...activeStatuses }
}

// True once the store has received its first snapshot (REST or WS). Consumers
// can use this to distinguish "still loading" from "genuinely no agents".
export function isAgentStatusesHydrated(): boolean {
  return hydrated
}

export async function loadInitialStatuses(): Promise<Record<string, AgentStatus>> {
  try {
    const res = await fetch('/api/agents/statuses')
    if (!res.ok) return {}
    const list = (await res.json())?.data as AgentStatus[]
    const next: Record<string, AgentStatus> = {}
    for (const s of list) {
      next[s.sessionId] = s
    }
    // Merge REST results underneath any live WS data so we never clobber
    // updates the WebSocket may have pushed between this fetch being
    // dispatched and completing, and never resurrect sessions the WS has
    // already removed.
    const merged: Record<string, AgentStatus> = {}
    for (const id of Object.keys(next)) {
      if (!activeStatuses[id]) merged[id] = next[id]
    }
    activeStatuses = { ...activeStatuses, ...merged }
    hydrated = true
    notify()
    return activeStatuses
  } catch (err) {
    console.error('Failed to load initial agent statuses:', err)
    return {}
  }
}
