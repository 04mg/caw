import { type AgentStatus, type AgentStatusEvent } from '../types'
import { wsMux } from '@/features/shared/services/wsMultiplexer'


type AgentStatusListener = (statuses: Record<string, AgentStatus>) => void

let activeStatuses: Record<string, AgentStatus> = {}
const listeners = new Set<AgentStatusListener>()
let unsubMux: (() => void) | null = null

function ensureMux() {
  if (unsubMux) return
  unsubMux = wsMux.subscribe('agents', (data) => {
    try {
      const ev = data as AgentStatusEvent
      if (!ev || !ev.sessionId) return

      if (ev.event === 'agent_stopped') {
        delete activeStatuses[ev.sessionId]
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

      notify()
    } catch (err) {
      console.error('Error handling agent status message:', err)
    }
  })
}

// Start the background WS subscription as soon as this module is imported so
// the store stays live for the whole app lifetime, regardless of whether the
// Command Center is open. The multiplexer's onSubscribe handler sends the
// full current status snapshot, which replaces the need for a REST fetch
// when the Kanban board mounts. This keeps the Command Center instant to
// open because it just reads from the already-populated store.
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

export async function loadInitialStatuses(): Promise<Record<string, AgentStatus>> {
  try {
    const res = await fetch('/api/agents/statuses')
    if (!res.ok) return {}
    const list = (await res.json())?.data as AgentStatus[]
    const next: Record<string, AgentStatus> = {}
    for (const s of list) {
      next[s.sessionId] = s
    }
    activeStatuses = next
    notify()
    return next
  } catch (err) {
    console.error('Failed to load initial agent statuses:', err)
    return {}
  }
}
