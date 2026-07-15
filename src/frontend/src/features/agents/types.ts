export interface AgentStatus {
  sessionId: string
  agentId: string
  cwd?: string
  status: string // "thinking", "executing", "waiting_input", "idle", "stopped"
  tool?: string
  details?: string
  title?: string
  timestamp: string
  // Sequence reflects the order in which agents were opened (assigned by the
  // backend). Used to keep a stable ordering in the UI instead of timestamps.
  sequence?: number
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
  sequence?: number
}

export interface AgentType {
  id: string
  label: string
  cmd: string[]
  env?: [string, string][] // [key, value] pairs injected into the PTY environment
  icon: any
}
