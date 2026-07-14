import { test, expect } from '@playwright/test'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import { setWorkspace, waitForAppReady } from '../../fixtures/terminal'

test.describe('Workspace flow', () => {
  let workspace: string

  test.beforeEach(() => {
    workspace = createTempGitRepo()
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('sets a workspace via API and sees workspace name in status bar', async ({ page, baseURL }) => {
    test.setTimeout(120_000)
    await setWorkspace(baseURL!, workspace)
    await waitForAppReady(page)
    const dirName = workspace.split(/[\\/]/).pop() || ''
    await expect(page.getByText(dirName, { exact: false }).first()).toBeVisible({ timeout: 10_000 })
  })

  test('open workspace picker with Alt+W', async ({ page }) => {
    await waitForAppReady(page)
    await page.keyboard.press('Alt+W')
    await page.waitForTimeout(500)
  })
})