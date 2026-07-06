import React from 'react'
import { OpenCode, Antigravity, Claude, Codex, GithubCopilot } from '@lobehub/icons'
import { Terminal } from 'lucide-react'

function PiIcon({ className }: { className?: string }) {
	return React.createElement('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 800 800', className, fill: 'currentColor' },
		React.createElement('path', { fillRule: 'evenodd', d: 'M165.29 165.29H517.36V400H400V517.36H282.65V634.72H165.29ZM282.65 282.65V400H400V282.65Z' }),
		React.createElement('path', { d: 'M517.36 400H634.72V634.72H517.36Z' })
	)
}

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
    icon: PiIcon,
  },
}