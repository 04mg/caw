const { test, expect, request } = require('@playwright/test');

const apiCtx = async () => await request.newContext({ baseURL: 'http://localhost:8099' });

test.describe('Agent API', () => {
  let api;
  test.beforeAll(async () => { api = await apiCtx(); });
  test.afterAll(async () => { await api.dispose(); });

  test('GET /api/agents returns available agents', async () => {
    const res = await api.get('/api/agents');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.data)).toBeTruthy();
  });

  test('POST /api/agents with empty projectPath returns error', async () => {
    const res = await api.post('/api/agents', { data: { projectPath: '', enableWorktrees: false } });
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.error).toBeTruthy();
  });

  test('POST /api/agents without worktrees returns worktreePath', async () => {
    const res = await api.post('/api/agents', {
      data: { projectPath: process.cwd(), enableWorktrees: false },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toHaveProperty('isGit');
    expect(body.data).toHaveProperty('worktreePath');
  });

  test('GET /api/agents/changes with empty path returns error', async () => {
    const res = await api.get('/api/agents/changes?worktreePath=&branchName=&baseBranch=');
    expect(res.status()).toBe(400);
  });

  test('GET /api/agents/statuses returns array', async () => {
    const res = await api.get('/api/agents/statuses');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.data)).toBeTruthy();
  });
});