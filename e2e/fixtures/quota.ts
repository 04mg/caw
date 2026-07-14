export interface QuotaSettings {
  [provider: string]: Record<string, string>
}

export async function getQuotaSettings(baseURL?: string): Promise<QuotaSettings> {
  const url = `${baseURL ?? ''}/api/quotas/settings`
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(15_000) })
    if (!res.ok) return {}
    const json = await res.json()
    return json?.data ?? {}
  } catch {
    return {}
  }
}

export function getInstalledQuotaProviders(): string[] {
  const raw = process.env.CAW_QUOTA_PROVIDERS
  if (!raw) return []
  try {
    return JSON.parse(raw) as string[]
  } catch {
    return []
  }
}

export const QUOTA_PROVIDER_IDS = [
  'claude',
  'codex',
  'copilot',
  'antigravity',
  'opencode',
  'ollama',
  'openrouter',
]

export function isQuotaProviderInstalled(provider: string): boolean {
  return getInstalledQuotaProviders().includes(provider)
}

export function skipIfQuotaProviderNotInstalled(test: any, provider: string) {
  test.skip(!isQuotaProviderInstalled(provider), `${provider} not installed/configured`)
}