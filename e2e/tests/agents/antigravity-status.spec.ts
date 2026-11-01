import { test } from '@playwright/test'
import { createAgentStatusTests, launchAndAwait, acceptTrustDialog } from '../../fixtures/agent-tests'
import { isAgentAvailable } from '../../fixtures/agents'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import {
  focusTerminal,
  typeInTerminal,
  sendEnter,
  waitForCardInColumn,
  waitForRedDot,
  getAgentStatuses,
  setWorkspace,
} from '../../fixtures/terminal'

createAgentStatusTests({
  agentId: 'agy',
  label: 'Antigravity',
  workPrompt: 'Run the shell command: sleep 8 (foreground, wait for it). Then reply only DONE.',
  failPrompt: 'Run the shell command: ls /nonexistent_xyz . Then reply only DONE.',
  responseTimeout: 180_000,
})

async function statusOf(baseURL: string, sessionId: string): Promise<string | undefined> {
  const all = await getAgentStatuses(baseURL)
  return all.find((s) => s.sessionId === sessionId)?.status
}

async function waitForStatus(baseURL: string, sessionId: string, want: string, timeout = 60_000): Promise<void> {
  const deadline = Date.now() + timeout
  let last: string | undefined
  while (Date.now() < deadline) {
    last = await statusOf(baseURL, sessionId)
    if (last === want) return
    await new Promise((r) => setTimeout(r, 300))
  }
  throw new Error(`session ${sessionId}: status ${last}, want ${want} within ${timeout}ms`)
}

// Antigravity-specific states. These rely on agy's conversation DB, which
// the watcher reads for live step state (permission/question prompts,
// cancellation) that the transcript never records.
test.describe('Antigravity specific status tracking', () => {
  test.skip(!isAgentAvailable('agy'), 'Antigravity not installed')

  let workspace: string
  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })
  test.afterEach(() => cleanupWorkspace(workspace))

  test('ask_question -> needs input -> answered -> idle', async ({ page, baseURL }) => {
    test.setTimeout(240_000)
    const session = await launchAndAwait(page, baseURL!, 'Antigravity', 'agy')
    await focusTerminal(page, session.sessionId)
    await acceptTrustDialog(page)
    await focusTerminal(page, session.sessionId)
    await page.waitForTimeout(3000)
    await typeInTerminal(page, session.sessionId, 'Use your ask_question tool to ask me: red or blue? Then reply only with my answer.')
    await sendEnter(page)

    await waitForCardInColumn(page, session.sessionId, 'needs_input', 120_000)
    await waitForStatus(baseURL!, session.sessionId, 'waiting_input', 5_000)

    // Pick the first (recommended) option.
    await focusTerminal(page, session.sessionId)
    await sendEnter(page)
    await waitForCardInColumn(page, session.sessionId, 'idle', 120_000)
    await waitForStatus(baseURL!, session.sessionId, 'idle', 10_000)
  })

  test('ESC while a command runs -> interrupted (red dot, idle column)', async ({ page, baseURL }) => {
    test.setTimeout(240_000)
    const session = await launchAndAwait(page, baseURL!, 'Antigravity', 'agy')
    await focusTerminal(page, session.sessionId)
    await acceptTrustDialog(page)
    await focusTerminal(page, session.sessionId)
    await page.waitForTimeout(3000)
    await typeInTerminal(page, session.sessionId, 'Run the shell command: sleep 8 (foreground). Then reply only DONE.')
    await sendEnter(page)

    await waitForStatus(baseURL!, session.sessionId, 'executing', 120_000)
    await focusTerminal(page, session.sessionId)
    await page.keyboard.press('Escape')

    await waitForStatus(baseURL!, session.sessionId, 'interrupted', 30_000)
    await waitForRedDot(page, session.sessionId, 10_000)
    await waitForCardInColumn(page, session.sessionId, 'idle', 10_000)
  })
})
