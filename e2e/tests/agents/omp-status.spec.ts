import { createPiFamilyStatusTests } from '../../fixtures/pi-family-status'

createPiFamilyStatusTests({
  agentId: 'omp',
  label: 'Oh My Pi',
  hasAskTool: true,
  responseTimeout: 180_000,
})
