export interface AgentStatus {
  sessionId: string
  agentId: string
  status: string // "thinking", "executing", "waiting_input", "idle", "stopped"
  tool?: string
  details?: string
  title?: string
  timestamp: string
}

export interface AgentStatusEvent {
  event: 'agent_started' | 'agent_stopped' | 'agent_status'
  sessionId: string
  agentId: string
  cwd?: string
  status?: string
  tool?: string
  details?: string
  title?: string
  timestamp: string
}

export interface AgentType {
  id: string
  label: string
  cmd: string[]
  icon: any
}
