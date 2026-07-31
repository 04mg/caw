import { test, expect } from '@playwright/test'
import { openCommandPalette, launchTerminal, waitForAppReady, setWorkspace } from '../../fixtures/terminal'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'

test.describe('Command Palette', () => {
  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })
  test('opens with Alt+P and shows search input', async ({ page }) => {
    await waitForAppReady(page)
    await openCommandPalette(page)
    await expect(page.getByTestId('command-palette-input')).toBeVisible()
  })

  test('opens with Alt+Shift+P and pre-fills command prompt', async ({ page }) => {
    await waitForAppReady(page)
    await page.keyboard.press('Alt+Shift+P')
    await expect(page.getByTestId('command-palette-input')).toBeVisible()
    await expect(page.getByTestId('command-palette-input')).toHaveValue('>')
    await expect(page.getByTestId('command-palette-item-new-terminal')).toBeVisible()
    await expect(page.getByTestId('command-palette-item-new-workspace')).toBeVisible()
    await page.getByTestId('command-palette-input').fill('> workspace')
    await expect(page.getByTestId('command-palette-item-new-workspace')).toBeVisible()
    await expect(page.getByTestId('command-palette-item-new-terminal')).toBeHidden()
  })

  test('lists built-in commands when empty', async ({ page }) => {
    await waitForAppReady(page)
    await openCommandPalette(page)
    await expect(page.getByTestId('command-palette-item-new-terminal')).toBeVisible()
    await expect(page.getByTestId('command-palette-item-new-workspace')).toBeVisible()
  })

  test('filters items by query', async ({ page }) => {
    await waitForAppReady(page)
    await openCommandPalette(page)
    const input = page.getByTestId('command-palette-input')
    await input.fill('terminal')
    await expect(page.getByTestId('command-palette-item-new-terminal')).toBeVisible()
    await expect(page.getByTestId('command-palette-item-new-workspace')).toBeHidden()
  })

  test('shows no results for gibberish query', async ({ page }) => {
    await waitForAppReady(page)
    await openCommandPalette(page)
    await page.getByTestId('command-palette-input').fill('zzzzzzzznotreal')
    await expect(page.getByText('No results')).toBeVisible()
  })

  test('closes on Escape', async ({ page }) => {
    await waitForAppReady(page)
    await openCommandPalette(page)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('command-palette-input')).toBeHidden()
  })

  test('launches a new terminal', async ({ page }) => {
    await waitForAppReady(page)
    await launchTerminal(page)
    const panels = page.locator('[data-testid^="terminal-panel-"]')
    await expect(panels.first()).toBeVisible({ timeout: 15_000 })
  })
})