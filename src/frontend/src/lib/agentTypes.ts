import { OpenCode, Antigravity, Claude, Codex, GithubCopilot } from '@lobehub/icons'
import { Terminal, Bot } from 'lucide-react'

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
  claude: {
    id: 'claude',
    label: 'Claude Code',
    cmd: ['claude', '--dangerously-skip-permissions'],
    icon: Claude.Color,
  },
  codex: {
    id: 'codex',
    label: 'Codex CLI',
    cmd: ['codex', '--sandbox', 'workspace-write', '--ask-for-approval', 'never'],
    icon: Codex.Color,
  },
  copilot: {
    id: 'copilot',
    label: 'GitHub Copilot',
    cmd: ['copilot', '--allow-all-tools', '--allow-all-paths'],
    icon: GithubCopilot,
  },
  agy: {
    id: 'agy',
    label: 'Antigravity',
    cmd: ['agy', '--dangerously-skip-permissions'],
    icon: Antigravity.Color,
  },
  opencode: {
    id: 'opencode',
    label: 'OpenCode',
    cmd: ['opencode', '--dangerously-skip-permissions'],
    icon: OpenCode,
  },
  pi: {
    id: 'pi',
    label: 'Pi',
    cmd: ['pi'],
    icon: Bot,
  },
}