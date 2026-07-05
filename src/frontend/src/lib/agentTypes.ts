import { OpenCode, Antigravity, Claude } from '@lobehub/icons'
import { Terminal } from 'lucide-react'

export interface AgentType {
  id: string
  label: string
  cmd: string[]
  icon: any
}

export const agentTypes: Record<string, AgentType> = {
  terminal: {
    id: 'terminal',
    label: 'New Terminal',
    cmd: [],
    icon: Terminal,
  },
  opencode: {
    id: 'opencode',
    label: 'OpenCode Go',
    cmd: ['opencode', '--dangerously-skip-permissions'],
    icon: OpenCode,
  },
  agy: {
    id: 'agy',
    label: 'Antigravity',
    cmd: ['agy', '--dangerously-skip-permissions'],
    icon: Antigravity,
  },
  claude: {
    id: 'claude',
    label: 'Claude Code',
    cmd: ['claude', '--dangerously-skip-permissions'],
    icon: Claude,
  },
}
