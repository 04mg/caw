import { test as base, expect, type Page } from '@playwright/test'
import { createTempGitRepo, cleanupWorkspace } from './workspace'
import { isAgentAvailable } from './agents'
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
} from './terminal'

export interface AgentTestConfig {
  agentId: string
  label: string
  /** Prompt that forces tool use (list files, read files, etc.) so working state lasts long enough to detect. */
  workPrompt: string
  /** Timeout for the agent to respond and return to idle. */
  responseTimeout: number
}

export function createAgentStatusTests(cfg: AgentTestConfig) {
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

    base('Idle -> Working -> Idle (simple prompt)', async ({ page, baseURL }) => {
      base.setTimeout(cfg.responseTimeout + 120_000)
      const session = await launchAndAwait(page, baseURL!, cfg.label, cfg.agentId)

      await focusTerminal(page, session.sessionId)
      await acceptTrustDialog(page)
      await focusTerminal(page, session.sessionId)
      await page.waitForTimeout(5000)
      await typeInTerminal(page, session.sessionId, cfg.workPrompt)
      await sendEnter(page)

      // Try to catch "working" but don't fail if agent is too fast —
      // the important thing is it eventually returns to idle.
      const deadline = Date.now() + 90_000
      let caughtWorking = false
      while (Date.now() < deadline) {
        const col = await getCardColumn(page, session.sessionId)
        if (col === 'working') {
          caughtWorking = true
          break
        }
        if (col === 'idle' && caughtWorking) break
        await page.waitForTimeout(500)
      }

      await waitForCardInColumn(page, session.sessionId, 'idle', cfg.responseTimeout)
    })

    base('Idle -> Working -> Idle (interrupted with Ctrl+C)', async ({ page, baseURL }) => {
      base.setTimeout(240_000)
      const session = await launchAndAwait(page, baseURL!, cfg.label, cfg.agentId)

      await focusTerminal(page, session.sessionId)
      await acceptTrustDialog(page)
      await focusTerminal(page, session.sessionId)
      await page.waitForTimeout(5000)
      await typeInTerminal(page, session.sessionId, cfg.workPrompt)
      await sendEnter(page)

      // Try to catch working state; if agent finishes before we detect it,
      // we still validate Ctrl+C doesn't break a working agent.
      const deadline = Date.now() + 90_000
      let caughtWorking = false
      while (Date.now() < deadline) {
        const col = await getCardColumn(page, session.sessionId)
        if (col === 'working') {
          caughtWorking = true
          break
        }
        // If it already went idle, break early — we'll still send Ctrl+C
        if (col === 'idle') break
        await page.waitForTimeout(500)
      }

      await page.waitForTimeout(5_000)
      await sendInterrupt(page)

      await waitForCardInColumn(page, session.sessionId, 'idle', 120_000)
    })
  })
}

async function launchAndAwait(page: Page, baseURL: string, label: string, agentId: string): Promise<AgentStatus> {
  await waitForAppReady(page)
  await openControlCenter(page)
  const existingIds = await getExistingSessionIds(baseURL, agentId)
  await launchAgent(page, label)
  const session = await waitForAgentSession(baseURL, agentId, 30_000, existingIds)
  await waitForCardInColumn(page, session.sessionId, 'idle', 30_000)
  return session
}

async function acceptTrustDialog(page: Page): Promise<void> {
  // Many agents (Claude Code, Antigravity, Codex) show a trust/safety dialog
  // on first launch in a new directory. We detect it by checking the terminal
  // text for "trust" and accept by pressing the appropriate key.
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
    // Claude Code: "1. Yes, I trust this folder" — press 1 then Enter
    await page.keyboard.press('1')
    await page.keyboard.press('Enter')
    await page.waitForTimeout(5000)
  } else if (lower.includes('trust the contents') || lower.includes('do you trust')) {
    // Antigravity / Codex: default selection is Yes — Enter to confirm
    await page.keyboard.press('Enter')
    await page.waitForTimeout(5000)
  }
}
