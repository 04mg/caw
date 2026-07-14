import { createAgentStatusTests } from '../../fixtures/agent-tests'

createAgentStatusTests({
  agentId: 'claude',
  label: 'Claude Code',
  workPrompt: 'list all files in this directory and describe each one in one sentence',
  responseTimeout: 180_000,
})