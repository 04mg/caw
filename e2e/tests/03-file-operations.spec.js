const { test, expect, request } = require('@playwright/test');

const apiCtx = async () => {
  return await request.newContext({ baseURL: 'http://localhost:8099' });
};

test.describe('File operations API', () => {
  let api;

  test.beforeAll(async () => { api = await apiCtx(); });
  test.afterAll(async () => { await api.dispose(); });

  test('GET /api/workspaces/details validates path', async () => {
    const res = await api.get('/api/workspaces/details?path=C:\\');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.path).toBeTruthy();
  });

  test('GET /api/workspaces/details with empty path returns error', async () => {
    const res = await api.get('/api/workspaces/details?path=');
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.error).toBeTruthy();
    expect(body.error.code).toBe('bad_request');
  });

  test('GET /api/workspaces/trees returns file tree', async () => {
    const res = await api.get('/api/workspaces/trees?path=C:\\');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toHaveProperty('name');
    expect(body.data).toHaveProperty('isDir', true);
    expect(body.data).toHaveProperty('children');
  });

  test('GET /api/workspaces/contents?dirs_only=true returns dirs', async () => {
    const res = await api.get('/api/workspaces/contents?dirs_only=true&path=C:\\');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.data)).toBeTruthy();
    for (const node of body.data) {
      expect(node.isDir).toBeTruthy();
    }
  });

  test('GET /api/workspaces/contents?path= returns all entries', async () => {
    const res = await api.get('/api/workspaces/contents?path=C:\\');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.data)).toBeTruthy();
  });

  test('GET /api/workspaces/directories returns dirs', async () => {
    const res = await api.get('/api/workspaces/directories?q=&root=C:\\');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.data)).toBeTruthy();
  });

  test('GET /api/workspaces/files?q=Windows returns search results', async () => {
    const res = await api.get('/api/workspaces/files?q=Windows&root=C:\\');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.data)).toBeTruthy();
  });

  test('GET /api/workspaces/files?path= reads file as JSON', async () => {
    const res = await api.get('/api/workspaces/files?path=C:\\Windows\\win.ini');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data).toHaveProperty('content');
    expect(typeof body.data.content).toBe('string');
  });

  test('PUT /api/workspaces/files writes and returns ok', async () => {
    const tmpPath = process.env.TEMP + '\\caw_e2e_test.txt';
    const res = await api.put('/api/workspaces/files', {
      data: { path: tmpPath, content: 'hello e2e' },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.status).toBe('ok');
    const readRes = await api.get('/api/workspaces/files?path=' + encodeURIComponent(tmpPath));
    const readBody = await readRes.json();
    expect(readBody.data.content).toBe('hello e2e');
  });

  test('PATCH /api/workspaces/files renames', async () => {
    const tmpPath = process.env.TEMP + '\\caw_e2e_rename.txt';
    const tmpPath2 = process.env.TEMP + '\\caw_e2e_renamed.txt';
    await api.put('/api/workspaces/files', { data: { path: tmpPath, content: 'rename me' } });
    const res = await api.patch('/api/workspaces/files', {
      data: { oldPath: tmpPath, newPath: tmpPath2 },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.status).toBe('ok');
  });

  test('POST /api/workspaces/files creates file', async () => {
    const tmpPath = process.env.TEMP + '\\caw_e2e_create.txt';
    const res = await api.post('/api/workspaces/files', {
      data: { path: tmpPath, type: 'file' },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.status).toBe('ok');
  });

  test('POST /api/workspaces/files creates directory', async () => {
    const tmpPath = process.env.TEMP + '\\caw_e2e_dir';
    const res = await api.post('/api/workspaces/files', {
      data: { path: tmpPath, type: 'dir' },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.status).toBe('ok');
  });

  test('POST /api/workspaces/files copies file', async () => {
    const srcPath = process.env.TEMP + '\\caw_e2e_copy_src.txt';
    const destPath = process.env.TEMP + '\\caw_e2e_copy_dest.txt';
    await api.put('/api/workspaces/files', { data: { path: srcPath, content: 'copy me' } });
    const res = await api.post('/api/workspaces/files', {
      data: { sourcePath: srcPath, destPath },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.status).toBe('ok');
  });

  test('DELETE /api/workspaces/files deletes file', async () => {
    const tmpPath = process.env.TEMP + '\\caw_e2e_delete.txt';
    await api.put('/api/workspaces/files', { data: { path: tmpPath, content: 'delete me' } });
    const res = await api.delete('/api/workspaces/files?path=' + encodeURIComponent(tmpPath));
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.status).toBe('ok');
  });

  test('POST /api/workspaces/history undo/redo', async () => {
    const tmpPath = process.env.TEMP + '\\caw_e2e_history.txt';
    await api.post('/api/workspaces/files', { data: { path: tmpPath, type: 'file' } });
    const undoRes = await api.post('/api/workspaces/history', { data: { action: 'undo' } });
    expect(undoRes.ok()).toBeTruthy();
    const undoBody = await undoRes.json();
    expect(undoBody.data.status).toBe('ok');
    const redoRes = await api.post('/api/workspaces/history', { data: { action: 'redo' } });
    expect(redoRes.ok()).toBeTruthy();
  });

  test('POST /api/workspaces/history invalid action returns error', async () => {
    const res = await api.post('/api/workspaces/history', { data: { action: 'invalid' } });
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.error).toBeTruthy();
  });
});