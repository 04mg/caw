import { test, expect } from '@playwright/test'
import { createAgentStatusTests, launchAndAwait, acceptTrustDialog } from '../../fixtures/agent-tests'
import { isAgentAvailable } from '../../fixtures/agents'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import {
  focusTerminal,
  typeInTerminal,
  sendEnter,
  setWorkspace,
  waitForCardInColumn,
  getCardColumn,
  waitForRedDot,
  getAgentStatuses,
} from '../../fixtures/terminal'

createAgentStatusTests({
  agentId: 'codex',
  label: 'Codex CLI',
  workPrompt: 'list all files in this directory and describe each one in one sentence',
  failPrompt: 'read the file /nonexistent/xyz.txt and tell me what is in it',
  responseTimeout: 180_000,
})

// Deterministic Codex state scenarios. They need Codex configured against
// the Caw test LLM proxy, which answers `CAWTEST:<scenario>` prompts with
// scripted responses (shell/ask/fail/slow). Enable with CAW_TEST_LLM_PROXY=1.
test.describe('Codex CLI status tracking (scripted LLM proxy)', () => {
  test.skip(!isAgentAvailable('codex'), 'Codex CLI not installed')
  test.skip(!process.env.CAW_TEST_LLM_PROXY, 'CAW_TEST_LLM_PROXY not set')

  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  async function startCodex(page: import('@playwright/test').Page, baseURL: string) {
    const session = await launchAndAwait(page, baseURL, 'Codex CLI', 'codex')
    await focusTerminal(page, session.sessionId)
    await acceptTrustDialog(page)
    await focusTerminal(page, session.sessionId)
    // Codex 0.16x connects to its app-server before the composer accepts
    // input; keystrokes sent earlier are dropped. Wait for the composer
    // placeholder.
    await expect
      .poll(async () => (await page.locator('.xterm-rows').textContent().catch(() => '')) ?? '', { timeout: 60_000 })
      .toContain('Ask Codex to do anything')
    await page.waitForTimeout(1000)
    return session
  }

  async function prompt(page: import('@playwright/test').Page, text: string) {
    await typeInTerminal(page, '', text)
    await page.waitForTimeout(300)
    await sendEnter(page)
  }

  async function apiStatus(baseURL: string, sessionId: string) {
    return (await getAgentStatuses(baseURL)).find((s) => s.sessionId === sessionId)
  }

  test('finished turn stays idle when a yielded command completes later', async ({ page, baseURL }) => {
    test.setTimeout(150_000)
    const session = await startCodex(page, baseURL!)
    // exec_command yields after ~10s; the turn ends while `sleep 20` keeps
    // running and its completion is written to the rollout after the turn.
    await prompt(page, 'CAWTEST:shell:sleep 20')
    await waitForCardInColumn(page, session.sessionId, 'working', 30_000)
    await waitForCardInColumn(page, session.sessionId, 'idle', 60_000)
    await page.waitForTimeout(20_000)
    expect(await getCardColumn(page, session.sessionId)).toBe('idle')
    expect((await apiStatus(baseURL!, session.sessionId))?.status).toBe('idle')
  })

  test('Plan mode question needs input, answering returns to idle', async ({ page, baseURL }) => {
    test.setTimeout(150_000)
    const session = await startCodex(page, baseURL!)
    await prompt(page, '/plan')
    await page.waitForTimeout(2000)
    await prompt(page, 'CAWTEST:ask')
    await waitForCardInColumn(page, session.sessionId, 'needs_input', 30_000)
    expect((await apiStatus(baseURL!, session.sessionId))?.tool).toBe('request_user_input')
    await sendEnter(page) // pick "Option 1"
    await waitForCardInColumn(page, session.sessionId, 'idle', 60_000)
  })

  test('API error ends the turn with a red dot instead of staying working', async ({ page, baseURL }) => {
    test.setTimeout(120_000)
    const session = await startCodex(page, baseURL!)
    await prompt(page, 'CAWTEST:fail:400')
    await waitForCardInColumn(page, session.sessionId, 'idle', 30_000)
    await waitForRedDot(page, session.sessionId, 10_000)
    const st = await apiStatus(baseURL!, session.sessionId)
    expect(st?.status).toBe('interrupted')
    expect(st?.details).toContain('CAWTEST scripted failure')
  })

  test('ESC during a long silent LLM wait reports interrupted', async ({ page, baseURL }) => {
    test.setTimeout(120_000)
    const session = await startCodex(page, baseURL!)
    await prompt(page, 'CAWTEST:slow:60')
    await waitForCardInColumn(page, session.sessionId, 'working', 30_000)
    await page.waitForTimeout(3000)
    await page.keyboard.press('Escape')
    await waitForCardInColumn(page, session.sessionId, 'idle', 30_000)
    await waitForRedDot(page, session.sessionId, 10_000)
    expect((await apiStatus(baseURL!, session.sessionId))?.status).toBe('interrupted')
  })
})
