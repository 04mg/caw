import { test, expect, type Page } from '@playwright/test'
import { waitForAppReady, setWorkspace } from '../../fixtures/terminal'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'

test.describe('Settings dialog', () => {
  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('opens and shows all section buttons', async ({ page }) => {
    await waitForAppReady(page)
    await openSettings(page)
    for (const section of ['appearance', 'notifications', 'workspaces', 'terminal', 'agents', 'limits']) {
      await expect(page.getByTestId(`settings-section-${section}`)).toBeVisible()
    }
  })

  test('switches to Limits section', async ({ page }) => {
    await waitForAppReady(page)
    await openSettings(page)
    await page.getByTestId('settings-section-limits').click()
    await expect(page.getByText('Select a provider to configure')).toBeVisible()
  })

  test('switches to Agents section', async ({ page }) => {
    await waitForAppReady(page)
    await openSettings(page)
    await page.getByTestId('settings-section-agents').click()
    await expect(page.getByText('Configure the command each agent runs')).toBeVisible()
  })

  test('closes via Escape', async ({ page }) => {
    await waitForAppReady(page)
    await openSettings(page)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('settings-dialog')).toBeHidden()
  })
})

async function openSettings(page: Page): Promise<void> {
  await page.getByTestId('settings-open-button').click()
  await expect(page.getByTestId('settings-dialog')).toBeVisible({ timeout: 10_000 })
}