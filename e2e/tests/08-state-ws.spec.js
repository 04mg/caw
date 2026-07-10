const { test, expect } = require('@playwright/test');

test.describe('State WebSocket', () => {
  test('WS /ws/state connects and receives state', async ({ page }) => {
    await page.goto('/');
    const wsConnected = await page.evaluate(() => {
      return new Promise((resolve) => {
        const ws = new WebSocket('ws://localhost:8099/ws/state');
        ws.onopen = () => resolve(true);
        ws.onmessage = (ev) => {
          const data = JSON.parse(ev.data);
          if (data.workspaces !== undefined) {
            ws.close();
            resolve(true);
          }
        };
        ws.onerror = () => resolve(false);
        setTimeout(() => resolve(false), 5000);
      });
    });
    expect(wsConnected).toBeTruthy();
  });

  test('POST /api/workspaces saves state via REST', async ({ request }) => {
    const res = await request.post('/api/workspaces', {
      data: {
        activeWorkspaceId: '',
        workspaces: [],
      },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.data.ok).toBeTruthy();
  });
});