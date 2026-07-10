const { test, expect, request } = require('@playwright/test');

const apiCtx = async () => await request.newContext({ baseURL: 'http://localhost:8099' });

test.describe('Terminal API', () => {
  let api;
  test.beforeAll(async () => { api = await apiCtx(); });
  test.afterAll(async () => { await api.dispose(); });

  test('POST /api/terminals creates a terminal', async () => {
    const res = await api.post('/api/terminals', {
      data: { cwd: process.cwd(), id: '', cmd: [] },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toHaveProperty('id');
    expect(typeof body.data.id).toBe('string');
  });

  test('DELETE /api/terminals/:id deletes terminal', async () => {
    const createRes = await api.post('/api/terminals', {
      data: { cwd: process.cwd(), id: '', cmd: [] },
    });
    const createBody = await createRes.json();
    const id = createBody.data.id;
    const delRes = await api.delete('/api/terminals/' + id);
    expect(delRes.ok()).toBeTruthy();
  });

  test('DELETE /api/terminals/:id non-existent returns 404', async () => {
    const res = await api.delete('/api/terminals/nonexistent-id-123');
    expect(res.status()).toBe(404);
    const body = await res.json();
    expect(body.error).toBeTruthy();
  });
});