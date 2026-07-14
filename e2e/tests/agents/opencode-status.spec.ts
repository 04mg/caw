import { createAgentStatusTests } from '../../fixtures/agent-tests'

createAgentStatusTests({
  agentId: 'opencode',
  label: 'OpenCode',
  workPrompt: 'list all files in this directory and describe each one in one sentence',
  responseTimeout: 180_000,
})