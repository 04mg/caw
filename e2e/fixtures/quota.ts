import Database from 'better-sqlite3'

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

/** Providers that don't implement IsInstalled() — gate by querying SQLite directly */
const DB_CHECKED_PROVIDERS: Record<string, (rows: Record<string, string>[]) => boolean> = {
  opencode: (rows) => rows.some((r) => r.key === 'cookie' && r.value) && rows.some((r) => r.key === 'workspaceId' && r.value),
  ollama: (rows) => rows.some((r) => r.key === 'cookie' && r.value),
  openrouter: (rows) => rows.some((r) => r.key === 'apiKey' && r.value),
}

function getDbPath(): string | undefined {
  return process.env.CAW_DB_PATH
}

function queryQuotaSettings(dbPath: string, provider: string): Record<string, string> {
  const db = new Database(dbPath, { readonly: true })
  try {
    const rows = db.prepare('SELECT key, value FROM quota_settings WHERE provider = ?').all(provider) as Record<string, string>[]
    const config: Record<string, string> = {}
    for (const row of rows) {
      config[row.key] = row.value
    }
    return config
  } finally {
    db.close()
  }
}

export function isDbQuotaProviderConfigured(provider: string): boolean {
  const check = DB_CHECKED_PROVIDERS[provider]
  if (!check) return false
  const dbPath = getDbPath()
  if (!dbPath) return false
  try {
    const rows = Object.entries(queryQuotaSettings(dbPath, provider)).map(([key, value]) => ({ key, value }))
    return check(rows)
  } catch {
    return false
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
  if (getInstalledQuotaProviders().includes(provider)) return true
  if (isDbQuotaProviderConfigured(provider)) return true
  return false
}

export function skipIfQuotaProviderNotInstalled(test: any, provider: string) {
  test.skip(!isQuotaProviderInstalled(provider), `${provider} not installed/configured`)
}
