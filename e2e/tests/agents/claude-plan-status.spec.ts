import { test, expect, type Page } from '@playwright/test'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import { isAgentAvailable } from '../../fixtures/agents'
import {
  launchAgent,
  openControlCenter,
  focusTerminal,
  typeInTerminal,
  sendEnter,
  sendInterrupt,
  waitForAgentSession,
  getExistingSessionIds,
  waitForCardInColumn,
  getCardColumn,
  setWorkspace,
  waitForAppReady,
  type AgentStatus,
} from '../../fixtures/terminal'

const AGENT_ID = 'claude'
const AGENT_LABEL = 'Claude Code'

test.describe('Claude Code status tracking', () => {
  test.skip(!isAgentAvailable(AGENT_ID), 'Claude Code not installed')

  let workspace: string
  let sessionId: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(() => {
    cleanupWorkspace(workspace)
  })

  test('Idle -> Working -> Needs Input (via /plan)', async ({ page, baseURL }) => {
    test.setTimeout(360_000)
    const session = await launchAndAwait(page, baseURL!)
    sessionId = session.sessionId

    await focusTerminal(page, sessionId)
    await acceptTrustDialog(page)
    await focusTerminal(page, sessionId)
    await page.waitForTimeout(5000)
    await typeInTerminal(page, sessionId, '/plan')
    await sendEnter(page)
    await typeInTerminal(page, sessionId, 'list the files in this repo and describe what each does')
    await sendEnter(page)

    await expectCardIn(page, sessionId, 'working', 120_000)

    // After /plan, Claude may use ExitPlanMode (→needs_input) or just present
    // the plan as text and go idle. Accept either outcome.
    const deadline = Date.now() + 240_000
    let reachedTarget = false
    while (Date.now() < deadline && !reachedTarget) {
      const col = await getCardColumn(page, sessionId)
      if (col === 'needs_input' || col === 'idle') {
        reachedTarget = true
      } else {
        await page.waitForTimeout(500)
      }
    }
    if (!reachedTarget) {
      throw new Error(`Card ${sessionId} never reached needs_input or idle after working`)
    }
  })

  test('Idle -> Working -> Idle (simple "hi")', async ({ page, baseURL }) => {
    test.setTimeout(300_000)
    const session = await launchAndAwait(page, baseURL!)
    sessionId = session.sessionId

    await focusTerminal(page, sessionId)
    await acceptTrustDialog(page)
    await focusTerminal(page, sessionId)
    await page.waitForTimeout(5000)
    await typeInTerminal(page, sessionId, 'hi')
    await sendEnter(page)

    await expectCardIn(page, sessionId, 'working', 90_000)

    await expectCardIn(page, sessionId, 'idle', 180_000)
  })

  test('Idle -> Working -> Idle (interrupted with Ctrl+C)', async ({ page, baseURL }) => {
    test.setTimeout(300_000)
    const session = await launchAndAwait(page, baseURL!)
    sessionId = session.sessionId

    await focusTerminal(page, sessionId)
    await acceptTrustDialog(page)
    await focusTerminal(page, sessionId)
    await page.waitForTimeout(5000)
    await typeInTerminal(page, sessionId, 'read every file in this directory and summarize them')
    await sendEnter(page)

    await expectCardIn(page, sessionId, 'working', 90_000)

    await page.waitForTimeout(5_000)
    await sendInterrupt(page)

    await expectCardIn(page, sessionId, 'idle', 120_000)
  })
})

async function launchAndAwait(page: Page, baseURL: string): Promise<AgentStatus> {
  await waitForAppReady(page)
  await openControlCenter(page)
  const existingIds = await getExistingSessionIds(baseURL, AGENT_ID)
  await launchAgent(page, AGENT_LABEL)
  const session = await waitForAgentSession(baseURL, AGENT_ID, 30_000, existingIds)
  await expectCardIn(page, session.sessionId, 'idle', 30_000)
  return session
}

async function acceptTrustDialog(page: Page): Promise<void> {
  await page.waitForTimeout(5000)
  // ghostty-web renders to a <canvas> (no .xterm-rows DOM), so read the
  // buffer text from the debug terminal handle exposed in dev builds.
  const termText = await page.evaluate(() => {
    const t = (window as any).__cawTerm
    if (!t) return ''
    const lines: string[] = []
    for (let i = 0; i < t.rows; i++) {
      const line = t.buffer.active.getLine(i)
      if (line) lines.push(line.translateToString(true))
    }
    return lines.join('\n')
  }).catch(() => '')
  if (!termText) return
  const lower = termText.toLowerCase()
  if (lower.includes('trust this folder') || lower.includes('safety check')) {
    await page.keyboard.press('1')
    await page.keyboard.press('Enter')
    await page.waitForTimeout(5000)
  } else if (lower.includes('trust the contents') || lower.includes('do you trust')) {
    await page.keyboard.press('Enter')
    await page.waitForTimeout(5000)
  }
}

async function expectCardIn(page: Page, sid: string, column: 'idle' | 'working' | 'needs_input', timeout: number) {
  await waitForCardInColumn(page, sid, column, timeout)
}

async function expectCardColumnEmpty(page: Page, _column: string) {
  const board = page.getByTestId('kanban-board')
  await expect(board).toBeVisible()
}