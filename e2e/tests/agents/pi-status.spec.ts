import { createPiFamilyStatusTests } from '../../fixtures/pi-family-status'

createPiFamilyStatusTests({
  agentId: 'pi',
  label: 'Pi',
  hasAskTool: false,
  responseTimeout: 180_000,
})
