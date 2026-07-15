import { test, expect } from '@playwright/test'
import { launchTerminal, waitForAppReady, setWorkspace } from '../../fixtures/terminal'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'

test.describe('Pane keyboard navigation', () => {
  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('Alt+ArrowLeft/Right cycles focus through side-by-side panes (Alt+H)', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })

    // Horizontal split = side-by-side (left + right)
    await page.keyboard.press('Alt+H')
    await expect(page.locator('[data-pane-id]')).toHaveCount(2, { timeout: 10_000 })

    const panes = page.locator('[data-pane-id]')
    const leftPane = panes.nth(0)
    const rightPane = panes.nth(1)

    // After split, the new (right) pane becomes active
    await expect(rightPane).toHaveAttribute('data-active', 'true')

    // Move focus left to the left pane
    await page.keyboard.press('Alt+ArrowLeft')
    await expect(leftPane).toHaveAttribute('data-active', 'true')

    // Move focus back right
    await page.keyboard.press('Alt+ArrowRight')
    await expect(rightPane).toHaveAttribute('data-active', 'true')
  })

  test('Alt+ArrowLeft/Right cycles focus through stacked panes (Alt+V)', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })

    // Vertical split = stacked (top + bottom)
    await page.keyboard.press('Alt+V')
    await expect(page.locator('[data-pane-id]')).toHaveCount(2, { timeout: 10_000 })

    const panes = page.locator('[data-pane-id]')
    const topPane = panes.nth(0)
    const bottomPane = panes.nth(1)

    // After split, the new (bottom) pane becomes active
    await expect(bottomPane).toHaveAttribute('data-active', 'true')

    // Alt+ArrowLeft cycles to the top pane
    await page.keyboard.press('Alt+ArrowLeft')
    await expect(topPane).toHaveAttribute('data-active', 'true')

    // Alt+ArrowRight cycles back to the bottom pane
    await page.keyboard.press('Alt+ArrowRight')
    await expect(bottomPane).toHaveAttribute('data-active', 'true')
  })

  test('Alt+ArrowLeft/Right wrap around with 2 panes', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })

    await page.keyboard.press('Alt+H')
    await expect(page.locator('[data-pane-id]')).toHaveCount(2, { timeout: 10_000 })

    const panes = page.locator('[data-pane-id]')
    const firstPane = panes.nth(0)
    const secondPane = panes.nth(1)

    // Active is the second (new) pane after split
    await expect(secondPane).toHaveAttribute('data-active', 'true')

    // Wrap forward: Alt+ArrowRight on last pane → first pane
    await page.keyboard.press('Alt+ArrowRight')
    await expect(firstPane).toHaveAttribute('data-active', 'true')

    // Wrap backward: Alt+ArrowLeft on first pane → last pane
    await page.keyboard.press('Alt+ArrowLeft')
    await expect(secondPane).toHaveAttribute('data-active', 'true')
  })

  test('Alt+ArrowLeft/Right do nothing on a single pane', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })

    const pane = page.locator('[data-pane-id]').first()
    await expect(pane).toHaveAttribute('data-active', 'true')

    await page.keyboard.press('Alt+ArrowLeft')
    await expect(pane).toHaveAttribute('data-active', 'true')

    await page.keyboard.press('Alt+ArrowRight')
    await expect(pane).toHaveAttribute('data-active', 'true')
  })

  test('Alt+W closes the active pane', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    await expect(page.locator('[data-testid^="terminal-panel-"]').first()).toBeVisible({ timeout: 15_000 })

    // Split horizontally (side-by-side)
    await page.keyboard.press('Alt+H')
    await expect(page.locator('[data-pane-id]')).toHaveCount(2, { timeout: 10_000 })

    // Close active pane with Alt+W
    await page.keyboard.press('Alt+W')
    await expect(page.locator('[data-pane-id]')).toHaveCount(1, { timeout: 10_000 })
  })
})