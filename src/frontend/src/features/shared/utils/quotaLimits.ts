export const QUOTA_PROVIDER_IDS = [
  'claude',
  'codex',
  'copilot',
  'antigravity',
  'opencode',
  'ollama',
  'openrouter',
  'commandcode',
  'zed',
] as const

export type QuotaProviderId = (typeof QUOTA_PROVIDER_IDS)[number]
export type QuotaSettingsMode = 'legacy' | 'accounts'
export type QuotaMetricKey = 'fiveHour' | 'weekly' | 'monthly'

export interface QuotaValue {
  used: number
  limit: number
  unit?: string
  resetTime?: string
}

export interface QuotaItem {
  name: string
  label: string
  description: string
  used: number
  limit: number
  unit?: string
  resetTime?: string
}

export interface QuotaGroup {
  name: string
  description: string
  items: QuotaItem[]
}

export interface QuotaResponseData {
  fiveHour?: QuotaValue
  weekly?: QuotaValue
  monthly?: QuotaValue
  groups?: QuotaGroup[]
}

export interface QuotaAccountSettings {
  id: string
  name: string
  config: Record<string, string>
}

export interface QuotaProviderSettings {
  installed?: boolean
  accounts: QuotaAccountSettings[]
  defaultAccountId?: string
}

export interface NormalizedQuotaAccount {
  id: string
  name: string
  data?: QuotaResponseData
  error?: string
  synthetic?: boolean
}

export interface NormalizedProviderQuota {
  error?: string
  accounts: NormalizedQuotaAccount[]
}

export interface QuotaSelection {
  providerId: string
  accountId?: string
  kind: 'window' | 'groupItem'
  metricKey?: QuotaMetricKey
  groupName?: string
  itemName?: string
}

export const QUOTA_PROVIDER_LABELS: Record<QuotaProviderId, string> = {
  claude: 'Claude',
  codex: 'Codex',
  copilot: 'Copilot',
  antigravity: 'Antigravity',
  opencode: 'OpenCode Go',
  ollama: 'Ollama',
  openrouter: 'OpenRouter',
  commandcode: 'Command Code',
  zed: 'Zed',
}

export const QUOTA_WINDOW_LABELS: Record<QuotaProviderId, Partial<Record<QuotaMetricKey, string>>> = {
  claude: {
    fiveHour: 'Session Limit',
    weekly: 'Weekly Limit',
    monthly: 'Monthly Limit',
  },
  codex: {
    fiveHour: '5h Limit',
    weekly: 'Weekly Limit',
  },
  copilot: {
    fiveHour: 'AI Credits',
    weekly: 'Chat Limit',
  },
  antigravity: {
    fiveHour: '5h Limit',
    weekly: 'Weekly Limit',
    monthly: 'Monthly Limit',
  },
  opencode: {
    fiveHour: '5h Limit',
    weekly: 'Weekly Limit',
    monthly: 'Monthly Limit',
  },
  ollama: {
    fiveHour: 'Session Limit',
    weekly: 'Weekly Limit',
  },
  openrouter: {
    fiveHour: 'Daily Usage',
    weekly: 'Weekly Usage',
    monthly: 'Monthly Usage',
  },
  commandcode: {
    fiveHour: '5h Limit',
    weekly: 'Weekly Limit',
    monthly: 'Monthly Limit',
  },
  zed: {
    monthly: 'Monthly Limit',
  },
}

const DEFAULT_ACCOUNT_ID = 'default'
const DEFAULT_ACCOUNT_NAME = 'Default'
const AUTO_CONFIGURED_PROVIDERS = new Set<QuotaProviderId>(['claude', 'codex', 'antigravity'])

