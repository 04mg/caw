import { test, expect } from '@playwright/test'
import {
  QUOTA_PROVIDER_IDS,
  getInstalledQuotaProviders,
  isQuotaProviderInstalled,
  getQuotaSettings,
} from '../../fixtures/quota'
import { waitForAppReady, setWorkspace } from '../../fixtures/terminal'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'

const REQUIRES_CREDENTIALS = new Set(['copilot', 'opencode', 'ollama', 'openrouter'])

async function getVisibleQuotaProviders(baseURL: string): Promise<string[]> {
  const settings = await getQuotaSettings(baseURL)
  const visible: string[] = []
  for (const provider of QUOTA_PROVIDER_IDS) {
    const cfg = settings[provider]
    if (!cfg) continue
    if (cfg.installed !== 'true' && !REQUIRES_CREDENTIALS.has(provider)) continue
    if (provider === 'copilot' && !cfg.token) continue
    if (provider === 'opencode' && !(cfg.cookie && cfg.workspaceId)) continue
    if (provider === 'ollama' && !cfg.cookie) continue
    if (provider === 'openrouter' && !cfg.apiKey) continue
    visible.push(provider)
  }
  return visible
}

test.describe('Quota UI', () => {
  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('quota trigger visible in status bar', async ({ page }) => {
    await waitForAppReady(page)
    await expect(page.getByTestId('status-bar-quota-trigger')).toBeVisible()
  })

  test('opening quota dropdown shows configured providers', async ({ page, baseURL }) => {
    await waitForAppReady(page)
    await page.getByTestId('status-bar-quota-trigger').click()
    const visible = await getVisibleQuotaProviders(baseURL!)

    if (visible.length === 0) {
      await expect(page.getByText('No providers configured')).toBeVisible()
      return
    }

    for (const provider of visible) {
      await expect(page.getByTestId(`quota-row-${provider}`)).toBeVisible({ timeout: 15_000 })
    }
  })

  test('non-installed providers not shown in dropdown', async ({ page }) => {
    await waitForAppReady(page)
    await page.getByTestId('status-bar-quota-trigger').click()
    const installed = new Set(getInstalledQuotaProviders())

    for (const provider of QUOTA_PROVIDER_IDS) {
      if (!installed.has(provider) && !REQUIRES_CREDENTIALS.has(provider)) {
        await expect(page.getByTestId(`quota-row-${provider}`)).toBeHidden()
      }
    }
  })

  test('refresh button triggers refetch', async ({ page }) => {
    await waitForAppReady(page)
    await page.getByTestId('status-bar-quota-trigger').click()
    await page.getByTestId('status-bar-quota-refresh').click()
  })

  test('Configure Providers button opens settings at Limits section', async ({ page }) => {
    await waitForAppReady(page)
    await page.getByTestId('status-bar-quota-trigger').click()
    await page.getByTestId('quota-configure-providers').click()
    await expect(page.getByTestId('settings-dialog')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('Select a provider to configure')).toBeVisible()
  })

  test('limits section shows all installed provider items', async ({ page }) => {
    await waitForAppReady(page)
    await page.getByTestId('settings-open-button').click()
    await expect(page.getByTestId('settings-dialog')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('settings-section-limits').click()

    const installed = getInstalledQuotaProviders()
    for (const provider of installed) {
      await expect(page.getByTestId(`settings-provider-${provider}`)).toBeVisible()
    }
  })

  for (const provider of QUOTA_PROVIDER_IDS) {
    test.describe(`${provider} provider UI (gated)`, () => {
      test.skip(!isQuotaProviderInstalled(provider), `${provider} not installed/configured`)

      test('provider item visible in settings limits section', async ({ page }) => {
        await waitForAppReady(page)
        await page.getByTestId('settings-open-button').click()
        await expect(page.getByTestId('settings-dialog')).toBeVisible({ timeout: 10_000 })
        await page.getByTestId('settings-section-limits').click()
        await expect(page.getByTestId(`settings-provider-${provider}`)).toBeVisible()
      })

      test('provider row visible in quota dropdown when configured', async ({ page, baseURL }) => {
        const visible = await getVisibleQuotaProviders(baseURL!)
        test.skip(!visible.includes(provider), `${provider} not configured with credentials`)
        await waitForAppReady(page)
        await page.getByTestId('status-bar-quota-trigger').click()
        await expect(page.getByTestId(`quota-row-${provider}`)).toBeVisible({ timeout: 15_000 })
      })
    })
  }
})