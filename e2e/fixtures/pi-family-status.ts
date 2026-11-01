import { test as base, expect, type Page } from '@playwright/test'
import { createTempGitRepo, cleanupWorkspace } from './workspace'
import { isAgentAvailable } from './agents'
import {
  openCommandPalette,
  openControlCenter,
  focusTerminal,
  typeInTerminal,
  sendEnter,
  waitForAgentSession,
  getExistingSessionIds,
  waitForCardInColumn,
  getCardColumn,
  hasRedDot,
  waitForRedDot,
  setWorkspace,
  waitForAppReady,
  type AgentStatus,
} from './terminal'

// Status-tracking specs for the Pi family (Pi and its fork Oh My Pi), which
// share Caw's Pi-format transcript watcher.
//
// Set CAW_E2E_LLM_PROXY=1 when the agents talk to the Caw test LLM proxy
// (testenv llm-proxy): prompts then use its deterministic CAWTEST:
// directives (a shell tool that really runs `sleep`, a read of a missing file,
// a question tool call) instead of natural-language prompts, so every state is
// reached reliably. Pi-family TUIs interrupt with ESC (Ctrl+C only clears the
// editor, and twice quits).

const PROXY = !!process.env.CAW_E2E_LLM_PROXY

export interface PiFamilyConfig {
  agentId: 'pi' | 'omp'
  label: string
  /** Whether the agent has a question tool (omp `ask`). */
  hasAskTool: boolean
  responseTimeout: number
}

const prompts = {
  work: PROXY ? 'CAWTEST:shell:sleep 8' : 'run the shell command `sleep 8` and then say done',
  longWork: PROXY ? 'CAWTEST:shell:sleep 30' : 'run the shell command `sleep 30` and then say done',
  fail: PROXY ? 'CAWTEST:read:/nonexistent/xyz.txt' : 'read the file /nonexistent/xyz.txt and tell me what is in it',
  ask: PROXY ? 'CAWTEST:ask' : 'use your ask tool to ask me whether to pick option A or B, then stop',
}

export function createPiFamilyStatusTests(cfg: PiFamilyConfig) {
  base.describe(`${cfg.label} status tracking`, () => {
    base.skip(!isAgentAvailable(cfg.agentId), `${cfg.label} not installed`)

    let workspace: string

    base.beforeEach(async ({ baseURL }) => {
      workspace = createTempGitRepo()
      await setWorkspace(baseURL!, workspace)
    })

    base.afterEach(() => {
      cleanupWorkspace(workspace)
    })

    base('Idle -> Working (first prompt) -> Idle', async ({ page, baseURL }) => {
      base.setTimeout(cfg.responseTimeout + 120_000)
      const session = await launchAndAwait(page, baseURL!, cfg)
      await prompt(page, session.sessionId, prompts.work)
      // The first prompt of a session is not on disk until the first
      // assistant message: the card must still leave idle while it runs.
      await waitForCardInColumn(page, session.sessionId, 'working', 30_000)
      await waitForCardInColumn(page, session.sessionId, 'idle', cfg.responseTimeout)
      expect(await hasRedDot(page, session.sessionId)).toBe(false)
    })

    base('Working -> interrupted with ESC (red dot, idle column)', async ({ page, baseURL }) => {
      base.setTimeout(cfg.responseTimeout + 120_000)
      const session = await launchAndAwait(page, baseURL!, cfg)
      await prompt(page, session.sessionId, prompts.longWork)
      await waitForCardInColumn(page, session.sessionId, 'working', 60_000)
      await page.waitForTimeout(3_000)
      await focusTerminal(page, session.sessionId)
      await page.keyboard.press('Escape')
      await waitForRedDot(page, session.sessionId, 30_000)
      await waitForCardInColumn(page, session.sessionId, 'idle', 30_000)
    })

    base('Failing tool call -> red dot while working -> Idle', async ({ page, baseURL }) => {
      base.setTimeout(cfg.responseTimeout + 120_000)
      const session = await launchAndAwait(page, baseURL!, cfg)
      await prompt(page, session.sessionId, prompts.fail)
      const deadline = Date.now() + cfg.responseTimeout
      while (Date.now() < deadline) {
        if ((await getCardColumn(page, session.sessionId)) === 'idle' && !(await hasRedDot(page, session.sessionId))) break
        await page.waitForTimeout(500)
      }
      await waitForCardInColumn(page, session.sessionId, 'idle', cfg.responseTimeout)
    })

    if (cfg.hasAskTool) {
      base('Question tool -> Needs Input -> answered -> Idle', async ({ page, baseURL }) => {
        base.setTimeout(cfg.responseTimeout + 120_000)
        const session = await launchAndAwait(page, baseURL!, cfg)
        await prompt(page, session.sessionId, prompts.ask)
        await waitForCardInColumn(page, session.sessionId, 'needs_input', cfg.responseTimeout)
        await focusTerminal(page, session.sessionId)
        await sendEnter(page) // pick the first option
        await waitForCardInColumn(page, session.sessionId, 'idle', cfg.responseTimeout)
      })
    }
  })
}

async function prompt(page: Page, sessionId: string, text: string): Promise<void> {
  await focusTerminal(page, sessionId)
  await typeInTerminal(page, sessionId, text)
  await sendEnter(page)
}

async function launchAndAwait(page: Page, baseURL: string, cfg: PiFamilyConfig): Promise<AgentStatus> {
  await waitForAppReady(page)
  await openControlCenter(page)
  const existingIds = await getExistingSessionIds(baseURL, cfg.agentId)
  // Select the palette item by agent id: a text filter on "Pi" would also
  // match "Oh My Pi".
  await openCommandPalette(page)
  await page.getByTestId('command-palette-input').fill(`> New ${cfg.label}`)
  const item = page.getByTestId(`command-palette-item-agent-${cfg.agentId}`)
  await expect(item).toBeVisible()
  await item.click()
  const session = await waitForAgentSession(baseURL, cfg.agentId, 30_000, existingIds)
  await waitForCardInColumn(page, session.sessionId, 'idle', 30_000)
  // Give the TUI time to finish booting (omp loads models/plugins).
  await page.waitForTimeout(8_000)
  return session
}
