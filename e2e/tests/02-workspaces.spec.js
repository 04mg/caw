const { test, expect } = require('@playwright/test');

test.describe('Workspace management', () => {
  test('app loads and shows workspace sidebar', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByText('Workspaces', { exact: true })).toBeVisible();
    await expect(page.getByText('No workspaces.')).toBeVisible();
  });

  test('status bar shows quota button', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByText('Select Limit')).toBeVisible({ timeout: 15000 });
  });

  test('quota dropdown shows provider limits', async ({ page }) => {
    await page.goto('/');
    await page.getByText('Select Limit').click();
    await expect(page.getByText('Usage limits')).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: 'Refresh Limits' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Configure Providers...')).toBeVisible({ timeout: 10000 });
  });

  test('add workspace dialog opens with directory picker', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Add workspace' }).click();
    await expect(page.getByRole('heading', { name: 'Create workspace' })).toBeVisible();
    await expect(page.getByText('Search for a directory by name.')).toBeVisible();
    await expect(page.getByPlaceholder('Search directory...')).toBeVisible();
  });

  test('directory picker shows root folders', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Add workspace' }).click();
    await page.waitForTimeout(1000);
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    const buttons = dialog.locator('button');
    const count = await buttons.count();
    expect(count).toBeGreaterThan(3);
  });

  test('directory picker navigates into subdirectories', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Add workspace' }).click();
    await page.waitForTimeout(1000);
    const dialog = page.getByRole('dialog');
    const firstDir = dialog.locator('button').filter({ hasNot: page.getByText('/') }).first();
    await firstDir.click();
    await page.waitForTimeout(500);
    const pathDisplay = dialog.locator('text=/^[A-Z]:\\\\/');
    await expect(pathDisplay).toBeVisible({ timeout: 5000 });
  });

  test('cancel closes add workspace dialog', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Add workspace' }).click();
    await expect(page.getByRole('heading', { name: 'Create workspace' })).toBeVisible();
    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('heading', { name: 'Create workspace' })).not.toBeVisible();
  });
});