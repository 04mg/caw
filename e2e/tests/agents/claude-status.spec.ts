import { createAgentStatusTests } from '../../fixtures/agent-tests'

createAgentStatusTests({
  agentId: 'claude',
  label: 'Claude Code',
  workPrompt: 'list all files in this directory and describe each one in one sentence',
  failPrompt: 'read the file /nonexistent/xyz.txt and tell me what is in it',
  responseTimeout: 180_000,
})