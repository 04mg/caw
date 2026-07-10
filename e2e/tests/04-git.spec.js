const { test, expect, request } = require('@playwright/test');

const apiCtx = async () => await request.newContext({ baseURL: 'http://localhost:8099' });

test.describe('Git API', () => {
  let api;
  test.beforeAll(async () => { api = await apiCtx(); });
  test.afterAll(async () => { await api.dispose(); });

  test('GET /api/git/statuses returns map', async () => {
    const res = await api.get('/api/git/statuses?path=' + encodeURIComponent(process.cwd()));
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toBeTruthy();
    expect(typeof body.data).toBe('object');
  });

  test('GET /api/git/statuses empty path returns error', async () => {
    const res = await api.get('/api/git/statuses?path=');
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.error).toBeTruthy();
  });

  test('GET /api/git/diffs returns content envelope', async () => {
    const res = await api.get('/api/git/diffs?path=' + encodeURIComponent(process.cwd()));
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toHaveProperty('content');
    expect(typeof body.data.content).toBe('string');
  });

  test('GET /api/git/diffs empty path returns error', async () => {
    const res = await api.get('/api/git/diffs?path=');
    expect(res.status()).toBe(400);
  });

  test('GET /api/git/originals returns content envelope', async () => {
    const res = await api.get('/api/git/originals?path=' + encodeURIComponent(process.cwd() + '\\README.md'));
    if (res.ok()) {
      const body = await res.json();
      expect(body.data).toHaveProperty('content');
      expect(typeof body.data.content).toBe('string');
    }
  });

  test('GET /api/git/originals empty path returns error', async () => {
    const res = await api.get('/api/git/originals?path=');
    expect(res.status()).toBe(400);
  });
});