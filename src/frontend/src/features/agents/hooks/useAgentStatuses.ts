import { useEffect, useState } from 'react'
import { subscribeAgentStatuses, getAgentStatuses } from '@/features/agents/stores/agentStatusStore'
import { type AgentStatus } from '@/features/agents/types'

export function useAgentStatuses(): Record<string, AgentStatus> {
  const [statuses, setStatuses] = useState<Record<string, AgentStatus>>(() => getAgentStatuses())
  useEffect(() => {
    return subscribeAgentStatuses((next) => setStatuses(next))
  }, [])
  return statuses
}
