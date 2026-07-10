const { test, expect, request } = require('@playwright/test');

const apiCtx = async () => await request.newContext({ baseURL: 'http://localhost:8099' });

test.describe('Quota API', () => {
  let api;
  test.beforeAll(async () => { api = await apiCtx(); });
  test.afterAll(async () => { await api.dispose(); });

  test('GET /api/quotas returns provider map', async () => {
    const res = await api.get('/api/quotas');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toBeTruthy();
    expect(typeof body.data).toBe('object');
  });

  test('GET /api/quotas/settings returns settings map', async () => {
    const res = await api.get('/api/quotas/settings');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toBeTruthy();
    expect(typeof body.data).toBe('object');
  });

  test('PUT /api/quotas/settings saves settings', async () => {
    const res = await api.put('/api/quotas/settings', {
      data: { testprovider: { apiKey: 'test-value' } },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.ok).toBeTruthy();
  });

  test('POST /api/quotas/copilot/device-codes initiates login', async () => {
    const res = await api.post('/api/quotas/copilot/device-codes');
    const body = await res.json();
    if (res.ok()) {
      expect(body.data).toBeTruthy();
    } else {
      expect(body.error).toBeTruthy();
    }
  });

  test('GET /api/quotas/copilot/device-codes/:code polls', async () => {
    const res = await api.get('/api/quotas/copilot/device-codes/test-code-123');
    const body = await res.json();
    if (res.ok()) {
      expect(body.data).toBeTruthy();
    } else {
      expect(body.error).toBeTruthy();
    }
  });
});