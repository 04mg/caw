import { Terminal } from 'lucide-react'
import { type AgentType } from '../types'
import { PiIcon } from '../components/PiIcon'
import { OmpIcon } from '../components/OmpIcon'
import { CommandCodeIcon } from '../components/CommandCodeIcon'
import { FxIcon } from '../components/FxIcon'
import { ClaudeIcon } from '../components/ClaudeIcon'
import { CodexIcon } from '../components/CodexIcon'
import { GithubCopilotIcon } from '../components/GithubCopilotIcon'
import { AntigravityIcon } from '../components/AntigravityIcon'
import { OpenCodeIcon } from '../components/OpenCodeIcon'
import { NousResearchIcon } from '../components/NousResearchIcon'

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
    icon: ClaudeIcon,
  },
  codex: {
    id: 'codex',
    label: 'Codex CLI',
    cmd: ['codex', '--sandbox', 'workspace-write', '--ask-for-approval', 'never'],
    icon: CodexIcon,
  },
  copilot: {
    id: 'copilot',
    label: 'GitHub Copilot',
    cmd: ['copilot', '--allow-all-tools', '--allow-all-paths'],
    icon: GithubCopilotIcon,
  },
  agy: {
    id: 'agy',
    label: 'Antigravity',
    cmd: ['agy', '--dangerously-skip-permissions'],
    icon: AntigravityIcon,
  },
  opencode: {
    id: 'opencode',
    label: 'OpenCode',
    cmd: ['opencode', '--auto'],
    icon: OpenCodeIcon,
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
    icon: NousResearchIcon,
  },
  commandcode: {
    id: 'commandcode',
    label: 'Command Code',
    cmd: ['command-code', '--yolo'],
    icon: CommandCodeIcon,
  },
  fx: {
    id: 'fx',
    label: 'Fx',
    cmd: ['fx'],
    icon: FxIcon,
  },
}
