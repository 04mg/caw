import { test, expect } from '@playwright/test'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import { setWorkspace, waitForAppReady } from '../../fixtures/terminal'
import * as fs from 'fs'
import * as path from 'path'

test.describe('File Explorer', () => {
  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('shows rename conflict dialog when renaming to an existing file name', async ({ page }) => {
    test.setTimeout(60_000)
    await waitForAppReady(page)

    // Open file explorer sidebar if collapsed using the "Workspace Files" title
    const openSidebarButton = page.locator('button[title="Workspace Files"]').first()
    if (await openSidebarButton.isVisible()) {
      await openSidebarButton.click()
    }

    // Create file a.txt and b.txt in workspace
    fs.writeFileSync(path.join(workspace, 'a.txt'), 'content a')
    fs.writeFileSync(path.join(workspace, 'b.txt'), 'content b')

    // Wait for the files to render in explorer sidebar
    const fileA = page.getByText('a.txt', { exact: true }).first()
    const fileB = page.getByText('b.txt', { exact: true }).first()
    await expect(fileA).toBeVisible({ timeout: 15_000 })
    await expect(fileB).toBeVisible({ timeout: 15_000 })

    // Right-click a.txt to open context menu and click Rename
    await fileA.click({ button: 'right' })
    const renameBtn = page.getByRole('button', { name: 'Rename' })
    await expect(renameBtn).toBeVisible()
    await renameBtn.click()

    // Type "b.txt" in editing input and press Enter
    // We target the input inside the active container or using explorer-sidebar input selector
    const input = page.locator('.explorer-sidebar input').first()
    await expect(input).toBeVisible()
    await input.fill('b.txt')
    await input.press('Enter')

    // Verify Rename Conflict Dialog is shown
    const dialog = page.locator('div[role="dialog"]')
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('Rename Conflict')).toBeVisible()
    await expect(dialog.getByText('b.txt')).toBeVisible()

    // Clicking "Replace" should perform rename and overwrite b.txt
    const replaceBtn = dialog.getByRole('button', { name: 'Replace' })
    await expect(replaceBtn).toBeVisible()
    await replaceBtn.click()

    // Dialog should close
    await expect(dialog).toBeHidden()

    // a.txt should be gone and only b.txt should exist
    await expect(page.getByText('a.txt', { exact: true })).toBeHidden()
    await expect(page.getByText('b.txt', { exact: true })).toBeVisible()
  })

  test('cancels rename conflict dialog and leaves original files intact', async ({ page }) => {
    test.setTimeout(60_000)
    await waitForAppReady(page)

    // Open file explorer sidebar
    const openSidebarButton = page.locator('button[title="Workspace Files"]').first()
    if (await openSidebarButton.isVisible()) {
      await openSidebarButton.click()
    }

    // Create file a.txt and b.txt in workspace
    fs.writeFileSync(path.join(workspace, 'a.txt'), 'content a')
    fs.writeFileSync(path.join(workspace, 'b.txt'), 'content b')

    // Wait for the files to render in explorer
    const fileA = page.getByText('a.txt', { exact: true }).first()
    await expect(fileA).toBeVisible({ timeout: 15_000 })

    // Rename a.txt -> b.txt
    await fileA.click({ button: 'right' })
    await page.getByRole('button', { name: 'Rename' }).click()

    const input = page.locator('.explorer-sidebar input').first()
    await expect(input).toBeVisible()
    await input.fill('b.txt')
    await input.press('Enter')

    // Dialog shows
    const dialog = page.locator('div[role="dialog"]')
    await expect(dialog).toBeVisible()

    // Click Cancel
    await dialog.getByRole('button', { name: 'Cancel' }).click()

    // Dialog should close
    await expect(dialog).toBeHidden()

    // Both a.txt and b.txt must still exist
    await expect(page.getByText('a.txt', { exact: true })).toBeVisible()
    await expect(page.getByText('b.txt', { exact: true })).toBeVisible()
  })
})
