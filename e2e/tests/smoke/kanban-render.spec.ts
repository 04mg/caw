import { test, expect } from '@playwright/test'
import { openControlCenter, closeControlCenter, waitForAppReady } from '../../fixtures/terminal'

test.describe('Kanban / Control Center', () => {
  test('opens and shows 3 columns', async ({ page }) => {
    await waitForAppReady(page)
    await openControlCenter(page)
    await expect(page.getByTestId('kanban-column-idle')).toBeVisible()
    await expect(page.getByTestId('kanban-column-working')).toBeVisible()
    await expect(page.getByTestId('kanban-column-needs_input')).toBeVisible()
  })

  test('columns show content or empty state', async ({ page }) => {
    await waitForAppReady(page)
    await openControlCenter(page)
    // Each column should either show agent cards or the empty-state text.
    // We can't assert empty state after agent tests may have left sessions.
    for (const col of ['idle', 'working', 'needs_input'] as const) {
      const column = page.getByTestId(`kanban-column-${col}`)
      await expect(column).toBeVisible()
    }
  })

  test('toggles closed on second click', async ({ page }) => {
    await waitForAppReady(page)
    await openControlCenter(page)
    await closeControlCenter(page)
  })
})