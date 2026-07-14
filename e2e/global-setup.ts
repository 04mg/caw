import { getAvailableAgents } from './fixtures/agents'
import { getQuotaSettings } from './fixtures/quota'

export default async function globalSetup() {
  const baseURL = `http://127.0.0.1:${process.env.CAW_PORT || '8080'}`

  // Retry detection — the server might take a moment to respond
  for (let i = 0; i < 10; i++) {
    const agents = await getAvailableAgents(baseURL)
    if (agents.length > 0 || i === 9) {
      process.env.CAW_AGENTS = JSON.stringify(agents.map((a) => a.id))
      break
    }
    await new Promise((r) => setTimeout(r, 2000))
  }

  for (let i = 0; i < 10; i++) {
    const settings = await getQuotaSettings(baseURL)
    if (Object.keys(settings).length > 0 || i === 9) {
      const installed: string[] = []
      for (const [provider, config] of Object.entries(settings)) {
        if (config?.installed === 'true') {
          installed.push(provider)
        }
      }
      process.env.CAW_QUOTA_PROVIDERS = JSON.stringify(installed)
      break
    }
    await new Promise((r) => setTimeout(r, 2000))
  }
}