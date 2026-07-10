const { test, expect } = require('@playwright/test');

test.describe('API envelope format', () => {
  test('GET /api/workspaces returns {data} envelope', async ({ request }) => {
    const res = await request.get('/api/workspaces');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('data');
    expect(body.data).toHaveProperty('workspaces');
    expect(Array.isArray(body.data.workspaces)).toBeTruthy();
    expect(body.data).toHaveProperty('activeWorkspaceId');
  });

  test('GET /api/quotas returns {data} envelope', async ({ request }) => {
    const res = await request.get('/api/quotas');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('data');
    expect(typeof body.data).toBe('object');
  });

  test('GET /api/quotas/settings returns {data} envelope', async ({ request }) => {
    const res = await request.get('/api/quotas/settings');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('data');
    expect(typeof body.data).toBe('object');
  });

  test('GET /api/agents returns {data} array envelope', async ({ request }) => {
    const res = await request.get('/api/agents');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('data');
    expect(Array.isArray(body.data)).toBeTruthy();
  });

  test('GET /api/agents/statuses returns {data} array envelope', async ({ request }) => {
    const res = await request.get('/api/agents/statuses');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('data');
    expect(Array.isArray(body.data)).toBeTruthy();
  });

  test('GET /api/git/diffs empty path returns {error} envelope', async ({ request }) => {
    const res = await request.get('/api/git/diffs?path=');
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body).toHaveProperty('error');
    expect(body.error).toHaveProperty('code');
    expect(body.error).toHaveProperty('message');
  });
});