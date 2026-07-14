import { test, expect } from '@playwright/test'
import {
  QUOTA_PROVIDER_IDS,
  getInstalledQuotaProviders,
  isQuotaProviderInstalled,
} from '../../fixtures/quota'

test.describe('Quota API', () => {
  test('GET /api/quotas returns 200', async ({ baseURL }) => {
    const res = await fetch(`${baseURL}/api/quotas`)
    expect(res.ok).toBeTruthy()
    const json = await res.json()
    expect(json.data).toBeDefined()
  })

  test('GET /api/quotas/settings returns 200', async ({ baseURL }) => {
    const res = await fetch(`${baseURL}/api/quotas/settings`)
    expect(res.ok).toBeTruthy()
    const json = await res.json()
    expect(json.data).toBeDefined()
  })

  for (const provider of QUOTA_PROVIDER_IDS) {
    test.describe(`${provider} (gated)`, () => {
      test.skip(!isQuotaProviderInstalled(provider), `${provider} not installed/configured`)

      test('returns quota data or error (not 500)', async ({ baseURL }) => {
        const res = await fetch(`${baseURL}/api/quotas`)
        expect(res.ok).toBeTruthy()
        const json = await res.json()
        const entry = json.data?.[provider]
        expect(entry).toBeDefined()
        expect(entry.data !== undefined || entry.error !== undefined).toBeTruthy()
      })

      test('settings reports installed=true', async ({ baseURL }) => {
        const res = await fetch(`${baseURL}/api/quotas/settings`)
        const json = await res.json()
        expect(json.data?.[provider]?.installed).toBe('true')
      })
    })
  }

  test('non-installed providers are absent from quotas response', async ({ baseURL }) => {
    const installed = new Set(getInstalledQuotaProviders())
    const res = await fetch(`${baseURL}/api/quotas`)
    const json = await res.json()
    for (const provider of QUOTA_PROVIDER_IDS) {
      if (!installed.has(provider)) {
        expect(json.data?.[provider]?.data).toBeUndefined()
      }
    }
  })
})