import { OpenCode, Antigravity, Claude, Codex, GithubCopilot } from '@lobehub/icons'
import { Terminal } from 'lucide-react'
import { type AgentType } from '../types'
import { PiIcon } from '../components/PiIcon'
import { OmpIcon } from '../components/OmpIcon'


const AGENT_CMDS_KEY = 'caw:agentCmds'

export function getAgentCmdOverrides(): Record<string, string[]> {
  try {
    const raw = localStorage.getItem(AGENT_CMDS_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

export function setAgentCmdOverride(agentId: string, cmd: string[] | null) {
  const overrides = getAgentCmdOverrides()
  if (cmd && cmd.length > 0) {
    overrides[agentId] = cmd
  } else {
    delete overrides[agentId]
  }
  localStorage.setItem(AGENT_CMDS_KEY, JSON.stringify(overrides))
}

export function getEffectiveAgentCmd(agentId: string, defaultCmd: string[]): string[] {
  const overrides = getAgentCmdOverrides()
  const override = overrides[agentId]
  if (override && Array.isArray(override) && override.length > 0) return override
  return defaultCmd
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
    env: [['IS_SANDBOX', '1']],
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
    cmd: ['opencode', '--auto'],
    icon: OpenCode,
  },
  pi: {
    id: 'pi',
    label: 'Pi',
    cmd: ['pi'],
    icon: PiIcon,
  },
  omp: {
    id: 'omp',
    label: 'Oh My Pi',
    cmd: ['omp'],
    icon: OmpIcon,
  },
}