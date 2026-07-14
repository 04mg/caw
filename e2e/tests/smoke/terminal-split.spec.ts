import { test, expect } from '@playwright/test'
import { launchTerminal, waitForAppReady, setWorkspace } from '../../fixtures/terminal'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'

test.describe('Terminal split and close', () => {
  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('launches a terminal pane', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    const panels = page.locator('[data-testid^="terminal-panel-"]')
    await expect(panels.first()).toBeVisible({ timeout: 15_000 })
  })

  test('splits horizontally with Alt+H', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })
    await page.keyboard.press('Alt+H')
    await expect(page.locator('[data-testid^="terminal-panel-"]')).toHaveCount(2, { timeout: 10_000 })
  })

  test('splits vertically with Alt+V', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })
    await page.keyboard.press('Alt+V')
    await expect(page.locator('[data-testid^="terminal-panel-"]')).toHaveCount(2, { timeout: 10_000 })
  })

  test('closes a pane with Alt+C', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })
    await page.keyboard.press('Alt+H')
    await expect(page.locator('[data-testid^="terminal-panel-"]')).toHaveCount(2, { timeout: 10_000 })
    await page.keyboard.press('Alt+C')
    await expect(page.locator('[data-testid^="terminal-panel-"]')).toHaveCount(1, { timeout: 10_000 })
  })
})