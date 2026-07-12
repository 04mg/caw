import { type AgentStatus, type AgentStatusEvent } from '../types'


type AgentStatusListener = (statuses: Record<string, AgentStatus>) => void

let activeStatuses: Record<string, AgentStatus> = {}
const listeners = new Set<AgentStatusListener>()
let statusWs: WebSocket | null = null

function ensureWs() {
  if (statusWs) return
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  statusWs = new WebSocket(`${protocol}//${location.host}/ws/agents/statuses`)

  statusWs.onmessage = (e) => {
    try {
      const data = JSON.parse(e.data) as AgentStatusEvent
      if (!data || !data.sessionId) return

      if (data.event === 'agent_stopped') {
        delete activeStatuses[data.sessionId]
      } else if (data.event === 'agent_started') {
        activeStatuses[data.sessionId] = {
          sessionId: data.sessionId,
          agentId: data.agentId,
          status: 'idle',
          timestamp: data.timestamp || new Date().toISOString(),
          sequence: typeof data.sequence === 'number' ? data.sequence : undefined,
        }
      } else {
        // agent_status or initial payload
        activeStatuses[data.sessionId] = {
          sessionId: data.sessionId,
          agentId: data.agentId,
          status: data.status || 'idle',
          tool: data.tool,
          details: data.details,
          title: data.title,
          timestamp: data.timestamp || new Date().toISOString(),
          sequence: typeof data.sequence === 'number' ? data.sequence : undefined,
        }
      }

      notify()
    } catch (err) {
      console.error('Error handling agent status message:', err)
    }
  }

  statusWs.onclose = () => {
    statusWs = null
    setTimeout(() => ensureWs(), 2000)
  }
}

function notify() {
  const current = { ...activeStatuses }
  for (const l of listeners) l(current)
}

export function subscribeAgentStatuses(cb: AgentStatusListener): () => void {
  listeners.add(cb)
  ensureWs()
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
