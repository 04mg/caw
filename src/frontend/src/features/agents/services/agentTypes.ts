import { OpenCode, Antigravity, Claude, Codex, GithubCopilot, NousResearch } from '@lobehub/icons'
import { Terminal } from 'lucide-react'
import { type AgentType } from '../types'
import { PiIcon } from '../components/PiIcon'
import { OmpIcon } from '../components/OmpIcon'

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
  hermes: {
    id: 'hermes',
    label: 'Hermes',
    cmd: ['hermes', '--yolo'],
    env: [['HERMES_TUI_BACKGROUND', '#000000']],
    icon: NousResearch.Avatar,
  },
}
