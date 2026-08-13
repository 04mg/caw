export interface AgentStatus {
  sessionId: string
  agentId: string
  cwd?: string
  status: string // "thinking", "executing", "waiting_input", "idle", "crashed",
                 // "interrupted" (user cancelled the turn — red dot, no push),
                 // "tool_failed" (last tool call failed — red dot, error in details)
  tool?: string
  details?: string
  title?: string
  timestamp: string
  // Sequence reflects the order in which agents were opened (assigned by the
  // backend). Used to keep a stable ordering in the UI instead of timestamps.
  sequence?: number
  // Terminal-state fields. Only populated when status === "crashed" (i.e. the
  // agent process died unexpectedly). A clean or user-killed session is
  // removed from the store and never carries these.
  endedAt?: string
  exitCode?: number
  exitReason?: string
  // lastColumn records the column the card was in just before the crash
  // ("working" | "needs_input" | "idle") so the board can keep the crashed
  // card in the column the user last saw it in.
  lastColumn?: string
}

export interface AgentStatusEvent {
  event: 'agent_started' | 'agent_stopped' | 'agent_status' | 'agent_crashed' | 'agent_snapshot'
  sessionId: string
  agentId: string
  cwd?: string
  status?: string
  tool?: string
  details?: string
  title?: string
  timestamp: string
  sequence?: number
  // Terminal-state fields, populated for "agent_crashed" events.
  endedAt?: string
  exitCode?: number
  exitReason?: string
  lastColumn?: string
  // Full authoritative snapshot, populated for "agent_snapshot" events.
  sessions?: AgentStatus[]
}

export interface AgentType {
  id: string
  label: string
  cmd: string[]
  env?: [string, string][] // [key, value] pairs injected into the PTY environment
  icon: any
}
