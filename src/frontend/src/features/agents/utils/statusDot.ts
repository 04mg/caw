import { type AgentStatus } from '@/features/agents/types'

export interface StatusDotColors {
  dot: string
  ring: string | null
}

export function getAgentStatusDot(agent: AgentStatus): StatusDotColors {
  if (agent.status === 'crashed') {
    return { dot: 'bg-red-500', ring: 'bg-red-500 animate-pulse' }
  }
  const s = agent.status.toLowerCase()
  if (s === 'interrupted' || s === 'tool_failed') {
    return { dot: 'bg-red-500', ring: 'bg-red-500 animate-pulse' }
  }
  if (s === 'waiting_input') {
    return { dot: 'bg-amber-400', ring: 'bg-amber-400 animate-pulse' }
  }
  if (s === 'thinking' || s === 'executing') {
    return { dot: 'bg-blue-400', ring: 'bg-blue-400 animate-ping' }
  }
  return { dot: 'bg-slate-400', ring: null }
}