const REQUIRED_CONFIG_KEYS: Record<QuotaProviderId, string[]> = {
  claude: [],
  codex: [],
  copilot: ['token'],
  antigravity: [],
  opencode: ['cookie', 'workspaceId'],
  ollama: ['cookie'],
  openrouter: ['apiKey'],
  commandcode: ['cookie'],
  zed: ['cookie'],
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function asString(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function asNumber(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return 0
}

function sanitizeAccountName(value: string, fallback: string): string {
  const trimmed = value.trim()
  return trimmed || fallback
}

function createFallbackAccount(providerId: string, index = 0): QuotaAccountSettings {
  return {
    id: index === 0 ? DEFAULT_ACCOUNT_ID : `${providerId}-${Date.now().toString(36)}-${index + 1}`,
    name: index === 0 ? DEFAULT_ACCOUNT_NAME : `Account ${index + 1}`,
    config: {},
  }
}

function normalizeAccountId(value: string, fallback: string): string {
  const trimmed = value.trim()
  return trimmed || fallback
}

function extractConfig(source: Record<string, unknown>, skipKeys: string[] = []): Record<string, string> {
  const skip = new Set(skipKeys)
  const config: Record<string, string> = {}
  for (const [key, value] of Object.entries(source)) {
    if (skip.has(key)) continue
    const stringValue = asString(value)
    if (stringValue !== '') {
      config[key] = stringValue
    }
  }
  return config
}

function safeJsonParse(value: string): unknown {
  try {
    return JSON.parse(value)
  } catch {
    return undefined
  }
}

function parseAccounts(raw: unknown, fallbackProviderId: string): QuotaAccountSettings[] {
  if (Array.isArray(raw)) {
    return raw
      .map((entry, index) => parseAccountEntry(entry, fallbackProviderId, index))
      .filter((entry): entry is QuotaAccountSettings => !!entry)
  }
  if (isRecord(raw)) {
    return Object.entries(raw)
      .map(([id, entry], index) => parseAccountEntry(entry, fallbackProviderId, index, id))
      .filter((entry): entry is QuotaAccountSettings => !!entry)
  }
  return []
}

function parseAccountEntry(raw: unknown, fallbackProviderId: string, index: number, fallbackId?: string): QuotaAccountSettings | null {
  if (!isRecord(raw)) {
    if (typeof raw === 'string' && raw.trim()) {
      return {
        id: normalizeAccountId(fallbackId ?? `${fallbackProviderId}-${index + 1}`, `${fallbackProviderId}-${index + 1}`),
        name: sanitizeAccountName(raw, `Account ${index + 1}`),
        config: {},
      }
    }
    return null
  }

  const configSource = isRecord(raw.config) ? raw.config : isRecord(raw.settings) ? raw.settings : raw
  const id = normalizeAccountId(
    asString(raw.id || raw.accountId || fallbackId),
    fallbackId || `${fallbackProviderId}-${index + 1}`,
  )
  const name = sanitizeAccountName(
    asString(raw.name || raw.label || raw.accountName),
    index === 0 ? DEFAULT_ACCOUNT_NAME : `Account ${index + 1}`,
  )

  return {
    id,
    name,
    config: extractConfig(configSource, ['id', 'name', 'label', 'accountId', 'accountName', 'config', 'settings']),
  }
}

function ensureAccounts(providerId: string, accounts: QuotaAccountSettings[]): QuotaAccountSettings[] {
  const normalized = accounts
    .map((account, index) => ({
      id: normalizeAccountId(account.id, index === 0 ? DEFAULT_ACCOUNT_ID : `${providerId}-${index + 1}`),
      name: sanitizeAccountName(account.name, index === 0 ? DEFAULT_ACCOUNT_NAME : `Account ${index + 1}`),
      config: account.config ?? {},
    }))
  return normalized.length > 0 ? normalized : [createFallbackAccount(providerId)]
}

function resolveDefaultAccountId(accounts: QuotaAccountSettings[], candidate?: string): string {
  if (candidate && accounts.some((account) => account.id === candidate)) {
    return candidate
  }
  return accounts[0]?.id || DEFAULT_ACCOUNT_ID
}

export function createQuotaAccount(providerId: string, existingAccounts: QuotaAccountSettings[]): QuotaAccountSettings {
  let index = existingAccounts.length + 1
  let id = `${providerId}-${index}`
  while (existingAccounts.some((account) => account.id === id)) {
    index += 1
    id = `${providerId}-${index}`
  }
  return {
    id,
    name: `Account ${index}`,
    config: {},
  }
}

export function normalizeQuotaSettingsPayload(raw: unknown): { mode: QuotaSettingsMode, providers: Record<string, QuotaProviderSettings> } {
  const providers: Record<string, QuotaProviderSettings> = {}
  let mode: QuotaSettingsMode = 'legacy'

  if (isRecord(raw)) {
    for (const [providerId, value] of Object.entries(raw)) {
      if (Array.isArray(value)) {
        const accounts = parseAccounts(value, providerId)
        if (accounts.length > 0) {
          mode = 'accounts'
          providers[providerId] = {
            accounts: ensureAccounts(providerId, accounts),
            defaultAccountId: accounts[0].id,
          }
        }
        continue
      }
      if (!isRecord(value)) continue

      let accountSource: unknown = value.accounts
      if (!accountSource && typeof value.accountsJson === 'string') {
        accountSource = safeJsonParse(value.accountsJson)
      }
      let accounts = parseAccounts(accountSource, providerId)
      if (accounts.length > 0) {
        mode = 'accounts'
      }

      const installed =
        value.installed === 'true' || value.installed === true
          ? true
          : value.installed === 'false' || value.installed === false
            ? false
            : undefined

      if (accounts.length === 0) {
        const fallbackName = sanitizeAccountName(asString(value.accountName), DEFAULT_ACCOUNT_NAME)
        accounts = [{
          id: DEFAULT_ACCOUNT_ID,
          name: fallbackName,
          config: extractConfig(value, ['installed', 'defaultAccountId', 'selectedAccountId', 'currentAccountId', 'accountName', 'accounts', 'accountsJson']),
        }]
      }

      const normalizedAccounts = ensureAccounts(providerId, accounts)
      const defaultAccountId = resolveDefaultAccountId(
        normalizedAccounts,
        asString(value.defaultAccountId || value.selectedAccountId || value.currentAccountId),
      )

      providers[providerId] = {
        installed,
        accounts: normalizedAccounts,
        defaultAccountId,
      }
    }
  }

  for (const providerId of QUOTA_PROVIDER_IDS) {
    if (!providers[providerId]) {
      providers[providerId] = {
        accounts: [createFallbackAccount(providerId)],
        defaultAccountId: DEFAULT_ACCOUNT_ID,
      }
    }
  }

  return { mode, providers }
}

export function serializeQuotaSettingsPayload(
  providers: Record<string, QuotaProviderSettings>,
  mode: QuotaSettingsMode,
): Record<string, unknown> {
  const payload: Record<string, unknown> = {}

  for (const providerId of QUOTA_PROVIDER_IDS) {
    const provider = providers[providerId]
    if (!provider) continue
    const accounts = ensureAccounts(providerId, provider.accounts).map((account) => ({
      id: account.id,
      name: sanitizeAccountName(account.name, DEFAULT_ACCOUNT_NAME),
      config: { ...account.config },
    }))

    if (mode === 'accounts') {
      payload[providerId] = {
        defaultAccountId: resolveDefaultAccountId(accounts, provider.defaultAccountId),
        accounts,
      }
      continue
    }

    const selectedAccount = accounts.find((account) => account.id === provider.defaultAccountId) || accounts[0]
    payload[providerId] = { ...selectedAccount.config }
  }

  return payload
}

export function getSelectedQuotaAccount(provider?: QuotaProviderSettings | null): QuotaAccountSettings {
  if (!provider) return createFallbackAccount(DEFAULT_ACCOUNT_ID)
  const accounts = ensureAccounts('default', provider.accounts)
  return accounts.find((account) => account.id === provider.defaultAccountId) || accounts[0]
}

function looksLikeQuotaResponse(value: unknown): value is QuotaResponseData {
  if (!isRecord(value)) return false
  return 'fiveHour' in value || 'weekly' in value || 'monthly' in value || 'groups' in value
}

function parseQuotaValue(value: unknown): QuotaValue | undefined {
  if (!isRecord(value)) return undefined
  return {
    used: asNumber(value.used),
    limit: asNumber(value.limit),
    unit: asString(value.unit) || undefined,
    resetTime: asString(value.resetTime) || undefined,
  }
}

function parseQuotaItem(value: unknown): QuotaItem | null {
  if (!isRecord(value)) return null
  return {
    name: asString(value.name) || 'item',
    label: asString(value.label) || asString(value.name) || 'Item',
    description: asString(value.description),
    used: asNumber(value.used),
    limit: asNumber(value.limit),
    unit: asString(value.unit) || undefined,
    resetTime: asString(value.resetTime) || undefined,
  }
}

function parseQuotaGroup(value: unknown): QuotaGroup | null {
  if (!isRecord(value)) return null
  const items = Array.isArray(value.items)
    ? value.items.map(parseQuotaItem).filter((item): item is QuotaItem => !!item)
    : []
  return {
    name: asString(value.name) || 'Group',
    description: asString(value.description),
    items,
  }
}

function parseQuotaResponse(value: unknown): QuotaResponseData | undefined {
  if (!looksLikeQuotaResponse(value)) return undefined
  return {
    fiveHour: parseQuotaValue(value.fiveHour),
    weekly: parseQuotaValue(value.weekly),
    monthly: parseQuotaValue(value.monthly),
    groups: Array.isArray(value.groups)
      ? value.groups.map(parseQuotaGroup).filter((group): group is QuotaGroup => !!group)
      : undefined,
  }
}

function parseQuotaAccount(raw: unknown, fallbackId: string, fallbackName: string): NormalizedQuotaAccount | null {
  if (!isRecord(raw)) return null
  const data = parseQuotaResponse(raw.data ?? raw.quota ?? raw.quotas ?? raw.response ?? raw)
  const id = normalizeAccountId(asString(raw.id || raw.accountId || fallbackId), fallbackId)
  const name = sanitizeAccountName(asString(raw.name || raw.label || raw.accountName), fallbackName)
  const error = asString(raw.error) || undefined

  if (!data && !error) {
    return null
  }

  return { id, name, data, error }
}

function parseQuotaAccountsContainer(raw: unknown): NormalizedQuotaAccount[] {
  if (Array.isArray(raw)) {
    return raw
      .map((entry, index) => parseQuotaAccount(entry, `${DEFAULT_ACCOUNT_ID}-${index + 1}`, `Account ${index + 1}`))
      .filter((entry): entry is NormalizedQuotaAccount => !!entry)
  }
  if (isRecord(raw)) {
    return Object.entries(raw)
      .map(([key, entry], index) => parseQuotaAccount(entry, key, `Account ${index + 1}`))
      .filter((entry): entry is NormalizedQuotaAccount => !!entry)
  }
  return []
}

export function normalizeProviderQuota(
  _providerId: string,
  raw: unknown,
  settings?: QuotaProviderSettings,
): NormalizedProviderQuota {
  if (!isRecord(raw)) {
    return { accounts: [] }
  }

  const nestedData = isRecord(raw.data) ? raw.data : undefined
  const accounts = [
    raw.accounts,
    raw.entries,
    raw.byAccount,
    nestedData?.accounts,
    nestedData?.entries,
    nestedData?.byAccount,
  ]
    .flatMap(parseQuotaAccountsContainer)

  const providerError = asString(raw.error) || undefined

  if (accounts.length > 0) {
    const settingsById = new Map((settings?.accounts || []).map((account) => [account.id, account]))
    const namedAccounts = accounts.map((account, index) => {
      const configured = settingsById.get(account.id)
      const fallbackName = configured?.name || (settings?.accounts.length === 1 ? settings.accounts[0].name : `Account ${index + 1}`)
      return {
        ...account,
        name: sanitizeAccountName(account.name, fallbackName || DEFAULT_ACCOUNT_NAME),
      }
    })
    return { error: providerError, accounts: namedAccounts }
  }

  const singleData = parseQuotaResponse(raw.data)
  if (!singleData) {
    return { error: providerError, accounts: [] }
  }

  const selectedAccount = getSelectedQuotaAccount(settings)
  return {
    error: providerError,
    accounts: [{
      id: selectedAccount.id,
      name: selectedAccount.name,
      data: singleData,
      synthetic: true,
    }],
  }
}

export function providerHasConfiguredAccount(
  providerId: QuotaProviderId,
  settings?: QuotaProviderSettings,
  quota?: NormalizedProviderQuota,
): boolean {
  if (quota?.accounts.some((account) => !!account.data)) {
    return true
  }
  if (AUTO_CONFIGURED_PROVIDERS.has(providerId)) {
    return settings?.installed !== false
  }
  if (!settings) return false

  return settings.accounts.some((account) => {
    const config = account.config || {}
    if (providerId === 'copilot') {
      return !!(config.token || config.accessToken || config.apiKey)
    }
    return REQUIRED_CONFIG_KEYS[providerId].every((key) => !!config[key])
  })
}

export function getQuotaMetricEntries(providerId: QuotaProviderId, data?: QuotaResponseData): Array<{ key: QuotaMetricKey, label: string, quota: QuotaValue }> {
  if (!data) return []
  const labels = QUOTA_WINDOW_LABELS[providerId]
  return (Object.entries(labels) as Array<[QuotaMetricKey, string]>)
    .map(([key, label]) => {
      const quota = data[key]
      if (!quota) return null
      return { key, label, quota }
    })
    .filter((entry): entry is { key: QuotaMetricKey, label: string, quota: QuotaValue } => !!entry)
}

export function serializeQuotaSelection(selection: QuotaSelection): string {
  return JSON.stringify(selection)
}

export function deserializeQuotaSelection(raw: string | null | undefined): QuotaSelection | null {
  if (!raw) return null
  if (raw.startsWith('{')) {
    try {
      const parsed = JSON.parse(raw) as QuotaSelection
      if (!parsed?.providerId || !parsed?.kind) return null
      return parsed
    } catch {
      return null
    }
  }

  const parts = raw.split(':')
  if (parts.length === 2) {
    return {
      providerId: parts[0],
      kind: 'window',
      metricKey: parts[1] as QuotaMetricKey,
    }
  }
  if (parts.length === 3) {
    return {
      providerId: parts[0],
      kind: 'groupItem',
      groupName: parts[1],
      itemName: parts[2],
    }
  }
  return null
}

export function formatQuotaValue(used: number, limit: number, unit?: string): { text: string, percentage: number } {
  if (unit === 'info') {
    return { text: '', percentage: 0 }
  }
  if (unit === 'percentage' || !unit) {
    return { text: `${used}%`, percentage: used }
  }
  const pct = limit > 0 ? Math.round((used / limit) * 100) : 0
  if (unit === 'currency') {
    return { text: `${used}$ / ${limit}$`, percentage: pct }
  }
  return { text: `${used}/${limit}`, percentage: pct }
}

export function formatResetTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (isNaN(then)) return iso
  const diff = then - Date.now()
  if (diff <= 0) return 'soon'
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  const minutes = Math.floor((diff % 3600000) / 60000)
  if (days >= 1) return `${days}d ${hours}h`
  if (hours >= 1) return `${hours}h ${minutes}m`
  return `${minutes}m`
}
