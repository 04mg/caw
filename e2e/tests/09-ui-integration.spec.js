const { test, expect } = require('@playwright/test');

test.describe('UI integration', () => {
  test('settings dialog opens', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Settings' }).click();
    await expect(page.getByRole('heading', { name: /settings/i })).toBeVisible({ timeout: 10000 });
  });

  test('command palette opens with Alt+P', async ({ page }) => {
    await page.goto('/');
    await page.keyboard.press('Alt+P');
    await expect(page.getByRole('dialog')).toBeVisible({ timeout: 5000 });
  });

  test('hotkeys section is visible', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByText('Alt+W')).toBeVisible();
    await expect(page.getByText('Alt+T')).toBeVisible();
    await expect(page.getByText('Alt+H')).toBeVisible();
    await expect(page.getByText('Alt+V')).toBeVisible();
    await expect(page.getByText('Alt+P')).toBeVisible();
  });

  test('footer shows Ready status', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByText('Ready')).toBeVisible();
  });
});