import { test, expect, type Page } from '@playwright/test'
import { createAgentStatusTests } from '../../fixtures/agent-tests'
import { isAgentAvailable } from '../../fixtures/agents'
import { createTempGitRepo } from '../../fixtures/workspace'
import {
  launchAgent,
  openControlCenter,
  closeControlCenter,
  focusTerminal,
  waitForAgentSession,
  getExistingSessionIds,
  getAgentStatuses,
  waitForCardInColumn,
  hasRedDot,
  setWorkspace,
  waitForAppReady,
  type AgentStatus,
} from '../../fixtures/terminal'

createAgentStatusTests({
  agentId: 'opencode',
  label: 'OpenCode',
  workPrompt: 'list all files in this directory and describe each one in one sentence',
  failPrompt: 'read the file /nonexistent/xyz.txt and tell me what is in it',
  responseTimeout: 180_000,
})

// Deterministic state coverage. Requires OpenCode to be configured against the
// Caw test LLM proxy, which answers `CAWTEST:<scenario>` prompts with scripted
// tool calls / errors instead of calling a model (see the proxy README in the
// test environment). Enable with CAW_E2E_SCRIPTED_LLM=1.
test.describe('OpenCode status tracking (scripted LLM)', () => {
  test.skip(!isAgentAvailable('opencode'), 'OpenCode not installed')
  test.skip(!process.env.CAW_E2E_SCRIPTED_LLM, 'CAW_E2E_SCRIPTED_LLM not set')

  let workspace: string

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    // Worktrees off: every pane runs in the same cwd, which is the case where
    // OpenCode's shared session DB makes panes compete for sessions.
    await setWorkspace(baseURL!, workspace, false)
  })

  // Workspace dirs are left for the global teardown (which removes the whole
  // temp root): removing a pane's cwd while Caw may still restore/relaunch a
  // pane in it makes the next launch exit immediately.

  test('shell tool: idle -> executing -> idle', async ({ page, baseURL }) => {
    test.setTimeout(180_000)
    await waitForAppReady(page)
    await openControlCenter(page)
    const s = await launchOpenCode(page, baseURL!)
    await submitPrompt(page, s.sessionId, 'CAWTEST:shell:sleep 15')
    await waitForStatus(baseURL!, s.sessionId, (st) => st.status === 'executing' && st.tool === 'bash', 60_000)
    await expectColumn(page, s.sessionId, 'working')
    await waitForStatus(baseURL!, s.sessionId, (st) => st.status === 'idle', 60_000)
    await expectColumn(page, s.sessionId, 'idle')
  })

  test('question tool: needs input -> answered -> idle', async ({ page, baseURL }) => {
    test.setTimeout(180_000)
    await waitForAppReady(page)
    await openControlCenter(page)
    const s = await launchOpenCode(page, baseURL!)
    await submitPrompt(page, s.sessionId, 'CAWTEST:ask')
    await waitForStatus(baseURL!, s.sessionId, (st) => st.status === 'waiting_input', 60_000)
    await expectColumn(page, s.sessionId, 'needs_input')
    await focusTerminal(page, s.sessionId)
    await page.keyboard.press('Enter') // pick the first option
    await waitForStatus(baseURL!, s.sessionId, (st) => st.status === 'idle', 60_000)
    await expectColumn(page, s.sessionId, 'idle')
  })

  test('API error ends the turn: idle column with red dot', async ({ page, baseURL }) => {
    test.setTimeout(180_000)
    await waitForAppReady(page)
    await openControlCenter(page)
    const s = await launchOpenCode(page, baseURL!)
    await submitPrompt(page, s.sessionId, 'CAWTEST:fail:400')
    await waitForStatus(baseURL!, s.sessionId, (st) => st.status === 'interrupted', 60_000)
    await expectColumn(page, s.sessionId, 'idle')
    await openControlCenter(page)
    expect(await hasRedDot(page, s.sessionId)).toBe(true)
  })

  test('two panes in one cwd: an idle pane does not take over its sibling session', async ({ page, baseURL }) => {
    test.setTimeout(240_000)
    await waitForAppReady(page)
    await openControlCenter(page)
    const idlePane = await launchOpenCode(page, baseURL!)
    await openControlCenter(page)
    const busyPane = await launchOpenCode(page, baseURL!)
    await submitPrompt(page, busyPane.sessionId, 'CAWTEST:shell:sleep 15')
    await waitForStatus(baseURL!, busyPane.sessionId, (st) => st.status === 'executing', 60_000)
    // The never-prompted pane must stay idle and untitled for the whole turn.
    const deadline = Date.now() + 15_000
    while (Date.now() < deadline) {
      const st = (await getAgentStatuses(baseURL!)).find((x) => x.sessionId === idlePane.sessionId)
      expect(st?.status).toBe('idle')
      expect(st?.title ?? '').toBe('')
      await page.waitForTimeout(1_000)
    }
    await waitForStatus(baseURL!, busyPane.sessionId, (st) => st.status === 'idle', 60_000)
  })
})

// launchOpenCode launches an OpenCode pane from the command palette and
// leaves the Control Center closed so the terminal is visible and focusable.
async function launchOpenCode(page: Page, baseURL: string): Promise<AgentStatus> {
  const existing = await getExistingSessionIds(baseURL, 'opencode')
  await launchAgent(page, 'OpenCode')
  const s = await waitForAgentSession(baseURL, 'opencode', 30_000, existing)
  await waitForCardInColumn(page, s.sessionId, 'idle', 30_000)
  await closeControlCenter(page)
  return s
}

// expectColumn opens the Control Center, asserts the card's Kanban column and
// closes it again.
async function expectColumn(page: Page, sessionId: string, col: 'idle' | 'working' | 'needs_input'): Promise<void> {
  await openControlCenter(page)
  await waitForCardInColumn(page, sessionId, col, 10_000)
  await closeControlCenter(page)
}

// submitPrompt waits until the OpenCode TUI accepts keyboard input (it drops
// keystrokes while it is still bootstrapping), then types the prompt and
// presses Enter. Acceptance is detected by the "Ask anything…" placeholder
// disappearing after a probe keystroke once the TUI has drawn.
async function submitPrompt(page: Page, sessionId: string, prompt: string): Promise<void> {
  const rows = page.getByTestId(`terminal-panel-${sessionId}`).locator('.xterm-rows')
  const deadline = Date.now() + 60_000
  for (;;) {
    await focusTerminal(page, sessionId)
    await page.keyboard.type('q')
    await page.waitForTimeout(700)
    const text = (await rows.textContent().catch(() => '')) ?? ''
    await page.keyboard.press('Backspace')
    // "ctrl+p commands" is in the TUI footer once it has drawn.
    if (text.includes('ctrl+p') && !text.includes('Ask anything')) break
    if (Date.now() > deadline) throw new Error('OpenCode prompt never accepted input')
    await page.waitForTimeout(1_000)
  }
  await page.waitForTimeout(300)
  await page.keyboard.type(prompt)
  await page.keyboard.press('Enter')
}

async function waitForStatus(
  baseURL: string,
  sessionId: string,
  pred: (s: AgentStatus) => boolean,
  timeout: number,
): Promise<AgentStatus> {
  const deadline = Date.now() + timeout
  let last: AgentStatus | undefined
  while (Date.now() < deadline) {
    last = (await getAgentStatuses(baseURL)).find((x) => x.sessionId === sessionId)
    if (last && pred(last)) return last
    await new Promise((r) => setTimeout(r, 300))
  }
  throw new Error(`status of ${sessionId} did not match within ${timeout}ms (last: ${JSON.stringify(last)})`)
}
