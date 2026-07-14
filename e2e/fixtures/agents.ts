import { test as base } from '@playwright/test'

export interface AgentInfo {
  id: string
  label: string
  cmd: string[]
}

export async function getAvailableAgents(baseURL?: string): Promise<AgentInfo[]> {
  const url = `${baseURL ?? ''}/api/agents`
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(15_000) })
    if (!res.ok) return []
    const json = await res.json()
    return json?.data ?? []
  } catch {
    return []
  }
}

export function isAgentAvailable(id: string): boolean {
  const raw = process.env.CAW_AGENTS
  if (!raw) return false
  try {
    const ids: string[] = JSON.parse(raw)
    return ids.includes(id)
  } catch {
    return false
  }
}

export const AGENT_IDS = ['claude', 'codex', 'copilot', 'agy', 'opencode', 'pi']

export function skipIfAgentNotAvailable(test: typeof base, agentId: string) {
  test.skip(!isAgentAvailable(agentId), `${agentId} not installed`)
}