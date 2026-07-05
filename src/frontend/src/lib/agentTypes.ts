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
    label: 'OpenCode',
    cmd: ['opencode'],
    icon: OpenCode,
  },
  agy: {
    id: 'agy',
    label: 'agy',
    cmd: ['agy'],
    icon: Antigravity,
  },
  claude: {
    id: 'claude',
    label: 'Claude Code',
    cmd: ['claude'],
    icon: Claude,
  },
}
