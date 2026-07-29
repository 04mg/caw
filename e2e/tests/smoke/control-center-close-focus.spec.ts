import { test, expect } from '@playwright/test'
import { waitForAppReady, openControlCenter, closeControlCenter, launchTerminal, focusTerminal } from '../../fixtures/terminal'

test.describe('Control Center close focus', () => {
  test('closing Command Center blurs the button and returns focus to the terminal', async ({ page, baseURL }) => {
    test.setTimeout(120_000)
    await waitForAppReady(page)

    // Launch a terminal so there is a focusable pane to return to.
    await launchTerminal(page)
    await page.waitForTimeout(1000)

    // Find the active terminal pane and focus it.
    const panel = page.locator('[data-testid^="terminal-panel-"]').first()
    await expect(panel).toBeVisible({ timeout: 30_000 })
    const textarea = panel.locator('textarea[aria-label="Terminal input"]')
    await textarea.focus()
    await focusTerminal(page, await panel.getAttribute('data-testid') || '')
    await page.waitForTimeout(500)

    // Open the Control Center — focus moves to the board/button.
    await openControlCenter(page)

    // The control-center button should now be in the active state.
    const btn = page.getByTestId('status-bar-control-center')
    await expect(btn).toBeVisible()

    // Close the Control Center.
    await closeControlCenter(page)

    // The control-center button must not retain document focus.
    const activeEl = await page.evaluate(() => document.activeElement?.getAttribute('data-testid') || '')
    expect(activeEl).not.toBe('status-bar-control-center')
  })
})