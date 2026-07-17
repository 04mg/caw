import { test, expect } from '@playwright/test'
import { waitForAppReady, openControlCenter, getAgentStatuses, getExistingSessionIds, waitForAgentSession, launchAgent, waitForCardInColumn } from '../../fixtures/terminal'
import { isAgentAvailable } from '../../fixtures/agents'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import { setWorkspace } from '../../fixtures/terminal'

test.describe('WS-only status tracking', () => {
  test('Command Center reflects agent status without REST fetch on open', async ({ page, baseURL }) => {
    test.setTimeout(240_000)
    // Pick the first available agent to exercise the real WS path.
    const agentId = isAgentAvailable('claude')
      ? 'claude'
      : isAgentAvailable('codex')
        ? 'codex'
        : isAgentAvailable('copilot')
          ? 'copilot'
          : null
    test.skip(!agentId, 'no agent available for WS status test')

    const workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)

    await waitForAppReady(page)
    await openControlCenter(page)

    // Block the REST statuses endpoint to prove the Command Center relies on
    // the background WS, not a per-open REST fetch. Any call to
    // /api/agents/statuses after this point should fail; the board must still
    // update via the live WS subscription that started at app load.
    await page.route('**/api/agents/statuses', (route) => route.abort())

    const existingIds = await getExistingSessionIds(baseURL!, agentId!)
    const label = agentId === 'claude' ? 'Claude Code' : agentId === 'codex' ? 'Codex CLI' : 'GitHub Copilot'
    await launchAgent(page, label)

    // The session must appear via WS broadcast (not REST). Poll the REST
    // endpoint from Node (not the page) just to learn the sessionId, then
    // assert the card shows up in the WS-driven board.
    const session = await waitForAgentSession(baseURL!, agentId!, 30_000, existingIds)
    await waitForCardInColumn(page, session.sessionId, 'idle', 60_000)
  })

  test('Closing and reopening Command Center does not refetch REST statuses', async ({ page, baseURL }) => {
    await waitForAppReady(page)
    await openControlCenter(page)
    await expect(page.getByTestId('kanban-board')).toBeVisible()

    // Record that no REST statuses fetch happens on reopen by aborting it and
    // confirming the board still renders (it reads from the live WS store).
    let restCalled = false
    await page.route('**/api/agents/statuses', (route) => {
      restCalled = true
      route.abort()
    })

    // Close and reopen.
    await page.getByTestId('status-bar-control-center').click()
    await expect(page.getByTestId('kanban-board')).toBeHidden({ timeout: 10_000 })
    await page.getByTestId('status-bar-control-center').click()
    await expect(page.getByTestId('kanban-board')).toBeVisible({ timeout: 10_000 })

    // The board rendered without calling the REST statuses endpoint.
    expect(restCalled).toBe(false)
  })
})