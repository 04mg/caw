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
  if (s === 'unknown') {
    return { dot: 'bg-violet-400', ring: 'bg-violet-400 animate-pulse' }
  }
  return { dot: 'bg-slate-400', ring: null }
}

// statusPriority ranks agent statuses by "strength" for aggregation (e.g. a
// workspace roll-up dot). Lower rank = stronger / shown first.
//   0 = failed (crashed / interrupted / tool_failed)
//   1 = needs input (waiting_input)
//   2 = working (thinking / executing)
//   3 = unknown (stale / unclassifiable — not a confident idle)
//   4 = idle (and anything else)
export function statusPriority(status: string): number {
  const s = status.toLowerCase()
  if (s === 'crashed' || s === 'interrupted' || s === 'tool_failed') return 0
  if (s === 'waiting_input') return 1
  if (s === 'thinking' || s === 'executing') return 2
  if (s === 'unknown') return 3
  return 4
}

// getStrongestStatus returns the strongest status (by priority) among the
// given statuses, or undefined when the list is empty. Ties keep the first
// encountered strongest status.
export function getStrongestStatus(statuses: AgentStatus[]): AgentStatus | undefined {
  if (statuses.length === 0) return undefined
  let best: AgentStatus | undefined
  let bestRank = Infinity
  for (const s of statuses) {
    const rank = statusPriority(s.status)
    if (rank < bestRank) {
      bestRank = rank
      best = s
    }
  }
  return best
}
